package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// Status is what a settings file says about braids' hooks.
type Status struct {
	// Events braids is attached to.
	Events []string
	// Elsewhere are braids hooks in the file that run a different binary than
	// the one asking: a second build, or one left behind by a binary that has
	// since moved. Those hooks fail on every event until they are replaced.
	Elsewhere []string
}

// Install adds braids' hook to each event it needs, leaving every other hook in
// place. It is idempotent: running it twice adds nothing the second time. A
// braids hook already there under a different path is replaced rather than
// duplicated.
func Install(settingsPath, command string) ([]string, error) {
	return edit(settingsPath, command, true)
}

// Remove takes braids' hooks back out, leaving every other hook in place —
// including braids hooks installed by a build at some other path, which are
// braids' own to clean up.
func Remove(settingsPath, command string) ([]string, error) {
	return edit(settingsPath, command, false)
}

// Inspect reports what the settings file says about braids' hooks.
func Inspect(settingsPath, command string) (Status, error) {
	_, byEvent, err := load(settingsPath)
	if err != nil {
		return Status{}, err
	}
	var status Status
	on, seen := map[string]bool{}, map[string]bool{}
	for name, groups := range byEvent {
		for _, group := range groups {
			_, mine := splitGroup(group)
			for _, found := range mine {
				on[name] = true
				if found != command && !seen[found] {
					seen[found] = true
					status.Elsewhere = append(status.Elsewhere, found)
				}
			}
		}
	}
	status.Events = ordered(on)
	sort.Strings(status.Elsewhere)
	return status, nil
}

// Installed reports which events braids' hook is currently attached to.
func Installed(settingsPath, command string) ([]string, error) {
	status, err := Inspect(settingsPath, command)
	return status.Events, err
}

func edit(settingsPath, command string, adding bool) ([]string, error) {
	settings, byEvent, err := load(settingsPath)
	if err != nil {
		return nil, err
	}
	if byEvent == nil {
		byEvent = map[string][]json.RawMessage{}
	}

	want := map[string]bool{}
	if adding {
		for _, name := range Wanted() {
			want[name] = true
		}
	}

	changed := map[string]bool{}
	// Every event in the file is swept, not just the ones braids wants now: an
	// event it has stopped using, or one left behind by a build at another
	// path, is still braids' own to take back. Nothing else is touched.
	for name, groups := range byEvent {
		kept := make([]json.RawMessage, 0, len(groups))
		var had []string
		for _, group := range groups {
			rest, mine := splitGroup(group)
			had = append(had, mine...)
			if rest != nil {
				kept = append(kept, rest)
			}
		}
		switch {
		case want[name]:
			ours, err := ownGroup(command)
			if err != nil {
				return nil, err
			}
			byEvent[name] = append(kept, ours)
			// Unchanged only when braids was already there exactly once, at
			// this command. Anything else was a duplicate or a stale path.
			if len(had) != 1 || had[0] != command {
				changed[name] = true
			}
			delete(want, name)
		case len(had) > 0:
			if len(kept) == 0 {
				delete(byEvent, name)
			} else {
				byEvent[name] = kept
			}
			changed[name] = true
		}
	}
	// Events braids wants that the file carried nothing for.
	for name := range want {
		ours, err := ownGroup(command)
		if err != nil {
			return nil, err
		}
		byEvent[name] = append(byEvent[name], ours)
		changed[name] = true
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
	return ordered(changed), nil
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
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush settings: %w", err)
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

// mine reports whether a hook entry runs braids, whatever path the binary sits
// at. Identity is the program, not the path it happens to occupy today: a
// second build in another directory is the same tool, and matching on the exact
// path makes it look like a different one — which is how a settings file ends
// up with two braids hooks, one of them pointing at a binary that is gone.
func mine(raw json.RawMessage) (string, bool) {
	var hook entry
	if json.Unmarshal(raw, &hook) != nil {
		return "", false
	}
	bin, ok := strings.CutSuffix(strings.TrimSpace(hook.Command), " hook")
	if !ok {
		return hook.Command, false
	}
	// Trimmed rather than split on spaces: an installed path may contain them.
	bin = strings.Trim(bin, `"'`)
	name := strings.TrimSuffix(filepath.Base(bin), ".exe")
	return hook.Command, strings.EqualFold(name, "braids")
}

// splitGroup separates braids' entries from the rest of a hook group. It
// returns the group without them — nil when nothing else was in it — and the
// commands they ran. Groups are edited entry by entry rather than dropped
// whole, so a group someone has added braids to alongside their own hook keeps
// that hook.
func splitGroup(group json.RawMessage) (json.RawMessage, []string) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(group, &fields) != nil {
		return group, nil
	}
	var list []json.RawMessage
	if json.Unmarshal(fields["hooks"], &list) != nil {
		return group, nil
	}
	var found []string
	kept := make([]json.RawMessage, 0, len(list))
	for _, raw := range list {
		if command, ours := mine(raw); ours {
			found = append(found, command)
			continue
		}
		kept = append(kept, raw)
	}
	switch {
	case len(found) == 0:
		return group, nil
	case len(kept) == 0:
		return nil, found
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return group, nil
	}
	fields["hooks"] = encoded
	rebuilt, err := json.Marshal(fields)
	if err != nil {
		return group, nil
	}
	return rebuilt, found
}

// ordered lists event names in the order braids wants them, so its own events
// read the same way everywhere, with anything unrecognised after them.
func ordered(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	seen := map[string]bool{}
	for _, name := range Wanted() {
		if set[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	var extra []string
	for name := range set {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(names, extra...)
}
