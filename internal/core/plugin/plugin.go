// Package plugin reads what Claude Code records about the plugins it has
// installed.
//
// braids can arrive on a machine twice: once as a binary somebody installed
// and pointed at their own settings, and once as a plugin that brings the same
// skill and the same hooks with it. Neither installation knows about the
// other, and from Claude Code's side they are two unrelated things that happen
// to agree. Finding the installed plugins is how braids notices.
package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// installedPlugins is where Claude Code records the plugins that are actually
// installed. The plugin cache is not: it keeps a copy of every version ever
// installed, including ones since removed, so a machine that tried a plugin
// and uninstalled it still has the files. This file is the record that
// changes when somebody uninstalls.
const installedPlugins = "installed_plugins.json"

// Roots returns the directory of every installed plugin, sorted.
//
// Every failure is "no plugins". This is asked on every `braids doctor` and
// every `braids skill`, and a warning invented out of a file that could not be
// read would be worse than no warning at all.
func Roots(plugins string) []string {
	body, err := os.ReadFile(filepath.Join(plugins, installedPlugins))
	if err != nil {
		return nil
	}
	var record struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		return nil
	}
	var out []string
	for _, installs := range record.Plugins {
		for _, at := range installs {
			if at.InstallPath == "" {
				continue
			}
			if info, err := os.Stat(at.InstallPath); err != nil || !info.IsDir() {
				continue
			}
			out = append(out, at.InstallPath)
		}
	}
	// Sorted because the record is a map: without this the answer to "which
	// plugin carries the skill" changes between runs on a machine with more
	// than one.
	sort.Strings(out)
	return out
}
