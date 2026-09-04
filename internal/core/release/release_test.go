package release

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

func TestAgeRoundsToSomethingSayable(t *testing.T) {
	for _, c := range []struct {
		days int
		want string
	}{
		{0, "today"}, {1, "yesterday"}, {2, "2 days"}, {20, "20 days"},
		{21, "3 weeks"}, {59, "8 weeks"}, {60, "2 months"}, {400, "13 months"},
		{800, "2 years"},
	} {
		if got := Age(time.Duration(c.days) * 24 * time.Hour); got != c.want {
			t.Errorf("Age(%d days) = %q, want %q", c.days, got, c.want)
		}
	}
}

// The stamp is the whole point: checking and finding yourself current has to
// reset the clock, or the notice repeats forever in exactly the case where
// there is nothing to do about it.
func TestCheckingResetsTheClockEvenWithoutAnUpdate(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "braids")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-90 * 24 * time.Hour)
	if err := os.Chtimes(exe, old, old); err != nil {
		t.Fatal(err)
	}

	if s := Read(home, exe); !s.Due(now) {
		t.Fatal("a build from 90 days ago is not due, so the test proves nothing")
	}

	// The installer ran, found nothing to do, and stamped anyway.
	written := now.Add(-2 * 24 * time.Hour)
	if err := os.WriteFile(filepath.Join(home, StampName),
		[]byte(written.Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Read(home, exe)
	if s.Due(now) {
		t.Error("still due after a check, so the notice would repeat forever")
	}
	if age, ok := s.Since(now); !ok || age > 3*24*time.Hour {
		t.Errorf("Since = %v, want about two days", age)
	}
}

// Nothing knowable means nothing said. A missing binary, an unreadable stamp
// and a clock in the future are all silence rather than a guess.
func TestUnknowableIsSilent(t *testing.T) {
	if _, ok := Read(t.TempDir(), "").Since(now); ok {
		t.Error("reported an age with no binary to date")
	}
	if Read(t.TempDir(), filepath.Join(t.TempDir(), "absent")).Due(now) {
		t.Error("a binary that is not there cannot be due")
	}
	home, exe := t.TempDir(), filepath.Join(t.TempDir(), "braids")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	ahead := now.Add(48 * time.Hour)
	if err := os.Chtimes(exe, ahead, ahead); err != nil {
		t.Fatal(err)
	}
	if _, ok := Read(home, exe).Since(now); ok {
		t.Error("a clock that disagrees was treated as evidence")
	}
}
