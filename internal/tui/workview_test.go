package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/artifacts"
	"github.com/Ashes47/braids/internal/core/index"
)

// workModel opens the work-products browser over a two-level fake tree.
func workModel(t *testing.T, discard func(string, []string) (int64, error)) (Model, *[][]string) {
	t.Helper()
	var discarded [][]string
	lanes := []index.LaneInfo{laneInfo("lane1234abcd", "import pipeline", "storefront", 100, time.Hour)}
	f := forestOf(lanes, nil)

	top := []artifacts.Entry{
		{Name: "tmp", Path: "/j/lane1234/tmp", Dir: true, Bytes: 900, Files: 3},
		{Name: "state.json", Path: "/j/lane1234/state.json", Bytes: 10, Files: 1, Reserved: true},
	}
	inner := []artifacts.Entry{
		{Name: "nodes.json", Path: "/j/lane1234/tmp/nodes.json", Bytes: 800, Files: 1},
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
	m, _ = m.workKey("enter")
	if got := m.workWhere(); got != "tmp" {
		t.Fatalf("after enter, here = %q, want tmp", got)
	}
	if got := plain(m.renderWork()); !strings.Contains(got, "nodes.json") {
		t.Errorf("tmp does not list nodes.json:\n%s", got)
	}
	// A new level is a new question: the cursor starts on the heaviest row.
	if m.work.cursor != 0 {
		t.Errorf("cursor = %d after descending, want the top row", m.work.cursor)
	}

	// esc retraces the way in rather than abandoning it.
	m, _ = m.workKey("esc")
	if got := m.workWhere(); got != "top" {
		t.Errorf("after esc, here = %q, want top", got)
	}
	if m.mode != workMode {
		t.Error("esc from a subdirectory left the browser entirely")
	}
	// esc at the top does leave.
	m, _ = m.workKey("esc")
	if m.mode == workMode || m.work != nil {
		t.Error("esc at the top did not return to the map")
	}
}

// Entering a file does nothing; only directories open.
func TestWorkBrowserDoesNotOpenAFile(t *testing.T) {
	m, _ := workModel(t, nil)
	m, _ = m.workKey("enter") // into tmp
	m, _ = m.workKey("j")     // onto deep
	m, _ = m.workKey("k")     // back onto nodes.json, a file
	before := m.workWhere()
	if m, _ = m.workKey("enter"); m.workWhere() != before {
		t.Errorf("entering a file moved to %q", m.workWhere())
	}
}

func TestWorkBrowserBinsTheSelectedEntry(t *testing.T) {
	m, discarded := workModel(t, nil)
	m, _ = m.workKey("enter") // into tmp, cursor on nodes.json
	m, _ = m.workKey("d")

	if len(*discarded) != 1 || (*discarded)[0][0] != "/j/lane1234/tmp/nodes.json" {
		t.Fatalf("discarded %v, want just nodes.json", *discarded)
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
	m, _ = m.workKey("j") // onto state.json
	entry, ok := m.workCursor()
	if !ok || !entry.Reserved {
		t.Fatalf("cursor is on %+v, want the reserved entry", entry)
	}
	m, _ = m.workKey("d")
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
	m, _ = m.workKey("enter")
	m, _ = m.workKey("d")
	if !m.work.failed || !strings.Contains(m.work.notice, "read-only") {
		t.Errorf("notice = %q (failed=%v), want the error", m.work.notice, m.work.failed)
	}
}

// Moving wraps, like every other list in braids.
func TestWorkBrowserScrollsCircularly(t *testing.T) {
	m, _ := workModel(t, nil)
	m, _ = m.workKey("k")
	if m.work.cursor != len(m.work.entries)-1 {
		t.Errorf("up from the first row = %d, want the last", m.work.cursor)
	}
	m, _ = m.workKey("j")
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

// fileModel is a work browser over one real file on disk.
func fileModel(t *testing.T, name string, body []byte) Model {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	lanes := []index.LaneInfo{laneInfo("lane1234abcd", "a conversation", "app", 100, time.Hour)}
	m := NewModel(forestOf(lanes, nil), Options{
		ASCII: true, Source: "claudecode",
		LoadWork: func(_, _ string) (WorkLevel, error) {
			return WorkLevel{Root: dir, Dir: dir, Entries: []artifacts.Entry{
				{Name: name, Path: path, Bytes: int64(len(body)), Files: 1},
			}}, nil
		},
	})
	m.now = func() time.Time { return now }
	m.width, m.height = 96, 18
	return m.openWork()
}

// ↵ on a file shows its head; ↵ on a directory still descends.
func TestWorkBrowserOpensAFile(t *testing.T) {
	body := []byte("first line\nsecond line\nthird line\n")
	m := fileModel(t, "notes.txt", body)
	m, _ = m.workKey("enter")
	if m.work.reading == nil {
		t.Fatal("↵ on a file did not open it")
	}
	out := plain(m.renderWork())
	for _, want := range []string{"notes.txt", "first line", "third line", "all of it", "text"} {
		if !strings.Contains(out, want) {
			t.Errorf("the viewer is missing %q:\n%s", want, out)
		}
	}
	// esc returns to the listing, not out of the screen.
	m, _ = m.workKey("esc")
	if m.work == nil || m.work.reading != nil {
		t.Fatal("esc did not return to the listing")
	}
	if m.mode != workMode {
		t.Error("esc left the work screen entirely")
	}
}

// Only the head is read, and the frame says so: these files reach hundreds of
// megabytes and a viewer that reads the file stalls the program.
func TestWorkViewerReadsOnlyTheHead(t *testing.T) {
	var body []byte
	for i := range 40000 {
		body = append(body, []byte(fmt.Sprintf("line %d padded out to make the file long\n", i))...)
	}
	if len(body) <= artifacts.PeekLimit {
		t.Fatalf("the fixture is only %d bytes, which does not exercise the limit", len(body))
	}
	m := fileModel(t, "big.log", body)
	m, _ = m.workKey("enter")

	doc := m.work.reading
	if doc.peek.Read != artifacts.PeekLimit {
		t.Errorf("read %d bytes, want the limit %d", doc.peek.Read, artifacts.PeekLimit)
	}
	if !doc.peek.Truncated() {
		t.Error("a partly read file is not reported as truncated")
	}
	out := plain(m.renderWork())
	if !strings.Contains(out, "the rest is on disk") {
		t.Errorf("the frame does not say it is showing part of the file:\n%s", out)
	}
	if !strings.Contains(out, "first 128 kB") {
		t.Errorf("the header does not say how much was read:\n%s", out)
	}
}

// Data is named rather than drawn, and the path is offered instead.
func TestWorkViewerNamesDataAndOffersThePath(t *testing.T) {
	m := fileModel(t, "index.db", append([]byte("SQLite format 3"), 0x00, 0x01, 0x02))
	m, _ = m.workKey("enter")

	out := plain(m.renderWork())
	for _, want := range []string{"data rather than text", "copies the path", "data — not shown"} {
		if !strings.Contains(out, want) {
			t.Errorf("the viewer is missing %q:\n%s", want, out)
		}
	}
	// y offers the path to the clipboard, which is the useful thing to do with
	// a file braids will not show.
	next, cmd := m.workKey("y")
	if cmd == nil {
		t.Error("y did not reach the clipboard")
	}
	if !strings.Contains(next.work.notice, "index.db") {
		t.Errorf("notice = %q, want the path", next.work.notice)
	}
}

// Lines are broken, never reflowed: a line of JSON or a listing means what its
// columns say.
func TestWorkViewerBreaksLinesWithoutReflowing(t *testing.T) {
	long := strings.Repeat("x", 300)
	m := fileModel(t, "one-line.json", []byte(long+"\n"))
	m, _ = m.workKey("enter")
	doc := m.work.reading
	if len(doc.lines) < 3 {
		t.Fatalf("a %d-character line became %d lines", len(long), len(doc.lines))
	}
	if strings.Join(doc.lines, "") != long {
		t.Error("breaking the line changed its contents")
	}
	for _, line := range doc.lines {
		if lipgloss.Width(line) > doc.width {
			t.Errorf("a line is %d columns, frame allows %d", lipgloss.Width(line), doc.width)
		}
	}
	// Scrolling stops at both ends.
	m, _ = m.workKey("k")
	if m.work.reading.offset != 0 {
		t.Error("scrolled above the first line")
	}
	m, _ = m.workKey("G")
	last := m.work.reading.offset
	m, _ = m.workKey("j")
	if m.work.reading.offset != last {
		t.Error("scrolled past the end")
	}
}

// An empty file says so rather than looking broken.
func TestWorkViewerOnAnEmptyFile(t *testing.T) {
	m := fileModel(t, "nothing.txt", nil)
	m, _ = m.workKey("enter")
	if got := plain(m.renderWork()); !strings.Contains(got, "this file is empty") {
		t.Errorf("the viewer does not say the file is empty:\n%s", got)
	}
}
