package tui

import "strings"

// filterInput is the one-line text field both screens filter with. Keeping it
// in one place means `/` behaves identically wherever it is pressed.
type filterInput struct {
	text   string
	active bool
}

// key applies a keypress, reporting whether the field consumed it. An
// unconsumed key falls through to the screen's own bindings.
//
// esc peels one layer at a time: it first leaves the field, then clears the
// text, and only then declines the key so the screen can act on it. That way
// escaping out of a filtered spine never skips straight back to the map.
func (f *filterInput) key(k string) bool {
	switch {
	case k == "esc":
		switch {
		case f.active:
			f.active = false
			f.text = ""
		case f.text != "":
			f.text = ""
		default:
			return false
		}
		return true

	case k == "enter" && f.active:
		f.active = false
		return true

	case k == "backspace" && f.active:
		if r := []rune(f.text); len(r) > 0 {
			f.text = string(r[:len(r)-1])
		}
		return true

	case k == "/" && !f.active:
		f.active = true
		return true

	case f.active && len([]rune(k)) == 1:
		f.text += k
		return true
	}
	return false
}

// on reports whether anything is being filtered out.
func (f filterInput) on() bool { return f.text != "" }

// matches reports whether the haystack contains the filter, case-insensitively.
func (f filterInput) matches(haystack string) bool {
	if f.text == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(f.text))
}

// label renders the filter for a panel title: "/query", or empty when unset.
func (f filterInput) label() string {
	if !f.on() {
		return ""
	}
	return "/" + f.text
}
