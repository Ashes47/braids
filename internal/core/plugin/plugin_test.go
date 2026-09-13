package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// record writes an installed_plugins.json naming the roots given, and creates
// the ones marked to exist.
func record(t *testing.T, plugins string, roots map[string]bool) {
	t.Helper()
	installs := map[string]any{}
	for root, exists := range roots {
		if exists {
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		installs[filepath.Base(root)+"@m"] = []any{map[string]any{"installPath": root}}
	}
	body, err := json.Marshal(map[string]any{"version": 2, "plugins": installs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "installed_plugins.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRootsFindsWhatIsInstalled(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	a, b := filepath.Join(home, "cache", "alpha"), filepath.Join(home, "cache", "beta")
	record(t, plugins, map[string]bool{a: true, b: true})

	got := Roots(plugins)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("Roots = %v, want [%s %s] in that order", got, a, b)
	}
}

// The record is a map, so without sorting the answer to "which plugin carries
// the skill" changes between runs on a machine with more than one.
func TestRootsComeBackInTheSameOrderEveryTime(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	roots := map[string]bool{}
	for _, name := range []string{"d", "a", "c", "b", "e"} {
		roots[filepath.Join(home, "cache", name)] = true
	}
	record(t, plugins, roots)

	first := Roots(plugins)
	for range 20 {
		if got := Roots(plugins); len(got) != len(first) {
			t.Fatalf("Roots length changed: %v then %v", first, got)
		} else {
			for i := range got {
				if got[i] != first[i] {
					t.Fatalf("Roots order changed: %v then %v", first, got)
				}
			}
		}
	}
}

// A plugin the record names but whose directory is gone is not installed in
// any sense that matters: reporting it sends somebody to uninstall nothing.
func TestRootsSkipsADirectoryThatIsNotThere(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	here, gone := filepath.Join(home, "cache", "here"), filepath.Join(home, "cache", "gone")
	record(t, plugins, map[string]bool{here: true, gone: false})

	got := Roots(plugins)
	if len(got) != 1 || got[0] != here {
		t.Fatalf("Roots = %v, want only %s", got, here)
	}
}

// Every failure is "no plugins". This is asked on every doctor run, and a
// duplicate invented out of a file that could not be read would send somebody
// to remove something they need.
func TestUnreadableIsNoPlugins(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"no file at all", ""},
		{"not json", "{{{"},
		{"an empty record", `{"version":2,"plugins":{}}`},
		{"an install with no path", `{"version":2,"plugins":{"a@m":[{"scope":"user"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plugins := t.TempDir()
			if tc.body != "" {
				path := filepath.Join(plugins, "installed_plugins.json")
				if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := Roots(plugins); len(got) != 0 {
				t.Fatalf("Roots = %v, want none", got)
			}
		})
	}
}

// A file where the directory should be is not a plugin root.
func TestRootsSkipsAFileInPlaceOfADirectory(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	notADir := filepath.Join(home, "cache", "impostor")
	if err := os.MkdirAll(filepath.Dir(notADir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notADir, []byte("not a plugin"), 0o600); err != nil {
		t.Fatal(err)
	}
	record(t, plugins, map[string]bool{notADir: false})

	if got := Roots(plugins); len(got) != 0 {
		t.Fatalf("Roots = %v, want none", got)
	}
}
