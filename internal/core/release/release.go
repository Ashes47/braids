// Package release reports how long it has been since this build was looked at,
// and nothing else.
//
// braids cannot tell you that a newer version exists, because finding out
// would mean asking, and braids does not make network calls. What it can do is
// notice that nobody has checked in a while, which is a claim it can support
// from two local facts: when this binary arrived, and when the installer last
// ran. The installer stamps a file every time it runs, including the run where
// it decides there is nothing to do, so checking and finding yourself current
// resets the clock. Without that stamp a notice could only be cleared by an
// actual update, which is exactly the case where there is none.
package release

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Interval is how long braids waits before mentioning updates again. A month
// rather than a fortnight: braids is a local reader with no server to stay
// compatible with and an index that rebuilds itself, so being a release behind
// costs nothing, and a reminder that arrives too often is a nag.
const Interval = 30 * 24 * time.Hour

// StampName is the file the installer touches on every run.
const StampName = "checked"

// State is what braids knows about its own age. Zero times mean "could not
// tell", which is reported as silence rather than as a guess.
type State struct {
	// Built is when this binary landed here. For a release build that is when
	// the archive was made, because tar keeps the timestamp; for `go install`
	// it is the build. Either way it answers "how old is what I am running".
	Built time.Time
	// Checked is when the installer last ran, from the stamp it leaves.
	Checked time.Time
	// Dir is where the running binary lives, so an update can be told to
	// replace it there rather than dropping a second copy somewhere on PATH.
	Dir string
}

// Read gathers what can be known locally. home is where braids keeps its own
// files, and exe is the running binary, both passed in so this is testable.
func Read(home, exe string) State {
	var s State
	if exe != "" {
		s.Dir = filepath.Dir(exe)
		if info, err := os.Stat(exe); err == nil {
			s.Built = info.ModTime()
		}
	}
	if raw, err := os.ReadFile(filepath.Join(home, StampName)); err == nil {
		if when, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw))); err == nil {
			s.Checked = when
		}
	}
	return s
}

// BuildAge reports how old the running binary is. That is a different
// question from Since: checking for updates and finding none leaves the build
// exactly as old as it was, and saying otherwise would be a lie told by
// arithmetic.
func (s State) BuildAge(now time.Time) (time.Duration, bool) {
	if s.Built.IsZero() || s.Built.After(now) {
		return 0, false
	}
	return now.Sub(s.Built), true
}

// Since reports how long it has been since either the build arrived or the
// last check, whichever is more recent, and whether that is knowable at all.
// This is what decides whether to say anything.
func (s State) Since(now time.Time) (time.Duration, bool) {
	last := s.Built
	if s.Checked.After(last) {
		last = s.Checked
	}
	if last.IsZero() || last.After(now) {
		// A clock that disagrees is not evidence of anything.
		return 0, false
	}
	return now.Sub(last), true
}

// Due reports whether it has been long enough to mention it.
func (s State) Due(now time.Time) bool {
	age, ok := s.Since(now)
	return ok && age >= Interval
}

// Age says how long ago something was, in the roundest terms that are still
// true. Nothing here is precise enough to deserve a decimal.
func Age(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 21:
		return plural(days, "day")
	case days < 60:
		return plural(days/7, "week")
	case days < 730:
		return plural(days/30, "month")
	default:
		return plural(days/365, "year")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
