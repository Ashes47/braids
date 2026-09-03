//go:build darwin

package claudecode

import (
	"os"
	"syscall"
	"time"
)

// birthTime reports when the file was created. APFS and HFS+ record this, and
// it is the signal that tells a forked transcript from the one it forked from.
func birthTime(info os.FileInfo) time.Time {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
}
