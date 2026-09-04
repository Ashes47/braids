//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// updateCommand builds the line the map's update key runs.
//
// BRAIDS_BIN_DIR points the installer at the directory this binary is running
// from, so it replaces braids where it already lives. Without it the script
// picks /usr/local/bin or ~/.local/bin, which for anyone who installed another
// way means a second braids on disk and PATH deciding which one they get.
func updateCommand() *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	script := fmt.Sprintf("echo '$ %s'; echo; %s", installCommand, installCommand)
	command := exec.Command("sh", "-c", script)
	command.Env = append(os.Environ(), "BRAIDS_BIN_DIR="+filepath.Dir(exe))
	return command
}
