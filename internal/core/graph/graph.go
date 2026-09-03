package graph

import (
	"sort"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
)

// Node is one lane placed in the forest.
type Node struct {
	Lane index.LaneInfo
	// ParentID is the lane this one forked from, empty for a root.
	ParentID string
	// ForkSeq is the turn number *in the parent* where this lane diverged.
	ForkSeq int
	// Depth is the distance from the root, used for indentation.
	Depth    int
	Children []*Node
}

// Forest is the whole map: every lane, arranged by fork relationship.
type Forest struct {
	Roots []*Node
	ByID  map[string]*Node
}

// pair accumulates what two lanes share.
type pair struct {
	count int
	seqA  int // last shared turn, numbered within lane A
	seqB  int // last shared turn, numbered within lane B
}

type key struct{ a, b string } // a < b, so each unordered pair appears once

// Build arranges lanes into a forest.
//
// Fork detection is exact rather than fuzzy: Claude Code copies the parent's
// records verbatim into a fork, so a shared message ID proves a shared prefix
// and the last shared turn is the fork point.
//
// Direction — which lane is the parent — is decided by file creation time,
// because nothing inside the files can settle it: a fork copies the parent's
// records verbatim, timestamps included, and people routinely fork and *then*
// carry on in the parent, so "diverged first" is not evidence of anything.
// Where the platform reports no birth time, weaker evidence is used in order:
// a lane that is a strict prefix of another is its parent, then the longer
// lane is taken as the parent.
func Build(lanes []index.LaneInfo, overlaps []index.Overlap, timelines map[string][]time.Time) *Forest {
	forest := &Forest{ByID: make(map[string]*Node, len(lanes))}
	for _, l := range lanes {
		forest.ByID[l.ID] = &Node{Lane: l}
	}

	// When a lane overlaps several others, the longest shared prefix is its
	// nearest ancestor, so keep the strongest relationship seen.
	best := make(map[string]int, len(lanes))
	for k, p := range sharedPairs(overlaps, forest.ByID) {
		child, parent, forkSeq := direction(k, p, forest.ByID, timelines)
		if p.count <= best[child] {
			continue
		}
		best[child] = p.count
		node := forest.ByID[child]
		node.ParentID = parent
		node.ForkSeq = forkSeq
	}

	link(forest)
	return forest
}

// sharedPairs counts, for every pair of lanes, how many messages they share and
// where the shared prefix ends in each.
func sharedPairs(overlaps []index.Overlap, known map[string]*Node) map[key]*pair {
	byMessage := make(map[string][]index.Overlap)
	for _, o := range overlaps {
		if _, ok := known[o.LaneID]; ok {
			byMessage[o.MessageID] = append(byMessage[o.MessageID], o)
		}
	}

	pairs := make(map[key]*pair)
	for _, group := range byMessage {
		for i := range group {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if a.LaneID == b.LaneID {
					continue
				}
				if a.LaneID > b.LaneID {
					a, b = b, a
				}
				k := key{a.LaneID, b.LaneID}
				p := pairs[k]
				if p == nil {
					p = &pair{}
					pairs[k] = p
				}
				p.count++
				p.seqA = max(p.seqA, a.Seq)
				p.seqB = max(p.seqB, b.Seq)
			}
		}
	}
	return pairs
}

// direction decides which lane of a pair forked from the other, returning the
// child, the parent, and the fork turn numbered within the parent.
func direction(k key, p *pair, nodes map[string]*Node, timelines map[string][]time.Time) (child, parent string, forkSeq int) {
	aFirst := func() (string, string, int) { return k.b, k.a, p.seqA } // a is the parent
	bFirst := func() (string, string, int) { return k.a, k.b, p.seqB } // b is the parent

	a, b := nodes[k.a].Lane, nodes[k.b].Lane

	// Best evidence: the file that existed first is the one that was forked.
	if !a.Created.IsZero() && !b.Created.IsZero() && !a.Created.Equal(b.Created) {
		if a.Created.Before(b.Created) {
			return aFirst()
		}
		return bFirst()
	}

	// Next best: a lane wholly contained in another cannot be its parent's child.
	aDiverges := diverges(timelines[k.a], p.seqA)
	bDiverges := diverges(timelines[k.b], p.seqB)
	switch {
	case !aDiverges && bDiverges:
		return aFirst()
	case aDiverges && !bDiverges:
		return bFirst()
	}

	// Weakest: assume the longer conversation is the one that was forked, and
	// failing that the one that stopped being touched first.
	switch {
	case b.Messages != a.Messages:
		if b.Messages > a.Messages {
			return bFirst()
		}
		return aFirst()
	case a.Updated.Before(b.Updated):
		return aFirst()
	default:
		return bFirst()
	}
}

// diverges reports whether a lane has any turn beyond a shared prefix of the
// given length. A lane that does not is wholly contained in the other.
func diverges(timeline []time.Time, sharedLen int) bool {
	return sharedLen >= 0 && sharedLen < len(timeline)
}

// link attaches children to parents, drops any edge that would create a cycle,
// and orders the result for display.
func link(f *Forest) {
	for _, n := range f.ByID {
		if n.ParentID != "" && wouldCycle(f, n) {
			n.ParentID = ""
			n.ForkSeq = 0
		}
	}
	for _, n := range f.ByID {
		if parent, ok := f.ByID[n.ParentID]; ok {
			parent.Children = append(parent.Children, n)
		} else {
			f.Roots = append(f.Roots, n)
		}
	}
	sortNodes(f.Roots)
	for _, n := range f.ByID {
		sortNodes(n.Children)
	}
	for _, r := range f.Roots {
		assignDepth(r, 0)
	}
}

// wouldCycle reports whether following ParentID from n returns to n.
func wouldCycle(f *Forest, n *Node) bool {
	seen := map[string]bool{n.Lane.ID: true}
	for cur := f.ByID[n.ParentID]; cur != nil; cur = f.ByID[cur.ParentID] {
		if seen[cur.Lane.ID] {
			return true
		}
		seen[cur.Lane.ID] = true
	}
	return false
}

func assignDepth(n *Node, depth int) {
	n.Depth = depth
	for _, c := range n.Children {
		assignDepth(c, depth+1)
	}
}

// sortNodes orders siblings most recently active first, which is the order
// someone scanning for "what was I doing" wants.
func sortNodes(nodes []*Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if !nodes[i].Lane.Updated.Equal(nodes[j].Lane.Updated) {
			return nodes[i].Lane.Updated.After(nodes[j].Lane.Updated)
		}
		return nodes[i].Lane.ID < nodes[j].Lane.ID
	})
}

// Flatten walks the forest depth-first, yielding the display order.
func (f *Forest) Flatten() []*Node {
	out := make([]*Node, 0, len(f.ByID))
	var walk func(*Node)
	walk = func(n *Node) {
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range f.Roots {
		walk(r)
	}
	return out
}
