package index

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

// fakeSource is a Source backed by in-memory fixtures.
type fakeSource struct {
	lanes    []model.Lane
	messages map[string][]model.Message
	// reads counts how many lanes were actually streamed, so a test can assert
	// that an incremental sync skipped the rest.
	reads int
}

func (f *fakeSource) Name() string                     { return "fake" }
func (f *fakeSource) Capabilities() store.Capabilities { return store.Capabilities{} }

func (f *fakeSource) Lanes(context.Context) ([]model.Lane, error) { return f.lanes, nil }

func (f *fakeSource) Messages(_ context.Context, lane model.Lane, visit store.Visit) error {
	f.reads++
	for _, m := range f.messages[lane.ID] {
		if err := visit(m); err != nil {
			return err
		}
	}
	return nil
}

func newFixture() *fakeSource {
	now := time.Unix(1_700_000_000, 0)
	return &fakeSource{
		lanes: []model.Lane{
			{ID: "main", Source: "fake", Project: "app", Path: "/tmp/main.jsonl", Title: "nvidia delivery", Updated: now},
			{ID: "branch", Source: "fake", Project: "app", Path: "/tmp/branch.jsonl", Title: "try option c", Updated: now},
		},
		messages: map[string][]model.Message{
			"main": {{
				ID: "m1", LaneID: "main", Role: model.RoleUser, At: now,
				Parts: []model.Part{{Kind: model.PartText, Text: "the gcsfuse mount is hard-coded to ten per second"}},
			}, {
				ID: "m2", LaneID: "main", Role: model.RoleAssistant, At: now,
				Parts: []model.Part{
					{Kind: model.PartToolUse, Tool: "Bash", Text: `{"command":"kubectl get pods"}`},
					{Kind: model.PartText, Text: "raising density needs the sidecar"},
					{Kind: model.PartText, Text: "   "}, // blank parts must not be indexed
				},
			}},
			"branch": {{
				ID: "b1", LaneID: "branch", Role: model.RoleUser, At: now,
				Parts: []model.Part{{Kind: model.PartText, Text: "does gcsfuse contend on the same MDT"}},
			}},
		},
	}
}

func openIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix
}

func TestRebuildAndSearch(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)

	stats, err := ix.Rebuild(ctx, newFixture())
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if stats.Lanes != 2 || stats.Messages != 3 || stats.Parts != 4 {
		t.Fatalf("stats = %+v, want 2 lanes / 3 messages / 4 parts (blank part skipped)", stats)
	}

	hits, err := ix.Search(ctx, Query{Text: "gcsfuse"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits across both lanes, got %d", len(hits))
	}
	for _, h := range hits {
		if h.Snippet == "" || h.LaneTitle == "" || h.Project != "app" {
			t.Errorf("hit missing context: %+v", h)
		}
	}
}

func TestSearchFilters(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	if _, err := ix.Rebuild(ctx, newFixture()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	t.Run("by lane", func(t *testing.T) {
		hits, err := ix.Search(ctx, Query{Text: "gcsfuse", Lane: "branch"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(hits) != 1 || hits[0].LaneID != "branch" {
			t.Fatalf("lane filter returned %+v", hits)
		}
	})

	t.Run("by kind", func(t *testing.T) {
		hits, err := ix.Search(ctx, Query{Text: "kubectl", Kinds: []model.PartKind{model.PartToolUse}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(hits) != 1 || hits[0].Kind != model.PartToolUse || hits[0].Tool != "Bash" {
			t.Fatalf("kind filter returned %+v", hits)
		}
	})

	t.Run("empty query is an error", func(t *testing.T) {
		if _, err := ix.Search(ctx, Query{Text: "  "}); err == nil {
			t.Fatal("want error for empty query")
		}
	})
}

func TestRebuildIsIdempotent(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()

	first, err := ix.Rebuild(ctx, src)
	if err != nil {
		t.Fatalf("first Rebuild: %v", err)
	}
	second, err := ix.Rebuild(ctx, src)
	if err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	if first.Parts != second.Parts {
		t.Fatalf("parts drifted: %d then %d", first.Parts, second.Parts)
	}
	hits, err := ix.Search(ctx, Query{Text: "gcsfuse"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("rebuild duplicated rows: got %d hits, want 2", len(hits))
	}
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	if len(lanes) != 2 {
		t.Fatalf("got %d lanes, want 2", len(lanes))
	}
	for _, l := range lanes {
		if l.Messages == 0 || l.Parts == 0 {
			t.Errorf("lane %s missing counts: %+v", l.ID, l)
		}
	}
}

func TestBuildMatch(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain words are quoted", "gcsfuse density", `"gcsfuse" "density"`},
		{"punctuation cannot become syntax", "batch-of-1", `"batch-of-1"`},
		{"quoted phrase passes through", `"NULLS LAST"`, `"NULLS LAST"`},
		{"boolean expression passes through", "lustre AND mdt", "lustre AND mdt"},
		{"NEAR passes through", "NEAR(delivery nvidia, 8)", "NEAR(delivery nvidia, 8)"},
		{"prefix search passes through", "gcsf*", "gcsf*"},
		{"surrounding space is trimmed", "  halt  ", `"halt"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildMatch(tt.in); got != tt.want {
				t.Errorf("BuildMatch(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOverlapsFindSharedMessages(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)

	now := time.Unix(1_700_000_000, 0)
	shared := model.Message{ID: "shared", Role: model.RoleUser, At: now,
		Parts: []model.Part{{Kind: model.PartText, Text: "common ancestor turn"}}}
	src := &fakeSource{
		lanes: []model.Lane{{ID: "parent"}, {ID: "child"}, {ID: "unrelated"}},
		messages: map[string][]model.Message{
			// A fork copies the parent's records verbatim, IDs included.
			"parent": {
				withLane(shared, "parent"),
				{ID: "p2", LaneID: "parent", Parts: []model.Part{{Kind: model.PartText, Text: "parent continued"}}},
			},
			"child": {
				withLane(shared, "child"),
				{ID: "c2", LaneID: "child", Parts: []model.Part{{Kind: model.PartText, Text: "child diverged"}}},
			},
			"unrelated": {
				{ID: "u1", LaneID: "unrelated", Parts: []model.Part{{Kind: model.PartText, Text: "nothing in common"}}},
			},
		},
	}
	if _, err := ix.Rebuild(ctx, src); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	overlaps, err := ix.Overlaps(ctx)
	if err != nil {
		t.Fatalf("Overlaps: %v", err)
	}
	if len(overlaps) != 2 {
		t.Fatalf("want the shared message reported once per lane, got %+v", overlaps)
	}
	lanes := map[string]bool{}
	for _, o := range overlaps {
		if o.MessageID != "shared" {
			t.Errorf("unexpected overlap %+v", o)
		}
		if o.Seq != 1 {
			t.Errorf("seq = %d, want 1", o.Seq)
		}
		lanes[o.LaneID] = true
	}
	if !lanes["parent"] || !lanes["child"] || lanes["unrelated"] {
		t.Errorf("overlap lanes = %v", lanes)
	}
}

func withLane(m model.Message, lane string) model.Message {
	m.LaneID = lane
	return m
}

func TestOpenDiscardsAStaleSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")

	// An index written by an older braids: right table name, wrong columns.
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := old.Exec(`CREATE TABLE lanes (id TEXT PRIMARY KEY, whatever TEXT)`); err != nil {
		t.Fatalf("seed stale schema: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	ix, err := Open(path)
	if err != nil {
		t.Fatalf("Open must recover from a stale schema, got: %v", err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup

	// The rebuild that follows must succeed against the current columns.
	if _, err := ix.Rebuild(context.Background(), newFixture()); err != nil {
		t.Fatalf("Rebuild after migration: %v", err)
	}
	lanes, err := ix.Lanes(context.Background())
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	if len(lanes) != 2 {
		t.Fatalf("got %d lanes after rebuild, want 2", len(lanes))
	}
}

func TestSyncOnlyRereadsWhatChanged(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()

	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Nothing changed: a second Sync must not re-read a single lane.
	src.reads = 0
	stats, err := ix.Sync(ctx, src)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if src.reads != 0 {
		t.Errorf("Sync re-read %d unchanged lanes, want 0", src.reads)
	}
	if stats.Messages != 3 || stats.Parts != 4 {
		t.Errorf("stats = %+v, want the counts carried forward", stats)
	}

	// A lane whose size and mtime moved is re-read; its siblings are not.
	src.lanes[1].Size = 999
	src.lanes[1].Updated = src.lanes[1].Updated.Add(time.Minute)
	src.reads = 0
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("third Sync: %v", err)
	}
	if src.reads != 1 {
		t.Errorf("Sync re-read %d lanes, want only the changed one", src.reads)
	}
}

func TestSyncToleratesSubSecondModTimes(t *testing.T) {
	// The index stores mtime at second precision. Comparing the raw ModTime
	// marks every lane changed on every run, which silently turns an
	// incremental sync back into a full rebuild.
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	for i := range src.lanes {
		src.lanes[i].Updated = src.lanes[i].Updated.Add(750 * time.Millisecond)
	}
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	src.reads = 0
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if src.reads != 0 {
		t.Errorf("sub-second mtimes caused %d needless re-reads", src.reads)
	}
}

func TestSyncDropsVanishedLanes(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	src.lanes = src.lanes[:1] // the branch lane is deleted from disk
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync after deletion: %v", err)
	}
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	if len(lanes) != 1 || lanes[0].ID != "main" {
		t.Fatalf("lanes = %+v, want only main", lanes)
	}
	hits, err := ix.Search(ctx, Query{Text: "gcsfuse"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("a deleted lane left %d hits behind, want 1", len(hits))
	}
}

func TestSyncPicksUpANewLane(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	fresh := model.Lane{ID: "fresh", Source: "fake", Project: "app", Title: "just branched"}
	src.lanes = append(src.lanes, fresh)
	src.messages["fresh"] = []model.Message{{
		ID: "f1", LaneID: "fresh", Role: model.RoleUser,
		Parts: []model.Part{{Kind: model.PartText, Text: "a brand new conversation"}},
	}}
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync after branch: %v", err)
	}
	hits, err := ix.Search(ctx, Query{Text: "brand new"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].LaneID != "fresh" {
		t.Errorf("a new lane was not indexed: %+v", hits)
	}
}

func TestSearchHitsCarryTheirTurnNumber(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	if _, err := ix.Rebuild(ctx, newFixture()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	hits, err := ix.Search(ctx, Query{Text: "sidecar", Lane: "main"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	// Without the turn number a result cannot be jumped to.
	if hits[0].Seq != 2 {
		t.Errorf("Seq = %d, want 2 (the second turn of the lane)", hits[0].Seq)
	}
}
