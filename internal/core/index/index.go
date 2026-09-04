package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ashes47/braids/internal/core/artifacts"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"

	// Pure-Go SQLite driver: no cgo, so braids cross-compiles cleanly.
	_ "modernc.org/sqlite"
)

// schemaVersion is bumped whenever the tables change, and whenever what is
// stored in them was wrong. The index holds no unique state, rebuilding from
// the transcripts in seconds, so an old schema is dropped and recreated rather
// than migrated.
//
// 13 is the second kind: the tables are unchanged, but previews written before
// it could be cut in the middle of a character.
const schemaVersion = 13

const dropAll = `
DROP TABLE IF EXISTS parts;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS lanes;
DROP TABLE IF EXISTS subagents;
DROP TABLE IF EXISTS compactions;`

const schema = `
CREATE TABLE IF NOT EXISTS lanes (
	id      TEXT PRIMARY KEY,
	source  TEXT NOT NULL,
	project TEXT NOT NULL,
	path    TEXT NOT NULL,
	title   TEXT NOT NULL,
	cwd     TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL DEFAULT 0,
	updated INTEGER NOT NULL,
	size    INTEGER NOT NULL,
	msg_count  INTEGER NOT NULL DEFAULT 0,
	part_count INTEGER NOT NULL DEFAULT 0,
	last_role  TEXT    NOT NULL DEFAULT '',
	last_tool  INTEGER NOT NULL DEFAULT 0,
	artifacts  INTEGER NOT NULL DEFAULT 0,
	artifact_path TEXT NOT NULL DEFAULT '',
	-- Where the last read of this transcript stopped, so the next one can
	-- start there. Zero means the whole file has to be read.
	indexed_bytes INTEGER NOT NULL DEFAULT 0,
	indexed_last  TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
	lane_id   TEXT    NOT NULL,
	seq       INTEGER NOT NULL,
	msg_id    TEXT    NOT NULL,
	parent_id TEXT    NOT NULL,
	role      TEXT    NOT NULL,
	at        INTEGER NOT NULL,
	preview   TEXT    NOT NULL DEFAULT '',
	tools     TEXT    NOT NULL DEFAULT '',
	failed    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (lane_id, seq)
);
CREATE INDEX IF NOT EXISTS messages_by_msg_id ON messages(msg_id);
CREATE TABLE IF NOT EXISTS compactions (
	lane_id     TEXT    NOT NULL,
	seq         INTEGER NOT NULL,
	trigger     TEXT    NOT NULL DEFAULT '',
	pre_tokens  INTEGER NOT NULL DEFAULT 0,
	post_tokens INTEGER NOT NULL DEFAULT 0,
	dropped     INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (lane_id, seq)
);
CREATE TABLE IF NOT EXISTS subagents (
	lane_id     TEXT    NOT NULL,
	agent_id    TEXT    NOT NULL,
	type        TEXT    NOT NULL DEFAULT '',
	task        TEXT    NOT NULL DEFAULT '',
	tool_use_id TEXT    NOT NULL DEFAULT '',
	depth       INTEGER NOT NULL DEFAULT 0,
	path        TEXT    NOT NULL DEFAULT '',
	msgs        INTEGER NOT NULL DEFAULT 0,
	parent_seq  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (lane_id, agent_id)
);
CREATE VIRTUAL TABLE IF NOT EXISTS parts USING fts5(
	body,
	lane_id UNINDEXED,
	msg_id  UNINDEXED,
	kind    UNINDEXED,
	role    UNINDEXED,
	tool    UNINDEXED,
	at      UNINDEXED,
	tokenize='porter unicode61'
);

-- docs holds everything searchable that is not a conversation turn: the
-- memories a project keeps, and the names of the work products a session left.
-- A separate table rather than a kind column on parts, because these have
-- nothing in common with a turn — no message, no role, no position in a
-- conversation — and crowding them in would make every column optional.
-- What the index last saw of each memory directory, so re-indexing memories
-- can be skipped when none of them moved.
CREATE TABLE IF NOT EXISTS memory_marks (
	dir    TEXT PRIMARY KEY,
	count  INTEGER NOT NULL,
	newest INTEGER NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS docs USING fts5(
	body,
	doc_type UNINDEXED,
	name     UNINDEXED,
	path     UNINDEXED,
	project  UNINDEXED,
	lane_id  UNINDEXED,
	at       UNINDEXED,
	tokenize='porter unicode61'
);`

// Index is a searchable snapshot of every lane a Source can see.
type Index struct {
	db *sql.DB
	// recreated records that the schema on disk was an older one and was
	// dropped. Everything the index held is gone, so whoever opened it has to
	// read the transcripts again before showing anybody an empty map.
	recreated bool
}

// Stalled is a conversation that grew without producing a single new message.
type Stalled struct {
	Lane   string
	Path   string
	Gained int64
}

// stalledGrowth is how much a transcript can gain, with nothing to show for
// it, before that is worth saying out loud.
//
// The Unreadable query catches a transcript that yielded nothing at all. This
// catches the subtler half: a format change that breaks only assistant turns
// leaves the user's still readable, so the count stays above zero and that
// check stays quiet while half the history stops arriving. Measured against a
// real corpus, the median lane produces a message every 6.8 kB and the worst
// every 15 kB, so 32 kB with nothing at all is twice as bad as anything real.
const stalledGrowth = 32 << 10

// Stats summarises a Rebuild.
type Stats struct {
	Lanes    int
	Messages int
	Parts    int
	Duration time.Duration
	// Stalled names conversations that gained bytes in this pass and no
	// messages, which is what a format change looks like while it is
	// happening.
	Stalled []Stalled
}

// Query selects messages. An empty Lane searches every lane, and empty Kinds
// searches every kind of content.
type Query struct {
	// Types narrows to conversations, memories or work products. Empty means
	// all of them, so search stays global unless asked otherwise.
	Types []Found
	Text  string
	Lane  string
	// Project narrows to one project, matched without regard to case because
	// it is a name people read off the screen and type back.
	Project string
	// Since and Until bound when a turn happened. Zero means unbounded.
	Since time.Time
	Until time.Time
	Kinds []model.PartKind
	Limit int
}

// when adds the date bounds to a query being built. Both tables store seconds.
func (q Query) when(column string, where []string, args []any) ([]string, []any) {
	if !q.Since.IsZero() {
		where = append(where, column+" >= ?")
		args = append(args, q.Since.Unix())
	}
	if !q.Until.IsZero() {
		where = append(where, column+" <= ?")
		args = append(args, q.Until.Unix())
	}
	return where, args
}

// LaneInfo is a lane plus the counts only the index knows. Keeping the counts
// here rather than on model.Lane leaves the Source port free of index concerns.
type LaneInfo struct {
	model.Lane
	Messages int
	Parts    int
	// Tail is where the last read of this transcript stopped. A zero offset
	// means it has never been read incrementally, so the next read is whole.
	Tail store.Tail
}

// Overlap records one message that appears in more than one lane. Claude Code
// forks copy the parent's records verbatim, so a shared message ID is exact
// evidence of a fork — no fingerprinting needed.
type Overlap struct {
	MessageID string
	LaneID    string
	Seq       int
}

// Hit is one search result, carrying enough context to render a row without a
// second lookup.
type Hit struct {
	// Of says what this is: a turn in a conversation, a memory, or the name of
	// a work product. A result you cannot tell the kind of is a result you
	// have to open before you understand it.
	Of Found
	// Name is a memory's slug or a work product's relative path. Empty for a
	// turn, which is named by its conversation and position instead.
	Name string
	// Path is the file it lives in, for the things that are files.
	Path      string
	Score     float64
	LaneID    string
	LaneTitle string
	Project   string
	MessageID string
	// Seq is the turn number within the lane, so a result can be jumped to.
	Seq     int
	Kind    model.PartKind
	Role    model.Role
	Tool    string
	Snippet string
	At      time.Time
}

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		// Another braids may be open: the map holds the index while it
		// watches. WAL lets readers and one writer coexist, and a busy timeout
		// makes a writer wait its turn instead of failing on contact.
		`PRAGMA busy_timeout=5000`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, errors.Join(fmt.Errorf("apply %s: %w", pragma, err), db.Close())
		}
	}
	dropped, err := migrate(db)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if err := restrict(path); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Index{db: db, recreated: dropped}, nil
}

// restrict keeps the index to its owner. It holds the full text of every
// message braids has read, and Claude Code keeps the transcripts it came from
// at 0700 — a tool that copies private data into a world-readable file has
// widened the user's exposure without being asked to. SQLite creates its
// database and sidecar files at whatever the umask allows, so the modes are set
// after the fact, and on every open rather than only at creation: an index made
// by an older build should be tightened too.
func restrict(path string) error {
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue // WAL and shared-memory files appear only once used
		}
		if err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if info.Mode().Perm()&0o077 == 0 {
			continue
		}
		if err := os.Chmod(name, 0o600); err != nil {
			return fmt.Errorf("restrict %s: %w", name, err)
		}
	}
	return nil
}

// migrate brings the database to the current schema, discarding an older one,
// and reports whether it discarded anything. A caller that ignores that is a
// caller showing an empty map to somebody who has just upgraded.
func migrate(db *sql.DB) (bool, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return false, fmt.Errorf("read schema version: %w", err)
	}
	dropped := version != schemaVersion && version != 0
	if version != schemaVersion {
		if _, err := db.Exec(dropAll); err != nil {
			return false, fmt.Errorf("drop stale schema v%d: %w", version, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		return false, fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, schemaVersion)); err != nil {
		return false, fmt.Errorf("stamp schema version: %w", err)
	}
	return dropped, nil
}

// Recreated reports that opening this index threw away an older schema, so it
// holds nothing until something reads the transcripts again.
func (ix *Index) Recreated() bool { return ix.recreated }

// Unreadable names the conversations braids has bytes for and no messages
// from.
//
// This is the alarm for the one failure braids cannot otherwise see. It skips
// most of what a transcript contains on purpose: of the eighteen record types
// in one real history, only two carry a turn and the other sixteen are
// bookkeeping. So an unfamiliar record type is not news, and a list of the
// types braids knows would have to grow every time the harness adds one.
//
// What is news is a transcript with content in it that produced nothing.
// Whether the harness renamed a type, moved the message body, or changed how
// content nests, the symptom is the same and this catches all three. Across a
// real history of 28 conversations no lane is in this state, and the least
// talkative readable one still yields a message every 15 kB.
func (ix *Index) Unreadable(ctx context.Context) ([]LaneInfo, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id, path, size FROM lanes WHERE size > 0 AND msg_count = 0 ORDER BY size DESC`)
	if err != nil {
		return nil, fmt.Errorf("find unreadable lanes: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []LaneInfo
	for rows.Next() {
		var l LaneInfo
		if err := rows.Scan(&l.ID, &l.Path, &l.Size); err != nil {
			return nil, fmt.Errorf("scan unreadable lane: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Close releases the underlying database.
func (ix *Index) Close() error { return ix.db.Close() }

// Rebuild replaces the whole index from src. A full rebuild is cheap enough
// (seconds over a large history) that braids never needs incremental updates
// to be correct — only to be fast.
func (ix *Index) Rebuild(ctx context.Context, src store.Source) (Stats, error) {
	start := time.Now()
	lanes, err := src.Lanes(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("list lanes: %w", err)
	}

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	for _, stmt := range []string{
		`DELETE FROM parts`, `DELETE FROM messages`,
		`DELETE FROM subagents`, `DELETE FROM compactions`, `DELETE FROM lanes`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return Stats{}, fmt.Errorf("clear index: %w", err)
		}
	}

	stats := Stats{Lanes: len(lanes)}
	for _, lane := range lanes {
		msgs, parts, err := replaceLane(ctx, tx, src, lane)
		if err != nil {
			return Stats{}, err
		}
		stats.Messages += msgs
		stats.Parts += parts
	}
	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("commit: %w", err)
	}
	stats.Duration = time.Since(start)
	return stats, nil
}

// Sync brings the index up to date, re-reading only what changed.
//
// A lane is re-read when its size or modification time differs from what was
// stored, and dropped when its file is gone. A full rebuild takes seconds; this
// takes milliseconds when nothing moved, which is what makes it usable after
// every branch rather than something the user has to remember to run.
func (ix *Index) Sync(ctx context.Context, src store.Source) (Stats, error) {
	start := time.Now()
	lanes, err := src.Lanes(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("list lanes: %w", err)
	}
	known, err := ix.Lanes(ctx)
	if err != nil {
		return Stats{}, err
	}
	stored := make(map[string]LaneInfo, len(known))
	for _, l := range known {
		stored[l.ID] = l
	}

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	stats := Stats{Lanes: len(lanes)}
	seen := make(map[string]bool, len(lanes))
	for _, lane := range lanes {
		seen[lane.ID] = true
		was, known := stored[lane.ID]
		// Compare at second precision: that is what the index stores, so
		// comparing the raw ModTime would mark every lane changed, every time.
		if known && was.Size == lane.Size && was.Updated.Unix() == lane.Updated.Unix() {
			stats.Messages += was.Messages
			stats.Parts += was.Parts
			continue
		}
		msgs, parts, err := indexLane(ctx, tx, src, lane, was, known)
		if err != nil {
			return Stats{}, err
		}
		stats.Messages += msgs
		stats.Parts += parts
		if gained := lane.Size - was.Size; known && gained >= stalledGrowth && msgs <= was.Messages {
			stats.Stalled = append(stats.Stalled,
				Stalled{Lane: lane.ID, Path: lane.Path, Gained: gained})
		}
	}
	for id := range stored {
		if !seen[id] {
			if err := deleteLane(ctx, tx, id); err != nil {
				return Stats{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("commit: %w", err)
	}
	stats.Duration = time.Since(start)
	return stats, nil
}

// enrich fills in the details that need the transcript open. They are cosmetic
// or convenience, so a failure must never fail an index.
func enrich(ctx context.Context, src store.Source, lane model.Lane) model.Lane {
	enricher, ok := src.(store.Enricher)
	if !ok {
		return lane
	}
	enriched, err := enricher.Enrich(ctx, lane)
	if err != nil {
		return lane
	}
	return enriched
}

func deleteLane(ctx context.Context, tx *sql.Tx, laneID string) error {
	for _, stmt := range []string{
		`DELETE FROM parts WHERE lane_id = ?`,
		`DELETE FROM messages WHERE lane_id = ?`,
		`DELETE FROM subagents WHERE lane_id = ?`,
		`DELETE FROM compactions WHERE lane_id = ?`,
		`DELETE FROM lanes WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, laneID); err != nil {
			return fmt.Errorf("drop lane %s: %w", laneID, err)
		}
	}
	return nil
}

// replaceLane re-reads one lane into the index, replacing whatever was there.
func replaceLane(ctx context.Context, tx *sql.Tx, src store.Source, lane model.Lane) (msgs, parts int, err error) {
	var activity model.Activity
	if err := deleteLane(ctx, tx, lane.ID); err != nil {
		return 0, 0, err
	}
	lane = enrich(ctx, src, lane)
	insertPart, err := tx.PrepareContext(ctx,
		`INSERT INTO parts (body,lane_id,msg_id,kind,role,tool,at) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare part insert: %w", err)
	}
	defer insertPart.Close() //nolint:errcheck // tx-scoped
	insertMsg, err := tx.PrepareContext(ctx,
		`INSERT INTO messages (lane_id,seq,msg_id,parent_id,role,at,preview,tools,failed) `+
			`VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare message insert: %w", err)
	}
	defer insertMsg.Close() //nolint:errcheck // tx-scoped
	insertCompaction, err := tx.PrepareContext(ctx,
		`INSERT INTO compactions (lane_id,seq,trigger,pre_tokens,post_tokens,dropped,duration_ms) `+
			`VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare compaction insert: %w", err)
	}
	defer insertCompaction.Close() //nolint:errcheck // tx-scoped

	// Subagents name the tool call they answer, so the turn each one attaches
	// to is learned while streaming rather than by a second pass.
	spawnedAt := map[string]int{}
	tail, err := readAll(ctx, src, lane, func(m model.Message) error {
		msgs++
		activity = activityOf(m)
		for _, p := range m.Parts {
			if p.Kind == model.PartToolUse && p.ID != "" {
				spawnedAt[p.ID] = msgs
			}
		}
		if c := m.Compaction; c != nil {
			if _, err := insertCompaction.ExecContext(ctx, lane.ID, msgs, c.Trigger,
				c.PreTokens, c.PostTokens, c.Dropped, c.Duration.Milliseconds()); err != nil {
				return fmt.Errorf("insert compaction: %w", err)
			}
		}
		if _, err := insertMsg.ExecContext(ctx, m.LaneID, msgs, m.ID,
			m.ParentID, string(m.Role), m.At.Unix(), previewOf(m), toolsOf(m),
			boolToInt(m.Failed())); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		for _, p := range m.Parts {
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			if _, err := insertPart.ExecContext(ctx, p.Text, m.LaneID, m.ID,
				string(p.Kind), string(m.Role), p.Tool, m.At.Unix()); err != nil {
				return fmt.Errorf("insert part: %w", err)
			}
			parts++
		}
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("index lane %s: %w", lane.ID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO lanes (id,source,project,path,title,cwd,created,updated,size,msg_count,part_count,last_role,last_tool,artifacts,artifact_path,indexed_bytes,indexed_last) `+
			`VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		lane.ID, lane.Source, lane.Project, lane.Path, lane.Title, lane.Cwd,
		unixOrZero(lane.Created), lane.Updated.Unix(), lane.Size, msgs, parts,
		string(activity.LastRole), boolToInt(activity.LastWasToolCall),
		lane.ArtifactBytes, lane.ArtifactPath, tail.Offset, tail.LastID); err != nil {
		return 0, 0, fmt.Errorf("insert lane %s: %w", lane.ID, err)
	}
	if err := indexSubagents(ctx, tx, src, lane, spawnedAt); err != nil {
		return 0, 0, err
	}
	return msgs, parts, nil
}

// indexSubagents records the conversations a lane spawned, each against the
// turn that spawned it.
func indexSubagents(ctx context.Context, tx *sql.Tx, src store.Source, lane model.Lane, spawnedAt map[string]int) error {
	sides, ok := src.(store.Sidechains)
	if !ok {
		return nil
	}
	agents, ok := readSubagents(ctx, sides, lane)
	if !ok {
		return nil // a lane is still worth indexing without its subagents
	}
	for _, a := range agents {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subagents (lane_id,agent_id,type,task,tool_use_id,depth,path,msgs,parent_seq) `+
				`VALUES (?,?,?,?,?,?,?,?,?)`,
			lane.ID, a.ID, a.Type, a.Task, a.ToolUseID, a.Depth, a.Path, a.Messages,
			spawnedAt[a.ToolUseID]); err != nil {
			return fmt.Errorf("insert subagent %s: %w", a.ID, err)
		}
	}
	return nil
}

// readSubagents fetches a lane's subagents, reporting false rather than an
// error: their absence is cosmetic next to the lane itself.
func readSubagents(ctx context.Context, sides store.Sidechains, lane model.Lane) ([]model.Subagent, bool) {
	agents, err := sides.Subagents(ctx, lane)
	if err != nil {
		return nil, false
	}
	return agents, true
}

// CompactionRow is a compaction together with the turn it happened at.
type CompactionRow struct {
	model.Compaction
	Seq int
}

// LaneCompactions returns where a conversation was compacted, in turn order.
func (ix *Index) LaneCompactions(ctx context.Context, laneID string) ([]CompactionRow, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT seq,trigger,pre_tokens,post_tokens,dropped,duration_ms
		 FROM compactions WHERE lane_id = ? ORDER BY seq`, laneID)
	if err != nil {
		return nil, fmt.Errorf("read compactions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []CompactionRow
	for rows.Next() {
		var r CompactionRow
		var ms int64
		if err := rows.Scan(&r.Seq, &r.Trigger, &r.PreTokens, &r.PostTokens, &r.Dropped, &ms); err != nil {
			return nil, fmt.Errorf("scan compaction: %w", err)
		}
		r.Duration = time.Duration(ms) * time.Millisecond
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate compactions: %w", err)
	}
	return out, nil
}

// SubagentRow is a subagent together with the turn it hangs from.
type SubagentRow struct {
	model.Subagent
	ParentSeq int
}

// RowsFrom reads a transcript into the rows a spine is built from, without
// touching the index. It is how a subagent can be read before any decision is
// made about it: looking should never require writing.
func RowsFrom(ctx context.Context, src store.Source, lane model.Lane) ([]MessageRow, error) {
	var rows []MessageRow
	err := src.Messages(ctx, lane, func(m model.Message) error {
		rows = append(rows, MessageRow{
			Seq:      len(rows) + 1,
			ID:       m.ID,
			ParentID: m.ParentID,
			Role:     m.Role,
			At:       m.At,
			Preview:  previewOf(m),
			Tools:    toolsOf(m),
			Failed:   m.Failed(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", lane.ID, err)
	}
	return rows, nil
}

// LaneSubagents returns the conversations a lane spawned, in turn order.
func (ix *Index) LaneSubagents(ctx context.Context, laneID string) ([]SubagentRow, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT agent_id,type,task,tool_use_id,depth,path,msgs,parent_seq
		 FROM subagents WHERE lane_id = ? ORDER BY parent_seq, agent_id`, laneID)
	if err != nil {
		return nil, fmt.Errorf("read subagents: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []SubagentRow
	for rows.Next() {
		r := SubagentRow{Subagent: model.Subagent{LaneID: laneID}}
		if err := rows.Scan(&r.ID, &r.Type, &r.Task, &r.ToolUseID, &r.Depth,
			&r.Path, &r.Messages, &r.ParentSeq); err != nil {
			return nil, fmt.Errorf("scan subagent: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subagents: %w", err)
	}
	return out, nil
}

// Search returns hits ranked by FTS5 relevance.
func (ix *Index) Search(ctx context.Context, q Query) ([]Hit, error) {
	if strings.TrimSpace(q.Text) == "" {
		return nil, errors.New("search: empty query")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	// Turns and documents are ranked by separate bm25 scales, so they are
	// queried apart and merged on score. Comparing bm25 across two tables is
	// approximate; it is still a far better order than showing one kind first
	// and burying the other.
	turns, err := ix.searchTurns(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	// Each kind is queried for its own best results. One query covering both
	// memories and work products would hand them a shared limit, and a
	// thousand filenames would starve the memories before the merge ever saw
	// them.
	sets := [][]Hit{turns}
	for _, of := range []Found{FoundMemory, FoundArtifact} {
		found, err := ix.searchDocs(ctx, q, of, limit)
		if err != nil {
			return nil, err
		}
		sets = append(sets, found)
	}
	return interleave(limit, sets...), nil
}

// searchTurns finds what was said in a conversation.
func (ix *Index) searchTurns(ctx context.Context, q Query, limit int) ([]Hit, error) {
	if !q.wants(FoundTurn) {
		return nil, nil
	}
	var (
		where = []string{"parts MATCH ?"}
		args  = []any{BuildMatch(q.Text)}
	)
	if q.Lane != "" {
		where = append(where, "parts.lane_id = ?")
		args = append(args, q.Lane)
	}
	if q.Project != "" {
		where = append(where, "lanes.project = ? COLLATE NOCASE")
		args = append(args, q.Project)
	}
	where, args = q.when("parts.at", where, args)
	if len(q.Kinds) > 0 {
		marks := make([]string, len(q.Kinds))
		for i, k := range q.Kinds {
			marks[i] = "?"
			args = append(args, string(k))
		}
		where = append(where, "parts.kind IN ("+strings.Join(marks, ",")+")")
	}
	args = append(args, limit)

	query := `
		SELECT parts.lane_id, COALESCE(lanes.title,''), COALESCE(lanes.project,''),
		       parts.msg_id, COALESCE(messages.seq,0), parts.kind, parts.role,
		       parts.tool, parts.at, snippet(parts, 0, '[', ']', '…', 12),
		       bm25(parts)
		FROM parts
		LEFT JOIN lanes ON lanes.id = parts.lane_id
		LEFT JOIN messages ON messages.lane_id = parts.lane_id AND messages.msg_id = parts.msg_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY rank LIMIT ?`

	rows, err := ix.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var hits []Hit
	for rows.Next() {
		var h Hit
		var kind, role string
		var at int64
		if err := rows.Scan(&h.LaneID, &h.LaneTitle, &h.Project, &h.MessageID, &h.Seq,
			&kind, &role, &h.Tool, &at, &h.Snippet, &h.Score); err != nil {
			return nil, fmt.Errorf("scan hit: %w", err)
		}
		h.Of = FoundTurn
		h.Kind = model.PartKind(kind)
		h.Role = model.Role(role)
		h.At = time.Unix(at, 0)
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hits: %w", err)
	}
	return hits, nil
}

// MessageRow is one turn as the spine needs it: enough to draw a line without
// reading the transcript again.
type MessageRow struct {
	Seq      int
	ID       string
	ParentID string
	Role     model.Role
	At       time.Time
	Preview  string
	Tools    string
	// Failed marks a turn whose tool call came back an error.
	Failed bool
}

// LanesWithCwd returns every conversation that recorded a working directory.
//
// Deciding which of them ran inside a given repository is the caller's job and
// not a query's: git reports a root with symlinks resolved and forward slashes
// even on Windows, while a transcript records the path the shell was in, and
// telling those apart means resolving both against the filesystem. There are
// tens of lanes, so reading them and comparing in Go costs nothing and is the
// only place the comparison can be made correctly.
func (ix *Index) LanesWithCwd(ctx context.Context) ([]LaneInfo, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id, COALESCE(title,''), COALESCE(project,''), cwd, path
		 FROM lanes WHERE cwd != '' ORDER BY updated DESC`)
	if err != nil {
		return nil, fmt.Errorf("list lanes with a working directory: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []LaneInfo
	for rows.Next() {
		var l LaneInfo
		if err := rows.Scan(&l.ID, &l.Title, &l.Project, &l.Cwd, &l.Path); err != nil {
			return nil, fmt.Errorf("scan lane: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// scanMessages reads the turn columns every message query selects.
func scanMessages(rows *sql.Rows) ([]MessageRow, error) {
	var out []MessageRow
	for rows.Next() {
		var r MessageRow
		var role string
		var at int64
		var failed int
		if err := rows.Scan(&r.Seq, &r.ID, &r.ParentID, &role, &at, &r.Preview, &r.Tools, &failed); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		r.Role = model.Role(role)
		r.At = time.Unix(at, 0)
		r.Failed = failed == 1
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return out, nil
}

// Around is what a conversation was doing in a window of time.
type Around struct {
	// Turns is everything that happened, tool calls and results included.
	Turns int
	// Spoke is the last turn in the window that said something, and Spoken
	// says whether there was one. Two thirds of turns in a real history are
	// tool calls and their results, which carry no text; quoting one of those
	// back tells the reader nothing about what was being discussed.
	Spoke  MessageRow
	Spoken bool
}

// Around reports one lane's activity in a window, oldest bound first.
//
// Explaining a file asks what was being discussed around the time it changed,
// which is a question about a slice of a conversation rather than all of it.
// Reading a whole lane to answer it would mean 25,000 rows to look at forty.
func (ix *Index) Around(ctx context.Context, laneID string, from, to time.Time) (Around, error) {
	var a Around
	if err := ix.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE lane_id = ? AND at >= ? AND at <= ?`,
		laneID, from.Unix(), to.Unix()).Scan(&a.Turns); err != nil {
		return Around{}, fmt.Errorf("count turns in window: %w", err)
	}
	if a.Turns == 0 {
		return a, nil
	}
	rows, err := ix.db.QueryContext(ctx,
		`SELECT seq, msg_id, parent_id, role, at, preview, tools, failed
		 FROM messages WHERE lane_id = ? AND at >= ? AND at <= ? AND preview != ''
		 ORDER BY at DESC LIMIT 1`, laneID, from.Unix(), to.Unix())
	if err != nil {
		return Around{}, fmt.Errorf("read last spoken turn: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only
	spoken, err := scanMessages(rows)
	if err != nil {
		return Around{}, err
	}
	if len(spoken) == 1 {
		a.Spoke, a.Spoken = spoken[0], true
	}
	return a, nil
}

// LaneMessages returns one lane's turns in file order.
func (ix *Index) LaneMessages(ctx context.Context, laneID string) ([]MessageRow, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT seq, msg_id, parent_id, role, at, preview, tools, failed
		 FROM messages WHERE lane_id = ? ORDER BY seq`, laneID)
	if err != nil {
		return nil, fmt.Errorf("read lane messages: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only
	return scanMessages(rows)
}

// previewMax bounds the stored one-line summary of a turn.
const previewMax = 240

// previewOf is the first readable text of a turn, collapsed to one line. Tool
// calls are summarised by name instead, since their raw input is rarely what a
// human is scanning for.
func previewOf(m model.Message) string {
	text := m.Text(model.PartText)
	if strings.TrimSpace(text) == "" {
		text = m.Text(model.PartThinking)
	}
	// A failed call says more through what came back than through anything the
	// turn said, which is usually nothing.
	if strings.TrimSpace(text) == "" && m.Failed() {
		text = failureOf(m)
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > previewMax {
		// Cut on a character boundary. Slicing bytes splits a multi-byte
		// character in half and puts invalid UTF-8 in the index, which reaches
		// the screen as a replacement mark, JSON as one too, and any reader
		// strict about encoding as an error. On a real history 117 of 21,353
		// previews were cut that way, every one of them a turn that quoted
		// box drawing.
		cut := previewMax
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	return text
}

// failureOf is what a failed tool call came back with.
func failureOf(m model.Message) string {
	for _, p := range m.Parts {
		if p.IsError {
			return p.Text
		}
	}
	return ""
}

// toolsOf lists the tools a turn invoked, in order, without repeats.
func toolsOf(m model.Message) string {
	var names []string
	seen := make(map[string]bool)
	for _, p := range m.Parts {
		if p.Kind != model.PartToolUse || p.Tool == "" || seen[p.Tool] {
			continue
		}
		seen[p.Tool] = true
		names = append(names, p.Tool)
	}
	return strings.Join(names, ",")
}

// Overlaps returns every message that appears in more than one lane, which is
// the raw material for fork detection.
func (ix *Index) Overlaps(ctx context.Context) ([]Overlap, error) {
	rows, err := ix.db.QueryContext(ctx, `
		SELECT msg_id, lane_id, seq FROM messages
		WHERE msg_id IN (
			SELECT msg_id FROM messages GROUP BY msg_id HAVING count(DISTINCT lane_id) > 1
		)
		ORDER BY msg_id, seq`)
	if err != nil {
		return nil, fmt.Errorf("find overlaps: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []Overlap
	for rows.Next() {
		var o Overlap
		if err := rows.Scan(&o.MessageID, &o.LaneID, &o.Seq); err != nil {
			return nil, fmt.Errorf("scan overlap: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overlaps: %w", err)
	}
	return out, nil
}

// Timelines returns each lane's message timestamps ordered by sequence. It is
// small enough to hold entirely (tens of thousands of int64s) and lets the
// graph decide fork direction exactly rather than by heuristic.
func (ix *Index) Timelines(ctx context.Context) (map[string][]time.Time, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT lane_id, at FROM messages ORDER BY lane_id, seq`)
	if err != nil {
		return nil, fmt.Errorf("read timelines: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	out := make(map[string][]time.Time)
	for rows.Next() {
		var lane string
		var at int64
		if err := rows.Scan(&lane, &at); err != nil {
			return nil, fmt.Errorf("scan timeline: %w", err)
		}
		out[lane] = append(out[lane], time.Unix(at, 0))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate timelines: %w", err)
	}
	return out, nil
}

// Lanes returns every indexed lane, most recently updated first.
func (ix *Index) Lanes(ctx context.Context) ([]LaneInfo, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id,source,project,path,title,cwd,created,updated,size,msg_count,part_count,last_role,last_tool,artifacts,artifact_path,indexed_bytes,indexed_last FROM lanes ORDER BY updated DESC`)
	if err != nil {
		return nil, fmt.Errorf("list lanes: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var lanes []LaneInfo
	for rows.Next() {
		var l LaneInfo
		var created, updated int64
		var lastRole string
		var lastTool int
		if err := rows.Scan(&l.ID, &l.Source, &l.Project, &l.Path, &l.Title, &l.Cwd,
			&created, &updated, &l.Size, &l.Messages, &l.Parts, &lastRole, &lastTool,
			&l.ArtifactBytes, &l.ArtifactPath, &l.Tail.Offset, &l.Tail.LastID); err != nil {
			return nil, fmt.Errorf("scan lane: %w", err)
		}
		if created > 0 {
			l.Created = time.Unix(created, 0)
		}
		l.Updated = time.Unix(updated, 0)
		l.Activity = model.Activity{LastRole: model.Role(lastRole), LastWasToolCall: lastTool == 1}
		lanes = append(lanes, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lanes: %w", err)
	}
	return lanes, nil
}

// activityOf reads a turn's contribution to the lane's state. Called for every
// turn, so the last one wins.
func activityOf(m model.Message) model.Activity {
	tool := false
	for _, p := range m.Parts {
		if p.Kind == model.PartToolUse {
			tool = true
		}
	}
	return model.Activity{LastRole: m.Role, LastWasToolCall: tool}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// unixOrZero keeps a zero time zero rather than writing a 1970 timestamp.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// advanced reports whether the user wrote an FTS5 expression themselves, in
// which case braids must not rewrite it.
func advanced(q string) bool {
	if strings.ContainsAny(q, `"*()`) {
		return true
	}
	for _, f := range strings.Fields(q) {
		switch f {
		case "AND", "OR", "NOT":
			return true
		}
		if strings.HasPrefix(f, "NEAR(") {
			return true
		}
	}
	return false
}

// BuildMatch turns user input into an FTS5 MATCH expression. Plain words are
// quoted so that punctuation in a search term cannot become syntax, while an
// explicit FTS5 expression is passed through untouched.
func BuildMatch(q string) string {
	q = strings.TrimSpace(q)
	if advanced(q) {
		return q
	}
	fields := strings.Fields(q)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, "")+`"`)
	}
	return strings.Join(quoted, " ")
}

// RefreshArtifacts re-measures every conversation's work products and stores
// what changed.
//
// Sync re-reads a conversation only when its transcript moved, which is right:
// nothing else can change what was said. But work products live outside the
// transcript, and deleting one leaves the conversation untouched — so the sizes
// on the map would stay as they were until something unrelated happened to the
// conversation. Whatever deletes or restores work products calls this.
//
// Measuring means walking the directories, tens of milliseconds against a few
// gigabytes, so it is called after an explicit action rather than on every
// refresh.
func (ix *Index) RefreshArtifacts(ctx context.Context, src store.Source) error {
	measurer, ok := src.(store.Measurer)
	if !ok {
		return nil // a source with no work products has nothing to re-measure
	}
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return err
	}
	for _, lane := range lanes {
		path, bytes := measurer.Artifacts(lane.ID)
		if path == lane.ArtifactPath && bytes == lane.ArtifactBytes {
			continue
		}
		if _, err := ix.db.ExecContext(ctx,
			`UPDATE lanes SET artifact_path = ?, artifacts = ? WHERE id = ?`,
			path, bytes, lane.ID); err != nil {
			return fmt.Errorf("update work products of %s: %w", lane.ID, err)
		}
	}
	return nil
}

// Found says what a hit is. Search returns turns, memories and work products
// together, and a result you cannot tell the kind of is a result you have to
// open to understand.
type Found string

// The kinds of thing search can find.
const (
	FoundTurn     Found = "conversation"
	FoundMemory   Found = "memory"
	FoundArtifact Found = "artifact"
)

// IsTurn reports whether a hit is something said in a conversation.
//
// The zero value counts as one: a turn is the common case and the original
// kind, so a Hit built without naming its kind is a turn rather than nothing.
func (h Hit) IsTurn() bool { return h.Of == "" || h.Of == FoundTurn }

// SyncMemories re-indexes memories when any of them changed.
//
// Memories are the thing you write and then immediately want to find, so
// leaving them out of search until the next `braids index` is the wrong answer.
// They are also tiny — a few hundred kilobytes — so the whole cost is deciding
// whether to bother, which is a directory listing per project. Work products
// are not done here: measuring those walks a tree of thousands of files.
func (ix *Index) SyncMemories(ctx context.Context, src store.Source) (bool, error) {
	rememberer, ok := src.(store.Rememberer)
	if !ok {
		return false, nil
	}
	locations, err := rememberer.MemoryDirs()
	if err != nil {
		return false, err
	}
	changed := false
	for _, location := range locations {
		count, newest := memory.Fingerprint(location)
		was, err := ix.memoryMark(ctx, location.Dir)
		if err != nil {
			return false, err
		}
		if was.count == count && was.newest == newest.Unix() {
			continue
		}
		changed = true
	}
	if !changed {
		return false, nil
	}

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if _, err := tx.ExecContext(ctx, `DELETE FROM docs WHERE doc_type = ?`, string(FoundMemory)); err != nil {
		return false, fmt.Errorf("clear memories: %w", err)
	}
	insert, err := tx.PrepareContext(ctx,
		`INSERT INTO docs (body, doc_type, name, path, project, lane_id, at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return false, fmt.Errorf("prepare docs: %w", err)
	}
	defer insert.Close() //nolint:errcheck // tx-scoped

	if err := indexMemories(ctx, insert, src); err != nil {
		return false, err
	}
	for _, location := range locations {
		count, newest := memory.Fingerprint(location)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_marks (dir, count, newest) VALUES (?,?,?)
			 ON CONFLICT(dir) DO UPDATE SET count=excluded.count, newest=excluded.newest`,
			location.Dir, count, newest.Unix()); err != nil {
			return false, fmt.Errorf("record memory mark: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit memories: %w", err)
	}
	return true, nil
}

// mark is what the index last saw of a memory directory.
type mark struct {
	count  int
	newest int64
}

func (ix *Index) memoryMark(ctx context.Context, dir string) (mark, error) {
	var m mark
	err := ix.db.QueryRowContext(ctx,
		`SELECT count, newest FROM memory_marks WHERE dir = ?`, dir).Scan(&m.count, &m.newest)
	if errors.Is(err, sql.ErrNoRows) {
		return mark{}, nil
	}
	if err != nil {
		return mark{}, fmt.Errorf("read memory mark: %w", err)
	}
	return m, nil
}

// SyncDocs rebuilds the searchable index of memories and work-product names.
//
// Rebuilt whole rather than incrementally: the corpus is a few hundred
// kilobytes of memories and some thousands of filenames, so tracking what
// changed would cost more than redoing it. Called by `braids index` rather than
// by every Sync, because measuring the work-product tree walks it — tens of
// milliseconds — and the map refreshes on every transcript write.
func (ix *Index) SyncDocs(ctx context.Context, src store.Source) error {
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return err
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if _, err := tx.ExecContext(ctx, `DELETE FROM docs`); err != nil {
		return fmt.Errorf("clear docs: %w", err)
	}
	insert, err := tx.PrepareContext(ctx,
		`INSERT INTO docs (body, doc_type, name, path, project, lane_id, at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare docs: %w", err)
	}
	defer insert.Close() //nolint:errcheck // tx-scoped

	if err := indexMemories(ctx, insert, src); err != nil {
		return err
	}
	if err := indexArtifacts(ctx, insert, lanes); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit docs: %w", err)
	}
	return nil
}

// indexMemories makes a project's memories searchable: name, description and
// the whole body, which is small enough to store outright.
func indexMemories(ctx context.Context, insert *sql.Stmt, src store.Source) error {
	rememberer, ok := src.(store.Rememberer)
	if !ok {
		return nil
	}
	locations, err := rememberer.MemoryDirs()
	if err != nil {
		return err
	}
	for _, location := range locations {
		set, err := memory.Read(location)
		if err != nil {
			return err
		}
		for _, m := range set.Memories {
			body, err := os.ReadFile(m.Path)
			if err != nil {
				// A memory that vanished between listing and reading is not a
				// reason to abandon the whole index.
				continue
			}
			searchable := m.Name + "\n" + m.Description + "\n" + string(body)
			if _, err := insert.ExecContext(ctx, searchable, string(FoundMemory),
				m.Name, m.Path, set.Project, m.Origin, m.Modified.Unix()); err != nil {
				return fmt.Errorf("index memory %s: %w", m.Name, err)
			}
		}
	}
	return nil
}

// indexArtifacts makes work products findable by name. Names only: one of them
// can be 231 MB, and reading them to index their contents would cost more than
// everything else braids does put together.
func indexArtifacts(ctx context.Context, insert *sql.Stmt, lanes []LaneInfo) error {
	for _, lane := range lanes {
		if lane.ArtifactPath == "" {
			continue
		}
		files, err := artifacts.Files(lane.ArtifactPath)
		if err != nil {
			continue // a job directory that went away mid-scan
		}
		for _, f := range files {
			if _, err := insert.ExecContext(ctx, f.Rel, string(FoundArtifact),
				f.Rel, f.Path, lane.Project, lane.ID, f.At.Unix()); err != nil {
				return fmt.Errorf("index work product %s: %w", f.Rel, err)
			}
		}
	}
	return nil
}

// searchDocs finds memories and work-product names.
func (ix *Index) searchDocs(ctx context.Context, q Query, of Found, limit int) ([]Hit, error) {
	if !q.wants(of) {
		return nil, nil
	}
	where := []string{"docs MATCH ?", "docs.doc_type = ?"}
	args := []any{BuildMatch(q.Text), string(of)}
	if q.Lane != "" {
		where = append(where, "docs.lane_id = ?")
		args = append(args, q.Lane)
	}
	if q.Project != "" {
		where = append(where, "docs.project = ? COLLATE NOCASE")
		args = append(args, q.Project)
	}
	where, args = q.when("docs.at", where, args)
	args = append(args, limit)

	query := `
		SELECT docs.doc_type, docs.name, docs.path, docs.project, docs.lane_id,
		       COALESCE(lanes.title,''), docs.at,
		       snippet(docs, 0, '[', ']', '…', 12), bm25(docs)
		FROM docs
		LEFT JOIN lanes ON lanes.id = docs.lane_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY rank LIMIT ?`

	rows, err := ix.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var hits []Hit
	for rows.Next() {
		var h Hit
		var of string
		var at int64
		if err := rows.Scan(&of, &h.Name, &h.Path, &h.Project, &h.LaneID,
			&h.LaneTitle, &at, &h.Snippet, &h.Score); err != nil {
			return nil, fmt.Errorf("scan document hit: %w", err)
		}
		h.Of = Found(of)
		h.At = time.Unix(at, 0)
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate document hits: %w", err)
	}
	return hits, nil
}

// wants reports whether a query asks for a kind of result. An empty Types
// means everything, so a plain search stays global.
func (q Query) wants(of Found) bool {
	if len(q.Types) == 0 {
		return true
	}
	for _, t := range q.Types {
		if t == of {
			return true
		}
	}
	return false
}

// interleave merges result sets by taking the best remaining of each kind in
// turn, rather than by score across all of them.
//
// bm25 rewards a match in a short document, and a work product's name is three
// words against a conversation turn's several hundred. Ranking them together
// buries every conversation under a list of filenames — which is the opposite
// of what a search across everything is for. Each kind keeps its own order;
// the merge only decides how many of each you see first.
func interleave(limit int, sets ...[]Hit) []Hit {
	byKind := map[Found][]Hit{}
	var order []Found
	for _, set := range sets {
		for _, h := range set {
			if _, seen := byKind[h.Of]; !seen {
				order = append(order, h.Of)
			}
			byKind[h.Of] = append(byKind[h.Of], h)
		}
	}
	hits := make([]Hit, 0, limit)
	for len(hits) < limit {
		took := false
		for _, of := range order {
			if len(byKind[of]) == 0 || len(hits) >= limit {
				continue
			}
			hits = append(hits, byKind[of][0])
			byKind[of] = byKind[of][1:]
			took = true
		}
		if !took {
			break
		}
	}
	return hits
}

// readAll reads a whole transcript, reporting where the read stopped so a
// later read can continue from there. A source that cannot be tailed reports
// nothing, and will be read whole every time.
func readAll(ctx context.Context, src store.Source, lane model.Lane, visit store.Visit) (store.Tail, error) {
	if tailer, ok := src.(store.Tailer); ok {
		return tailer.MessagesFrom(ctx, lane, store.Tail{}, visit)
	}
	return store.Tail{}, src.Messages(ctx, lane, visit)
}

// appendLane indexes what a transcript has gained, leaving what is already
// indexed alone.
//
// This is the whole point of tracking an offset. A live session appends a few
// hundred bytes at a time; re-reading its transcript to see them costs the
// whole file, which on a 145 MB conversation is over three seconds of parsing
// for every turn. Appending costs the bytes that arrived.
func appendLane(ctx context.Context, tx *sql.Tx, src store.Source, lane model.Lane, was LaneInfo) (msgs, parts int, err error) {
	tailer, ok := src.(store.Tailer)
	if !ok {
		return 0, 0, errNotTailable
	}
	insertPart, err := tx.PrepareContext(ctx,
		`INSERT INTO parts (body,lane_id,msg_id,kind,role,tool,at) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare part insert: %w", err)
	}
	defer insertPart.Close() //nolint:errcheck // tx-scoped
	insertMsg, err := tx.PrepareContext(ctx,
		`INSERT INTO messages (lane_id,seq,msg_id,parent_id,role,at,preview,tools,failed) `+
			`VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare message insert: %w", err)
	}
	defer insertMsg.Close() //nolint:errcheck // tx-scoped
	insertCompaction, err := tx.PrepareContext(ctx,
		`INSERT INTO compactions (lane_id,seq,trigger,pre_tokens,post_tokens,dropped,duration_ms) `+
			`VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare compaction insert: %w", err)
	}
	defer insertCompaction.Close() //nolint:errcheck // tx-scoped

	msgs, parts = was.Messages, was.Parts
	activity := was.Activity
	spawnedAt := map[string]int{}

	tail, err := tailer.MessagesFrom(ctx, lane, was.Tail, func(m model.Message) error {
		msgs++
		activity = activityOf(m)
		for _, p := range m.Parts {
			if p.Kind == model.PartToolUse && p.ID != "" {
				spawnedAt[p.ID] = msgs
			}
		}
		if c := m.Compaction; c != nil {
			if _, err := insertCompaction.ExecContext(ctx, lane.ID, msgs, c.Trigger,
				c.PreTokens, c.PostTokens, c.Dropped, c.Duration.Milliseconds()); err != nil {
				return fmt.Errorf("insert compaction: %w", err)
			}
		}
		if _, err := insertMsg.ExecContext(ctx, m.LaneID, msgs, m.ID,
			m.ParentID, string(m.Role), m.At.Unix(), previewOf(m), toolsOf(m),
			boolToInt(m.Failed())); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		for _, p := range m.Parts {
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			if _, err := insertPart.ExecContext(ctx, p.Text, m.LaneID, m.ID,
				string(p.Kind), string(m.Role), p.Tool, m.At.Unix()); err != nil {
				return fmt.Errorf("insert part: %w", err)
			}
			parts++
		}
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("append lane %s: %w", lane.ID, err)
	}

	// A rename carried in the tail is taken; a title is otherwise left as the
	// full read established it. Work products are re-measured, because a
	// session writing turns is usually writing files too.
	title, cwd := was.Title, was.Cwd
	if tail.Title != "" {
		title = tail.Title
	}
	if tail.Cwd != "" {
		cwd = tail.Cwd
	}
	artifactPath, artifactBytes := was.ArtifactPath, was.ArtifactBytes
	if measurer, ok := src.(store.Measurer); ok {
		artifactPath, artifactBytes = measurer.Artifacts(lane.ID)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE lanes SET title=?, cwd=?, updated=?, size=?, msg_count=?, part_count=?, `+
			`last_role=?, last_tool=?, artifacts=?, artifact_path=?, indexed_bytes=?, indexed_last=? `+
			`WHERE id=?`,
		title, cwd, lane.Updated.Unix(), lane.Size, msgs, parts,
		string(activity.LastRole), boolToInt(activity.LastWasToolCall),
		artifactBytes, artifactPath, tail.Offset, tail.LastID, lane.ID); err != nil {
		return 0, 0, fmt.Errorf("update lane %s: %w", lane.ID, err)
	}
	if err := appendSubagents(ctx, tx, src, lane, spawnedAt); err != nil {
		return 0, 0, err
	}
	return msgs, parts, nil
}

// appendSubagents adds the subagents a conversation has gained and refreshes
// the turn counts of the ones it already had.
//
// The rows already there are right and their turn numbers cannot be recomputed
// without re-reading the transcript, so they are left alone. A subagent spawned
// in the tail has its tool call in the tail too, which is where its turn number
// comes from.
func appendSubagents(ctx context.Context, tx *sql.Tx, src store.Source, lane model.Lane, spawnedAt map[string]int) error {
	sides, ok := src.(store.Sidechains)
	if !ok {
		return nil
	}
	agents, ok := readSubagents(ctx, sides, lane)
	if !ok {
		return nil // a lane is still worth indexing without its subagents
	}
	rows, err := tx.QueryContext(ctx, `SELECT agent_id FROM subagents WHERE lane_id = ?`, lane.ID)
	if err != nil {
		return fmt.Errorf("read subagents of %s: %w", lane.ID, err)
	}
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return errors.Join(fmt.Errorf("scan subagent: %w", err), rows.Close())
		}
		known[id] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("iterate subagents: %w", err)
	}

	for _, a := range agents {
		if known[a.ID] {
			// A running subagent grows; its turn count is what the spine shows.
			if _, err := tx.ExecContext(ctx,
				`UPDATE subagents SET msgs=? WHERE lane_id=? AND agent_id=?`,
				a.Messages, lane.ID, a.ID); err != nil {
				return fmt.Errorf("update subagent %s: %w", a.ID, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO subagents (lane_id,agent_id,type,task,tool_use_id,depth,path,msgs,parent_seq) `+
				`VALUES (?,?,?,?,?,?,?,?,?)`,
			lane.ID, a.ID, a.Type, a.Task, a.ToolUseID, a.Depth, a.Path, a.Messages,
			spawnedAt[a.ToolUseID]); err != nil {
			return fmt.Errorf("insert subagent %s: %w", a.ID, err)
		}
	}
	return nil
}

// errNotTailable says a source cannot be read from an offset, so the caller
// must fall back to reading the transcript whole.
var errNotTailable = errors.New("source cannot be tailed")

// indexLane brings one conversation up to date, appending where it can and
// re-reading it whole where it cannot.
//
// Appending is only safe while the file has strictly grown from a prefix that
// was already read. Every other shape — never indexed, shrunk, rewritten to
// the same length, an offset past the end, a source that cannot be tailed —
// falls back to the whole file. Being wrong about this would corrupt a
// conversation's history, and re-reading is merely slow.
func indexLane(ctx context.Context, tx *sql.Tx, src store.Source, lane model.Lane, was LaneInfo, known bool) (msgs, parts int, err error) {
	if appendable(lane, was, known) {
		msgs, parts, err = appendLane(ctx, tx, src, lane, was)
		if err == nil {
			return msgs, parts, nil
		}
		if !errors.Is(err, errNotTailable) {
			return 0, 0, err
		}
	}
	return replaceLane(ctx, tx, src, lane)
}

// appendable reports whether a transcript can be read from where the last read
// stopped.
func appendable(lane model.Lane, was LaneInfo, known bool) bool {
	switch {
	case !known || was.Tail.Offset <= 0:
		return false // never read, or read by a build that did not record where
	case lane.Size < was.Tail.Offset:
		return false // shorter than what was already read: not an append
	case lane.Size == was.Tail.Offset:
		// Nothing new past the last complete record. The file may still have
		// grown by a partial line, which is nothing to index yet.
		return true
	default:
		return true
	}
}
