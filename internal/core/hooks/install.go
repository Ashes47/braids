package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Settings files are shaped:
//
//	{ "hooks": { "Stop": [ { "matcher": "*", "hooks": [ {...} ] } ] }, ... }
//
// Everything outside the entries braids owns is carried through untouched as
// raw JSON. A settings file is something the user depends on, and braids has no
// business rewriting parts of it that it does not understand.
type entry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// Install adds braids' hook to each event it needs, leaving every other hook in
// place. It is idempotent: running it twice adds nothing the second time.
func Install(settingsPath, command string) ([]string, error) {
	return edit(settingsPath, command, true)
}

// Remove takes braids' hook back out, leaving every other hook in place.
func Remove(settingsPath, command string) ([]string, error) {
	return edit(settingsPath, command, false)
}

// Installed reports which events braids' hook is currently attached to.
func Installed(settingsPath, command string) ([]string, error) {
	_, byEvent, err := load(settingsPath)
	if err != nil {
		return nil, err
	}
	var on []string
	for _, name := range Wanted() {
		for _, group := range byEvent[name] {
			if hasCommand(group, command) {
				on = append(on, name)
				break
			}
		}
	}
	return on, nil
}

func edit(settingsPath, command string, adding bool) ([]string, error) {
	settings, byEvent, err := load(settingsPath)
	if err != nil {
		return nil, err
	}
	if byEvent == nil {
		byEvent = map[string][]json.RawMessage{}
	}

	var changed []string
	for _, name := range Wanted() {
		groups := byEvent[name]
		present := false
		kept := make([]json.RawMessage, 0, len(groups))
		for _, group := range groups {
			if hasCommand(group, command) {
				present = true
				if adding {
					kept = append(kept, group)
				}
				continue
			}
			kept = append(kept, group)
		}
		switch {
		case adding && !present:
			ours, err := ownGroup(command)
			if err != nil {
				return nil, err
			}
			byEvent[name] = append(kept, ours)
			changed = append(changed, name)
		case !adding && present:
			if len(kept) == 0 {
				delete(byEvent, name)
			} else {
				byEvent[name] = kept
			}
			changed = append(changed, name)
		default:
			byEvent[name] = groups
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}

	encoded, err := json.Marshal(byEvent)
	if err != nil {
		return nil, fmt.Errorf("encode hooks: %w", err)
	}
	settings["hooks"] = encoded
	if err := write(settingsPath, settings); err != nil {
		return nil, err
	}
	return changed, nil
}

// load reads a settings file, keeping every key it does not understand as raw
// JSON so it can be written back exactly as it was.
func load(path string) (map[string]json.RawMessage, map[string][]json.RawMessage, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, map[string][]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	settings := map[string]json.RawMessage{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &settings); err != nil {
			// Refuse rather than overwrite: a settings file that cannot be
			// parsed is one whose contents must not be replaced by a guess.
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	byEvent := map[string][]json.RawMessage{}
	if raw, ok := settings["hooks"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &byEvent); err != nil {
			return nil, nil, fmt.Errorf("parse hooks in %s: %w", path, err)
		}
	}
	return settings, byEvent, nil
}

// write replaces the settings file, keeping a copy of what was there.
func write(path string, settings map[string]json.RawMessage) error {
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if previous, err := os.ReadFile(path); err == nil {
		backup := fmt.Sprintf("%s.braids-%s.bak", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, previous, 0o600); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return fmt.Errorf("create temp settings: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best effort once renamed

	if _, err := tmp.Write(append(body, '\n')); err != nil {
		return errors.Join(fmt.Errorf("write settings: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("place settings: %w", err)
	}
	return nil
}

// ownGroup is the entry braids adds: no matcher, so it applies to everything
// the event covers.
func ownGroup(command string) (json.RawMessage, error) {
	encoded, err := json.Marshal(map[string]any{
		"hooks": []entry{{Type: "command", Command: command}},
	})
	if err != nil {
		return nil, fmt.Errorf("encode hook: %w", err)
	}
	return encoded, nil
}

// hasCommand reports whether a group runs the given command.
func hasCommand(group json.RawMessage, command string) bool {
	var parsed struct {
		Hooks []entry `json:"hooks"`
	}
	if json.Unmarshal(group, &parsed) != nil {
		return false
	}
	for _, h := range parsed.Hooks {
		if h.Command == command {
			return true
		}
	}
	return false
}
