package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallReadRemove(t *testing.T) {
	skills := t.TempDir()

	if s, err := Read(skills); err != nil || s.Installed {
		t.Fatalf("Read of an empty directory = %+v, %v", s, err)
	}

	s, err := Install(skills)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Installed || !s.Current {
		t.Errorf("after Install: %+v", s)
	}
	body, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != Text() {
		t.Error("what was written is not what is embedded")
	}
	if !strings.HasPrefix(string(body), "---") {
		t.Error("the skill lost its front matter, so Claude Code will not read it")
	}

	// Installing twice is not an error and leaves one file.
	if _, err := Install(skills); err != nil {
		t.Fatalf("installing over an existing skill: %v", err)
	}

	removed, err := Remove(skills)
	if err != nil || !removed {
		t.Fatalf("Remove = %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Dir(Path(skills))); !os.IsNotExist(err) {
		t.Error("the directory braids made was left behind empty")
	}
	if removed, err := Remove(skills); err != nil || removed {
		t.Errorf("removing nothing = %v, %v; it should be quiet", removed, err)
	}
}

// An older braids leaves an older skill, which may describe flags a command no
// longer takes. Saying so is the whole reason Read reports more than a boolean.
func TestAnOlderSkillIsInstalledButNotCurrent(t *testing.T) {
	skills := t.TempDir()
	if _, err := Install(skills); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(skills), []byte("---\nname: braids\n---\n\nolder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Read(skills)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Installed || s.Current {
		t.Errorf("an older skill read as %+v", s)
	}

	// And line endings alone are not a difference: a file checked out on
	// Windows arrives with CRLF and is otherwise the same skill.
	if err := os.WriteFile(Path(skills),
		[]byte(strings.ReplaceAll(Text(), "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := Read(skills); err != nil || !s.Current {
		t.Errorf("CRLF made the same skill look different: %+v, %v", s, err)
	}
}

// Whatever a person put beside the skill is theirs.
func TestRemoveKeepsWhateverElseIsThere(t *testing.T) {
	skills := t.TempDir()
	if _, err := Install(skills); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(filepath.Dir(Path(skills)), "notes.md")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(skills); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Errorf("Remove took a file it did not put there: %v", err)
	}
}
