package tui

import (
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

func lastTurn(role model.Role, tool bool, msgs int, ago time.Duration) index.LaneInfo {
	l := index.LaneInfo{Messages: msgs}
	l.Updated = now.Add(-ago)
	l.Activity = model.Activity{LastRole: role, LastWasToolCall: tool}
	return l
}

func TestStateOf(t *testing.T) {
	tests := []struct {
		name string
		lane index.LaneInfo
		want laneState
	}{
		{
			"the assistant answered and nobody replied",
			lastTurn(model.RoleAssistant, false, 10, time.Minute),
			stateYourTurn,
		},
		{
			"an old answer is still the person's turn",
			lastTurn(model.RoleAssistant, false, 10, 30*24*time.Hour),
			stateYourTurn,
		},
		{
			"an outstanding tool call on a moving file is running",
			lastTurn(model.RoleAssistant, true, 10, 10*time.Second),
			stateWorking,
		},
		{
			"an outstanding tool call on a still file was interrupted",
			lastTurn(model.RoleAssistant, true, 10, time.Hour),
			stateStopped,
		},
		{
			"a fresh prompt means a reply is in flight",
			lastTurn(model.RoleUser, false, 10, 10*time.Second),
			stateThinking,
		},
		{
			"a stale prompt was never answered — a branch nobody resumed",
			lastTurn(model.RoleUser, false, 3, time.Hour),
			stateUnanswered,
		},
		{"nothing said yet", index.LaneInfo{}, stateEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stateOf(tt.lane, nil, now); got != tt.want {
				t.Errorf("stateOf = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWaitingIsOnlyOpenLoops(t *testing.T) {
	tests := []struct {
		name string
		lane index.LaneInfo
		want bool
	}{
		{"a recent answer is owed a reply", lastTurn(model.RoleAssistant, false, 5, time.Hour), true},
		{"a month-old answer is not a queue item", lastTurn(model.RoleAssistant, false, 5, 30*24*time.Hour), false},
		{"an interrupted tool call needs a person", lastTurn(model.RoleAssistant, true, 5, time.Hour), true},
		{"a branch nobody resumed needs a person", lastTurn(model.RoleUser, false, 2, time.Hour), true},
		{"work in progress does not", lastTurn(model.RoleAssistant, true, 5, time.Second), false},
		{"a reply in flight does not", lastTurn(model.RoleUser, false, 5, time.Second), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := waiting(tt.lane, nil, now); got != tt.want {
				t.Errorf("waiting = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAReportedBlockOutranksWhatTheFilesShow(t *testing.T) {
	// A tool call outstanding on a moving file reads as working, which is all
	// the transcript can say. A session that reported it is waiting says more.
	lane := lastTurn(model.RoleAssistant, true, 10, 10*time.Second)
	if got := stateOf(lane, nil, now); got != stateWorking {
		t.Fatalf("without a report this is %q, want working", got)
	}

	blocked := &hooks.Event{Name: hooks.PermissionRequest, At: lane.Updated}
	if got := stateOf(lane, blocked, now); got != stateNeedsYou {
		t.Errorf("with a report it is %q, want needs you", got)
	}
	if !waiting(lane, blocked, now) {
		t.Error("a session waiting on a person is owed something")
	}
}

func TestAStaleBlockGivesWayToTheFiles(t *testing.T) {
	// The session carried on without reporting it — the hook was removed, or
	// the session predates it. The transcript is then the better witness.
	lane := lastTurn(model.RoleAssistant, false, 10, time.Minute)
	old := &hooks.Event{Name: hooks.PermissionRequest, At: lane.Updated.Add(-time.Hour)}
	if got := stateOf(lane, old, now); got != stateYourTurn {
		t.Errorf("state = %q, want the file to win once the block is stale", got)
	}
}

func TestOnlyBlockingEventsChangeTheState(t *testing.T) {
	lane := lastTurn(model.RoleAssistant, true, 10, 10*time.Second)
	for _, name := range []string{hooks.Stop, hooks.SessionStart, hooks.SubagentStop} {
		e := &hooks.Event{Name: name, At: lane.Updated}
		if got := stateOf(lane, e, now); got != stateWorking {
			t.Errorf("%s changed the state to %q, want working", name, got)
		}
	}
}
