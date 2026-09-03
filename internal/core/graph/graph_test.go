package graph

import (
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
)

var base = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }

func lane(id string, msgs int, updated time.Time) index.LaneInfo {
	li := index.LaneInfo{Messages: msgs}
	li.ID = id
	li.Updated = updated
	return li
}

// born stamps a lane's file creation time, the signal that settles fork
// direction on platforms that report it.
func born(li index.LaneInfo, t time.Time) index.LaneInfo {
	li.Created = t
	return li
}

// shared marks message m as present in both lanes at the given turn numbers.
func shared(m, laneA string, seqA int, laneB string, seqB int) []index.Overlap {
	return []index.Overlap{
		{MessageID: m, LaneID: laneA, Seq: seqA},
		{MessageID: m, LaneID: laneB, Seq: seqB},
	}
}

func TestBuildDetectsForkAndDirection(t *testing.T) {
	// main runs 1..4; fork copies turns 1..2 and diverges afterwards. The fork's
	// own turn happens *before* main's turn 3 — the common case of forking and
	// then carrying on in the original — so only the file birth time settles it.
	lanes := []index.LaneInfo{
		born(lane("main", 4, at(40)), at(-10)),
		born(lane("fork", 3, at(50)), at(5)),
	}
	var overlaps []index.Overlap
	overlaps = append(overlaps, shared("m1", "main", 1, "fork", 1)...)
	overlaps = append(overlaps, shared("m2", "main", 2, "fork", 2)...)
	timelines := map[string][]time.Time{
		"main": {at(0), at(10), at(20), at(30)},
		"fork": {at(0), at(10), at(15)}, // diverges before main does
	}

	f := Build(lanes, overlaps, timelines)
	if len(f.Roots) != 1 || f.Roots[0].Lane.ID != "main" {
		t.Fatalf("roots = %v, want just main", ids(f.Roots))
	}
	fork := f.ByID["fork"]
	if fork.ParentID != "main" {
		t.Errorf("ParentID = %q, want main", fork.ParentID)
	}
	if fork.ForkSeq != 2 {
		t.Errorf("ForkSeq = %d, want 2 (last shared turn in the parent)", fork.ForkSeq)
	}
	if fork.Depth != 1 {
		t.Errorf("Depth = %d, want 1", fork.Depth)
	}
}

func TestBuildTreatsStrictPrefixAsParent(t *testing.T) {
	// "stub" was forked and then abandoned, so it has no turns of its own.
	// No birth times, so the graph must fall back on containment.
	lanes := []index.LaneInfo{lane("stub", 2, at(10)), lane("grown", 4, at(60))}
	var overlaps []index.Overlap
	overlaps = append(overlaps, shared("m1", "stub", 1, "grown", 1)...)
	overlaps = append(overlaps, shared("m2", "stub", 2, "grown", 2)...)
	timelines := map[string][]time.Time{
		"stub":  {at(0), at(5)},
		"grown": {at(0), at(5), at(30), at(40)},
	}

	f := Build(lanes, overlaps, timelines)
	if got := f.ByID["grown"].ParentID; got != "stub" {
		t.Errorf("a lane with no divergence must be the parent; got parent %q", got)
	}
}

func TestBuildChoosesNearestAncestor(t *testing.T) {
	// root 1..6, mid forks at 2, leaf forks from mid at 4. leaf overlaps root
	// too, but shares more with mid, so mid is the nearer ancestor.
	lanes := []index.LaneInfo{
		born(lane("root", 6, at(60)), at(-30)),
		born(lane("mid", 5, at(70)), at(-20)),
		born(lane("leaf", 5, at(80)), at(-10)),
	}
	var overlaps []index.Overlap
	overlaps = append(overlaps, shared("m1", "root", 1, "mid", 1)...)
	overlaps = append(overlaps, shared("m2", "root", 2, "mid", 2)...)
	overlaps = append(overlaps, shared("m1", "root", 1, "leaf", 1)...)
	overlaps = append(overlaps, shared("m2", "root", 2, "leaf", 2)...)
	overlaps = append(overlaps, shared("m1", "mid", 1, "leaf", 1)...)
	overlaps = append(overlaps, shared("m2", "mid", 2, "leaf", 2)...)
	overlaps = append(overlaps, shared("x3", "mid", 3, "leaf", 3)...)
	overlaps = append(overlaps, shared("x4", "mid", 4, "leaf", 4)...)
	timelines := map[string][]time.Time{
		"root": {at(0), at(5), at(10), at(15), at(20), at(25)},
		"mid":  {at(0), at(5), at(30), at(35), at(40)},
		"leaf": {at(0), at(5), at(30), at(35), at(50)},
	}

	f := Build(lanes, overlaps, timelines)
	if got := f.ByID["mid"].ParentID; got != "root" {
		t.Errorf("mid parent = %q, want root", got)
	}
	if got := f.ByID["leaf"].ParentID; got != "mid" {
		t.Errorf("leaf parent = %q, want mid (nearest ancestor)", got)
	}
	if got := f.ByID["leaf"].Depth; got != 2 {
		t.Errorf("leaf depth = %d, want 2", got)
	}
	if order := ids(f.Flatten()); len(order) != 3 || order[0] != "root" {
		t.Errorf("flatten order = %v, want root first", order)
	}
}

func TestBuildLeavesUnrelatedLanesAsRoots(t *testing.T) {
	lanes := []index.LaneInfo{lane("a", 2, at(10)), lane("b", 2, at(20))}
	f := Build(lanes, nil, map[string][]time.Time{
		"a": {at(0), at(1)},
		"b": {at(0), at(1)},
	})
	if len(f.Roots) != 2 {
		t.Fatalf("want both lanes as roots, got %v", ids(f.Roots))
	}
	// Most recently active first.
	if f.Roots[0].Lane.ID != "b" {
		t.Errorf("root order = %v, want b first", ids(f.Roots))
	}
}

func ids(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Lane.ID)
	}
	return out
}

func TestBuildBreaksAmbiguousTiesTowardsTheShallowestAncestor(t *testing.T) {
	// root 1..4; mid forked at 4 so it holds 1..4; leaf forked at 2 so it holds
	// 1..2. leaf's prefix is byte-identical in root and mid — both share exactly
	// two turns with it — so which one it came from is unknowable from content.
	// Attaching to root keeps the tree honest rather than inventing depth.
	lanes := []index.LaneInfo{
		born(lane("root", 4, at(40)), at(-30)),
		born(lane("mid", 6, at(50)), at(-20)),
		born(lane("leaf", 2, at(60)), at(-10)),
	}
	var overlaps []index.Overlap
	for i, id := range []string{"m1", "m2"} {
		overlaps = append(overlaps, shared(id, "root", i+1, "mid", i+1)...)
		overlaps = append(overlaps, shared(id, "root", i+1, "leaf", i+1)...)
		overlaps = append(overlaps, shared(id, "mid", i+1, "leaf", i+1)...)
	}
	overlaps = append(overlaps, shared("m3", "root", 3, "mid", 3)...)
	overlaps = append(overlaps, shared("m4", "root", 4, "mid", 4)...)
	timelines := map[string][]time.Time{
		"root": {at(0), at(5), at(10), at(15)},
		"mid":  {at(0), at(5), at(10), at(15), at(20), at(25)},
		"leaf": {at(0), at(5)},
	}

	// Repeat: the pair map iterates randomly, so a tie must not flip between runs.
	for range 25 {
		f := Build(lanes, overlaps, timelines)
		if got := f.ByID["leaf"].ParentID; got != "root" {
			t.Fatalf("leaf parent = %q, want root (shallowest ancestor on a tie)", got)
		}
		if got := f.ByID["mid"].ParentID; got != "root" {
			t.Fatalf("mid parent = %q, want root", got)
		}
	}
}
