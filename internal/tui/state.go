package tui

import (
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// runningWindow is how recently a lane must have been written to for an
// outstanding tool call to read as running rather than abandoned.
const runningWindow = 2 * time.Minute

// freshWindow is how recently a conversation must have finished for it to feel
// like something still owed a reply.
const freshWindow = 6 * time.Hour

// laneState is what a conversation is waiting on.
type laneState string

const (
	// stateNeedsYou: the session said it is waiting on a person. Only a hook
	// can report this; the transcript alone cannot tell it from working.
	stateNeedsYou laneState = "needs you"
	// stateYourTurn: the assistant answered and nobody has replied.
	stateYourTurn laneState = "your turn"
	// stateWorking: a tool call is outstanding and the file is still moving.
	stateWorking laneState = "working"
	// stateStopped: a tool call is outstanding but nothing has moved since —
	// the session was interrupted mid-call.
	stateStopped laneState = "stopped"
	// stateThinking: the last turn is the person's and the file is still
	// moving, so a reply is in flight.
	stateThinking laneState = "thinking"
	// stateUnanswered: the last turn is a prompt nobody ever answered. Cutting
	// a branch at a question leaves one, so this reads as "never resumed".
	stateUnanswered laneState = "unanswered"
	// stateEmpty: nothing has been said yet.
	stateEmpty laneState = "empty"
)

// stateOf reads a conversation's state from its last turn and how long ago it
// was written.
//
// Files cannot distinguish a tool that is running from one waiting for
// permission — both are an unanswered call. Saying "working" is the honest
// reading; a hook is what would let braids say "needs you".
// staleBlock is how far the transcript may move past a reported block before
// the file is believed instead. A session that carried on without saying so —
// because the hook was removed, or the session predates it — should not look
// blocked forever.
const staleBlock = 30 * time.Second

func stateOf(lane index.LaneInfo, live *hooks.Event, now time.Time) laneState {
	// A session that said it is waiting outranks anything inferred, for as
	// long as the transcript has not moved on past it.
	if live != nil && hooks.Blocked(*live) && lane.Updated.Sub(live.At) < staleBlock {
		return stateNeedsYou
	}
	switch {
	case lane.Messages == 0 || lane.Activity.LastRole == "":
		return stateEmpty
	case lane.Activity.LastRole == model.RoleUser && now.Sub(lane.Updated) < runningWindow:
		return stateThinking
	case lane.Activity.LastRole == model.RoleUser:
		return stateUnanswered
	case lane.Activity.LastWasToolCall && now.Sub(lane.Updated) < runningWindow:
		return stateWorking
	case lane.Activity.LastWasToolCall:
		return stateStopped
	default:
		return stateYourTurn
	}
}

// waiting reports whether a conversation is owed something by a person.
func waiting(lane index.LaneInfo, live *hooks.Event, now time.Time) bool {
	switch stateOf(lane, live, now) {
	case stateNeedsYou:
		return true
	case stateYourTurn:
		return now.Sub(lane.Updated) < freshWindow
	case stateStopped, stateUnanswered:
		// Both are open loops that go nowhere until a person resumes them.
		return true
	default:
		return false
	}
}

// styleFor colours a state.
//
// The scale is deliberately steep. A session stopped and waiting on a person is
// the only thing that gets the loud treatment, because it is the only thing
// that cannot proceed without them. Anything alive is green. Something owed a
// reply is plain text — there are usually a great many of those, and a screen
// where everything is urgent says nothing.
func (m Model) styleFor(lane index.LaneInfo, state laneState) lipgloss.Style {
	switch state {
	case stateNeedsYou:
		return m.theme.Urgent
	case stateWorking, stateThinking:
		return m.theme.Alive
	case stateStopped, stateUnanswered:
		return m.theme.Accent
	case stateYourTurn:
		if m.now().Sub(lane.Updated) < freshWindow {
			return m.theme.Value
		}
		return m.theme.Faint
	default:
		return m.theme.Faint
	}
}
