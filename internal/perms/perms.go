// Package perms says whether file permission bits mean what braids promises
// they mean.
//
// braids keeps its index at 0600 and leaves the files it edits at whatever
// mode it found them, because the index holds the full text of every message
// it has read and a memory holds what somebody asked to be remembered. Those
// are POSIX guarantees. Windows has no such bits: Go reports every file as
// 0666, or 0444 when it is read only, and a chmod there toggles that one flag
// and nothing else. On Windows the privacy comes from the permissions on the
// user's profile directory, which braids neither sets nor can assert.
//
// This exists so the tests that read modes say why they are not running,
// rather than asserting something that is not true of the platform.
package perms

import (
	"runtime"
	"testing"
)

// POSIX reports whether this platform has permission bits braids can set.
func POSIX() bool { return runtime.GOOS != "windows" }

// RequirePOSIX skips a test that can only mean something where modes do.
func RequirePOSIX(t *testing.T) {
	t.Helper()
	if !POSIX() {
		t.Skip("file modes are not something braids can set on Windows; " +
			"what it writes is protected by the profile directory instead")
	}
}
