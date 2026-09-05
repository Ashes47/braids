// Package skill carries the Claude Code skill that teaches braids.
//
// The text is embedded rather than read from the repository, because the
// binary that installs it is usually a release build with no checkout in
// sight. It lives here, in the package that installs it, so there is one copy
// and not one here and one on disk that disagree.
package skill

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed SKILL.md
var content string

// Name is the directory the skill installs into.
const Name = "braids"

// FileName is what Claude Code looks for inside that directory.
const FileName = "SKILL.md"

// Text is the skill as it will be written.
func Text() string { return content }

// Path is where the skill goes, given the directory Claude Code keeps skills
// in.
func Path(skills string) string { return filepath.Join(skills, Name, FileName) }

// State is what is installed at a path, and whether it matches this build.
type State struct {
	Path      string
	Installed bool
	// Current says the file on disk is what this braids would write. An older
	// braids leaves an older skill behind, and a skill that describes flags a
	// command no longer takes is worse than none.
	Current bool
}

// Read reports what is installed.
func Read(skills string) (State, error) {
	s := State{Path: Path(skills)}
	body, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read the installed skill: %w", err)
	}
	s.Installed = true
	s.Current = normalise(string(body)) == normalise(content)
	return s, nil
}

// Install writes the skill, replacing an older one.
func Install(skills string) (State, error) {
	s := State{Path: Path(skills)}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return s, fmt.Errorf("make %s: %w", filepath.Dir(s.Path), err)
	}
	// Written whole and renamed into place, so an interrupted install leaves
	// the old skill rather than half a new one.
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".braids-skill-*")
	if err != nil {
		return s, fmt.Errorf("write the skill: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name) //nolint:errcheck // best effort once renamed
	if _, err := tmp.WriteString(content); err != nil {
		return s, errors.Join(fmt.Errorf("write the skill: %w", err), tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return s, fmt.Errorf("flush the skill: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return s, fmt.Errorf("write the skill: %w", err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return s, fmt.Errorf("set the skill's mode: %w", err)
	}
	if err := os.Rename(name, s.Path); err != nil {
		return s, fmt.Errorf("put the skill in place: %w", err)
	}
	s.Installed, s.Current = true, true
	return s, nil
}

// Remove takes the skill back out, and the directory with it when braids put
// nothing else there. Anything a person added beside it stays.
func Remove(skills string) (bool, error) {
	path := Path(skills)
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("remove the skill: %w", err)
	}
	dir := filepath.Dir(path)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	return true, nil
}

// normalise ignores the line endings, because a file checked out on Windows
// arrives with CRLF and is otherwise the same skill.
func normalise(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}
