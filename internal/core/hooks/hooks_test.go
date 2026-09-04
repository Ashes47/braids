package hooks

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordKeepsWhatBraidsActsOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "events.jsonl")
	payload := `{"hook_event_name":"PermissionRequest","session_id":"abc","tool_name":"Bash",
	             "cwd":"/repo","transcript_path":"/x.jsonl","tool_input":{"command":"rm -rf /"}}`
	if err := Record(path, strings.NewReader(payload)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	events, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Name != "PermissionRequest" || e.Session != "abc" || e.Tool != "Bash" || e.Cwd != "/repo" {
		t.Errorf("event = %+v", e)
	}
	if e.At.IsZero() {
		t.Error("an event with no time cannot be weighed against the transcript")
	}
}

func TestRecordRefusesAPayloadWithNoSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := Record(path, strings.NewReader(`{"hook_event_name":"Stop"}`)); err == nil {
		t.Error("an event that names no session is of no use")
	}
	if err := Record(path, strings.NewReader("not json")); err == nil {
		t.Error("want an error for a payload that cannot be parsed")
	}
}

func TestReadSkipsATornLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	for _, e := range []Event{
		{At: time.Now(), Name: Stop, Session: "one"},
		{At: time.Now(), Name: Stop, Session: "two"},
	} {
		if err := Append(path, e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// A hook writing from another session can leave a half-written line.
	if err := Append(path, Event{At: time.Now(), Name: Stop, Session: "three"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	events, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
}

func TestReadAMissingLogIsEmpty(t *testing.T) {
	events, err := Read(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil || events != nil {
		t.Errorf("Read on a missing log = %v, %v", events, err)
	}
}

func TestLatestKeepsTheNewestPerSession(t *testing.T) {
	base := time.Now()
	latest := Latest([]Event{
		{At: base, Name: PermissionRequest, Session: "a"},
		{At: base.Add(time.Second), Name: Stop, Session: "a"},
		{At: base, Name: PermissionRequest, Session: "b"},
		{At: base.Add(-time.Minute), Name: Stop, Session: "b"},
	})
	if latest["a"].Name != Stop {
		t.Errorf("session a = %q, want the newer event", latest["a"].Name)
	}
	if latest["b"].Name != PermissionRequest {
		t.Errorf("session b = %q, want the newer event even when it came first in the log", latest["b"].Name)
	}
}

func TestBlocked(t *testing.T) {
	for _, name := range []string{PermissionRequest, Notification} {
		if !Blocked(Event{Name: name}) {
			t.Errorf("%s should mean the session is waiting on a person", name)
		}
	}
	for _, name := range []string{Stop, SessionStart, SessionEnd, SubagentStop, ""} {
		if Blocked(Event{Name: name}) {
			t.Errorf("%s should not", name)
		}
	}
}
