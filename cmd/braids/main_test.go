package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ashes47/braids/internal/core/model"
)

// fixtureRoot writes a minimal Claude Code transcript tree.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "-Users-me-src-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lines := []string{
		`{"type":"ai-title","aiTitle":"mount density","sessionId":"s1"}`,
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-08-21T11:04:00Z",` +
			`"message":{"role":"user","content":"the gcsfuse mount is hard-coded to ten"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-08-21T11:05:00Z",` +
			`"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"mount | grep gcsfuse"}}]}}`,
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := run(args, &buf); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return buf.String()
}

func TestIndexSearchAndLanesEndToEnd(t *testing.T) {
	root := fixtureRoot(t)
	db := filepath.Join(t.TempDir(), "index.db")

	out := runCmd(t, "index", "--root", root, "--db", db)
	if !strings.Contains(out, "1 lanes") || !strings.Contains(out, "2 messages") {
		t.Fatalf("index output = %q", out)
	}

	out = runCmd(t, "search", "--db", db, "gcsfuse")
	if !strings.Contains(out, "[gcsfuse]") {
		t.Errorf("expected a highlighted snippet, got %q", out)
	}
	if !strings.Contains(out, "mount density") {
		t.Errorf("expected the lane title in the row, got %q", out)
	}
	if !strings.Contains(out, "2 hits") {
		t.Errorf("expected both the text and the tool call to match, got %q", out)
	}

	out = runCmd(t, "search", "--db", db, "--kind", "tool_use", "gcsfuse")
	if !strings.Contains(out, "Bash") || strings.Contains(out, "2 hits") {
		t.Errorf("kind filter should leave only the Bash call, got %q", out)
	}

	out = runCmd(t, "lanes", "--db", db)
	if !strings.Contains(out, "mount density") || !strings.Contains(out, "app") {
		t.Errorf("lanes output = %q", out)
	}
}

func TestSearchFlagsWorkAfterTheQuery(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", fixtureRoot(t), "--db", db)

	// The natural way to type it: flags trailing the query.
	after := runCmd(t, "search", "--db", db, "gcsfuse", "--limit", "1")
	if !strings.Contains(after, "1 hits") {
		t.Errorf("trailing --limit ignored: %q", after)
	}
	before := runCmd(t, "search", "--db", db, "--limit", "1", "gcsfuse")
	if !strings.Contains(before, "1 hits") {
		t.Errorf("leading --limit ignored: %q", before)
	}
	// A multi-word query still reads as one query, not as stray flags.
	multi := runCmd(t, "search", "--db", db, "gcsfuse", "mount", "--limit", "5")
	if strings.Contains(multi, "no matches") {
		t.Errorf("multi-word query broke: %q", multi)
	}
}

func TestSearchWithNoMatchesIsNotAnError(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", fixtureRoot(t), "--db", db)
	if out := runCmd(t, "search", "--db", db, "zzzznotpresent"); !strings.Contains(out, "no matches") {
		t.Errorf("out = %q", out)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"frobnicate"}, "unknown command"},
		{"search without a query", []string{"search"}, "needs a query"},
		{"unknown kind", []string{"search", "--kind", "nonsense", "x"}, "unknown kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := run(tt.args, &buf)
			if err == nil {
				t.Fatalf("want an error, got output %q", buf.String())
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	// Bare `braids` opens the map, so only the explicit help verbs print usage.
	for _, args := range [][]string{{"help"}, {"--help"}} {
		if out := runCmd(t, args...); !strings.Contains(out, "manage Claude Code conversations") {
			t.Errorf("run(%v) = %q", args, out)
		}
	}
}

func TestMapRefusesToOpenWithNothingIndexed(t *testing.T) {
	// An empty index must fail with advice rather than reaching for a TTY.
	db := filepath.Join(t.TempDir(), "index.db")
	var buf bytes.Buffer
	err := run([]string{"map", "--db", db}, &buf)
	if err == nil {
		t.Fatal("want an error for an empty index")
	}
	if !strings.Contains(err.Error(), "braids index") {
		t.Errorf("err = %v, want it to suggest running braids index", err)
	}
}

func TestParseKinds(t *testing.T) {
	got, err := parseKinds(" text , tool_use ")
	if err != nil {
		t.Fatalf("parseKinds: %v", err)
	}
	want := []model.PartKind{model.PartText, model.PartToolUse}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parseKinds = %v, want %v", got, want)
	}
	if k, err := parseKinds("   "); err != nil || k != nil {
		t.Errorf("blank kinds = %v, %v; want nil, nil", k, err)
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	tests := []struct {
		in, want string
		n        int
	}{
		{"short", "short", 10},
		{"exactly-ten", "exactly-…", 9},
		{"héllo wörld", "héllo…", 6},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.n); got != tt.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestSlugMakesADirectoryName(t *testing.T) {
	tests := []struct{ name, fallback, want string }{
		{"try option c", "abc12345678", "try-option-c"},
		{"NULLS LAST / starved", "abc12345678", "nulls-last-starved"},
		{"  spaced  out  ", "abc12345678", "spaced-out"},
		{"", "abc12345678", "abc12345"},
		{"!!!", "abc12345678", "abc12345"},
		{"already-fine", "abc12345678", "already-fine"},
	}
	for _, tt := range tests {
		if got := slug(tt.name, tt.fallback); got != tt.want {
			t.Errorf("slug(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestWorktreeOKRejectsSomewhereThatIsNotARepo(t *testing.T) {
	if err := worktreeOK(t.TempDir()); err == nil {
		t.Error("a directory that is not a repository cannot hold a worktree")
	}
	if err := worktreeOK(""); err == nil {
		t.Error("a conversation with no directory cannot either")
	}
}
