package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func proseModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, Options{ASCII: true, Source: "claudecode"})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 24
	return m
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// styledParts returns the text that each escape sequence introduces, so a test
// can assert what is emphasised rather than how.
func styledParts(line string) map[string]string {
	out := map[string]string{}
	for _, match := range regexp.MustCompile(`\x1b\[([0-9;]*)m([^\x1b]*)`).FindAllStringSubmatch(line, -1) {
		if strings.TrimSpace(match[2]) != "" {
			out[strings.TrimSpace(match[2])] = match[1]
		}
	}
	return out
}

func TestProseRendersEmphasisRatherThanItsMarkers(t *testing.T) {
	m := proseModel(t)
	lines := m.prose("A **bold phrase** and an *italic one* and `some code`.", 74)
	if len(lines) != 1 {
		t.Fatalf("wrapped to %d lines: %q", len(lines), lines)
	}
	plainText := escapes.ReplaceAllString(lines[0], "")
	// The markers are gone; the words remain.
	for _, gone := range []string{"**", "`", "*"} {
		if strings.Contains(plainText, gone) {
			t.Errorf("marker %q survived into the text: %q", gone, plainText)
		}
	}
	for _, kept := range []string{"bold phrase", "italic one", "some code"} {
		if !strings.Contains(plainText, kept) {
			t.Errorf("text %q was lost: %q", kept, plainText)
		}
	}
	parts := styledParts(lines[0])
	if !strings.Contains(parts["bold phrase"], "1") {
		t.Errorf("the bold phrase is not bold: %q", parts["bold phrase"])
	}
	if !strings.Contains(parts["italic one"], "3") {
		t.Errorf("the italic phrase is not italic: %q", parts["italic one"])
	}
	// A whole phrase is styled once rather than word by word, which is what
	// keeps a line of prose from tripling in length.
	if !strings.Contains(lines[0], m.theme.Strong.Render("bold phrase")) {
		t.Error("the bold phrase is styled in pieces rather than as one run")
	}
	if !strings.Contains(lines[0], m.theme.Code.Render("some code")) {
		t.Error("the code span is styled in pieces rather than as one run")
	}
}

// The rule that keeps arithmetic arithmetic: a marker followed by a space is
// punctuation the writer meant.
func TestProseLeavesUnmatchedMarkersAlone(t *testing.T) {
	m := proseModel(t)
	for _, text := range []string{
		"An unmatched * stays a star, and 2 * 3 is arithmetic.",
		"A lone ** means nothing here.",
		"snake_case_names stay whole",
	} {
		lines := m.prose(text, 74)
		got := escapes.ReplaceAllString(strings.Join(lines, " "), "")
		if got != text {
			t.Errorf("prose(%q) = %q, want it unchanged", text, got)
		}
	}
}

func TestProseHandlesBlocksAndWrapping(t *testing.T) {
	m := proseModel(t)
	src := "## A heading\n\n" +
		"- **First** item that runs on long enough that it has to wrap onto a second line of the frame\n" +
		"3. A numbered one\n" +
		"> a quoted aside\n\n" +
		"```\nverbatim   spacing    preserved\n```\n"
	lines := m.prose(src, 40)

	joined := escapes.ReplaceAllString(strings.Join(lines, "\n"), "")
	if strings.Contains(joined, "```") {
		t.Error("the code fence markers were shown to the reader")
	}
	if strings.Contains(joined, "## ") {
		t.Error("the heading markers were shown to the reader")
	}
	if !strings.Contains(joined, "verbatim   spacing    preserved") {
		t.Errorf("code was reflowed:\n%s", joined)
	}
	if !strings.Contains(joined, "· First item") {
		t.Errorf("the bullet is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "3. A numbered one") {
		t.Errorf("the number was not kept:\n%s", joined)
	}
	if !strings.Contains(joined, "│ a quoted aside") {
		t.Errorf("the quote marker is missing:\n%s", joined)
	}
	// A wrapped bullet hangs under its own text, not under the marker.
	var wrapped string
	for _, line := range lines {
		plainLine := escapes.ReplaceAllString(line, "")
		if strings.HasPrefix(plainLine, "  ") && strings.Contains(plainLine, "wrap") {
			wrapped = plainLine
		}
	}
	if wrapped == "" {
		t.Errorf("the long bullet did not wrap with a hanging indent:\n%s", joined)
	}
	// Nothing exceeds the frame.
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 40 {
			t.Errorf("line is %d columns, frame is 40: %q", got, escapes.ReplaceAllString(line, ""))
		}
	}
}

// An empty frame is drawn, not crashed into.
func TestProseToleratesNothing(t *testing.T) {
	m := proseModel(t)
	if got := m.prose("", 40); len(got) != 1 || got[0] != "" {
		t.Errorf("prose of nothing = %q", got)
	}
	for width := 1; width < 12; width++ {
		if lines := m.prose("some words here to lay out", width); len(lines) == 0 {
			t.Errorf("width %d produced nothing", width)
		}
	}
}

// A paragraph is one piece of markdown even when it is typed across several
// lines, so emphasis may open on one line and close on the next. Read line by
// line, the marks never match and the reader shows the asterisks.
func TestEmphasisSpanningALineBreak(t *testing.T) {
	m := proseModel(t)
	rendered := m.prose("The rule: **narrow the\nlock to what it protects**, always.", 60)
	got := escapes.ReplaceAllString(strings.Join(rendered, "\n"), "")
	if strings.Contains(got, "*") {
		t.Errorf("emphasis across a line break left its marks:\n%s", got)
	}
	if !strings.Contains(got, "narrow the lock to what it protects") {
		t.Errorf("the paragraph did not reflow:\n%s", got)
	}
}

// A heading, a bullet and a quote each stand alone: joining them into the
// paragraph above would turn a list into a sentence.
func TestBlocksAreNotJoinedIntoAParagraph(t *testing.T) {
	m := proseModel(t)
	lines := m.prose("Some prose here.\n## A heading\n- first\n- second\n> quoted", 60)
	if len(lines) < 5 {
		t.Errorf("blocks were joined together:\n%s", strings.Join(lines, "\n"))
	}
}
