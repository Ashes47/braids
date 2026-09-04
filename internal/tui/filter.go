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
//
// Opening the field is the screen's job, bound to f. This deliberately does not
// claim "/": that key means "search everything" throughout braids, and a field
// that opens itself on it swallows every keystroke after — silently, because an
// empty field looks like no field at all.
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

	case f.active:
		return f.edit(k)
	}
	return false
}

// edit applies a keypress to the text itself: a character or a backspace.
// Shared by every one-line field so they all behave the same way.
func (f *filterInput) edit(k string) bool {
	switch {
	case k == "backspace":
		if r := []rune(f.text); len(r) > 0 {
			f.text = string(r[:len(r)-1])
		}
		return true
	case k == "space":
		f.text += " "
		return true
	case len([]rune(k)) == 1:
		f.text += k
		return true
	}
	return false
}

// paste appends pasted text, flattened to one line: a text field is one line,
// and a newline in the middle of a query is never what was meant.
func (f *filterInput) paste(text string) {
	f.text += strings.Join(strings.Fields(text), " ")
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
