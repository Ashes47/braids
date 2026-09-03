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
	// Branching reports whether the Source can cut a new lane from an existing
	// one, i.e. whether it also implements Brancher.
	Branching bool
}

// BranchRequest describes a new lane cut from an existing one.
type BranchRequest struct {
	// Lane is the conversation to branch from.
	Lane model.Lane
	// AtMessage is the turn to branch at. Everything from the root up to and
	// including it is carried over; everything after it is left behind.
	AtMessage string
	// Name is the display name for the new lane.
	Name string
}

// Enricher fills in the lane details that cost a read of the transcript, such
// as its title and working directory.
//
// It is separate from Lanes because listing should cost a directory scan.
// Callers enrich a lane only when they are already re-reading it.
type Enricher interface {
	Enrich(ctx context.Context, lane model.Lane) (model.Lane, error)
}

// Brancher creates a new lane from a prefix of an existing one.
//
// Implementations must never modify the source lane: a branch is a new file, so
// a failed branch can lose nothing that already existed.
type Brancher interface {
	Branch(ctx context.Context, req BranchRequest) (model.Lane, error)
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
