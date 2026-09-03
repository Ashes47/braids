package graph

import (
	"testing"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// msg is a terse message builder: seq, id, parent, role.
func msg(seq int, id, parent string, role model.Role, preview, tools string) index.MessageRow {
	return index.MessageRow{Seq: seq, ID: id, ParentID: parent, Role: role, Preview: preview, Tools: tools}
}

func kinds(segs []Segment) []SegmentKind {
	out := make([]SegmentKind, 0, len(segs))
	for _, s := range segs {
		out = append(out, s.Kind)
	}
	return out
}

func TestSpineAlternatesTurnsAndRuns(t *testing.T) {
	segs := Spine([]index.MessageRow{
		msg(1, "u1", "", model.RoleUser, "why is it slow", ""),
		msg(2, "a1", "u1", model.RoleAssistant, "looking", "Bash"),
		msg(3, "a2", "a1", model.RoleAssistant, "still looking", "Bash"),
		msg(4, "a3", "a2", model.RoleAssistant, "found it", "Read"),
		msg(5, "u2", "a3", model.RoleUser, "fix it", ""),
	})

	want := []SegmentKind{SegTurn, SegRun, SegTurn}
	if got := kinds(segs); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	run := segs[1]
	if run.Count != 3 {
		t.Errorf("run collapsed %d turns, want 3", run.Count)
	}
	if len(run.Tally) != 2 || run.Tally[0].Tool != "Bash" || run.Tally[0].Count != 2 {
		t.Errorf("tally = %+v, want Bash x2 first", run.Tally)
	}
	if segs[0].Preview != "why is it slow" || segs[0].Role != model.RoleUser {
		t.Errorf("first segment = %+v", segs[0])
	}
}

func TestSpineKeepsALoneTurnVisible(t *testing.T) {
	segs := Spine([]index.MessageRow{
		msg(1, "u1", "", model.RoleUser, "question", ""),
		msg(2, "a1", "u1", model.RoleAssistant, "answer", ""),
	})
	if got := kinds(segs); len(got) != 2 || got[1] != SegTurn {
		t.Fatalf("kinds = %v, want a lone assistant turn kept whole", got)
	}
	if segs[1].Preview != "answer" {
		t.Errorf("preview = %q, want the answer text", segs[1].Preview)
	}
}

func TestSpineReportsBranchesLeavingTheActivePath(t *testing.T) {
	// u1 was answered twice: an abandoned attempt (a1, a2) and the live one.
	segs := Spine([]index.MessageRow{
		msg(1, "u1", "", model.RoleUser, "try something", ""),
		msg(2, "a1", "u1", model.RoleAssistant, "first attempt", ""),
		msg(3, "a2", "a1", model.RoleAssistant, "first attempt continued", ""),
		msg(4, "a3", "u1", model.RoleAssistant, "second attempt", ""),
		msg(5, "u2", "a3", model.RoleUser, "better", ""),
	})

	if len(segs[0].Alternates) != 1 || segs[0].Alternates[0] != 2 {
		t.Fatalf("alternates at the junction = %v, want one branch of 2 turns", segs[0].Alternates)
	}
	// The abandoned branch must not appear on the spine itself.
	for _, s := range segs {
		if s.Preview == "first attempt" {
			t.Error("an abandoned branch leaked onto the active path")
		}
	}
	last := segs[len(segs)-1]
	if last.Preview != "better" {
		t.Errorf("spine should end at the last record, got %q", last.Preview)
	}
}

func TestSpineFollowsTheLastRecordNotTheLongestBranch(t *testing.T) {
	// The abandoned branch is longer; the live one is whatever was written last.
	segs := Spine([]index.MessageRow{
		msg(1, "u1", "", model.RoleUser, "root", ""),
		msg(2, "x1", "u1", model.RoleAssistant, "long branch a", ""),
		msg(3, "x2", "x1", model.RoleAssistant, "long branch b", ""),
		msg(4, "x3", "x2", model.RoleAssistant, "long branch c", ""),
		msg(5, "y1", "u1", model.RoleAssistant, "the live one", ""),
	})
	last := segs[len(segs)-1]
	if last.Preview != "the live one" {
		t.Errorf("active path ended at %q, want the last record", last.Preview)
	}
	if len(segs[0].Alternates) != 1 || segs[0].Alternates[0] != 3 {
		t.Errorf("alternates = %v, want the 3-turn abandoned branch", segs[0].Alternates)
	}
}

func TestSpineHandlesEmptyAndBrokenInput(t *testing.T) {
	if segs := Spine(nil); segs != nil {
		t.Errorf("Spine(nil) = %v, want nil", segs)
	}
	// A record whose parent is itself must not loop forever.
	segs := Spine([]index.MessageRow{msg(1, "a", "a", model.RoleUser, "self parented", "")})
	if len(segs) != 1 {
		t.Errorf("cyclic input produced %d segments, want 1", len(segs))
	}
}
