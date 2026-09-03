package graph

import (
	"sort"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// SegmentKind classifies one line of the spine.
type SegmentKind string

// The kinds of line a spine is made of.
const (
	// SegTurn is a landmark turn drawn in full.
	SegTurn SegmentKind = "turn"
	// SegRun is a collapsed stretch of unremarkable turns.
	SegRun SegmentKind = "run"
)

// Segment is one line of the spine.
type Segment struct {
	Kind    SegmentKind
	Seq     int
	Role    model.Role
	Preview string
	Tools   []string

	// Count and Tally describe a SegRun: how many turns it swallowed and which
	// tools ran inside it.
	Count int
	Tally []ToolCount

	// Alternates lists the branches that leave the active path at this turn,
	// largest first. A conversation with none is a straight line.
	Alternates []int
}

// ToolCount is one tool and how often it ran inside a run.
type ToolCount struct {
	Tool  string
	Count int
}

// Spine reduces one lane to the line a reader follows.
//
// The active path is the parent chain walked back from the *last* record, which
// is exactly how Claude Code reconstructs context — so the spine shows the
// conversation the model would see, not merely the file in order. Turns that
// left the path are reported as alternates at the junction they left from.
func Spine(msgs []index.MessageRow) []Segment {
	if len(msgs) == 0 {
		return nil
	}
	byID := make(map[string]index.MessageRow, len(msgs))
	children := make(map[string][]string, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
		children[m.ParentID] = append(children[m.ParentID], m.ID)
	}

	path := activePath(msgs, byID)
	onPath := make(map[string]bool, len(path))
	for _, m := range path {
		onPath[m.ID] = true
	}
	sizes := subtreeSizes(msgs, children)

	var out []Segment
	var run []index.MessageRow
	flush := func() {
		switch len(run) {
		case 0:
		case 1:
			// Collapsing a single turn hides more than it saves.
			m := run[0]
			out = append(out, Segment{
				Kind: SegTurn, Seq: m.Seq, Role: m.Role,
				Preview: m.Preview, Tools: splitTools(m.Tools),
			})
		default:
			out = append(out, runSegment(run))
		}
		run = nil
	}
	for _, m := range path {
		alternates := alternatesAt(m, children, onPath, sizes)
		if !landmark(m) && len(alternates) == 0 {
			run = append(run, m)
			continue
		}
		flush()
		out = append(out, Segment{
			Kind:       SegTurn,
			Seq:        m.Seq,
			Role:       m.Role,
			Preview:    m.Preview,
			Tools:      splitTools(m.Tools),
			Alternates: alternates,
		})
	}
	flush()
	return out
}

// landmark reports whether a turn is one a reader navigates by.
//
// Only a human turn qualifies, and only one that actually said something: a
// tool result is recorded with the user role but is the harness returning
// output, not a person speaking, so treating it as a landmark would leave a
// long conversation almost entirely uncollapsed.
func landmark(m index.MessageRow) bool {
	return m.Role == model.RoleUser && m.Preview != ""
}

// activePath walks back from the last record, which is the branch the harness
// would resume, and returns it root-first.
func activePath(msgs []index.MessageRow, byID map[string]index.MessageRow) []index.MessageRow {
	cur := msgs[len(msgs)-1]
	var reversed []index.MessageRow
	seen := make(map[string]bool, len(msgs))
	// The seen check also guards against a malformed chain looping forever.
	for ok := true; ok && !seen[cur.ID]; cur, ok = byID[cur.ParentID] {
		seen[cur.ID] = true
		reversed = append(reversed, cur)
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

// alternatesAt reports the size of every branch leaving the path at m.
func alternatesAt(m index.MessageRow, children map[string][]string, onPath map[string]bool, sizes map[string]int) []int {
	var out []int
	for _, child := range children[m.ID] {
		if onPath[child] {
			continue
		}
		out = append(out, sizes[child])
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// subtreeSizes counts each message plus everything descended from it.
func subtreeSizes(msgs []index.MessageRow, children map[string][]string) map[string]int {
	sizes := make(map[string]int, len(msgs))
	var size func(id string) int
	size = func(id string) int {
		if n, ok := sizes[id]; ok {
			return n
		}
		sizes[id] = 1 // guards against a cycle re-entering this node
		n := 1
		for _, c := range children[id] {
			n += size(c)
		}
		sizes[id] = n
		return n
	}
	for _, m := range msgs {
		size(m.ID)
	}
	return sizes
}

// runSegment collapses a stretch of turns into one line with a tool tally.
func runSegment(run []index.MessageRow) Segment {
	counts := make(map[string]int)
	for _, m := range run {
		for _, tool := range splitTools(m.Tools) {
			counts[tool]++
		}
	}
	tally := make([]ToolCount, 0, len(counts))
	for tool, n := range counts {
		tally = append(tally, ToolCount{Tool: tool, Count: n})
	}
	sort.Slice(tally, func(i, j int) bool {
		if tally[i].Count != tally[j].Count {
			return tally[i].Count > tally[j].Count
		}
		return tally[i].Tool < tally[j].Tool
	})
	return Segment{
		Kind:  SegRun,
		Seq:   run[0].Seq,
		Count: len(run),
		Tally: tally,
	}
}

func splitTools(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
