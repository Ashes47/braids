package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// Memories are markdown, and a reader should see what the writer meant rather
// than the marks they typed to mean it.
//
// This is deliberately a small subset — emphasis, code spans, headings, list
// bullets and fenced blocks — because that is what these files actually use. A
// full markdown renderer would be a dependency larger than braids' own terminal
// UI, for prose that is a few hundred words long.
//
// Styling happens before wrapping, and wrapping is done here rather than by a
// general text wrapper, because a wrapper that knows nothing about styles
// leaves them open across a line break: the bold spills into the frame border
// and every line after it. Wrapping on styled words instead means each output
// line opens and closes its own styling.

// span is a run of text that shares one style.
type span struct {
	text  string
	style lipgloss.Style
}

// prose lays a memory's markdown out for a frame of the given width.
func (m Model) prose(text string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out, paragraph []string
	fenced := false
	// A paragraph is styled as one piece rather than line by line, because
	// markdown reflows it and emphasis may open on one line and close on the
	// next. Read a line at a time, `**narrow the / lock**` is two unmatched
	// marks and the reader shows the asterisks, which is what it used to do.
	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		out = append(out, m.proseLine(strings.Join(paragraph, " "), width)...)
		paragraph = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			// The fence itself is a marker, not content. Showing it would be
			// showing punctuation the reader did not write for them.
			flush()
			fenced = !fenced
			continue
		}
		if fenced {
			// Code is verbatim: no emphasis to find, and reflowing it would
			// change what it says.
			out = append(out, m.clipCode(line, width)...)
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			out = append(out, "")
			continue
		}
		if m.standalone(line) {
			flush()
			out = append(out, m.proseLine(line, width)...)
			continue
		}
		if len(paragraph) == 0 {
			paragraph = append(paragraph, strings.TrimRight(line, " \t"))
			continue
		}
		paragraph = append(paragraph, strings.TrimSpace(line))
	}
	flush()
	return out
}

// standalone reports whether a line is a block of its own rather than
// paragraph text: a heading, a list item or a quote. It asks blockOf rather
// than repeating its rules, so the two cannot disagree about what a bullet is.
func (m Model) standalone(line string) bool {
	_, marker, body, _ := m.blockOf(line)
	return marker != "" || body != strings.TrimRight(strings.TrimLeft(line, " \t"), " \t")
}

// clipCode lays out a line of a fenced block, breaking rather than reflowing.
func (m Model) clipCode(line string, width int) []string {
	out := breakLine(line, width)
	for i, part := range out {
		out[i] = m.theme.Code.Render(part)
	}
	return out
}

// breakLine splits one line to fit a width without reflowing it. Data is not
// prose: a line of JSON or a directory listing means what its columns say, and
// rewrapping it on word boundaries would rearrange the meaning.
func breakLine(line string, width int) []string {
	line = strings.ReplaceAll(line, "\t", "    ")
	if line == "" {
		return []string{""}
	}
	if width < 1 {
		width = 1
	}
	var out []string
	for rest := []rune(line); len(rest) > 0; {
		take := min(len(rest), width)
		out = append(out, string(rest[:take]))
		rest = rest[take:]
	}
	return out
}

// hardWrap lays out text that is not prose, one line at a time.
func hardWrap(text string, width int) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		out = append(out, breakLine(strings.TrimRight(line, "\r"), width)...)
	}
	return out
}

// proseLine lays out one source line: its block form, then its emphasis, then
// its wrapping.
func (m Model) proseLine(line string, width int) []string {
	if strings.TrimSpace(line) == "" {
		return []string{""}
	}
	indent, marker, body, base := m.blockOf(line)
	spans := m.inline(body, base)
	// A wrapped bullet hangs under its own text rather than under the marker,
	// so the list reads as a list.
	hanging := indent + strings.Repeat(" ", lipgloss.Width(marker))
	return wrapSpans(spans, width, indent+marker, hanging)
}

// blockOf reads the shape of a line: how far it is indented, what marks it as a
// heading or a list item, and the style its text takes.
func (m Model) blockOf(line string) (indent, marker, body string, base lipgloss.Style) {
	trimmed := strings.TrimLeft(line, " \t")
	indent = strings.Repeat(" ", len(line)-len(trimmed))

	if hashes := countPrefix(trimmed, '#'); hashes > 0 && hashes <= 6 &&
		strings.HasPrefix(trimmed[hashes:], " ") {
		return indent, "", strings.TrimSpace(trimmed[hashes:]), m.theme.Heading
	}
	for _, bullet := range []string{"- ", "* ", "+ "} {
		if rest, ok := strings.CutPrefix(trimmed, bullet); ok {
			return indent, "· ", rest, m.theme.Value
		}
	}
	if n, rest, ok := numberedItem(trimmed); ok {
		return indent, n, rest, m.theme.Value
	}
	if rest, ok := strings.CutPrefix(trimmed, "> "); ok {
		return indent, "│ ", rest, m.theme.Faint
	}
	return indent, "", trimmed, m.theme.Value
}

// numberedItem reads "12. text", which markdown uses and braids keeps as
// written: renumbering someone's list is not a rendering decision.
func numberedItem(line string) (marker, rest string, ok bool) {
	digits := 0
	for digits < len(line) && unicode.IsDigit(rune(line[digits])) {
		digits++
	}
	if digits == 0 || !strings.HasPrefix(line[digits:], ". ") {
		return "", "", false
	}
	return line[:digits+2], line[digits+2:], true
}

// isWordRune reports whether a character binds to the word beside it, which is
// what decides whether an underscore is emphasis or part of a name.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func countPrefix(s string, c byte) int {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	return n
}

// inline splits text on emphasis and code markers.
func (m Model) inline(text string, base lipgloss.Style) []span {
	var spans []span
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			spans = append(spans, span{plain.String(), base})
			plain.Reset()
		}
	}
	runes := []rune(text)
	for i := 0; i < len(runes); {
		// Longest marker first, or **bold** is read as two italics.
		for _, mark := range []struct {
			open  string
			style lipgloss.Style
			// intraword is whether the marker may open in the middle of a
			// word. Underscores may not: snake_case_names is a name, and
			// emphasising the middle of it would eat the underscores.
			intraword bool
		}{
			{open: "**", style: m.theme.Strong, intraword: true},
			{open: "__", style: m.theme.Strong},
			{open: "`", style: m.theme.Code, intraword: true},
			{open: "*", style: m.theme.Emphasis, intraword: true},
			{open: "_", style: m.theme.Emphasis},
		} {
			rest := string(runes[i:])
			if !strings.HasPrefix(rest, mark.open) {
				continue
			}
			if !mark.intraword && i > 0 && isWordRune(runes[i-1]) {
				continue
			}
			inner, ok := closedSpan(rest[len(mark.open):], mark.open)
			if !ok {
				continue
			}
			// A closing underscore inside a word is part of the word too.
			if !mark.intraword {
				after := i + len([]rune(mark.open))*2 + len([]rune(inner))
				if after < len(runes) && isWordRune(runes[after]) {
					continue
				}
			}
			flush()
			spans = append(spans, span{inner, mark.style})
			i += len([]rune(mark.open))*2 + len([]rune(inner))
			goto next
		}
		plain.WriteRune(runes[i])
		i++
	next:
	}
	flush()
	return spans
}

// closedSpan finds the text before the closing marker.
//
// It reports false for an unmatched marker, and for one whose content begins or
// ends with a space — that is the rule that keeps "2 * 3 is arithmetic" as
// arithmetic instead of italicising everything up to the next star.
func closedSpan(rest, mark string) (string, bool) {
	end := strings.Index(rest, mark)
	if end <= 0 {
		return "", false
	}
	inner := rest[:end]
	if strings.ContainsAny(inner, "\n") {
		return "", false
	}
	first, _ := utf8.DecodeRuneInString(inner)
	last, _ := utf8.DecodeLastRuneInString(inner)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return "", false
	}
	return inner, true
}

// wrapSpans breaks styled text into lines that each fit the width, keeping
// every style opened and closed within one line.
//
// Words of the same span are gathered and styled together rather than one at a
// time: styling each word separately is correct but emits an escape sequence
// per word, which triples the length of a line of prose for no gain.
func wrapSpans(spans []span, width int, first, hanging string) []string {
	var out []string
	var rendered strings.Builder
	var pending strings.Builder
	current := lipgloss.NewStyle()
	line, room := first, width-lipgloss.Width(first)
	rendered.WriteString(first)

	settle := func() {
		if pending.Len() > 0 {
			rendered.WriteString(current.Render(pending.String()))
			pending.Reset()
		}
	}
	flush := func(prefix string) {
		settle()
		out = append(out, rendered.String())
		rendered.Reset()
		rendered.WriteString(prefix)
		line, room = prefix, width-lipgloss.Width(prefix)
	}

	for _, s := range spans {
		if pending.Len() > 0 {
			settle()
		}
		current = s.style
		for _, word := range splitKeepingSpaces(s.text) {
			w := lipgloss.Width(word)
			switch {
			case strings.TrimSpace(word) == "" && strings.TrimSpace(line) == "":
				continue // no leading space on a fresh line
			case w > room && strings.TrimSpace(line) != "":
				flush(hanging)
				if strings.TrimSpace(word) == "" {
					continue
				}
			}
			line += word
			room -= w
			pending.WriteString(word)
		}
	}
	settle()
	if strings.TrimSpace(line) != "" || len(out) == 0 {
		out = append(out, rendered.String())
	}
	return out
}

// splitKeepingSpaces breaks text into words and the spaces between them, so
// wrapping can drop a space at a line break and keep it anywhere else.
func splitKeepingSpaces(text string) []string {
	var out []string
	var current strings.Builder
	space := false
	for _, r := range text {
		isSpace := r == ' ' || r == '\t'
		if current.Len() > 0 && isSpace != space {
			out = append(out, current.String())
			current.Reset()
		}
		space = isSpace
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}
