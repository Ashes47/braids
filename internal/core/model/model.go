// Package model defines the harness-neutral types every braids component
// speaks. Nothing here may reference a specific agent's on-disk format.
package model

import (
	"strings"
	"time"
)

// Role identifies who produced a Message.
type Role string

// The roles a Message may carry.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// PartKind classifies one content block of a Message.
type PartKind string

// The kinds of content a Message may contain.
const (
	PartText       PartKind = "text"
	PartThinking   PartKind = "thinking"
	PartToolUse    PartKind = "tool_use"
	PartToolResult PartKind = "tool_result"
)

// Part is one content block of a Message.
type Part struct {
	Kind PartKind
	// Tool is the tool name, set only for PartToolUse.
	Tool string
	// ID identifies a tool call: the call's own id on a PartToolUse, and the
	// id of the call being answered on a PartToolResult. It is what joins a
	// subagent to the turn that spawned it.
	ID string
	// Text is the searchable rendering of the block. For PartToolUse it is the
	// serialised tool input.
	Text string
	// IsError marks a tool call that came back a failure.
	IsError bool
}

// Failed reports whether any tool call in a turn came back a failure.
func (m Message) Failed() bool {
	for _, p := range m.Parts {
		if p.IsError {
			return true
		}
	}
	return false
}

// Compaction is what a context compaction discarded.
//
// It is not a turn but a hole where turns used to be: the conversation carries
// on from a summary, and everything behind it stops being sent to the model.
// The transcript still holds all of it, which is what makes branching from
// before a compaction worth offering.
type Compaction struct {
	// Trigger is "auto" when the harness ran out of room, or "manual".
	Trigger string
	// PreTokens and PostTokens bracket the compaction; Dropped is what was let
	// go across the whole conversation.
	PreTokens, PostTokens, Dropped int
	Duration                       time.Duration
}

// Message is one record in a Lane. ParentID is the *logical* parent: a Source
// is responsible for stitching over any boundary its harness introduces, so
// walking ParentID always yields the true conversation.
type Message struct {
	ID       string
	ParentID string
	LaneID   string
	Role     Role
	At       time.Time
	Parts    []Part
	// Compaction is set on the summary that opens a compacted stretch, naming
	// what the compaction let go.
	Compaction *Compaction
}

// Text concatenates the text of every part matching kinds. With no kinds given
// it returns the text of all parts.
func (m Message) Text(kinds ...PartKind) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if len(kinds) > 0 && !containsKind(kinds, p.Kind) {
			continue
		}
		if p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

func containsKind(kinds []PartKind, k PartKind) bool {
	for _, want := range kinds {
		if want == k {
			return true
		}
	}
	return false
}

// Origin records where a lane actually came from.
//
// Inference cannot always tell: two lanes can hold byte-identical prefixes, so
// a third cut from either is indistinguishable by content. When braids makes
// the branch itself it knows the answer, and recording it beats guessing.
type Origin struct {
	Parent  string `json:"parent"`
	ForkSeq int    `json:"forkSeq"`
}

// Activity is what a conversation was doing when it was last written to.
//
// It is derived from the last turn alone, which is enough to tell the states
// that matter: a conversation whose last turn is the assistant answering is
// waiting on a person, while one whose last turn is the assistant calling a
// tool has no result yet — the harness appends the result as a later turn, so
// its absence means the call is still outstanding.
type Activity struct {
	// LastRole is who spoke last.
	LastRole Role
	// LastWasToolCall reports whether that turn ended in an unanswered tool
	// call. Files cannot say whether it is running or waiting for permission:
	// both look identical until a hook says otherwise.
	LastWasToolCall bool
}

// Subagent is a conversation a lane spawned and then collapsed into a single
// tool call. The harness stores it as a transcript of its own, so the parent
// shows one call where a whole exchange happened.
type Subagent struct {
	ID     string
	LaneID string
	// Type is the kind of agent, such as "Explore".
	Type string
	// Task is the one-line description it was given.
	Task string
	// ToolUseID joins it to the turn in the parent that spawned it.
	ToolUseID string
	// Depth is 1 for an agent spawned by the conversation itself, higher for
	// one spawned by another agent.
	Depth    int
	Path     string
	Messages int
}

// Lane is one linear conversation as a harness stores it. A branch is simply
// another Lane; braids never treats them as different things.
type Lane struct {
	ID      string
	Source  string
	Project string
	Path    string
	Title   string
	// Cwd is the directory the conversation ran in. Resuming from anywhere else
	// files the transcript under a different project, so a launcher needs it.
	Cwd string
	// Created is when the transcript file itself came into existence. It is
	// the only reliable evidence of which of two lanes forked from the other,
	// because a fork copies the parent's records — timestamps included — so
	// nothing inside the file can distinguish them. Zero when the platform
	// does not report a birth time.
	Created time.Time
	Updated time.Time
	Size    int64
	// ArtifactBytes is what the conversation's work products occupy: the
	// scratch files and job records a harness keeps outside the transcript.
	// They are usually far larger than the conversation, and discarding them
	// is a different decision from discarding it.
	ArtifactBytes int64
	// ArtifactPath is where those work products live, empty when there are none.
	ArtifactPath string
	// Activity is what the conversation was doing when last written to.
	Activity Activity
}
