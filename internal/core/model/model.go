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
	// Text is the searchable rendering of the block. For PartToolUse it is the
	// serialised tool input.
	Text string
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

// Lane is one linear conversation as a harness stores it. A branch is simply
// another Lane; braids never treats them as different things.
type Lane struct {
	ID      string
	Source  string
	Project string
	Path    string
	Title   string
	Updated time.Time
	Size    int64
}
