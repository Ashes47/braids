package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/brand"
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
			`"message":{"role":"user","content":"the blobstore mount is hard-coded to ten"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-08-21T11:05:00Z",` +
			`"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"mount | grep blobstore"}}]}}`,
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

	out = runCmd(t, "search", "--db", db, "blobstore")
	if !strings.Contains(out, "[blobstore]") {
		t.Errorf("expected a highlighted snippet, got %q", out)
	}
	if !strings.Contains(out, "mount density") {
		t.Errorf("expected the lane title in the row, got %q", out)
	}
	if !strings.Contains(out, "2 hits") {
		t.Errorf("expected both the text and the tool call to match, got %q", out)
	}

	out = runCmd(t, "search", "--db", db, "--kind", "tool_use", "blobstore")
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
	after := runCmd(t, "search", "--db", db, "blobstore", "--limit", "1")
	if !strings.Contains(after, "1 hits") {
		t.Errorf("trailing --limit ignored: %q", after)
	}
	before := runCmd(t, "search", "--db", db, "--limit", "1", "blobstore")
	if !strings.Contains(before, "1 hits") {
		t.Errorf("leading --limit ignored: %q", before)
	}
	// A multi-word query still reads as one query, not as stray flags.
	multi := runCmd(t, "search", "--db", db, "blobstore", "mount", "--limit", "5")
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

// The JSON surface is what makes braids usable by the thing it watches, so its
// shape is pinned: whole IDs, and an empty result that is still a list.
func TestJSONIsMachineReadable(t *testing.T) {
	root := fixtureRoot(t)
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	var listed struct {
		Lanes []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Messages int    `json:"messages"`
			Resume   string `json:"resume"`
		} `json:"lanes"`
	}
	decode(t, runCmd(t, "lanes", "--db", db, "--json"), &listed)
	if len(listed.Lanes) == 0 {
		t.Fatal("no lanes in JSON output")
	}
	lane := listed.Lanes[0]
	if strings.Contains(lane.ID, "…") {
		t.Errorf("id = %q, want the whole thing, not a shortened one", lane.ID)
	}
	if !strings.Contains(lane.Resume, lane.ID) {
		t.Errorf("resume = %q, want it to carry the whole id", lane.Resume)
	}

	// The ID that came out has to be one that goes back in.
	var agents struct {
		Lane   string `json:"lane"`
		Agents []any  `json:"agents"`
	}
	decode(t, runCmd(t, "agents", "--lane", lane.ID, "--db", db, "--json"), &agents)
	if agents.Lane != lane.ID {
		t.Errorf("agents lane = %q, want %q", agents.Lane, lane.ID)
	}
	if agents.Agents == nil {
		t.Error("agents = null, want [] so a caller can iterate it unguarded")
	}

	var found struct {
		Hits []struct {
			Lane string `json:"lane"`
			Turn int    `json:"turn"`
		} `json:"hits"`
		Count int `json:"count"`
	}
	decode(t, runCmd(t, "search", "--db", db, "--json", "nothing-matches-this-xyzzy"), &found)
	if found.Count != 0 || found.Hits == nil {
		t.Errorf("empty search = %+v, want zero hits as an empty list", found)
	}
}

// An ID copied off a table arrives with the ellipsis that made it fit. Braids
// printed it, so braids accepts it.
func TestShortenedIDPastedBackResolves(t *testing.T) {
	root := fixtureRoot(t)
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	// Whatever the table showed, an ID wearing the ellipsis that made it fit
	// has to resolve to the conversation it names.
	var listed struct {
		Lanes []struct {
			ID string `json:"id"`
		} `json:"lanes"`
	}
	decode(t, runCmd(t, "lanes", "--db", db, "--json"), &listed)
	if len(listed.Lanes) == 0 {
		t.Fatal("nothing indexed")
	}
	pasted := listed.Lanes[0].ID + "…"
	if out := runCmd(t, "agents", "--lane", pasted, "--db", db); strings.Contains(out, "no conversation") {
		t.Errorf("pasting back %q was refused: %s", pasted, out)
	}
}

func decode(t *testing.T, body string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, body)
	}
}

// The mark is coloured on a terminal and plain in a pipe: escape codes in a
// pipe are noise in somebody's grep.
func TestMarkIsPlainWhenPiped(t *testing.T) {
	restore := stdoutIsTerminal
	t.Cleanup(func() { stdoutIsTerminal = restore })

	stdoutIsTerminal = func() bool { return false }
	plain := runCmd(t, "help")
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("piped help carries escape codes:\n%q", plain[:120])
	}
	for _, want := range []string{"|___  /", brand.Tagline, "manage Claude Code conversations"} {
		if !strings.Contains(plain, want) {
			t.Errorf("help is missing %q", want)
		}
	}

	stdoutIsTerminal = func() bool { return true }
	if coloured := runCmd(t, "help"); !strings.Contains(coloured, "\x1b[38;2;240;136;62m") {
		t.Error("help on a terminal is not wearing the accent")
	}
}

// version carries the smaller mark, so it stays readable in a narrow window.
func TestVersionCarriesTheSmallMark(t *testing.T) {
	restore := stdoutIsTerminal
	t.Cleanup(func() { stdoutIsTerminal = restore })
	stdoutIsTerminal = func() bool { return false }

	out := runCmd(t, "version")
	if !strings.Contains(out, strings.TrimRight(brand.Small()[len(brand.Small())-1], " ")) {
		t.Errorf("version is not showing the small mark:\n%s", out)
	}
	if strings.Contains(out, brand.Full()[1]) {
		t.Error("version is showing the full mark, which is wider than it needs")
	}
}

// Only `braids index` may create an index. A read that quietly creates an empty
// one answers a mistyped --db with "nothing found" — a wrong answer shaped like
// a right one — and leaves a database behind where the typo pointed.
func TestOnlyIndexCreatesTheIndex(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.db")

	for _, args := range [][]string{
		{"search", "--db", missing, "anything"},
		{"lanes", "--db", missing},
		{"agents", "--lane", "x", "--db", missing},
		{"map", "--print", "--db", missing},
		// A map flag with no command in front of it is still the map. If
		// dispatch read it as a command name this would fail with "unknown
		// command" instead, which is what `braids --print` used to answer.
		{"--print", "--db", missing},
	} {
		err := run(args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "no index at") {
			t.Errorf("run(%v) = %v, want it to refuse a missing index", args, err)
		}
		if _, statErr := os.Stat(missing); statErr == nil {
			t.Fatalf("run(%v) created %s", args, missing)
		}
	}

	// And the one command that is allowed to make it, does.
	runCmd(t, "index", "--root", fixtureRoot(t), "--db", missing)
	if _, err := os.Stat(missing); err != nil {
		t.Fatalf("index did not create the database: %v", err)
	}
	if out := runCmd(t, "lanes", "--db", missing); !strings.Contains(out, "mount density") {
		t.Errorf("lanes after index = %q", out)
	}
}

// Everything under ~/.braids is conversation data. MkdirAll leaves an existing
// directory's mode alone, so one made by an older build has to be tightened on
// the way past rather than only at creation.
func TestBraidsDirectoryIsPrivate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BRAIDS_DB", "")

	dir := filepath.Join(home, ".braids")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := defaultDB(); err != nil {
		t.Fatalf("defaultDB: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("~/.braids is mode %o, reachable beyond its owner", perm)
	}
}

// The work command browses one level at a time and will not be walked out of
// the tree the caller asked about.
func TestWorkBrowsesAndStaysInside(t *testing.T) {
	root := t.TempDir()
	// Job directories are named by the first eight characters of the session
	// ID, so the fixture uses an ID shaped like a real one.
	const session = "a1b2c3d4-0000-4000-8000-000000000001"
	job := filepath.Join(root, "jobs", session[:8])
	for rel, size := range map[string]int{
		"tmp/huge.json": 4000, "tmp/small.txt": 20, "state.json": 30,
	} {
		path := filepath.Join(job, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"ai-title","aiTitle":"work","sessionId":"` + session + `"}` + "\n" +
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-09-01T10:00:00Z",` +
		`"message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", filepath.Join(root, "projects"), "--db", db)

	if out := runCmd(t, "work", "--lane", session[:8], "--db", db); !strings.Contains(out, "tmp/") {
		t.Errorf("work = %q, want it to list tmp", out)
	}
	// Descending, and the reserved file named for what it is.
	deeper := runCmd(t, "work", "--lane", session[:8], "--path", "tmp", "--db", db)
	if !strings.Contains(deeper, "huge.json") {
		t.Errorf("work --path tmp = %q", deeper)
	}
	if top := runCmd(t, "work", "--lane", session[:8], "--db", db); !strings.Contains(top, "harness") {
		t.Errorf("state.json is not marked as the harness's own: %q", top)
	}

	// A path from a command line must not walk out of the job directory.
	if err := run([]string{"work", "--lane", session[:8], "--path", "../../..", "--db", db}, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "outside") {
		t.Errorf("traversal = %v, want a refusal", err)
	}
	if err := run([]string{"work", "--db", db}, io.Discard); err == nil {
		t.Error("work with neither --lane nor --orphans was accepted")
	}
}

// The memories command reports what is remembered and, more usefully, where
// the index and the files have drifted apart.
func TestMemoriesReportsWhatTheIndexOmits(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-Users-me-src-alpha", "memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("listed.md", "---\nname: listed\ndescription: in the index\nmetadata:\n  type: project\n---\n\nsee [[nowhere]]\n")
	write("hidden.md", "---\nname: hidden\ndescription: not in the index\nmetadata:\n  type: feedback\n---\n\nbody\n")
	write("MEMORY.md", "# Memory index\n\n- [Listed](listed.md) — in the index\n- [Gone](gone.md) — no file\n")

	t.Setenv("HOME", root)

	// DefaultRoot resolves under HOME, so lay the tree out the way it expects.
	claudeProjects := filepath.Join(root, ".claude", "projects", "-Users-me-src-alpha", "memory")
	if err := os.MkdirAll(claudeProjects, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"listed.md", "hidden.md", "MEMORY.md"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(claudeProjects, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out := runCmd(t, "memories")
	for _, want := range []string{
		"listed", "hidden",
		"hidden is not in MEMORY.md, so nothing ever loads it",
		"MEMORY.md names gone, which is not there",
		"listed is waiting on [[nowhere]]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("memories output is missing %q:\n%s", want, out)
		}
	}

	// The JSON surface keeps the same three findings apart.
	var got struct {
		Projects []struct {
			Project  string   `json:"project"`
			Unlisted []string `json:"unlisted"`
			Orphaned []string `json:"orphaned"`
			Dangling []struct {
				From, To string
			} `json:"dangling"`
			Memories []struct {
				Name   string `json:"name"`
				Listed bool   `json:"listed"`
			} `json:"memories"`
		} `json:"projects"`
	}
	decode(t, runCmd(t, "memories", "--json"), &got)
	if len(got.Projects) != 1 {
		t.Fatalf("json reported %d projects", len(got.Projects))
	}
	p := got.Projects[0]
	if len(p.Unlisted) != 1 || p.Unlisted[0] != "hidden" {
		t.Errorf("unlisted = %v", p.Unlisted)
	}
	if len(p.Orphaned) != 1 || p.Orphaned[0] != "gone" {
		t.Errorf("orphaned = %v", p.Orphaned)
	}
	if len(p.Dangling) != 1 || p.Dangling[0].To != "nowhere" {
		t.Errorf("dangling = %+v", p.Dangling)
	}
	if len(p.Memories) != 2 {
		t.Errorf("memories = %+v", p.Memories)
	}
}

// A memory with no recorded origin was not written by an unnamed conversation:
// no conversation was recorded at all, and saying "(unnamed)" claims one.
func TestMemoriesMarksAnAbsentOriginAsNothing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-p", "memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("with-origin.md", "---\nname: with-origin\ndescription: d\nmetadata:\n  type: project\n"+
		"  originSessionId: abc12345-0000-4000-8000-000000000001\n---\n\nbody\n")
	write("no-origin.md", "---\nname: no-origin\ndescription: d\nmetadata:\n  type: feedback\n---\n\nbody\n")
	write("MEMORY.md", "# Memory index\n\n- [With](with-origin.md) — d\n- [Without](no-origin.md) — d\n")

	out := runCmd(t, "memories", "--root", root)
	if strings.Contains(out, "(unnamed)") {
		t.Errorf("an absent origin is reported as an unnamed conversation:\n%s", out)
	}
	if !strings.Contains(out, "abc1234") {
		t.Errorf("a recorded origin is missing:\n%s", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("an absent origin is not marked as absent:\n%s", out)
	}
	// --root also makes the command reachable anywhere, like index and work.
	if !strings.Contains(out, "no-origin") || !strings.Contains(out, "with-origin") {
		t.Errorf("--root did not read the set:\n%s", out)
	}
}

// A date or an age, because both are what people type at a terminal. A bare
// date means the whole of that day, so the same date on both bounds is that
// day rather than an empty range.
func TestParseWhenReadsDatesAndAges(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		in       string
		endOfDay bool
		want     time.Time
	}{
		{"2026-08-01", false, time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)},
		{"2026-08-01", true, time.Date(2026, 8, 1, 23, 59, 59, 0, time.Local)},
		{"30d", false, now.AddDate(0, 0, -30)},
		{"6w", false, now.Add(-6 * 7 * 24 * time.Hour)},
		{"12h", false, now.Add(-12 * time.Hour)},
		{"45m", false, now.Add(-45 * time.Minute)},
		{"", false, time.Time{}},
	} {
		got, err := parseWhen(c.in, now, c.endOfDay)
		if err != nil {
			t.Errorf("parseWhen(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseWhen(%q, endOfDay=%v) = %v, want %v", c.in, c.endOfDay, got, c.want)
		}
	}
	for _, bad := range []string{"yesterday", "last tuesday", "5", "3y", "-2d", "d"} {
		if _, err := parseWhen(bad, now, false); err == nil {
			t.Errorf("parseWhen(%q) was accepted", bad)
		}
	}
}

// The two new filters have to actually narrow, or they are decoration.
func TestSearchNarrowsByProjectAndDate(t *testing.T) {
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", fixtureRoot(t), "--db", db)

	all := runCmd(t, "search", "--db", db, "--limit", "50", "--json", "mount")
	var everything struct {
		Hits []struct {
			Project string `json:"project"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(all), &everything); err != nil {
		t.Fatalf("parse search json: %v", err)
	}
	if len(everything.Hits) == 0 {
		t.Fatal("the fixture matched nothing, so the test proves nothing")
	}
	project := everything.Hits[0].Project

	narrowed := runCmd(t, "search", "--db", db, "--project", project,
		"--limit", "50", "--json", "mount")
	var scoped struct {
		Hits []struct {
			Project string `json:"project"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(narrowed), &scoped); err != nil {
		t.Fatalf("parse scoped json: %v", err)
	}
	for _, h := range scoped.Hits {
		if h.Project != "" && !strings.EqualFold(h.Project, project) {
			t.Errorf("--project %s let %q through", project, h.Project)
		}
	}

	// Nothing in a fixture was written in 1999.
	empty := runCmd(t, "search", "--db", db, "--until", "1999-01-01",
		"--limit", "50", "--json", "mount")
	var none struct {
		Hits []struct{} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(empty), &none); err != nil {
		t.Fatalf("parse empty json: %v", err)
	}
	if len(none.Hits) != 0 {
		t.Errorf("--until 1999 returned %d hits", len(none.Hits))
	}
}
