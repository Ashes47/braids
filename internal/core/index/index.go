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

const schema = `
CREATE TABLE IF NOT EXISTS lanes (
	id      TEXT PRIMARY KEY,
	source  TEXT NOT NULL,
	project TEXT NOT NULL,
	path    TEXT NOT NULL,
	title   TEXT NOT NULL,
	updated INTEGER NOT NULL,
	size    INTEGER NOT NULL,
	msg_count  INTEGER NOT NULL DEFAULT 0,
	part_count INTEGER NOT NULL DEFAULT 0
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
	if _, err := db.Exec(schema); err != nil {
		return nil, errors.Join(fmt.Errorf("create schema: %w", err), db.Close())
	}
	return &Index{db: db}, nil
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

	for _, stmt := range []string{`DELETE FROM parts`, `DELETE FROM lanes`} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return Stats{}, fmt.Errorf("clear index: %w", err)
		}
	}
	insertLane, err := tx.PrepareContext(ctx,
		`INSERT INTO lanes (id,source,project,path,title,updated,size,msg_count,part_count) `+
			`VALUES (?,?,?,?,?,?,?,?,?)`)
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

	stats := Stats{Lanes: len(lanes)}
	for _, lane := range lanes {
		var laneMsgs, laneParts int
		err := src.Messages(ctx, lane, func(m model.Message) error {
			stats.Messages++
			laneMsgs++
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
			lane.Path, lane.Title, lane.Updated.Unix(), lane.Size, laneMsgs, laneParts); err != nil {
			return Stats{}, fmt.Errorf("insert lane %s: %w", lane.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("commit: %w", err)
	}
	stats.Duration = time.Since(start)
	return stats, nil
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

// Lanes returns every indexed lane, most recently updated first.
func (ix *Index) Lanes(ctx context.Context) ([]LaneInfo, error) {
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id,source,project,path,title,updated,size,msg_count,part_count FROM lanes ORDER BY updated DESC`)
	if err != nil {
		return nil, fmt.Errorf("list lanes: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var lanes []LaneInfo
	for rows.Next() {
		var l LaneInfo
		var updated int64
		if err := rows.Scan(&l.ID, &l.Source, &l.Project, &l.Path, &l.Title,
			&updated, &l.Size, &l.Messages, &l.Parts); err != nil {
			return nil, fmt.Errorf("scan lane: %w", err)
		}
		l.Updated = time.Unix(updated, 0)
		lanes = append(lanes, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lanes: %w", err)
	}
	return lanes, nil
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
