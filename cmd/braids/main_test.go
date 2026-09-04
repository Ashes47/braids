package main

import (
	"bytes"
	"errors"
	"io"
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

// A mistyped command should name the one that was meant. Reaching for the
// manual to find a letter you already typed correctly is a poor trade.
func TestUnknownCommandSuggestsTheOneMeant(t *testing.T) {
	for _, tt := range []struct{ typed, want string }{
		{"brnach", `did you mean "branch"`},
		{"serch", `did you mean "search"`},
		{"hooks--install", "try: braids help"}, // too far from anything to guess
		{"frobnicate", "try: braids help"},
	} {
		err := run([]string{tt.typed}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("run(%q) = %v, want it to mention %q", tt.typed, err, tt.want)
		}
	}
}

// braids hook is run by the harness, which pipes a payload in. Typed by hand it
// used to wait on a terminal that would never send one, looking like a hang.
func TestHookRefusesATerminalInsteadOfWaitingOnIt(t *testing.T) {
	restore := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = restore })

	stdinIsTerminal = func() bool { return true }
	err := run([]string{"hook"}, io.Discard)
	if err == nil {
		t.Fatal("want an error explaining who runs hook")
	}
	for _, want := range []string{"the harness runs it", "braids hooks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}

	// Piped, it stays silent: a hook that fails loudly breaks the session it
	// is reporting on.
	stdinIsTerminal = func() bool { return false }
	t.Setenv("HOME", t.TempDir())
	if err := run([]string{"hook"}, io.Discard); err != nil {
		t.Errorf("piped hook = %v, want it to record quietly", err)
	}
}

// A flag mistake should answer with the flags the command actually takes,
// spelled the way braids spells them everywhere else.
func TestFlagMistakeListsTheRealFlags(t *testing.T) {
	err := run([]string{"hooks", "--instal"}, io.Discard)
	if err == nil {
		t.Fatal("want an error for an undefined flag")
	}
	for _, want := range []string{"not defined", "--install", "--remove", "--settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Usage of") {
		t.Errorf("err = %v, want braids' own voice, not the flag package's", err)
	}
}

// -h on a command prints that command's flags and exits cleanly.
func TestCommandHelpPrintsItsOwnFlags(t *testing.T) {
	var buf bytes.Buffer
	if err := run([]string{"merge", "-h"}, &buf); err != nil && !errors.Is(err, errShown) {
		t.Fatalf("merge -h = %v", err)
	}
	for _, want := range []string{"--lane", "--from", "braids help"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("merge -h = %q, want it to mention %q", buf.String(), want)
		}
	}
}

func TestVersionFlagsMatchTheVersionCommand(t *testing.T) {
	want := runCmd(t, "version")
	for _, args := range [][]string{{"-v"}, {"--version"}} {
		if got := runCmd(t, args...); got != want {
			t.Errorf("run(%v) = %q, want %q", args, got, want)
		}
	}
}

// Every command braids offers as a suggestion has to be one it answers to.
func TestKnownCommandsAllDispatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range known {
		err := run([]string{name, "--help"}, io.Discard)
		if err != nil && strings.Contains(err.Error(), "unknown command") {
			t.Errorf("%q is offered as a suggestion but does not dispatch", name)
		}
	}
}
