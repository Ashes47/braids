package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// commitAt makes a repository with one file and commits it at a chosen moment,
// so a test can put a conversation either side of that moment on purpose.
func repoWithCommit(t *testing.T, when time.Time, subject string) (repo, file string) {
	t.Helper()
	repo = t.TempDir()
	file = filepath.Join(repo, "service.go")
	if err := os.WriteFile(file, []byte("package service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := when.Format(time.RFC3339)
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "test"},
		{"add", "service.go"},
		{"commit", "--quiet", "-m", subject, "--date", stamp},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable or refused (%v): %s", err, out)
		}
	}
	return repo, file
}

// transcript writes one conversation whose session ran in cwd, with a turn at
// each moment given. A turn with text is a landmark; one with a tool call is
// not, which is the distinction explain has to respect.
func transcript(t *testing.T, root, id, title, cwd string, said map[time.Time]string) {
	t.Helper()
	dir := filepath.Join(root, "-"+strings.ReplaceAll(strings.TrimPrefix(cwd, "/"), "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{fmt.Sprintf(`{"type":"ai-title","aiTitle":%q,"sessionId":%q}`, title, id)}
	prev := ""
	i := 0
	for _, when := range sortedTimes(said) {
		i++
		uid := fmt.Sprintf("%s-%d", id, i)
		parent := "null"
		if prev != "" {
			parent = fmt.Sprintf("%q", prev)
		}
		text := said[when]
		if text == "" {
			// A tool call, which carries no preview.
			lines = append(lines, fmt.Sprintf(
				`{"type":"assistant","uuid":%q,"parentUuid":%s,"timestamp":%q,`+
					`"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash",`+
					`"input":{"command":"go build ./..."}}]}}`,
				uid, parent, when.UTC().Format(time.RFC3339)))
		} else {
			lines = append(lines, fmt.Sprintf(
				`{"type":"user","uuid":%q,"parentUuid":%s,"timestamp":%q,"cwd":%q,`+
					`"message":{"role":"user","content":%q}}`,
				uid, parent, when.UTC().Format(time.RFC3339), cwd, text))
		}
		prev = uid
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sortedTimes(m map[time.Time]string) []time.Time {
	out := make([]time.Time, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].Before(out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestExplainNamesTheConversationThatWasLive(t *testing.T) {
	commit := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	repo, file := repoWithCommit(t, commit, "narrow the lock")

	root := t.TempDir()
	transcript(t, root, "s1", "checkout stalls under load", repo, map[time.Time]string{
		commit.Add(-40 * time.Minute): "the lock is held across a network call",
		commit.Add(-20 * time.Minute): "", // a tool call, no text
		commit.Add(-5 * time.Minute):  "move the lock inside the tax client",
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	out := runCmd(t, "explain", "--db", db, file)
	if !strings.Contains(out, "checkout stalls under load") {
		t.Errorf("explain did not name the conversation:\n%s", out)
	}
	if !strings.Contains(out, "narrow the lock") {
		t.Errorf("explain did not name the commit:\n%s", out)
	}
	// The last thing said, not the tool call that came after it.
	if !strings.Contains(out, "move the lock inside the tax client") {
		t.Errorf("explain quoted something other than the last spoken turn:\n%s", out)
	}
	// And it must not claim the conversation caused the commit.
	if strings.Contains(strings.ToLower(out), "because") {
		t.Errorf("explain claimed a reason:\n%s", out)
	}
}

func TestExplainRespectsTheWindow(t *testing.T) {
	commit := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	repo, file := repoWithCommit(t, commit, "narrow the lock")

	root := t.TempDir()
	transcript(t, root, "s1", "long before", repo, map[time.Time]string{
		commit.Add(-10 * time.Hour): "this was a different day's work",
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	out := runCmd(t, "explain", "--db", db, file)
	if strings.Contains(out, "long before") {
		t.Errorf("a conversation ten hours earlier was counted as context:\n%s", out)
	}
	if !strings.Contains(out, "nothing was being written") {
		t.Errorf("explain did not say the window was empty:\n%s", out)
	}

	// Widened, it should reach.
	out = runCmd(t, "explain", "--db", db, "--window", "12h", file)
	if !strings.Contains(out, "long before") {
		t.Errorf("--window 12h did not reach it:\n%s", out)
	}
}

func TestExplainRanksTheBusierConversationFirst(t *testing.T) {
	commit := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	repo, file := repoWithCommit(t, commit, "narrow the lock")

	root := t.TempDir()
	busy := map[time.Time]string{}
	for i := 1; i <= 8; i++ {
		busy[commit.Add(-time.Duration(i)*time.Minute)] = fmt.Sprintf("working on it %d", i)
	}
	transcript(t, root, "s1", "the busy one", repo, busy)
	transcript(t, root, "s2", "the quiet one", repo, map[time.Time]string{
		commit.Add(-2 * time.Minute): "just passing through",
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	out := runCmd(t, "explain", "--db", db, file)
	busyAt := strings.Index(out, "the busy one")
	quietAt := strings.Index(out, "the quiet one")
	if busyAt < 0 || quietAt < 0 {
		t.Fatalf("both conversations should appear:\n%s", out)
	}
	if busyAt > quietAt {
		t.Errorf("the quieter conversation was ranked first:\n%s", out)
	}
}

func TestExplainSaysWhenItKnowsNothing(t *testing.T) {
	commit := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	_, file := repoWithCommit(t, commit, "narrow the lock")

	root := t.TempDir()
	transcript(t, root, "s1", "somewhere else", "/some/other/place", map[time.Time]string{
		commit.Add(-5 * time.Minute): "unrelated work",
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	out := runCmd(t, "explain", "--db", db, file)
	if !strings.Contains(out, "no conversations") {
		t.Errorf("explain should say it has nothing, got:\n%s", out)
	}
	if strings.Contains(out, "somewhere else") {
		t.Errorf("a conversation from another directory was offered:\n%s", out)
	}
}

func TestExplainRefusesAPathOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "loose.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", fixtureRoot(t), "--db", db)

	err := run([]string{"explain", "--db", db, file}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Errorf("explain outside a repo said: %v", err)
	}
}

func TestExplainJSONCarriesTheEvidence(t *testing.T) {
	commit := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	repo, file := repoWithCommit(t, commit, "narrow the lock")

	root := t.TempDir()
	// A real session id, because the promise json makes is that it comes back
	// whole: a caller handed a shortened one has been handed nothing.
	const id = "a1b2c3d4-0000-4000-8000-000000000001"
	transcript(t, root, id, "checkout stalls", repo, map[time.Time]string{
		commit.Add(-5 * time.Minute): "move the lock inside the tax client",
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", root, "--db", db)

	var got struct {
		File    string `json:"file"`
		Commits []struct {
			Commit        string `json:"commit"`
			Conversations []struct {
				Lane    string `json:"lane"`
				Turns   int    `json:"turns_in_window"`
				Preview string `json:"preview"`
				Turn    int    `json:"turn"`
			} `json:"conversations"`
		} `json:"commits"`
	}
	if err := json.Unmarshal([]byte(runCmd(t, "explain", "--db", db, "--json", file)), &got); err != nil {
		t.Fatalf("parse explain json: %v", err)
	}
	if len(got.Commits) != 1 || len(got.Commits[0].Conversations) != 1 {
		t.Fatalf("json shape: %+v", got)
	}
	c := got.Commits[0].Conversations[0]
	if c.Lane != id {
		t.Errorf("lane came back as %q, want the whole id %q", c.Lane, id)
	}
	if c.Turns != 1 || c.Turn == 0 || c.Preview == "" {
		t.Errorf("evidence missing from json: %+v", c)
	}
}

func TestExplainHelpers(t *testing.T) {
	if got := idPrefix("13afa894-df23-463c"); got != "13afa894" {
		t.Errorf("idPrefix = %q, want an id a command can use", got)
	}
	if got := idPrefix("short"); got != "short" {
		t.Errorf("idPrefix truncated a short id to %q", got)
	}
	if got := collapse("two\n  lines   here"); got != "two lines here" {
		t.Errorf("collapse = %q", got)
	}
	for d, want := range map[time.Duration]string{
		30 * time.Second: "under a minute before",
		90 * time.Second: "a minute before",
		20 * time.Minute: "20 minutes before",
		90 * time.Minute: "an hour before",
		5 * time.Hour:    "5.0 hours before",
	} {
		if got := humanGap(d); got != want {
			t.Errorf("humanGap(%v) = %q, want %q", d, got, want)
		}
	}
}
