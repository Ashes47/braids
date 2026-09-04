//go:build windows

package main

import "os/exec"

// updateCommand returns nothing on Windows, so the map does not offer a key
// that cannot work.
//
// The installer is a POSIX shell script, and braids will not ship a PowerShell
// one it has no way to run before releasing it. `braids version` still says how
// old the build is; the docs say how to replace it. Nothing here is a
// degradation of what Windows can do, only of what braids can honestly claim
// to have tested.
func updateCommand() *exec.Cmd { return nil }
