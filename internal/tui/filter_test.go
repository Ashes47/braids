package tui

import "testing"

func typeInto(f *filterInput, keys ...string) []bool {
	consumed := make([]bool, 0, len(keys))
	for _, k := range keys {
		consumed = append(consumed, f.key(k))
	}
	return consumed
}

func TestFilterInputTyping(t *testing.T) {
	// Screens open the field with f; the field itself only takes keys once
	// open. It deliberately does not claim "/", which means global search.
	f := filterInput{active: true}
	typeInto(&f, "g", "c", "s")
	if !f.active || f.text != "gcs" {
		t.Fatalf("after typing: active=%v text=%q", f.active, f.text)
	}
	typeInto(&f, "backspace")
	if f.text != "gc" {
		t.Errorf("backspace left %q", f.text)
	}
	typeInto(&f, "enter")
	if f.active || f.text != "gc" {
		t.Errorf("enter should keep the text and leave the field: active=%v text=%q", f.active, f.text)
	}
}

func TestFilterInputEscapePeelsOneLayer(t *testing.T) {
	f := filterInput{active: true}
	typeInto(&f, "a", "b")

	if !f.key("esc") || f.active || f.text != "" {
		t.Fatalf("first esc should leave the field and clear: active=%v text=%q", f.active, f.text)
	}
	// With nothing left to clear, esc declines so the screen can handle it.
	if f.key("esc") {
		t.Error("esc on an empty filter should not be consumed")
	}

	// Typed, then committed: esc clears before declining.
	f.active = true
	typeInto(&f, "x", "enter")
	if !f.key("esc") || f.text != "" {
		t.Errorf("esc should clear a committed filter, got %q", f.text)
	}
	if f.key("esc") {
		t.Error("a second esc should fall through")
	}
}

func TestFilterInputPassesKeysThroughWhenInactive(t *testing.T) {
	var f filterInput
	for _, k := range []string{"j", "q", "enter", "backspace", "n"} {
		if f.key(k) {
			t.Errorf("inactive filter consumed %q", k)
		}
	}
	// An inactive field never opens itself, not even on "/": a field that
	// opens itself swallows every keystroke after it, invisibly.
	if f.key("/") || f.active {
		t.Error("an inactive filter opened itself on /")
	}
	// Once open, a slash is a character like any other.
	f.active = true
	typeInto(&f, "a", "/")
	if f.text != "a/" {
		t.Errorf("text = %q, want a slash to be typed literally", f.text)
	}
}

func TestFilterMatchesIsCaseInsensitive(t *testing.T) {
	f := filterInput{text: "BlobStore"}
	if !f.matches("the blobstore mount") {
		t.Error("filter should ignore case")
	}
	if f.matches("something else") {
		t.Error("unrelated text should not match")
	}
	if !(filterInput{}).matches("anything") {
		t.Error("an empty filter matches everything")
	}
}
