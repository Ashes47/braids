package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Nothing touches the index while braids is closed. A conversation written
// while it was shut therefore did not appear when it was opened again: the map
// drew whatever the index happened to hold, and only caught up when something
// changed on disk while somebody was watching. Which for a project you are not
// working in that day is never.
func TestOpeningTheMapCatchesUpFirst(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	projects := filepath.Join(home, ".claude", "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(session, title, said string, when time.Time) {
		t.Helper()
		body := `{"type":"ai-title","aiTitle":"` + title + `","sessionId":"` + session + `"}` + "\n" +
			`{"type":"user","uuid":"u-` + session[:8] + `","parentUuid":null,"timestamp":"` +
			when.Format(time.RFC3339) + `","cwd":"/tmp/x","message":{"role":"user","content":"` +
			said + `"}}` + "\n"
		if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	const first = "a1b2c3d4-0000-4000-8000-00000000ee01"
	const later = "a1b2c3d4-0000-4000-8000-00000000ee02"

	write(first, "the one that was indexed", "hello", at)
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	// Now a conversation happens while braids is not running. Nothing indexes
	// it, because nothing is watching.
	write(later, "written while braids was closed", "the second one", at.Add(time.Hour))

	// Opening the map has to show it.
	out := runCmd(t, "--print", "--db", db, "--width", "200", "--height", "40")
	if !strings.Contains(out, "written while braids was closed") {
		t.Errorf("opening the map did not pick up a conversation written while it was shut:\n%s", out)
	}
	if !strings.Contains(out, "the one that was indexed") {
		t.Errorf("opening the map lost what was already indexed:\n%s", out)
	}
}
