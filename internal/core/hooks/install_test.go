package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// existing is a settings file with hooks already in it, plus configuration
// braids knows nothing about.
const existing = `{
  "model": "opus",
  "permissions": {"allow": ["Bash(git:*)"]},
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "/theirs/telemetry.sh"}]}],
    "PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "/theirs/telemetry.sh"}]}]
  }
}`

func settingsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, body)
	}
	return out
}

func TestInstallLeavesEverythingElseAlone(t *testing.T) {
	path := settingsFile(t, existing)
	added, err := Install(path, "/bin/braids hook")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(added) != len(Wanted()) {
		t.Errorf("added %v, want every event braids asks for", added)
	}

	after := read(t, path)
	// Configuration braids does not understand must survive untouched.
	if after["model"] != "opus" {
		t.Errorf("model = %v, want it carried through", after["model"])
	}
	if _, ok := after["permissions"]; !ok {
		t.Error("permissions were dropped")
	}

	// And so must hooks that were already there.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n := strings.Count(string(body), "/theirs/telemetry.sh"); n != 2 {
		t.Errorf("existing hooks appear %d times, want both kept", n)
	}
	// SessionStart had one already; ours is added beside it, not over it.
	if on, err := Installed(path, "/bin/braids hook"); err != nil || len(on) != len(Wanted()) {
		t.Errorf("Installed = %v, %v", on, err)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	path := settingsFile(t, existing)
	if _, err := Install(path, "/bin/braids hook"); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	added, err := Install(path, "/bin/braids hook")
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("second install changed %v, want nothing", added)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if string(first) != string(second) {
		t.Error("a second install rewrote the file")
	}
}

func TestRemoveTakesBackOnlyWhatBraidsAdded(t *testing.T) {
	path := settingsFile(t, existing)
	if _, err := Install(path, "/bin/braids hook"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	removed, err := Remove(path, "/bin/braids hook")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(removed) != len(Wanted()) {
		t.Errorf("removed %v, want every event", removed)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "braids hook") {
		t.Error("braids' hook survived removal")
	}
	if n := strings.Count(string(body), "/theirs/telemetry.sh"); n != 2 {
		t.Errorf("removal took %d of the user's hooks with it", 2-n)
	}
	after := read(t, path)
	if after["model"] != "opus" {
		t.Error("removal disturbed configuration it does not own")
	}
}

func TestInstallRefusesToOverwriteWhatItCannotRead(t *testing.T) {
	path := settingsFile(t, `{"model": "opus", oops`)
	if _, err := Install(path, "/bin/braids hook"); err == nil {
		t.Fatal("a settings file that cannot be parsed must not be replaced by a guess")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "oops") {
		t.Error("the unreadable file was modified")
	}
}

func TestInstallKeepsABackup(t *testing.T) {
	path := settingsFile(t, existing)
	if _, err := Install(path, "/bin/braids hook"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".braids-") && strings.HasSuffix(e.Name(), ".bak") {
			found = true
			body, err := os.ReadFile(filepath.Join(filepath.Dir(path), e.Name()))
			if err != nil || string(body) != existing {
				t.Errorf("the backup is not what was there before")
			}
		}
	}
	if !found {
		t.Error("editing a settings file should leave a copy of what it replaced")
	}
}

func TestInstallCreatesASettingsFileThatIsNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(path, "/bin/braids hook"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if on, err := Installed(path, "/bin/braids hook"); err != nil || len(on) != len(Wanted()) {
		t.Errorf("Installed = %v, %v", on, err)
	}
}
