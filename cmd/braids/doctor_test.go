package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// doctorHome builds a fixture home with the transcripts given, as
// session id -> the text of its one user turn. An empty text writes a file
// holding only session state, which is a real thing Claude Code leaves behind
// and a thing braids never indexes.
func doctorHome(t *testing.T, said map[string]string) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	projects := filepath.Join(home, ".claude", "projects", "-p")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	for session, text := range said {
		var body string
		if text == "" {
			body = `{"type":"ai-title","aiTitle":"state only","sessionId":"` + session + `"}` + "\n" +
				`{"type":"cost-state","sessionId":"` + session + `","totalCostUSD":0.26}` + "\n"
		} else {
			body = `{"type":"ai-title","aiTitle":"a conversation","sessionId":"` + session + `"}` + "\n" +
				`{"type":"user","uuid":"u-` + session[:8] + `","parentUuid":null,"timestamp":"` +
				at.Format(time.RFC3339) + `","cwd":"/tmp/x","message":{"role":"user","content":"` +
				text + `"}}` + "\n"
		}
		if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		// Backdate it, or it looks like a transcript being written right now.
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(filepath.Join(projects, session+".jsonl"), old, old); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func doctorJSON(t *testing.T, db string) map[string]checkup {
	t.Helper()
	var got struct {
		Checks  []checkup `json:"checks"`
		Healthy bool      `json:"healthy"`
		Notes   int       `json:"notes"`
	}
	raw := runCmd(t, "doctor", "--db", db, "--json")
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, raw)
	}
	by := map[string]checkup{}
	for _, c := range got.Checks {
		by[c.Name] = c
	}
	return by
}

// With no index at all there is nothing to be right about, and the one thing
// worth saying is how to get one.
func TestDoctorSaysWhenThereIsNoIndex(t *testing.T) {
	doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000d1": "hello"})
	db := filepath.Join(t.TempDir(), "index.db")

	checks := doctorJSON(t, db)
	if c := checks["index"]; c.OK || !strings.Contains(c.Fix, "braids index") {
		t.Errorf("with no index, the index check said %+v", c)
	}
	// And the checks that do not need an index still answer.
	if _, ok := checks["build"]; !ok {
		t.Error("a missing index stopped the other checks from running")
	}
}

// A transcript written after the last index is the thing the check exists for.
func TestDoctorNoticesAStaleIndex(t *testing.T) {
	home := doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000d2": "first"})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	if c := doctorJSON(t, db)["index"]; !c.OK {
		t.Fatalf("a freshly indexed history was reported as stale: %+v", c)
	}

	// A second conversation appears while braids is not looking.
	projects := filepath.Join(home, ".claude", "projects", "-p")
	body := `{"type":"user","uuid":"u-later","parentUuid":null,"timestamp":"2026-08-21T11:00:00Z",` +
		`"cwd":"/tmp/x","message":{"role":"user","content":"written later"}}` + "\n"
	path := filepath.Join(projects, "a1b2c3d4-0000-4000-8000-0000000000d3.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	c := doctorJSON(t, db)["index"]
	if c.OK || !strings.Contains(c.Detail, "changed since the last index") {
		t.Errorf("a conversation written after the index did not show: %+v", c)
	}
}

// The bug this check shipped with. A file holding only session state is one
// Sync skips on purpose, so counting it as unindexed reports an index that can
// never be made current: indexing it will never store it, and the note never
// goes away. Found by running doctor against a real machine, where it insisted
// one conversation had changed however many times the index was rebuilt.
func TestDoctorDoesNotCallSessionStateAStaleIndex(t *testing.T) {
	doctorHome(t, map[string]string{
		"a1b2c3d4-0000-4000-8000-0000000000d4": "a real conversation",
		"a1b2c3d4-0000-4000-8000-0000000000d5": "", // state only, never indexed
	})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	c := doctorJSON(t, db)["index"]
	if !c.OK {
		t.Errorf("session state was counted as an index that has fallen behind: %+v", c)
	}
	if !strings.Contains(c.Detail, "1 conversation") {
		t.Errorf("the count included the file that is not a conversation: %q", c.Detail)
	}
}

// A transcript being written right now is the session running the check. It
// must not read as an index nobody has run.
func TestDoctorTreatsALiveWriteAsLive(t *testing.T) {
	home := doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000d6": "first"})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	// Touch it now, as a session writing a turn would.
	path := filepath.Join(home, ".claude", "projects", "-p",
		"a1b2c3d4-0000-4000-8000-0000000000d6.jsonl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	more := string(body) + `{"type":"user","uuid":"u-now","parentUuid":"u-a1b2c3d4",` +
		`"timestamp":"2026-08-21T10:05:00Z","cwd":"/tmp/x",` +
		`"message":{"role":"user","content":"still typing"}}` + "\n"
	if err := os.WriteFile(path, []byte(more), 0o600); err != nil {
		t.Fatal(err)
	}

	c := doctorJSON(t, db)["index"]
	if !c.OK {
		t.Errorf("a transcript being written now was reported as a stale index: %+v", c)
	}
	if !strings.Contains(c.Detail, "written right now") {
		t.Errorf("it did not say why it was letting it pass: %q", c.Detail)
	}
}

// Notes are not failures. An agent is told a non-zero exit is a real failure,
// so a doctor that exits 1 because a hook is missing would be lying to it.
func TestDoctorExitsZeroWithNotes(t *testing.T) {
	doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000d7": "hello"})
	db := filepath.Join(t.TempDir(), "index.db")

	if err := run([]string{"doctor", "--db", db}, os.Stdout); err != nil {
		t.Errorf("doctor with notes to report returned an error: %v", err)
	}
}

// writeHooks puts a braids hook on every event named, in a file shaped like
// the hooks block of a settings file. A plugin's hooks.json has that shape
// too, which is why one helper writes both.
func writeHooks(t *testing.T, path, command string, events ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	byEvent := map[string]any{}
	for _, event := range events {
		byEvent[event] = []any{map[string]any{
			"matcher": "*",
			"hooks":   []any{map[string]any{"type": "command", "command": command}},
		}}
	}
	body, err := json.Marshal(map[string]any{"hooks": byEvent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// installPlugin records a plugin as installed and returns its root, the way
// Claude Code lays one out.
func installPlugin(t *testing.T, home, name string) string {
	t.Helper()
	root := filepath.Join(home, ".claude", "plugins", "cache", name, name, "abc123")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"version": 2,
		"plugins": map[string]any{
			name + "@" + name: []any{map[string]any{
				"scope":       "user",
				"installPath": root,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// The case this was written for. Installing the plugin on a machine that had
// already run `braids hooks --install` leaves two hooks on every event, and
// Claude Code runs both: the events log grows at twice the rate for good.
// Nothing else can see it -- waiting states are read from the newest event for
// a session, so a duplicate is the same answer -- which is exactly why doctor
// has to.
func TestDoctorNoticesHooksInstalledTwice(t *testing.T) {
	home := doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e1": "hello"})
	writeHooks(t, filepath.Join(home, ".claude", "settings.json"),
		"/usr/local/bin/braids hook", "Stop", "SessionStart")
	root := installPlugin(t, home, "braids")
	writeHooks(t, filepath.Join(root, "hooks", "hooks.json"),
		"braids hook", "Stop", "SessionStart")

	got := doctorJSON(t, filepath.Join(t.TempDir(), "index.db"))["hook"]
	if got.OK {
		t.Fatalf("hooks installed twice reported ok: %+v", got)
	}
	if !strings.Contains(got.Detail, "twice") {
		t.Errorf("detail does not say it is installed twice: %q", got.Detail)
	}
	if !strings.Contains(got.Detail, "2 events") {
		t.Errorf("detail does not count the doubled events: %q", got.Detail)
	}
	if got.Fix != "braids hooks --remove, and keep the plugin" {
		t.Errorf("fix = %q, which does not remove the copy that is safe to remove", got.Fix)
	}
}

// The plugin alone is a complete installation and must not be nagged about.
// It registers the same events `braids hooks --install` would, so a doctor
// that only read the settings file called this "not installed" and sent
// somebody to create the duplicate above.
func TestDoctorAcceptsHooksFromThePluginAlone(t *testing.T) {
	home := doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e2": "hello"})
	root := installPlugin(t, home, "braids")
	writeHooks(t, filepath.Join(root, "hooks", "hooks.json"),
		"braids hook", "Stop", "SessionStart", "SessionEnd")

	got := doctorJSON(t, filepath.Join(t.TempDir(), "index.db"))["hook"]
	if !got.OK {
		t.Fatalf("the plugin's own hooks reported as a problem: %+v", got)
	}
	if !strings.Contains(got.Detail, "plugin") {
		t.Errorf("detail does not say where they came from: %q", got.Detail)
	}
}

// A plugin that is not braids registers hooks too. Reading every installed
// plugin's hooks file and counting whatever is in it would call any of them a
// duplicate braids.
func TestDoctorIgnoresHooksBelongingToAnotherPlugin(t *testing.T) {
	home := doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e3": "hello"})
	writeHooks(t, filepath.Join(home, ".claude", "settings.json"),
		"/usr/local/bin/braids hook", "Stop")
	root := installPlugin(t, home, "somethingelse")
	writeHooks(t, filepath.Join(root, "hooks", "hooks.json"),
		"somethingelse hook", "Stop")

	got := doctorJSON(t, filepath.Join(t.TempDir(), "index.db"))["hook"]
	if strings.Contains(got.Detail, "twice") {
		t.Fatalf("another plugin's hooks counted as braids': %+v", got)
	}
	if strings.Contains(got.Fix, "--remove") {
		t.Fatalf("offered to remove hooks that are not duplicated: %+v", got)
	}
}

// An uninstalled plugin leaves its files in the cache. Walking the cache
// rather than the record of what is installed reports a duplicate that is not
// there, and the fix it offers removes hooks the machine needs.
func TestDoctorIgnoresAnUninstalledPluginLeftInTheCache(t *testing.T) {
	home := doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e4": "hello"})
	writeHooks(t, filepath.Join(home, ".claude", "settings.json"),
		"/usr/local/bin/braids hook", "Stop")
	stale := filepath.Join(home, ".claude", "plugins", "cache", "braids", "braids", "old")
	writeHooks(t, filepath.Join(stale, "hooks", "hooks.json"), "braids hook", "Stop")
	// Installed: nothing.
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"plugins":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := doctorJSON(t, filepath.Join(t.TempDir(), "index.db"))["hook"]
	if strings.Contains(got.Detail, "twice") {
		t.Fatalf("a cached, uninstalled plugin counted as installed: %+v", got)
	}
	if strings.Contains(got.Fix, "--remove") {
		t.Fatalf("offered to remove the hooks this machine is relying on: %+v", got)
	}
}
