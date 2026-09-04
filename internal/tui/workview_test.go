package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/artifacts"
	"github.com/Ashes47/braids/internal/core/index"
)

// workModel opens the work-products browser over a two-level fake tree.
func workModel(t *testing.T, discard func(string, []string) (int64, error)) (Model, *[][]string) {
	t.Helper()
	var discarded [][]string
	lanes := []index.LaneInfo{laneInfo("lane1234abcd", "annotation pipeline", "microagi", 100, time.Hour)}
	f := forestOf(lanes, nil)

	top := []artifacts.Entry{
		{Name: "tmp", Path: "/j/lane1234/tmp", Dir: true, Bytes: 900, Files: 3},
		{Name: "state.json", Path: "/j/lane1234/state.json", Bytes: 10, Files: 1, Reserved: true},
	}
	inner := []artifacts.Entry{
		{Name: "pods.json", Path: "/j/lane1234/tmp/pods.json", Bytes: 800, Files: 1},
		{Name: "deep", Path: "/j/lane1234/tmp/deep", Dir: true, Bytes: 100, Files: 2},
	}
	if discard == nil {
		discard = func(_ string, paths []string) (int64, error) {
			discarded = append(discarded, paths)
			return 800, nil
		}
	}
	m := NewModel(f, Options{
		ASCII: true, Source: "claudecode",
		LoadWork: func(_, dir string) (WorkLevel, error) {
			switch dir {
			case "":
				return WorkLevel{Root: "/j/lane1234", Dir: "/j/lane1234", Entries: top}, nil
			case "tmp":
				return WorkLevel{Root: "/j/lane1234", Dir: "/j/lane1234/tmp", Entries: inner}, nil
			default:
				return WorkLevel{}, errors.New("no such directory: " + dir)
			}
		},
		DiscardPaths: discard,
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 96, 22
	return m.openWork(), &discarded
}

func TestWorkBrowserDescendsAndComesBack(t *testing.T) {
	m, _ := workModel(t, nil)
	if m.mode != workMode {
		t.Fatalf("mode = %v, want the work browser", m.mode)
	}
	if got := plain(m.renderWork()); !strings.Contains(got, "tmp/") {
		t.Errorf("top level does not list tmp:\n%s", got)
	}

	// Descend into the directory under the cursor.
	m = m.workKey("enter")
	if got := m.workWhere(); got != "tmp" {
		t.Fatalf("after enter, here = %q, want tmp", got)
	}
	if got := plain(m.renderWork()); !strings.Contains(got, "pods.json") {
		t.Errorf("tmp does not list pods.json:\n%s", got)
	}
	// A new level is a new question: the cursor starts on the heaviest row.
	if m.work.cursor != 0 {
		t.Errorf("cursor = %d after descending, want the top row", m.work.cursor)
	}

	// esc retraces the way in rather than abandoning it.
	m = m.workKey("esc")
	if got := m.workWhere(); got != "top" {
		t.Errorf("after esc, here = %q, want top", got)
	}
	if m.mode != workMode {
		t.Error("esc from a subdirectory left the browser entirely")
	}
	// esc at the top does leave.
	m = m.workKey("esc")
	if m.mode == workMode || m.work != nil {
		t.Error("esc at the top did not return to the map")
	}
}

// Entering a file does nothing; only directories open.
func TestWorkBrowserDoesNotOpenAFile(t *testing.T) {
	m, _ := workModel(t, nil)
	m = m.workKey("enter") // into tmp
	m = m.workKey("j")     // onto deep
	m = m.workKey("k")     // back onto pods.json, a file
	before := m.workWhere()
	if m = m.workKey("enter"); m.workWhere() != before {
		t.Errorf("entering a file moved to %q", m.workWhere())
	}
}

func TestWorkBrowserBinsTheSelectedEntry(t *testing.T) {
	m, discarded := workModel(t, nil)
	m = m.workKey("enter") // into tmp, cursor on pods.json
	m = m.workKey("d")

	if len(*discarded) != 1 || (*discarded)[0][0] != "/j/lane1234/tmp/pods.json" {
		t.Fatalf("discarded %v, want just pods.json", *discarded)
	}
	// The notice has to say the room is not back yet: the bin still holds it.
	if got := m.work.notice; !strings.Contains(got, "bin") || !strings.Contains(got, "800 B") {
		t.Errorf("notice = %q, want it to name the bin and the size", got)
	}
	if m.work.failed {
		t.Error("a successful deletion was reported as a failure")
	}
}

// The harness's own record of a job is shown so the sizes add up, and refused
// so a running session's state is not destroyed.
func TestWorkBrowserRefusesTheHarnessRecord(t *testing.T) {
	m, discarded := workModel(t, nil)
	m = m.workKey("j") // onto state.json
	entry, ok := m.workCursor()
	if !ok || !entry.Reserved {
		t.Fatalf("cursor is on %+v, want the reserved entry", entry)
	}
	m = m.workKey("d")
	if len(*discarded) != 0 {
		t.Fatalf("the harness's record was discarded: %v", *discarded)
	}
	if !m.work.failed || !strings.Contains(m.work.notice, "leaves it alone") {
		t.Errorf("notice = %q (failed=%v), want a refusal", m.work.notice, m.work.failed)
	}
}

// A failure to discard is reported, not swallowed.
func TestWorkBrowserReportsAFailedDelete(t *testing.T) {
	m, _ := workModel(t, func(string, []string) (int64, error) {
		return 0, errors.New("read-only file system")
	})
	m = m.workKey("enter")
	m = m.workKey("d")
	if !m.work.failed || !strings.Contains(m.work.notice, "read-only") {
		t.Errorf("notice = %q (failed=%v), want the error", m.work.notice, m.work.failed)
	}
}

// Moving wraps, like every other list in braids.
func TestWorkBrowserScrollsCircularly(t *testing.T) {
	m, _ := workModel(t, nil)
	m = m.workKey("k")
	if m.work.cursor != len(m.work.entries)-1 {
		t.Errorf("up from the first row = %d, want the last", m.work.cursor)
	}
	m = m.workKey("j")
	if m.work.cursor != 0 {
		t.Errorf("down from the last row = %d, want the first", m.work.cursor)
	}
}

// Without the capability the screen says so instead of opening empty.
func TestWorkBrowserWithoutTheCapability(t *testing.T) {
	lanes := []index.LaneInfo{laneInfo("lane1", "x", "p", 1, time.Hour)}
	m := NewModel(forestOf(lanes, nil), Options{ASCII: true, Source: "claudecode"})
	m.now = func() time.Time { return now }
	m.width, m.height = 90, 20
	m = m.openWork()
	if m.mode == workMode {
		t.Error("the browser opened with no way to read work products")
	}
	if !strings.Contains(m.notice, "unavailable") {
		t.Errorf("notice = %q", m.notice)
	}
}
