package graph

import (
	"sort"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
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
// Where braids made the branch itself, the recorded origin is used instead of
// any of this: it is the only evidence that cannot be wrong.
//
// Direction — which lane is the parent — is decided by file creation time,
// because nothing inside the files can settle it: a fork copies the parent's
// records verbatim, timestamps included, and people routinely fork and *then*
// carry on in the parent, so "diverged first" is not evidence of anything.
// Where the platform reports no birth time, weaker evidence is used in order:
// a lane that is a strict prefix of another is its parent, then the longer
// lane is taken as the parent.
func Build(lanes []index.LaneInfo, overlaps []index.Overlap, timelines map[string][]time.Time, recorded map[string]model.Origin) *Forest {
	forest := &Forest{ByID: make(map[string]*Node, len(lanes))}
	for _, l := range lanes {
		forest.ByID[l.ID] = &Node{Lane: l}
	}

	for child, c := range candidates(sharedPairs(overlaps, forest.ByID), forest.ByID, timelines) {
		node := forest.ByID[child]
		node.ParentID = c.parent
		node.ForkSeq = c.forkSeq
	}

	// Recorded provenance wins: when braids made the branch it knows where it
	// came from, and no amount of inference can beat that.
	for child, origin := range recorded {
		node, ok := forest.ByID[child]
		if !ok {
			continue
		}
		parent, forkSeq, found := survivingAncestor(child, origin, recorded, forest.ByID)
		if !found {
			continue // nothing recorded survives; fall back to what was inferred
		}
		node.ParentID = parent
		node.ForkSeq = forkSeq
	}

	link(forest)
	return forest
}

// candidate is one lane's best-supported parent.
type candidate struct {
	parent  string
	forkSeq int
	count   int
}

// candidates picks each lane's parent from every lane it overlaps.
//
// The longest shared prefix wins: that is the nearest ancestor. A tie is
// genuinely ambiguous — two lanes can hold byte-identical prefixes — so it is
// broken towards the *earliest* candidate, attaching to the shallowest common
// ancestor rather than inventing a deeper relationship, and then by ID so the
// same transcripts always draw the same tree.
func candidates(pairs map[key]*pair, nodes map[string]*Node, timelines map[string][]time.Time) map[string]candidate {
	best := make(map[string]candidate, len(nodes))
	for k, p := range pairs {
		child, parent, forkSeq := direction(k, p, nodes, timelines)
		next := candidate{parent: parent, forkSeq: forkSeq, count: p.count}
		cur, seen := best[child]
		if !seen || better(next, cur, nodes) {
			best[child] = next
		}
	}
	return best
}

// better reports whether a beats b as a parent for the same lane.
func better(a, b candidate, nodes map[string]*Node) bool {
	if a.count != b.count {
		return a.count > b.count
	}
	ac, bc := nodes[a.parent].Lane.Created, nodes[b.parent].Lane.Created
	if !ac.IsZero() && !bc.IsZero() && !ac.Equal(bc) {
		return ac.Before(bc)
	}
	return a.parent < b.parent
}

// survivingAncestor walks a recorded chain past lanes that no longer exist.
//
// Deleting a conversation leaves its branches behind — they hold their own copy
// of the shared prefix — but they have to be drawn somewhere. Inference would
// send them to the shallowest common ancestor, because every fork of a fork
// shares a byte-identical prefix with the whole line above it and the counts
// tie. Following what was recorded keeps them where they belong: under the
// nearest conversation that is still there.
func survivingAncestor(child string, origin model.Origin, recorded map[string]model.Origin, alive map[string]*Node) (parent string, forkSeq int, found bool) {
	seen := map[string]bool{child: true}
	parent, forkSeq = origin.Parent, origin.ForkSeq
	for parent != "" && !seen[parent] {
		if _, ok := alive[parent]; ok {
			return parent, forkSeq, true
		}
		seen[parent] = true
		up, ok := recorded[parent]
		if !ok {
			return "", 0, false
		}
		// A branch cut deeper than its parent's own fork point can only be
		// placed at the last turn they actually shared.
		forkSeq = min(forkSeq, up.ForkSeq)
		parent = up.Parent
	}
	return "", 0, false
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
