package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// briefHome writes conversations in two projects, each with its own working
// directory, so the scoping has something to get wrong.
func briefHome(t *testing.T, lanes map[string][2]string) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	at := time.Now().Add(-2 * time.Hour)
	for session, pair := range lanes {
		project, cwd := pair[0], pair[1]
		dir := filepath.Join(home, ".claude", "projects", "-"+project)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// A Windows path is full of backslashes, and a backslash in a JSON
		// string is an escape. Interpolating one raw makes the record
		// unparseable, nothing indexes, and the failure arrives as an empty
		// brief rather than as a broken fixture.
		where, err := json.Marshal(cwd)
		if err != nil {
			t.Fatal(err)
		}
		body := `{"type":"ai-title","aiTitle":"work in ` + project + `","sessionId":"` + session + `"}` + "\n" +
			`{"type":"user","uuid":"u-` + session[:8] + `","parentUuid":null,"timestamp":"` +
			at.Format(time.RFC3339) + `","cwd":` + string(where) + `,"message":{"role":"user",` +
			`"content":"the thing I was doing in ` + project + `"}}` + "\n"
		path := filepath.Join(dir, session+".jsonl")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		// A lane's age is its file's modification time, not the timestamps
		// inside it, so a fixture written now looks like a conversation from
		// this second however old its turns claim to be.
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// The question a brief answers is "what is going on here", so here has to
// mean the directory somebody is standing in and not everything braids knows.
func TestBriefScopesToWhereYouAre(t *testing.T) {
	work := t.TempDir()
	alpha, beta := filepath.Join(work, "alpha"), filepath.Join(work, "beta")
	for _, d := range []string{alpha, beta} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	briefHome(t, map[string][2]string{
		"a1b2c3d4-0000-4000-8000-00000000b001": {"alpha", alpha},
		"a1b2c3d4-0000-4000-8000-00000000b002": {"beta", beta},
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	t.Chdir(alpha)
	out := runCmd(t, "brief", "--db", db)
	if !strings.Contains(out, "alpha") {
		t.Errorf("standing in alpha, the brief did not mention it:\n%s", out)
	}
	if strings.Contains(out, "work in beta") {
		t.Errorf("standing in alpha, the brief included beta:\n%s", out)
	}
}

// And naming a project wins over where you happen to be.
func TestBriefTakesAProjectByName(t *testing.T) {
	work := t.TempDir()
	alpha := filepath.Join(work, "alpha")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	briefHome(t, map[string][2]string{
		"a1b2c3d4-0000-4000-8000-00000000b003": {"alpha", alpha},
		"a1b2c3d4-0000-4000-8000-00000000b004": {"beta", filepath.Join(work, "beta")},
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	t.Chdir(alpha)
	out := runCmd(t, "brief", "--db", db, "--project", "beta")
	if !strings.Contains(out, "work in beta") {
		t.Errorf("--project beta did not show beta:\n%s", out)
	}
	if strings.Contains(out, "work in alpha") {
		t.Errorf("--project beta showed alpha too:\n%s", out)
	}
}

// A brief is a glance, so it is bounded by a window and by a count.
func TestBriefIsBounded(t *testing.T) {
	work := t.TempDir()
	briefHome(t, map[string][2]string{
		"a1b2c3d4-0000-4000-8000-00000000b005": {"alpha", work},
		"a1b2c3d4-0000-4000-8000-00000000b006": {"alpha", work},
		"a1b2c3d4-0000-4000-8000-00000000b007": {"alpha", work},
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	var got struct {
		Conversations int `json:"conversations"`
		Recent        []struct {
			Title string `json:"title"`
		} `json:"recent"`
	}
	raw := runCmd(t, "brief", "--db", db, "--project", "alpha", "--limit", "2", "--json")
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Conversations != 3 {
		t.Errorf("counted %d conversations, want all 3", got.Conversations)
	}
	if len(got.Recent) != 2 {
		t.Errorf("--limit 2 described %d of them", len(got.Recent))
	}

	// The window leaves out what is older than it.
	raw = runCmd(t, "brief", "--db", db, "--project", "alpha", "--since", "1m", "--json")
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Conversations != 0 {
		t.Errorf("a one minute window found %d two-hour-old conversations", got.Conversations)
	}
}

// Nothing to report is an answer, not an empty screen.
func TestBriefSaysWhenThereIsNothing(t *testing.T) {
	briefHome(t, map[string][2]string{
		"a1b2c3d4-0000-4000-8000-00000000b008": {"alpha", t.TempDir()},
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	out := runCmd(t, "brief", "--db", db, "--project", "alpha", "--since", "1m")
	if !strings.Contains(out, "nothing in the last") {
		t.Errorf("an empty window said:\n%s", out)
	}
}
