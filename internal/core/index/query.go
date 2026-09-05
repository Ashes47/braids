package index

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Ashes47/braids/internal/core/model"
)

// ParseQuery pulls filters out of a query the way somebody types one.
//
// The command line has flags for these, which is right for a script and wrong
// for a person mid-sentence in a search field. So `project:braids since:30d
// lock` narrows exactly as the flags do, and the same text works in both
// places: whatever you type on one can be pasted into the other.
//
// A word that looks like a filter but is not readable as one stays in the
// query. That matters more than it sounds, because the field searches on every
// keystroke: halfway through typing `since:30d` you have `since:3`, and a
// parser that failed there would blank the screen while you type.
func ParseQuery(text string, now time.Time) Query {
	var (
		q     Query
		words []string
	)
	for _, word := range strings.Fields(text) {
		key, value, ok := strings.Cut(word, ":")
		if !ok || value == "" {
			words = append(words, word)
			continue
		}
		if !apply(&q, strings.ToLower(key), value, now) {
			words = append(words, word)
		}
	}
	q.Text = strings.Join(words, " ")
	return q
}

// apply sets one filter, and reports whether it understood the word at all.
func apply(q *Query, key, value string, now time.Time) bool {
	switch key {
	case "project":
		q.Project = value
	case "lane":
		q.Lane = value
	case "since", "after":
		when, ok := When(value, now, false)
		if !ok {
			return false
		}
		q.Since = when
	case "until", "before":
		when, ok := When(value, now, true)
		if !ok {
			return false
		}
		q.Until = when
	case "kind":
		kinds, err := ParseKinds(value)
		if err != nil {
			return false
		}
		q.Kinds = append(q.Kinds, kinds...)
	case "type", "is":
		types, err := ParseTypes(value)
		if err != nil {
			return false
		}
		q.Types = append(q.Types, types...)
	default:
		return false
	}
	return true
}

// When reads a point in time the way people type one at a terminal: either a
// date, or how long ago. A bare date means the whole of that day, so a search
// bounded above and below by the same date is that day and not an empty range.
func When(text string, now time.Time, endOfDay bool) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, false
	}
	if day, err := time.ParseInLocation("2006-01-02", text, time.Local); err == nil {
		if endOfDay {
			return day.AddDate(0, 0, 1).Add(-time.Second), true
		}
		return day, true
	}
	if stamp, err := time.Parse(time.RFC3339, text); err == nil {
		return stamp, true
	}
	if ago, ok := age(text); ok {
		return now.Add(-ago), true
	}
	return time.Time{}, false
}

// age reads "30d", "6w", "12h", "45m". Go's own parser stops at hours, and
// days are what anyone searching their own history counts in.
func age(text string) (time.Duration, bool) {
	if len(text) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(text[:len(text)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	switch text[len(text)-1] {
	case 'm':
		return time.Duration(n) * time.Minute, true
	case 'h':
		return time.Duration(n) * time.Hour, true
	case 'd':
		return time.Duration(n) * 24 * time.Hour, true
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, true
	}
	return 0, false
}

// ParseKinds validates the --kind flag, refusing unknown kinds rather than
// silently returning nothing.
func ParseKinds(s string) ([]model.PartKind, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	valid := map[string]model.PartKind{
		string(model.PartText):       model.PartText,
		string(model.PartThinking):   model.PartThinking,
		string(model.PartToolUse):    model.PartToolUse,
		string(model.PartToolResult): model.PartToolResult,
	}
	var kinds []model.PartKind
	for _, raw := range strings.Split(s, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		k, ok := valid[name]
		if !ok {
			return nil, fmt.Errorf("unknown kind %q (want text, thinking, tool_use or tool_result)", name)
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

// ParseTypes reads the --type list: what sort of thing to look in. Empty means
// all of them, so a plain search stays global.
func ParseTypes(s string) ([]Found, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	valid := map[string]Found{
		string(FoundTurn):     FoundTurn,
		string(FoundMemory):   FoundMemory,
		string(FoundArtifact): FoundArtifact,
	}
	var types []Found
	for _, raw := range strings.Split(s, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		t, ok := valid[name]
		if !ok {
			return nil, fmt.Errorf("unknown type %q (want conversation, memory or artifact)", name)
		}
		types = append(types, t)
	}
	return types, nil
}
