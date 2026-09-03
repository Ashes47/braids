package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"

	// Pure-Go SQLite driver: no cgo, so braids cross-compiles cleanly.
	_ "modernc.org/sqlite"
)

// schemaVersion is bumped whenever the tables change. The index holds no unique
// state — it rebuilds from the transcripts in seconds — so an old schema is
// dropped and recreated rather than migrated.
const schemaVersion = 4

const dropAll = `
DROP TABLE IF EXISTS parts;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS lanes;`

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
	part_count INTEGER NOT NULL DEFAULT 0
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
	PRIMARY KEY (lane_id, seq)
);
CREATE INDEX IF NOT EXISTS messages_by_msg_id ON messages(msg_id);
CREATE VIRTUAL TABLE IF NOT EXISTS parts USING fts5(
	body,
	lane_id UNINDEXED,
	msg_id  UNINDEXED,
	kind    UNINDEXED,
	role    UNINDEXED,
	tool    UNINDEXED,
	at      UNINDEXED,
	tokenize='porter unicode61'
);`

// Index is a searchable snapshot of every lane a Source can see.
type Index struct {
	db *sql.DB
}

// Stats summarises a Rebuild.
type Stats struct {
	Lanes    int
	Messages int
	Parts    int
	Duration time.Duration
}

// Query selects messages. An empty Lane searches every lane, and empty Kinds
// searches every kind of content.
type Query struct {
	Text  string
	Lane  string
	Kinds []model.PartKind
	Limit int
}

// LaneInfo is a lane plus the counts only the index knows. Keeping the counts
// here rather than on model.Lane leaves the Source port free of index concerns.
type LaneInfo struct {
	model.Lane
	Messages int
	Parts    int
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
	LaneID    string
	LaneTitle string
	Project   string
	MessageID string
	Kind      model.PartKind
	Role      model.Role
	Tool      string
	Snippet   string
	At        time.Time
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
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, errors.Join(fmt.Errorf("apply %s: %w", pragma, err), db.Close())
		}
	}
	if err := migrate(db); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Index{db: db}, nil
}

// migrate brings the database to the current schema, discarding an older one.
// Callers must rebuild afterwards; Lanes on a freshly dropped index simply
// reports nothing, which the CLI and the map both already handle.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version != schemaVersion {
		if _, err := db.Exec(dropAll); err != nil {
			return fmt.Errorf("drop stale schema v%d: %w", version, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, schemaVersion)); err != nil {
		return fmt.Errorf("stamp schema version: %w", err)
	}
	return nil
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

	for _, stmt := range []string{`DELETE FROM parts`, `DELETE FROM messages`, `DELETE FROM lanes`} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return Stats{}, fmt.Errorf("clear index: %w", err)
		}
	}
	insertLane, err := tx.PrepareContext(ctx,
		`INSERT INTO lanes (id,source,project,path,title,cwd,created,updated,size,msg_count,part_count) `+
			`VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return Stats{}, fmt.Errorf("prepare lane insert: %w", err)
	}
	defer insertLane.Close() //nolint:errcheck // tx-scoped
	insertPart, err := tx.PrepareContext(ctx,
		`INSERT INTO parts (body,lane_id,msg_id,kind,role,tool,at) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return Stats{}, fmt.Errorf("prepare part insert: %w", err)
	}
	defer insertPart.Close() //nolint:errcheck // tx-scoped
	insertMsg, err := tx.PrepareContext(ctx,
		`INSERT INTO messages (lane_id,seq,msg_id,parent_id,role,at,preview,tools) `+
			`VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return Stats{}, fmt.Errorf("prepare message insert: %w", err)
	}
	defer insertMsg.Close() //nolint:errcheck // tx-scoped

	stats := Stats{Lanes: len(lanes)}
	for _, lane := range lanes {
		lane = enrich(ctx, src, lane)
		var laneMsgs, laneParts int
		err := src.Messages(ctx, lane, func(m model.Message) error {
			stats.Messages++
			laneMsgs++
			if _, err := insertMsg.ExecContext(ctx, m.LaneID, laneMsgs, m.ID,
				m.ParentID, string(m.Role), m.At.Unix(), previewOf(m), toolsOf(m)); err != nil {
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
				stats.Parts++
				laneParts++
			}
			return nil
		})
		if err != nil {
			return Stats{}, fmt.Errorf("index lane %s: %w", lane.ID, err)
		}
		if _, err := insertLane.ExecContext(ctx, lane.ID, lane.Source, lane.Project,
			lane.Path, lane.Title, lane.Cwd, unixOrZero(lane.Created), lane.Updated.Unix(),
			lane.Size, laneMsgs, laneParts); err != nil {
			return Stats{}, fmt.Errorf("insert lane %s: %w", lane.ID, err)
		}
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
		// Compare at second precision: that is what the index stores, so
		// comparing the raw ModTime would mark every lane changed, every time.
		if was, ok := stored[lane.ID]; ok && was.Size == lane.Size && was.Updated.Unix() == lane.Updated.Unix() {
			stats.Messages += was.Messages
			stats.Parts += was.Parts
			continue
		}
		msgs, parts, err := replaceLane(ctx, tx, src, lane)
		if err != nil {
			return Stats{}, err
		}
		stats.Messages += msgs
		stats.Parts += parts
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
		`INSERT INTO messages (lane_id,seq,msg_id,parent_id,role,at,preview,tools) `+
			`VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare message insert: %w", err)
	}
	defer insertMsg.Close() //nolint:errcheck // tx-scoped

	err = src.Messages(ctx, lane, func(m model.Message) error {
		msgs++
		if _, err := insertMsg.ExecContext(ctx, m.LaneID, msgs, m.ID,
			m.ParentID, string(m.Role), m.At.Unix(), previewOf(m), toolsOf(m)); err != nil {
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
		`INSERT INTO lanes (id,source,project,path,title,cwd,created,updated,size,msg_count,part_count) `+
			`VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		lane.ID, lane.Source, lane.Project, lane.Path, lane.Title, lane.Cwd,
		unixOrZero(lane.Created), lane.Updated.Unix(), lane.Size, msgs, parts); err != nil {
		return 0, 0, fmt.Errorf("insert lane %s: %w", lane.ID, err)
	}
	return msgs, parts, nil
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

	var (
		where = []string{"parts MATCH ?"}
		args  = []any{BuildMatch(q.Text)}
	)
	if q.Lane != "" {
		where = append(where, "parts.lane_id = ?")
		args = append(args, q.Lane)
	}
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
		       parts.msg_id, parts.kind, parts.role, parts.tool, parts.at,
		       snippet(parts, 0, '[', ']', '…', 12)
		FROM parts LEFT JOIN lanes ON lanes.id = parts.lane_id
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
		if err := rows.Scan(&h.LaneID, &h.LaneTitle, &h.Project, &h.MessageID,
			&kind, &role, &h.Tool, &at, &h.Snippet); err != nil {
			return nil, fmt.Errorf("scan hit: %w", err)
		}
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
}

// LaneMessages returns one lane's turns in file order.
func (ix *Index) LaneMessages(ctx context.Context, laneID string) ([]MessageRow, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT seq, msg_id, parent_id, role, at, preview, tools
		 FROM messages WHERE lane_id = ? ORDER BY seq`, laneID)
	if err != nil {
		return nil, fmt.Errorf("read lane messages: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var out []MessageRow
	for rows.Next() {
		var r MessageRow
		var role string
		var at int64
		if err := rows.Scan(&r.Seq, &r.ID, &r.ParentID, &role, &at, &r.Preview, &r.Tools); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		r.Role = model.Role(role)
		r.At = time.Unix(at, 0)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return out, nil
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
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > previewMax {
		text = text[:previewMax]
	}
	return text
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
		`SELECT id,source,project,path,title,cwd,created,updated,size,msg_count,part_count FROM lanes ORDER BY updated DESC`)
	if err != nil {
		return nil, fmt.Errorf("list lanes: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var lanes []LaneInfo
	for rows.Next() {
		var l LaneInfo
		var created, updated int64
		if err := rows.Scan(&l.ID, &l.Source, &l.Project, &l.Path, &l.Title, &l.Cwd,
			&created, &updated, &l.Size, &l.Messages, &l.Parts); err != nil {
			return nil, fmt.Errorf("scan lane: %w", err)
		}
		if created > 0 {
			l.Created = time.Unix(created, 0)
		}
		l.Updated = time.Unix(updated, 0)
		lanes = append(lanes, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lanes: %w", err)
	}
	return lanes, nil
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
