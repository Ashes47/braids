package tui

import "strings"

// filterInput is the one-line text field both screens filter with. Keeping it
// in one place means `/` behaves identically wherever it is pressed.
type filterInput struct {
	text   string
	active bool
	// cursor is where the next character goes, counted in runes from the
	// start. Without it the only way to fix the first word of a query is to
	// delete everything after it and type that again.
	cursor int
}

// typing opens a field, with text already in it and the caret after it.
//
// A field pre-filled with a suggestion is the common case here: renaming a
// conversation offers its current title, branching offers a name made from the
// turn. Opening those with the caret at the start means the first thing typed
// lands in front of the suggestion and the first backspace does nothing, which
// is how this was found.
func typing(text string) filterInput {
	return filterInput{active: true, text: text, cursor: len([]rune(text))}
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
			f.clear()
		case f.text != "":
			f.clear()
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

// edit applies a keypress to the text itself: moving within it, or changing
// it. Shared by every one-line field so they all behave the same way.
//
// The arrows are claimed only while the field is active, which is why this is
// reached before a screen's own bindings: on the screens where left means go
// back, it still does, as soon as the field is closed.
func (f *filterInput) edit(k string) bool {
	f.clamp()
	runes := []rune(f.text)
	switch {
	case k == "left" || k == "ctrl+b":
		if f.cursor > 0 {
			f.cursor--
		}
		return true
	case k == "right" || k == "ctrl+f":
		if f.cursor < len(runes) {
			f.cursor++
		}
		return true
	case k == "home" || k == "ctrl+a":
		f.cursor = 0
		return true
	case k == "end" || k == "ctrl+e":
		f.cursor = len(runes)
		return true
	case k == "backspace":
		if f.cursor > 0 {
			f.text = string(runes[:f.cursor-1]) + string(runes[f.cursor:])
			f.cursor--
		}
		return true
	case k == "delete":
		if f.cursor < len(runes) {
			f.text = string(runes[:f.cursor]) + string(runes[f.cursor+1:])
		}
		return true
	case k == "space":
		f.insert(" ")
		return true
	case len([]rune(k)) == 1:
		f.insert(k)
		return true
	}
	return false
}

// insert puts text in at the caret and leaves the caret after it.
func (f *filterInput) insert(text string) {
	f.clamp()
	runes := []rune(f.text)
	f.text = string(runes[:f.cursor]) + text + string(runes[f.cursor:])
	f.cursor += len([]rune(text))
}

// clear empties the field, caret and all.
func (f *filterInput) clear() { f.text, f.cursor = "", 0 }

// clamp keeps the caret inside the text, which matters because the text can be
// set from outside this file.
func (f *filterInput) clamp() {
	f.cursor = min(max(f.cursor, 0), len([]rune(f.text)))
}

// split returns the text either side of the caret, so a field draws the caret
// where the next character will go rather than always at the end.
func (f filterInput) split() (before, after string) {
	runes := []rune(f.text)
	at := min(max(f.cursor, 0), len(runes))
	return string(runes[:at]), string(runes[at:])
}

// paste appends pasted text, flattened to one line: a text field is one line,
// and a newline in the middle of a query is never what was meant.
func (f *filterInput) paste(text string) {
	f.insert(strings.Join(strings.Fields(text), " "))
}

// fit returns the text either side of the caret, trimmed to a width, with the
// view following the caret. A field narrower than its contents that always
// showed the start would hide the caret the moment somebody edited the end of
// a long name, and a field that always showed the end would hide it the moment
// they went back to the start.
func (f filterInput) fit(width int) (before, after string) {
	if width < 1 {
		return "", ""
	}
	before, after = f.split()
	if b := []rune(before); len(b) > width {
		before = string(b[len(b)-width:])
	}
	room := width - len([]rune(before))
	if a := []rune(after); len(a) > room {
		after = string(a[:room])
	}
	return before, after
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

// caretIn draws a field's text with the caret in it, to a width. The caret is
// one column, so the result is width+1 columns wide.
func caretIn(f filterInput, width int) string {
	before, after := f.fit(width)
	return before + "▏" + after
}
