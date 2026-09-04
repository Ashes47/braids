// Package hooks records what a harness reports about a live session.
//
// It exists for one thing the transcripts cannot say. A session running a tool
// and a session waiting for permission to run one are the same record — an
// assistant turn with no result — so the files can only call both "working". A
// hook fires the moment a session blocks, which is the difference between a
// switchboard and a good guess.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Event is one thing the harness reported. Only the fields braids acts on are
// named; the rest of the payload is deliberately not modelled, because the
// shape differs per event and per version and guessing at it would age badly.
type Event struct {
	At      time.Time `json:"at"`
	Name    string    `json:"event"`
	Session string    `json:"session"`
	Tool    string    `json:"tool,omitempty"`
	Cwd     string    `json:"cwd,omitempty"`
}

// The events braids acts on. Anything else is recorded and ignored.
const (
	// Blocked means the session is waiting on a person.
	PermissionRequest = "PermissionRequest"
	Notification      = "Notification"
	// Stop means a turn finished, so the person is owed a reply.
	Stop = "Stop"
	// SessionStart and SessionEnd bracket a live session.
	SessionStart = "SessionStart"
	SessionEnd   = "SessionEnd"
	SubagentStop = "SubagentStop"
)

// Wanted lists the events braids asks the harness to report.
func Wanted() []string {
	return []string{PermissionRequest, Notification, Stop, SubagentStop, SessionStart, SessionEnd}
}

// Record reads a hook payload and appends what braids needs from it.
//
// It never fails loudly: a hook that errors is reported by the harness in the
// middle of someone's session, and nothing braids observes is worth
// interrupting work for.
func Record(path string, in io.Reader) error {
	body, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return fmt.Errorf("read hook payload: %w", err)
	}
	var payload struct {
		Event   string `json:"hook_event_name"`
		Session string `json:"session_id"`
		Tool    string `json:"tool_name"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse hook payload: %w", err)
	}
	if payload.Session == "" {
		return errors.New("hook payload names no session")
	}
	return Append(path, Event{
		At:      time.Now(),
		Name:    payload.Event,
		Session: payload.Session,
		Tool:    payload.Tool,
		Cwd:     payload.Cwd,
	})
}

// Append adds one event to the log.
//
// The log is a file rather than a socket so that a hook works whether or not
// braids is running: events that arrive while it is closed are still there when
// it opens.
func Append(path string, e Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // the write below is what matters
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	return nil
}

// Read returns the events in the log, oldest first. A malformed line is skipped
// rather than failing the read: the log is appended to by a hook running in
// someone else's session, and a torn write must not blind the map.
func Read(path string) ([]Event, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var out []Event
	for _, line := range splitLines(body) {
		var e Event
		if json.Unmarshal(line, &e) != nil || e.Session == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func splitLines(body []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i <= len(body); i++ {
		if i == len(body) || body[i] == '\n' {
			if i > start {
				out = append(out, body[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// Latest reduces the log to the most recent event per session, which is all the
// map needs: what each conversation is waiting on now.
func Latest(events []Event) map[string]Event {
	out := make(map[string]Event, len(events))
	for _, e := range events {
		if prev, ok := out[e.Session]; ok && prev.At.After(e.At) {
			continue
		}
		out[e.Session] = e
	}
	return out
}

// Blocked reports whether an event means the session is waiting on a person.
func Blocked(e Event) bool {
	return e.Name == PermissionRequest || e.Name == Notification
}
