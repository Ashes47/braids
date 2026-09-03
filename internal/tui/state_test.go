package tui

import (
	"testing"
	"time"

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
			if got := stateOf(tt.lane, now); got != tt.want {
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
			if got := waiting(tt.lane, now); got != tt.want {
				t.Errorf("waiting = %v, want %v", got, tt.want)
			}
		})
	}
}
