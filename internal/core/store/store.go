package store

import (
	"context"

	"github.com/Ashes47/braids/internal/core/memory"
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

// Sidechains reports the conversations a lane spawned and collapsed into a
// single tool call. Sources without subagents simply do not implement it.
type Sidechains interface {
	Subagents(ctx context.Context, lane model.Lane) ([]model.Subagent, error)
}

// Rememberer lists the memory directories a harness keeps. A harness with no
// notion of memory simply does not implement it, and braids offers no memory
// screen for that source.
type Rememberer interface {
	MemoryDirs() ([]memory.Location, error)
}

// Measurer re-measures a conversation's work products.
//
// They change without the transcript changing — deleting a scratch file leaves
// the conversation untouched — so an index that re-reads only what moved will
// never notice. Whatever changed them has to say so.
type Measurer interface {
	Artifacts(laneID string) (path string, bytes int64)
}

// MergeRequest describes joining a branch back into the conversation it left.
type MergeRequest struct {
	// Base is the conversation to carry on from.
	Base model.Lane
	// Incoming is the branch whose divergent turns are brought over.
	Incoming model.Lane
	// Name is the display name for the merged conversation.
	Name string
}

// MergePlan is what a merge would do, so it can be shown before it is done.
type MergePlan struct {
	// Shared is how many records the two conversations already have in common.
	Shared int
	// Incoming is how many records would be carried over.
	Incoming int
	// BaseTurns and IncomingTurns count only conversational turns, which is
	// what a person is deciding about.
	BaseTurns, IncomingTurns int
	// BaseOnlyTurns is how many turns the base has that the branch does not.
	// When it is zero the branch already contains the base, and merging would
	// produce a copy of the branch rather than joining anything.
	BaseOnlyTurns int
}

// Worthwhile reports whether a merge would actually join two histories rather
// than duplicate one. A merge is only ever useful when both sides carried on
// after they parted: if only one did, the other is already contained in it.
func (p MergePlan) Worthwhile() bool {
	return p.BaseOnlyTurns > 0 && p.IncomingTurns > 0
}

// Merger joins a branch back into the conversation it left, as a new
// conversation. Neither of the originals is touched.
type Merger interface {
	// PlanMerge reports what a merge would do without doing it.
	PlanMerge(ctx context.Context, req MergeRequest) (MergePlan, error)
	// Merge writes the joined conversation.
	Merge(ctx context.Context, req MergeRequest) (model.Lane, error)
}

// Promoter turns a subagent's transcript into a conversation of its own.
type Promoter interface {
	Promote(ctx context.Context, agent model.Subagent) (model.Lane, error)
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
