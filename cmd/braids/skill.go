package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ashes47/braids/internal/skill"
)

// cmdSkill installs the Claude Code skill that teaches braids.
//
// It mirrors `braids hooks`: opt in, one command, reversible, and it says what
// state it is in when asked. braids writes into the user's Claude Code
// directory in exactly two places and both are typed on purpose.
func cmdSkill(args []string, out *printer) error {
	fs := newFlagSet("skill")
	fs.SetOutput(out)
	install := fs.Bool("install", false, "write the skill where Claude Code will find it")
	remove := fs.Bool("remove", false, "take it back out")
	show := fs.Bool("show", false, "print the skill instead of installing it")
	dir := fs.String("dir", "", "skills directory (default ~/.claude/skills)")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	if *install && *remove {
		return errors.New("choose one of --install or --remove")
	}
	if *show {
		out.printf("%s", skill.Text())
		return out.Err()
	}

	skills := *dir
	if skills == "" {
		var err error
		if skills, err = defaultSkills(); err != nil {
			return err
		}
	}

	var (
		state skill.State
		err   error
	)
	switch {
	case *install:
		state, err = skill.Install(skills)
	case *remove:
		var removed bool
		if removed, err = skill.Remove(skills); err == nil {
			state = skill.State{Path: skill.Path(skills)}
			if !removed && !*asJSON {
				out.printf("no skill was installed at %s\n", state.Path)
				return out.Err()
			}
		}
	default:
		state, err = skill.Read(skills)
	}
	if err != nil {
		return err
	}

	if *asJSON {
		return out.emit(struct {
			Path      string `json:"path"`
			Installed bool   `json:"installed"`
			Current   bool   `json:"current"`
		}{state.Path, state.Installed, state.Current})
	}
	switch {
	case *install:
		out.printf("skill installed: %s\n", state.Path)
		out.printf("Claude will use braids when you refer to earlier work, ask why\n")
		out.printf("something is the way it is, or propose something already tried.\n")
	case *remove:
		out.printf("skill removed: %s\n", state.Path)
	case !state.Installed:
		out.printf("no skill installed (run: braids skill --install)\n")
	case !state.Current:
		out.printf("an older skill is installed at %s\n", state.Path)
		out.printf("It may describe flags this braids no longer takes.\n")
		out.printf("Replace it with: braids skill --install\n")
	default:
		out.printf("skill installed and current: %s\n", state.Path)
	}
	return out.Err()
}

// defaultSkills is where Claude Code looks for skills.
func defaultSkills() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "skills"), nil
}
