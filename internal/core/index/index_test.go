package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"
	"unicode/utf8"

	"github.com/Ashes47/braids/internal/perms"

	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
)

// fakeSource is a Source backed by in-memory fixtures.
type fakeSource struct {
	lanes    []model.Lane
	messages map[string][]model.Message
	agents   map[string][]model.Subagent
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
			{ID: "main", Source: "fake", Project: "app", Path: "/tmp/main.jsonl", Title: "checkout delivery", Updated: now},
			{ID: "branch", Source: "fake", Project: "app", Path: "/tmp/branch.jsonl", Title: "try option c", Updated: now},
		},
		messages: map[string][]model.Message{
			"main": {{
				ID: "m1", LaneID: "main", Role: model.RoleUser, At: now,
				Parts: []model.Part{{Kind: model.PartText, Text: "the blobstore mount is hard-coded to ten per second"}},
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
				Parts: []model.Part{{Kind: model.PartText, Text: "does blobstore contend on the same shard index"}},
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

	hits, err := ix.Search(ctx, Query{Text: "blobstore"})
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
		hits, err := ix.Search(ctx, Query{Text: "blobstore", Lane: "branch"})
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
	hits, err := ix.Search(ctx, Query{Text: "blobstore"})
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
		{"plain words are quoted", "blobstore density", `"blobstore" "density"`},
		{"punctuation cannot become syntax", "batch-of-1", `"batch-of-1"`},
		{"quoted phrase passes through", `"NULLS LAST"`, `"NULLS LAST"`},
		{"boolean expression passes through", "lustre AND shard", "lustre AND shard"},
		{"NEAR passes through", "NEAR(delivery checkout, 8)", "NEAR(delivery checkout, 8)"},
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

func TestMigrateDiscardsAStaleSchema(t *testing.T) {
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

	// Refused, because repairing means reading every transcript again and only
	// `braids index` and the map are in a position to do that.
	if _, err := Open(path); !errors.Is(err, ErrSchemaChanged) {
		t.Fatalf("Open of a stale schema = %v, want ErrSchemaChanged", err)
	}
	ix, err := Migrate(path)
	if err != nil {
		t.Fatalf("Migrate must recover from a stale schema, got: %v", err)
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
	hits, err := ix.Search(ctx, Query{Text: "blobstore"})
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

func (f *fakeSource) Subagents(_ context.Context, lane model.Lane) ([]model.Subagent, error) {
	return f.agents[lane.ID], nil
}

func TestSubagentsAreAttachedToTheTurnThatSpawnedThem(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	src.agents = map[string][]model.Subagent{
		"main": {{
			ID: "agent-1", LaneID: "main", Type: "Explore", Task: "look around",
			ToolUseID: "toolu_1", Depth: 1, Path: "/tmp/agent-1.jsonl", Messages: 42,
		}},
	}
	// The tool call the agent answers lives on the lane's second turn.
	src.messages["main"][1].Parts[0].ID = "toolu_1"

	if _, err := ix.Rebuild(ctx, src); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	agents, err := ix.LaneSubagents(ctx, "main")
	if err != nil {
		t.Fatalf("LaneSubagents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("want 1 subagent, got %d", len(agents))
	}
	got := agents[0]
	if got.ParentSeq != 2 {
		t.Errorf("ParentSeq = %d, want 2 — without it the agent cannot be placed", got.ParentSeq)
	}
	if got.Type != "Explore" || got.Task != "look around" || got.Messages != 42 {
		t.Errorf("subagent = %+v", got)
	}

	// A lane that spawned none reports none.
	none, err := ix.LaneSubagents(ctx, "branch")
	if err != nil || none != nil {
		t.Errorf("branch subagents = %v, %v", none, err)
	}
}

func TestSyncReplacesSubagentsRatherThanDuplicating(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	src.agents = map[string][]model.Subagent{
		"main": {{ID: "agent-1", LaneID: "main", Type: "Explore", ToolUseID: "toolu_1"}},
	}
	src.messages["main"][1].Parts[0].ID = "toolu_1"

	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	src.lanes[0].Size = 1234 // force a re-read
	src.lanes[0].Updated = src.lanes[0].Updated.Add(time.Minute)
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	agents, err := ix.LaneSubagents(ctx, "main")
	if err != nil {
		t.Fatalf("LaneSubagents: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("re-reading a lane duplicated its subagents: %d", len(agents))
	}
}

// The index holds the full text of every message braids has read, and Claude
// Code keeps the transcripts it came from at 0700. Copying that into a
// world-readable file widens the user's exposure without being asked to.
func TestIndexIsPrivateToItsOwner(t *testing.T) {
	perms.RequirePOSIX(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")

	ix, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := ix.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s is mode %o, readable beyond its owner", filepath.Base(name), perm)
		}
	}

	// An index left loose by an older build is tightened on the next open,
	// not only when it is created.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close() //nolint:errcheck // test cleanup
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("reopening a loose index left it at mode %o", perm)
	}
}

// Work products change without the transcript changing, so Sync — which
// re-reads only what moved — cannot see it. The map carries a work-products
// column, and a stale number there is a lie about a disk.
func TestRefreshArtifactsSeesWhatSyncCannot(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	const session = "a1b2c3d4-0000-4000-8000-000000000001"
	body := `{"type":"ai-title","aiTitle":"work","sessionId":"` + session + `"}` + "\n" +
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z",` +
		`"message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	job := filepath.Join(root, "jobs", session[:8], "tmp")
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(job, "dump.json")
	if err := os.WriteFile(scratch, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup

	src := claudecode.New(filepath.Join(root, "projects"))
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	before := laneOf(t, ctx, ix, session)
	if before.ArtifactBytes < 4096 {
		t.Fatalf("indexed work products as %d bytes, want at least the scratch file", before.ArtifactBytes)
	}

	// Take the scratch file away. The transcript has not moved.
	if err := os.Remove(scratch); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync after delete: %v", err)
	}
	if stale := laneOf(t, ctx, ix, session); stale.ArtifactBytes != before.ArtifactBytes {
		t.Log("Sync noticed on its own; the guard below is then belt and braces")
	}

	if err := ix.RefreshArtifacts(ctx, src); err != nil {
		t.Fatalf("RefreshArtifacts: %v", err)
	}
	if after := laneOf(t, ctx, ix, session); after.ArtifactBytes >= before.ArtifactBytes {
		t.Errorf("work products still recorded as %d bytes, was %d before the delete",
			after.ArtifactBytes, before.ArtifactBytes)
	}
}

func laneOf(t *testing.T, ctx context.Context, ix *Index, id string) LaneInfo {
	t.Helper()
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		t.Fatalf("Lanes: %v", err)
	}
	for _, l := range lanes {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("no lane %s", id)
	return LaneInfo{}
}

// Search covers conversations, memories and work-product names at once, and
// says which kind each result is.
func TestSearchFindsAllThreeKinds(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	memories := filepath.Join(projects, "memory")
	if err := os.MkdirAll(memories, 0o700); err != nil {
		t.Fatal(err)
	}
	const session = "a1b2c3d4-0000-4000-8000-000000000001"
	transcript := `{"type":"ai-title","aiTitle":"the nodes work","sessionId":"` + session + `"}` + "\n" +
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z",` +
		`"message":{"role":"user","content":"the nodes are stalling again"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memories, "node-scheduling.md"),
		[]byte("---\nname: node-scheduling\ndescription: how nodes get placed\nmetadata:\n  type: project\n  originSessionId: "+
			session+"\n---\n\nNodes are picked by the scheduler.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memories, memory.IndexFile),
		[]byte("- [Node scheduling](node-scheduling.md) — placement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := filepath.Join(root, "jobs", session[:8], "tmp")
	if err := os.MkdirAll(filepath.Join(job, "node_modules", "junk"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(job, "nodes.json"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}
	// A vendored directory nobody searches their own machine for.
	if err := os.WriteFile(filepath.Join(job, "node_modules", "junk", "nodes.js"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup

	src := claudecode.New(filepath.Join(root, "projects"))
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := ix.SyncDocs(ctx, src); err != nil {
		t.Fatalf("SyncDocs: %v", err)
	}

	hits, err := ix.Search(ctx, Query{Text: "nodes", Limit: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := map[Found]string{}
	for _, h := range hits {
		found[h.Of] = h.Name
		if h.Of == "" {
			t.Errorf("a hit came back with no kind: %+v", h)
		}
	}
	for _, want := range []Found{FoundTurn, FoundMemory, FoundArtifact} {
		if _, ok := found[want]; !ok {
			t.Errorf("no %s in %v", want, found)
		}
	}
	if found[FoundMemory] != "node-scheduling" {
		t.Errorf("memory hit named %q", found[FoundMemory])
	}
	if found[FoundArtifact] != filepath.Join("tmp", "nodes.json") {
		t.Errorf("work-product hit named %q", found[FoundArtifact])
	}
	for _, h := range hits {
		if strings.Contains(h.Name, "node_modules") {
			t.Errorf("a vendored file was indexed: %s", h.Name)
		}
	}

	// Narrowing to one kind returns only that kind.
	only, err := ix.Search(ctx, Query{Text: "nodes", Types: []Found{FoundMemory}, Limit: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(only) != 1 || only[0].Of != FoundMemory {
		t.Errorf("--type memory returned %+v", only)
	}
}

// bm25 rewards a match in a short document, so filenames would bury every
// conversation. Each kind gets a place in the first results instead.
func TestInterleaveGivesEachKindAPlace(t *testing.T) {
	turns := []Hit{{Of: FoundTurn, Name: "t1"}, {Of: FoundTurn, Name: "t2"}, {Of: FoundTurn, Name: "t3"}}
	docs := []Hit{
		{Of: FoundArtifact, Name: "a1"}, {Of: FoundArtifact, Name: "a2"},
		{Of: FoundMemory, Name: "m1"},
	}
	got := interleave(4, turns, docs)
	if len(got) != 4 {
		t.Fatalf("got %d hits, want the limit", len(got))
	}
	kinds := map[Found]int{}
	for _, h := range got {
		kinds[h.Of]++
	}
	if kinds[FoundTurn] == 0 || kinds[FoundArtifact] == 0 || kinds[FoundMemory] == 0 {
		t.Errorf("a kind was crowded out: %v", kinds)
	}
	// Within a kind the original order stands.
	if got[0].Name != "t1" {
		t.Errorf("first hit is %q, want the best turn", got[0].Name)
	}
	// Asking for more than exists returns everything, once.
	all := interleave(50, turns, docs)
	if len(all) != 6 {
		t.Errorf("got %d hits, want all six", len(all))
	}
}

// The append path must produce exactly what a full read produces. Being wrong
// about this corrupts a conversation's history, so it is compared row for row.
func TestAppendingMatchesReadingWhole(t *testing.T) {
	const session = "a1b2c3d4-0000-4000-8000-000000000001"
	// A transcript with the awkward shapes: bookkeeping records in the parent
	// chain, a compaction boundary, a tool call, and a rename.
	turns := []string{
		`{"type":"ai-title","aiTitle":"first name","sessionId":"` + session + `"}`,
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z","cwd":"/w","message":{"role":"user","content":"one"}}`,
		`{"type":"attachment","uuid":"x1","parentUuid":"u1","timestamp":"2026-09-01T10:00:01Z"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"x1","timestamp":"2026-09-01T10:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"two"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-09-01T10:01:00Z","message":{"role":"user","content":"three"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-09-01T10:01:02Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"s1","parentUuid":null,"logicalParentUuid":"a2","timestamp":"2026-09-01T10:02:00Z","compactMetadata":{"trigger":"manual","preTokens":900,"postTokens":100}}`,
		`{"type":"user","uuid":"u3","parentUuid":"s1","timestamp":"2026-09-01T10:02:01Z","message":{"role":"user","content":"four"}}`,
		`{"type":"custom-title","customTitle":"renamed later","sessionId":"` + session + `"}`,
		`{"type":"assistant","uuid":"a3","parentUuid":"u3","timestamp":"2026-09-01T10:03:00Z","message":{"role":"assistant","content":[{"type":"text","text":"five"}]}}`,
	}

	// snapshot indexes a transcript made of the first n lines, one way or the
	// other, and returns what the index holds.
	snapshot := func(t *testing.T, upTo int, incremental bool) (LaneInfo, []MessageRow) {
		t.Helper()
		root := t.TempDir()
		projects := filepath.Join(root, "projects", "-p")
		if err := os.MkdirAll(projects, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(projects, session+".jsonl")
		write := func(lines []string) {
			body := strings.Join(lines, "\n") + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		ctx := context.Background()
		ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer ix.Close() //nolint:errcheck // test cleanup
		src := claudecode.New(filepath.Join(root, "projects"))

		if incremental {
			// Grow the file one turn at a time, syncing after each, so the
			// append path does all the work after the first read.
			for n := 1; n <= upTo; n++ {
				write(turns[:n])
				// Second-precision mtimes: make each state look distinct.
				stamp := time.Now().Add(time.Duration(n) * time.Second)
				if err := os.Chtimes(path, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				if _, err := ix.Sync(ctx, src); err != nil {
					t.Fatalf("sync at %d lines: %v", n, err)
				}
			}
		} else {
			write(turns[:upTo])
			if _, err := ix.Sync(ctx, src); err != nil {
				t.Fatalf("sync: %v", err)
			}
		}
		lane := laneOf(t, ctx, ix, session)
		rows, err := ix.LaneMessages(ctx, session)
		if err != nil {
			t.Fatalf("LaneMessages: %v", err)
		}
		// A compaction indexed twice, or at the wrong turn, would be invisible
		// in the message rows and obvious in the spine.
		compactions, err := ix.LaneCompactions(ctx, session)
		if err != nil {
			t.Fatalf("LaneCompactions: %v", err)
		}
		if len(compactions) != 1 {
			t.Errorf("recorded %d compactions, want exactly one", len(compactions))
		}
		return lane, rows
	}

	wholeLane, wholeRows := snapshot(t, len(turns), false)
	tailLane, tailRows := snapshot(t, len(turns), true)

	if tailLane.Tail.Offset == 0 {
		t.Fatal("the incremental run recorded no offset, so it never appended")
	}
	if len(tailRows) != len(wholeRows) {
		t.Fatalf("appending produced %d messages, reading whole produced %d",
			len(tailRows), len(wholeRows))
	}
	for i := range wholeRows {
		w, g := wholeRows[i], tailRows[i]
		if w.Seq != g.Seq || w.ID != g.ID || w.ParentID != g.ParentID ||
			w.Role != g.Role || w.Preview != g.Preview || w.Failed != g.Failed {
			t.Errorf("row %d differs:\n whole %+v\n tail  %+v", i, w, g)
		}
	}
	if wholeLane.Messages != tailLane.Messages || wholeLane.Parts != tailLane.Parts {
		t.Errorf("counts differ: whole %d/%d, tail %d/%d",
			wholeLane.Messages, wholeLane.Parts, tailLane.Messages, tailLane.Parts)
	}
	// The rename arrived in the tail and must have been picked up.
	if wholeLane.Title != tailLane.Title {
		t.Errorf("title: whole %q, tail %q", wholeLane.Title, tailLane.Title)
	}
	if tailLane.Title != "renamed later" {
		t.Errorf("title = %q, want the rename carried in the tail", tailLane.Title)
	}
}

// Appending is only safe while a file has grown from a prefix already read.
// Every other shape falls back to reading it whole, because being wrong here
// corrupts history and re-reading is merely slow.
func TestAppendableOnlyWhenTheFileGrew(t *testing.T) {
	lane := func(size int64) model.Lane { return model.Lane{Size: size} }
	was := func(offset int64) LaneInfo { return LaneInfo{Tail: store.Tail{Offset: offset}} }

	for _, tc := range []struct {
		name  string
		lane  model.Lane
		was   LaneInfo
		known bool
		want  bool
	}{
		{"never indexed", lane(100), LaneInfo{}, false, false},
		{"indexed by a build with no offset", lane(100), was(0), true, false},
		{"grew", lane(200), was(100), true, true},
		{"grew by a partial line only", lane(100), was(100), true, true},
		{"shrank", lane(50), was(100), true, false},
		{"rewritten shorter than the offset", lane(1), was(100), true, false},
	} {
		if got := appendable(tc.lane, tc.was, tc.known); got != tc.want {
			t.Errorf("%s: appendable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A transcript that is truncated and rewritten must be re-read whole, not
// appended to, or the index keeps rows for turns that no longer exist.
func TestATruncatedTranscriptIsReadWhole(t *testing.T) {
	const session = "a1b2c3d4-0000-4000-8000-000000000001"
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projects, session+".jsonl")
	turn := func(n string) string {
		return `{"type":"user","uuid":"u` + n + `","parentUuid":null,` +
			`"timestamp":"2026-09-01T10:0` + n + `:00Z","message":{"role":"user","content":"turn ` + n + `"}}`
	}
	write := func(lines ...string) {
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(time.Duration(len(lines)) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup
	src := claudecode.New(filepath.Join(root, "projects"))

	write(turn("1"), turn("2"), turn("3"))
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	if got := laneOf(t, ctx, ix, session).Messages; got != 3 {
		t.Fatalf("indexed %d messages, want 3", got)
	}

	// Rewritten shorter: the offset now points past the end.
	write(turn("9"))
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	lane := laneOf(t, ctx, ix, session)
	if lane.Messages != 1 {
		t.Errorf("after truncation the index holds %d messages, want 1", lane.Messages)
	}
	rows, err := ix.LaneMessages(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "u9" {
		t.Errorf("rows = %+v, want only the rewritten turn", rows)
	}
}

// A memory written now must be findable now: it is the thing you write and
// then immediately search for. Deciding whether to re-index is a directory
// listing, so this can run on every refresh.
func TestSyncMemoriesPicksUpANewMemoryAndSkipsWhenNothingMoved(t *testing.T) {
	root := t.TempDir()
	memories := filepath.Join(root, "projects", "-p", "memory")
	if err := os.MkdirAll(memories, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(memories, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(name, text string) string {
		return "---\nname: " + name + "\ndescription: d\nmetadata:\n  type: project\n---\n\n" + text + "\n"
	}
	write("first.md", entry("first", "the shard manifest is written twice"))
	write(memory.IndexFile, "- [First](first.md) — d\n")

	ctx := context.Background()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup
	src := claudecode.New(filepath.Join(root, "projects"))

	changed, err := ix.SyncMemories(ctx, src)
	if err != nil || !changed {
		t.Fatalf("first SyncMemories = %v, %v; want it to index", changed, err)
	}
	if hits, _ := ix.Search(ctx, Query{Text: "manifest", Limit: 5}); len(hits) != 1 {
		t.Fatalf("found %d hits for a memory just indexed", len(hits))
	}

	// Nothing moved: no work.
	if changed, err := ix.SyncMemories(ctx, src); err != nil || changed {
		t.Errorf("second SyncMemories = %v, %v; want it skipped", changed, err)
	}

	// A new memory appears.
	write("second.md", entry("second", "the reader contract came later"))
	if changed, err := ix.SyncMemories(ctx, src); err != nil || !changed {
		t.Fatalf("SyncMemories after a new memory = %v, %v", changed, err)
	}
	if hits, _ := ix.Search(ctx, Query{Text: "contract", Limit: 5}); len(hits) != 1 {
		t.Error("a memory written after the last index is not searchable")
	}

	// One is rewritten in place: same count, newer mtime.
	write("second.md", entry("second", "the reader contract was abandoned"))
	stamp := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(memories, "second.md"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if changed, err := ix.SyncMemories(ctx, src); err != nil || !changed {
		t.Fatalf("SyncMemories after a rewrite = %v, %v", changed, err)
	}
	if hits, _ := ix.Search(ctx, Query{Text: "abandoned", Limit: 5}); len(hits) != 1 {
		t.Error("a rewritten memory was not re-indexed")
	}
	if hits, _ := ix.Search(ctx, Query{Text: "came later", Limit: 5}); len(hits) != 0 {
		t.Error("the old text of a rewritten memory is still searchable")
	}
}

// An index written by another version is refused, not repaired.
//
// Repairing means dropping everything and reading the transcripts again, which
// a search is in no position to do. Before this it dropped the conversation
// tables and carried on, so the first search after an upgrade emptied the index
// and then answered from the one table the drop had not covered: a wrong answer
// wearing the shape of a right one, with the data gone.
func TestOpeningAnIndexFromAnotherVersionRefusesAndKeepsIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")

	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Rebuild(ctx, newFixture()); err != nil {
		t.Fatal(err)
	}
	before := countMessages(t, ix)
	if before == 0 {
		t.Fatal("the fixture indexed nothing, so the test proves nothing")
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	// What the next release looks like from here.
	stamp(t, path, schemaVersion-1)

	if _, err := Open(path); !errors.Is(err, ErrSchemaChanged) {
		t.Fatalf("Open of an older schema = %v, want ErrSchemaChanged", err)
	}
	// And it left the data where it was.
	again, err := Migrate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close() //nolint:errcheck // test
	if !again.Recreated() {
		t.Error("Migrate did not report that it replaced the index")
	}
	if after := countMessages(t, again); after != 0 {
		t.Errorf("Migrate left %d messages behind; it is meant to hand back an empty index", after)
	}
}

// The data survives being refused, which is the whole point of refusing.
func TestARefusedOpenTouchesNothing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Rebuild(ctx, newFixture()); err != nil {
		t.Fatal(err)
	}
	before := countMessages(t, ix)
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	stamp(t, path, schemaVersion-1)
	for range 3 {
		if _, err := Open(path); !errors.Is(err, ErrSchemaChanged) {
			t.Fatalf("Open = %v", err)
		}
	}

	// Put the version back and the rows are all still there.
	stamp(t, path, schemaVersion)
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close() //nolint:errcheck // test
	if after := countMessages(t, reopened); after != before {
		t.Errorf("held %d messages before being refused and %d after", before, after)
	}
}

func countMessages(t *testing.T, ix *Index) int {
	t.Helper()
	var n int
	if err := ix.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func stamp(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, version)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// A transcript with bytes in it that yields no messages is what a format
// change looks like from inside braids: sixteen of the eighteen record types
// in a real history are bookkeeping it skips on purpose, so an unfamiliar type
// is not news, and a lane that produced nothing is. Without this braids goes
// on drawing a confident map of a history it can no longer read.
func TestUnreadableNamesALaneThatYieldedNothing(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()

	// Same shape as a healthy lane, and the source hands back no messages.
	src.lanes = append(src.lanes, model.Lane{
		ID: "changed", Source: "fake", Project: "app",
		Path: "/tmp/changed.jsonl", Size: 4 << 20,
		Updated: time.Unix(1_700_000_000, 0),
	})

	if _, err := ix.Rebuild(ctx, src); err != nil {
		t.Fatal(err)
	}

	unreadable, err := ix.Unreadable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadable) != 1 {
		t.Fatalf("Unreadable found %d lanes, want the one with bytes and no messages", len(unreadable))
	}
	if unreadable[0].ID != "changed" {
		t.Errorf("Unreadable named %q", unreadable[0].ID)
	}
	if unreadable[0].Size != 4<<20 {
		t.Errorf("size came back as %d, so the report cannot say how much is unread", unreadable[0].Size)
	}
}

// An empty transcript is not a broken one, and neither is a healthy history.
func TestUnreadableStaysQuietWhenNothingIsWrong(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	src.lanes = append(src.lanes, model.Lane{
		ID: "empty", Source: "fake", Project: "app", Path: "/tmp/empty.jsonl", Size: 0,
	})
	if _, err := ix.Rebuild(ctx, src); err != nil {
		t.Fatal(err)
	}
	unreadable, err := ix.Unreadable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadable) != 0 {
		t.Errorf("cried wolf over %d lanes", len(unreadable))
	}
}

// LanesWithCwd hands back every conversation that recorded where it ran, and
// nothing else: deciding which of them is inside a given repository needs the
// filesystem, so it is not a query's job.
func TestLanesWithCwdReturnsOnlyThoseThatRecordedOne(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	src := newFixture()
	now := time.Unix(1_700_000_000, 0)
	src.lanes = []model.Lane{
		{ID: "here", Source: "fake", Path: "/t/here.jsonl", Cwd: "/src/app", Updated: now},
		{ID: "below", Source: "fake", Path: "/t/below.jsonl", Cwd: "/src/app/internal", Updated: now},
		{ID: "nowhere", Source: "fake", Path: "/t/no.jsonl", Cwd: "", Updated: now},
	}
	src.messages = map[string][]model.Message{}
	if _, err := ix.Rebuild(ctx, src); err != nil {
		t.Fatal(err)
	}

	got, err := ix.LanesWithCwd(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d lanes, want the two that recorded a directory", len(got))
	}
	for _, l := range got {
		if l.Cwd == "" {
			t.Errorf("%s came back with no working directory", l.ID)
		}
		if l.ID == "nowhere" {
			t.Error("a lane with no working directory was included")
		}
	}
}

// Around counts everything that happened but quotes only what was said, since
// two thirds of a real history is tool calls carrying no text.
func TestAroundCountsEverythingAndQuotesWhatWasSaid(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	base := time.Unix(1_700_000_000, 0)
	src := &fakeSource{
		lanes: []model.Lane{{ID: "l", Source: "fake", Path: "/t/l.jsonl", Updated: base}},
		messages: map[string][]model.Message{"l": {
			{ID: "m1", LaneID: "l", Role: model.RoleUser, At: base,
				Parts: []model.Part{{Kind: model.PartText, Text: "the first thing said"}}},
			{ID: "m2", LaneID: "l", ParentID: "m1", Role: model.RoleUser, At: base.Add(time.Minute),
				Parts: []model.Part{{Kind: model.PartText, Text: "the last thing said"}}},
			{ID: "m3", LaneID: "l", ParentID: "m2", Role: model.RoleAssistant, At: base.Add(2 * time.Minute),
				Parts: []model.Part{{Kind: model.PartToolUse, Tool: "Bash", Text: "go build ./..."}}},
			{ID: "m4", LaneID: "l", ParentID: "m3", Role: model.RoleUser, At: base.Add(time.Hour),
				Parts: []model.Part{{Kind: model.PartText, Text: "much later, outside the window"}}},
		}},
	}
	if _, err := ix.Rebuild(ctx, src); err != nil {
		t.Fatal(err)
	}

	got, err := ix.Around(ctx, "l", base.Add(-time.Minute), base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Turns != 3 {
		t.Errorf("counted %d turns in the window, want all 3 including the tool call", got.Turns)
	}
	if !got.Spoken {
		t.Fatal("nothing was quoted, though two turns had text")
	}
	if !strings.Contains(got.Spoke.Preview, "the last thing said") {
		t.Errorf("quoted %q, want the last turn that said something", got.Spoke.Preview)
	}

	// A window with nothing in it says so rather than reaching outside itself.
	empty, err := ix.Around(ctx, "l", base.Add(10*time.Minute), base.Add(20*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Turns != 0 || empty.Spoken {
		t.Errorf("an empty window reported %+v", empty)
	}
}

// The subtler half of a format change: a transcript braids used to read keeps
// growing and stops producing messages. The total never drops to zero, so the
// Unreadable check stays quiet while half the history stops arriving.
func TestSyncNoticesATranscriptThatStoppedProducingMessages(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	base := time.Unix(1_700_000_000, 0)
	src := &fakeSource{
		lanes: []model.Lane{
			{ID: "l", Source: "fake", Path: "/t/l.jsonl", Size: 4 << 10, Updated: base},
		},
		messages: map[string][]model.Message{"l": {
			{ID: "m1", LaneID: "l", Role: model.RoleUser, At: base,
				Parts: []model.Part{{Kind: model.PartText, Text: "readable"}}},
		}},
	}
	first, err := ix.Sync(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Stalled) != 0 {
		t.Errorf("a first read reported %d stalled lanes; there is nothing to compare against",
			len(first.Stalled))
	}

	// It grows, and braids gets nothing new out of it.
	src.lanes[0].Size = 4<<10 + stalledGrowth
	src.lanes[0].Updated = base.Add(time.Minute)
	stalled, err := ix.Sync(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(stalled.Stalled) != 1 {
		t.Fatalf("growth with no messages reported %d stalled lanes, want 1", len(stalled.Stalled))
	}
	if stalled.Stalled[0].Gained != stalledGrowth {
		t.Errorf("reported %d bytes gained, want %d", stalled.Stalled[0].Gained, stalledGrowth)
	}
	if stalled.Stalled[0].Path != "/t/l.jsonl" {
		t.Errorf("reported %q, which does not tell anyone which file to look at",
			stalled.Stalled[0].Path)
	}
}

// Growing and producing messages is what healthy looks like, and a small
// amount of growth with none is ordinary bookkeeping rather than an alarm.
func TestSyncStaysQuietWhenGrowthIsNormal(t *testing.T) {
	ctx := context.Background()
	ix := openIndex(t)
	base := time.Unix(1_700_000_000, 0)
	src := &fakeSource{
		lanes: []model.Lane{
			{ID: "l", Source: "fake", Path: "/t/l.jsonl", Size: 4 << 10, Updated: base},
		},
		messages: map[string][]model.Message{"l": {
			{ID: "m1", LaneID: "l", Role: model.RoleUser, At: base,
				Parts: []model.Part{{Kind: model.PartText, Text: "readable"}}},
		}},
	}
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}

	// Grew a lot, and said something: healthy.
	src.lanes[0].Size = 4<<10 + 10*stalledGrowth
	src.lanes[0].Updated = base.Add(time.Minute)
	src.messages["l"] = append(src.messages["l"], model.Message{
		ID: "m2", LaneID: "l", ParentID: "m1", Role: model.RoleAssistant, At: base.Add(time.Minute),
		Parts: []model.Part{{Kind: model.PartText, Text: "still readable"}}})
	if got, err := ix.Sync(ctx, src); err != nil || len(got.Stalled) != 0 {
		t.Errorf("a healthy read reported %v (%v)", got.Stalled, err)
	}

	// Grew a little, said nothing: bookkeeping, not an alarm.
	src.lanes[0].Size += stalledGrowth - 1
	src.lanes[0].Updated = base.Add(2 * time.Minute)
	if got, err := ix.Sync(ctx, src); err != nil || len(got.Stalled) != 0 {
		t.Errorf("growth under the threshold cried wolf: %v (%v)", got.Stalled, err)
	}
}

// A preview is stored at a fixed length, and cutting a string by bytes splits
// a multi-byte character in half. That leaves invalid UTF-8 in the index,
// which reaches the screen as a replacement mark and JSON as one too.
func TestPreviewIsCutOnACharacterBoundary(t *testing.T) {
	// Box drawing is three bytes a character and is what turns quoting braids'
	// own output are full of, which is where this was found.
	for _, pad := range []int{0, 1, 2, 3, 4} {
		text := strings.Repeat("a", previewMax-pad) + strings.Repeat("─", 40)
		got := previewOf(model.Message{
			Parts: []model.Part{{Kind: model.PartText, Text: text}},
		})
		if !utf8.ValidString(got) {
			t.Errorf("pad %d: preview is not valid UTF-8: %q", pad, got)
		}
		if len(got) > previewMax {
			t.Errorf("pad %d: preview is %d bytes, over the %d limit", pad, len(got), previewMax)
		}
	}

	// Short text is untouched, multi-byte or not.
	short := "a ─ b ─ c"
	if got := previewOf(model.Message{
		Parts: []model.Part{{Kind: model.PartText, Text: short}},
	}); got != short {
		t.Errorf("a short preview was altered: %q", got)
	}
}

// A rebuild drops every table and writes them again. Where the new contents are
// smaller than the old, the pages the old ones used stay free inside the file
// rather than going back to the disk, and nothing later reuses them all. On a
// real history that was 77 MB of a 201 MB file against 124 MB for the same data
// freshly written.
func TestRebuildGivesTheSpaceBack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close() //nolint:errcheck // test

	base := time.Unix(1_700_000_000, 0)
	corpus := func(n int) *fakeSource {
		src := &fakeSource{
			lanes:    []model.Lane{{ID: "l", Source: "fake", Path: "/t/l.jsonl"}},
			messages: map[string][]model.Message{"l": {}},
		}
		for i := range n {
			src.messages["l"] = append(src.messages["l"], model.Message{
				ID: fmt.Sprintf("m%d", i), LaneID: "l", Role: model.RoleUser,
				At: base.Add(time.Duration(i) * time.Second),
				Parts: []model.Part{{Kind: model.PartText,
					Text: strings.Repeat("a lot of searchable words ", 60)}},
			})
		}
		return src
	}

	if _, err := ix.Rebuild(ctx, corpus(6000)); err != nil {
		t.Fatal(err)
	}
	big, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// The transcripts shrank: conversations were deleted, or one was trimmed.
	if _, err := ix.Rebuild(ctx, corpus(50)); err != nil {
		t.Fatal(err)
	}
	small, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if small.Size() >= big.Size()/2 {
		t.Errorf("the index held %d bytes for a large corpus and still holds %d for a "+
			"small one, so a rebuild is not giving the space back",
			big.Size(), small.Size())
	}
}

// Claude Code writes session state into the same directory, with the same
// extension, as the conversations themselves. Resuming a session leaves a file
// holding its title, its mode and its cost and no turns at all, because the
// turns are recorded under the id it was resumed from.
//
// Read as a conversation that file is empty. braids used to index it anyway,
// which put a second, empty copy of a real conversation in the map under the
// same title, and then reported it as a possible format change with a link to
// the issue tracker. It is neither.
func TestSessionStateIsNotAConversation(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	const real = "a1b2c3d4-0000-4000-8000-000000000001"
	const state = "a1b2c3d4-0000-4000-8000-000000000002"

	// The conversation.
	body := `{"type":"ai-title","aiTitle":"ask before running bash","sessionId":"` + real + `"}` + "\n" +
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z",` +
		`"message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, real+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// The state left beside it: every record real, none of them a turn.
	only := `{"type":"last-prompt","lastPrompt":"ask before running bash","leafUuid":"u1","sessionId":"` + state + `"}` + "\n" +
		`{"type":"ai-title","aiTitle":"ask before running bash","sessionId":"` + state + `"}` + "\n" +
		`{"type":"mode","mode":"normal","sessionId":"` + state + `"}` + "\n" +
		`{"type":"permission-mode","permissionMode":"default","sessionId":"` + state + `"}` + "\n" +
		`{"type":"atis-latch","atis":"","sessionId":"` + state + `"}` + "\n" +
		`{"type":"cost-state","sessionId":"` + state + `","totalCostUSD":0.26}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, state+".jsonl"), []byte(only), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	src := claudecode.New(filepath.Join(root, "projects"))

	for _, how := range []struct {
		name string
		run  func(*Index) (Stats, error)
	}{
		{"Sync", func(ix *Index) (Stats, error) { return ix.Sync(ctx, src) }},
		{"Rebuild", func(ix *Index) (Stats, error) { return ix.Rebuild(ctx, src) }},
	} {
		t.Run(how.name, func(t *testing.T) {
			ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer ix.Close() //nolint:errcheck // test cleanup

			stats, err := how.run(ix)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Lanes != 1 {
				t.Errorf("indexed %d lanes, want only the conversation", stats.Lanes)
			}
			lanes, err := ix.Lanes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range lanes {
				if l.ID == state {
					t.Errorf("session state was indexed as a conversation, titled %q", l.Title)
				}
			}
			// And nothing to report, because nothing is wrong.
			unreadable, err := ix.Unreadable(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(unreadable) != 0 {
				t.Errorf("cried format change over %d lanes: %+v", len(unreadable), unreadable)
			}
		})
	}
}

// The other half, and the one that must not be sacrificed to fix the first: a
// transcript that does hold turns but yields no messages is what a real format
// change looks like, and it has to keep its row and be reported.
func TestATranscriptWithTurnsButNoMessagesIsStillReported(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	const session = "a1b2c3d4-0000-4000-8000-000000000003"
	// Turn-shaped records, carrying uuids, whose bodies braids cannot read:
	// this is the shape of the harness moving the message somewhere new.
	body := `{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z",` +
		`"speech":{"role":"user","words":"hello"}}` + "\n" +
		`{"type":"assistant","uuid":"u2","parentUuid":"u1","timestamp":"2026-09-01T10:00:01Z",` +
		`"speech":{"role":"assistant","words":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup

	if _, err := ix.Sync(ctx, claudecode.New(filepath.Join(root, "projects"))); err != nil {
		t.Fatal(err)
	}
	unreadable, err := ix.Unreadable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadable) != 1 || unreadable[0].ID != session {
		t.Fatalf("a transcript braids could not read went unreported: %+v", unreadable)
	}
}

// A conversation braids has already read and can no longer read must keep its
// row whatever else changes, because that lane's history is the thing the
// report exists to protect.
func TestAKnownLaneIsNeverDroppedForHavingNoTurns(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	const session = "a1b2c3d4-0000-4000-8000-000000000004"
	path := filepath.Join(projects, session+".jsonl")
	good := `{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z",` +
		`"message":{"role":"user","content":"hello, and here is a good deal more text ` +
		`so that the state written over it later is unmistakably shorter"}}` + "\n"
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	src := claudecode.New(filepath.Join(root, "projects"))
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup

	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	if got := laneOf(t, ctx, ix, session); got.Messages != 1 {
		t.Fatalf("the conversation indexed as %d messages", got.Messages)
	}

	// Now the file becomes something braids reads nothing from. It has to
	// shrink, so the read cannot be a tail off the end of what was already
	// indexed: that would keep the messages already stored and never ask the
	// question. A whole re-read that yields nothing is the dangerous case.
	state := `{"type":"mode","mode":"normal","sessionId":"` + session + `"}` + "\n"
	if err := os.WriteFile(path, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lanes {
		if l.ID == session {
			found = true
		}
	}
	if !found {
		t.Error("a conversation braids had already read was dropped from the index")
	}
}

// A phantom already in the index has to be cleared without a rebuild.
//
// The file it came from is session state and will never change again, so the
// pass that would notice never runs: an untouched lane is skipped before it is
// read. Somebody who indexed with an older braids would otherwise keep the
// empty duplicate in their map for good.
func TestAPhantomFromAnOlderIndexIsClearedOut(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	const state = "a1b2c3d4-0000-4000-8000-000000000005"
	path := filepath.Join(projects, state+".jsonl")
	body := `{"type":"ai-title","aiTitle":"ask before running bash","sessionId":"` + state + `"}` + "\n" +
		`{"type":"cost-state","sessionId":"` + state + `","totalCostUSD":0.26}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close() //nolint:errcheck // test cleanup

	// Exactly what an older braids left behind: a row whose size and mtime
	// match the file on disk, so the next sync has no reason to read it.
	if _, err := ix.db.ExecContext(ctx,
		`INSERT INTO lanes (id,source,project,path,title,cwd,created,updated,size,msg_count,part_count)
		 VALUES (?,?,?,?,?,?,?,?,?,0,0)`,
		state, "claudecode", "-p", path, "ask before running bash", "",
		fi.ModTime().Unix(), fi.ModTime().Unix(), fi.Size()); err != nil {
		t.Fatal(err)
	}

	if _, err := ix.Sync(ctx, claudecode.New(filepath.Join(root, "projects"))); err != nil {
		t.Fatal(err)
	}
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lanes {
		if l.ID == state {
			t.Fatalf("the phantom survived a sync that never had to read its file")
		}
	}
	unreadable, err := ix.Unreadable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadable) != 0 {
		t.Errorf("still reporting %d lanes as unreadable", len(unreadable))
	}
}
