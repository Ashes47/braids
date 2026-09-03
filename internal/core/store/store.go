package store

import (
	"context"

	"github.com/Ashes47/braids/internal/core/model"
)

// Capabilities declares the optional features a Source provides. braids
// degrades rather than fails when one is absent: Claude Code offers all of
// them, a flat-log harness such as Codex offers none.
type Capabilities struct {
	// InFileBranching reports whether one transcript can itself contain
	// branch points, rather than branches only existing as separate lanes.
	InFileBranching bool
	// Subagents reports whether nested agent conversations are recorded.
	Subagents bool
	// Compaction reports whether context-compaction events are recorded.
	Compaction bool
	// StableIDs reports whether a fork reuses the parent's message IDs, which
	// makes fork detection exact rather than fingerprint-based.
	StableIDs bool
}

// Visit receives each Message of a lane in file order. Returning a non-nil
// error stops iteration and is returned by Messages.
type Visit func(model.Message) error

// Source reads one agent harness's local transcripts. Implementations live in
// subpackages; nothing above this package may assume a particular harness.
type Source interface {
	// Name is the harness identifier, e.g. "claudecode".
	Name() string
	// Capabilities describes what this Source can offer.
	Capabilities() Capabilities
	// Lanes enumerates every conversation the Source can see.
	Lanes(ctx context.Context) ([]model.Lane, error)
	// Messages streams one lane's messages in file order.
	Messages(ctx context.Context, lane model.Lane, visit Visit) error
}
