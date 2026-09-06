package tui

import (
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
)

// A one-line field you can only edit at the end means fixing the first word of
// a query costs deleting everything after it and typing that again.
func TestTheFieldCanBeEditedAnywhere(t *testing.T) {
	var f filterInput
	f.active = true
	for _, k := range []string{"l", "o", "c", "k"} {
		f.edit(k)
	}
	if f.text != "lock" || f.cursor != 4 {
		t.Fatalf("typing gave %q with the caret at %d", f.text, f.cursor)
	}

	// Back to the start and insert there.
	f.edit("home")
	f.edit("t")
	f.edit("h")
	f.edit("e")
	f.edit("space")
	if f.text != "the lock" {
		t.Errorf("editing the front gave %q", f.text)
	}
	if f.cursor != 4 {
		t.Errorf("the caret is at %d, not after what was just typed", f.cursor)
	}

	// Backspace takes the character before the caret, not the last one.
	f.edit("backspace")
	if f.text != "thelock" {
		t.Errorf("backspace mid-text gave %q", f.text)
	}

	// delete takes the one after it.
	f.edit("delete")
	if f.text != "theock" {
		t.Errorf("delete mid-text gave %q", f.text)
	}
}

func TestTheCaretStaysInsideTheText(t *testing.T) {
	f := typing("abc")
	if f.cursor != 3 {
		t.Fatalf("a field opened with text put the caret at %d, want the end", f.cursor)
	}
	for i := 0; i < 5; i++ {
		f.edit("right")
	}
	if f.cursor != 3 {
		t.Errorf("right ran past the end, to %d", f.cursor)
	}
	for i := 0; i < 5; i++ {
		f.edit("left")
	}
	if f.cursor != 0 {
		t.Errorf("left ran past the start, to %d", f.cursor)
	}
	// Backspace at the start is a no-op, and still consumes the key.
	if !f.edit("backspace") || f.text != "abc" {
		t.Errorf("backspace at the start gave %q", f.text)
	}
	f.edit("end")
	if !f.edit("delete") || f.text != "abc" {
		t.Errorf("delete at the end gave %q", f.text)
	}
}

// Multi-byte characters are one caret step, not one byte.
func TestTheCaretMovesByCharacterNotByte(t *testing.T) {
	f := typing("héllo→")
	f.edit("left")
	f.edit("left")
	f.edit("X")
	if f.text != "héllXo→" {
		t.Errorf("inserting two back from the end gave %q", f.text)
	}
	f.edit("home")
	f.edit("right")
	f.edit("backspace")
	if f.text != "éllXo→" {
		t.Errorf("backspace after one step gave %q", f.text)
	}
}

// Pasting goes in at the caret, like typing does.
func TestPasteLandsAtTheCaret(t *testing.T) {
	f := typing("lock")
	f.edit("home")
	f.paste("the  \n ")
	if f.text != "thelock" {
		t.Errorf("paste at the start gave %q", f.text)
	}
}

// The caret has to stay visible in a field narrower than its contents.
func TestTheViewFollowsTheCaret(t *testing.T) {
	f := typing("a-very-long-conversation-name")
	before, after := f.fit(8)
	if len([]rune(before)) > 8 || before == "" {
		t.Errorf("at the end, the window showed %q + %q", before, after)
	}
	if after != "" {
		t.Errorf("at the end there is nothing after the caret, got %q", after)
	}

	f.edit("home")
	before, after = f.fit(8)
	if before != "" {
		t.Errorf("at the start, %q was shown before the caret", before)
	}
	if got := len([]rune(after)); got != 8 {
		t.Errorf("at the start the window showed %d characters after the caret, want 8", got)
	}
	if total := len([]rune(before)) + len([]rune(after)); total > 8 {
		t.Errorf("the window is %d wide, want at most 8", total)
	}
}

// Through the real key path: the search screen must let the arrows move within
// the query, while up and down still walk the results.
func TestSearchArrowsEditTheQuery(t *testing.T) {
	m := newTestModel(t, forestOf([]index.LaneInfo{
		laneInfo("aaaaaaaa-0000-4000-8000-000000000001", "a conversation", "proj", 4, time.Hour),
	}, nil))
	m = m.openSearch()
	for _, k := range []string{"l", "o", "c", "k"} {
		m = m.searchKey(k)
	}
	m = m.searchKey("left")
	m = m.searchKey("left")
	m = m.searchKey("X")
	if got := m.search.input.text; got != "loXck" {
		t.Errorf("arrows in the search field gave %q, want loXck", got)
	}
	// And the caret is drawn where it is, not at the end.
	before, after := m.search.input.split()
	if before != "loX" || after != "ck" {
		t.Errorf("the caret splits the query as %q|%q", before, after)
	}
}

// On a screen where left means go back, it still does once the field is shut.
func TestLeftStillLeavesWhenTheFieldIsClosed(t *testing.T) {
	var f filterInput
	if f.key("left") {
		t.Error("a closed field swallowed left, so the screen can never go back")
	}
	f.active = true
	if !f.key("left") {
		t.Error("an open field let left through, so typing moves the screen instead of the caret")
	}
}

// The caret is drawn where it is. Every field renders through split or fit, so
// this is what stops the caret being pinned to the end of the text again.
func TestTheCaretIsDrawnWhereItIs(t *testing.T) {
	f := typing("lock")
	if got := caretIn(f, 12); got != "lock▏" {
		t.Errorf("at the end, drew %q", got)
	}
	f.edit("left")
	f.edit("left")
	if got := caretIn(f, 12); got != "lo▏ck" {
		t.Errorf("two back from the end, drew %q", got)
	}
	f.edit("home")
	if got := caretIn(f, 12); got != "▏lock" {
		t.Errorf("at the start, drew %q", got)
	}
}
