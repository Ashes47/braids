package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The reported bug: on the work and memory screens every keystroke was taken
// as filter text without the filter having been opened, because / silently
// activated it. Driven through the real key path, not the screen's handler.
func TestSlashDoesNotHijackListScreens(t *testing.T) {
	press := func(m Model, keys ...string) Model {
		for _, k := range keys {
			next, _ := m.key(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
			m = next.(Model)
		}
		return m
	}
	t.Run("work", func(t *testing.T) {
		m, _ := workModel(t, nil)
		m = press(m, "/")
		if m.work != nil && m.work.filter.active {
			t.Error("/ activated the local filter instead of opening search")
		}
		// And with no global search wired, / must not swallow the next keys.
		m, _ = workModel(t, nil)
		m = press(m, "/", "j")
		if m.work.filter.text != "" {
			t.Errorf("keys became filter text: %q", m.work.filter.text)
		}
	})
	t.Run("memories", func(t *testing.T) {
		m := memoryModel(t, nil)
		m = press(m, "/", "j", "k")
		if m.memories != nil && m.memories.filter.text != "" {
			t.Errorf("keys became filter text: %q", m.memories.filter.text)
		}
	})
	t.Run("f still filters, and says so", func(t *testing.T) {
		m, _ := workModel(t, nil)
		m = press(m, "f")
		if !m.work.filter.active {
			t.Fatal("f did not open the filter")
		}
		if got := plain(m.renderWork()); !strings.Contains(got, "filter:") {
			t.Errorf("an active filter is invisible:\n%s", got)
		}
		// q types rather than quitting while a field is taking keys.
		if m.editing() != true {
			t.Error("editing() does not know the work filter is open")
		}
	})
}
