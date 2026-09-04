package tui

import "testing"

// "now ago" is not something anyone says, and the bin shows it for anything
// deleted in the last minute, which is exactly when you are most likely to be
// looking at the bin.
func TestJustDeletedReadsAsAMoment(t *testing.T) {
	for age, want := range map[string]string{
		"now": "just now",
		"12m": "12m ago",
		"3h":  "3h ago",
		"14d": "14d ago",
	} {
		if got := deletedAt(age); got != want {
			t.Errorf("deletedAt(%q) = %q, want %q", age, got, want)
		}
	}
}
