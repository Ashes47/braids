package index

import (
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/model"
)

var noon = time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)

func TestParseQueryPullsFiltersOutOfTheWords(t *testing.T) {
	q := ParseQuery("project:braids since:30d kind:text lock across a call", noon)
	if q.Text != "lock across a call" {
		t.Errorf("text = %q, want the words with the filters taken out", q.Text)
	}
	if q.Project != "braids" {
		t.Errorf("project = %q", q.Project)
	}
	if want := noon.AddDate(0, 0, -30); !q.Since.Equal(want) {
		t.Errorf("since = %v, want %v", q.Since, want)
	}
	if len(q.Kinds) != 1 || q.Kinds[0] != model.PartText {
		t.Errorf("kinds = %v", q.Kinds)
	}
}

// The field searches on every keystroke, so halfway through typing `since:30d`
// you have `since:3`. A parser that failed there would blank the screen while
// somebody is typing, so a filter it cannot read stays a word.
func TestAHalfTypedFilterIsJustAWord(t *testing.T) {
	for _, text := range []string{
		"since:3 lock", "since: lock", "project: lock", "until:soon lock",
		"kind:nonsense lock", "type:whatever lock", "nonsense:x lock",
		"http://example.invalid lock", "ratio 3:1 lock",
	} {
		q := ParseQuery(text, noon)
		if q.Text != text {
			t.Errorf("ParseQuery(%q) took something out: text = %q", text, q.Text)
		}
		if q.Project != "" || !q.Since.IsZero() || !q.Until.IsZero() ||
			len(q.Kinds) != 0 || len(q.Types) != 0 {
			t.Errorf("ParseQuery(%q) invented a filter: %+v", text, q)
		}
	}
}

func TestParseQueryReadsDatesAndAges(t *testing.T) {
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	q := ParseQuery("since:2026-08-01 until:2026-08-01 lock", noon)
	if !q.Since.Equal(day) {
		t.Errorf("since = %v, want the start of the day", q.Since)
	}
	// A bare date on both bounds is that whole day, not an empty range.
	if !q.Until.After(q.Since) || q.Until.Sub(q.Since) < 23*time.Hour {
		t.Errorf("until = %v, want the end of the same day", q.Until)
	}
	// The words people also use.
	if a := ParseQuery("after:30d x", noon); a.Since.IsZero() {
		t.Error("after: was not read as since:")
	}
	if b := ParseQuery("before:2026-08-01 x", noon); b.Until.IsZero() {
		t.Error("before: was not read as until:")
	}
	// And the key is not case sensitive, since it is typed in a hurry.
	if c := ParseQuery("Project:braids x", noon); c.Project != "braids" {
		t.Errorf("Project: was not read: %+v", c)
	}
}

func TestWhenRefusesWhatItCannotRead(t *testing.T) {
	for _, bad := range []string{"", "yesterday", "5", "3y", "-2d", "d", "2026-13-01"} {
		if _, ok := When(bad, noon, false); ok {
			t.Errorf("When(%q) was accepted", bad)
		}
	}
}

func TestParseKindsAndTypes(t *testing.T) {
	got, err := ParseKinds(" text , tool_use ")
	if err != nil {
		t.Fatalf("ParseKinds: %v", err)
	}
	want := []model.PartKind{model.PartText, model.PartToolUse}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ParseKinds = %v, want %v", got, want)
	}
	if k, err := ParseKinds("   "); err != nil || k != nil {
		t.Errorf("blank kinds = %v, %v; want nil, nil", k, err)
	}
	if _, err := ParseKinds("nonsense"); err == nil {
		t.Error("an unknown kind was accepted")
	}

	types, err := ParseTypes("memory,artifact")
	if err != nil || len(types) != 2 {
		t.Errorf("ParseTypes = %v, %v", types, err)
	}
	if _, err := ParseTypes("conversationz"); err == nil {
		t.Error("an unknown type was accepted")
	}
}
