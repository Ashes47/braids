package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A status line runs on every render of somebody's terminal. The two things
// it must never do are break and nag.
//
// The map counts an open loop from three weeks ago, and is right to: it is
// still open. A status line that says the same number every day is saying
// nothing, so this one only looks back over a window.
func TestStatusOnlyCountsWhatIsRecent(t *testing.T) {
	doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e1": "hello"})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	// The fixture's transcript is an hour old, so a day's window includes it.
	if out := runCmd(t, "status", "--db", db, "--within", "24h"); !strings.Contains(out, "1 waiting") {
		t.Errorf("a conversation from an hour ago was not counted: %q", out)
	}
	// A minute's window does not.
	if out := runCmd(t, "status", "--db", db, "--within", "1m"); strings.TrimSpace(out) != "" {
		t.Errorf("a conversation from an hour ago was counted inside a minute: %q", out)
	}
	// And nothing at all is said when nothing is in the window, unless asked.
	if out := runCmd(t, "status", "--db", db, "--within", "1m", "--always"); !strings.Contains(out, "0 waiting") {
		t.Errorf("--always said nothing: %q", out)
	}
}

// No index is not an error here. A status line that fails is a broken prompt
// on every keystroke, and braids not being set up yet is not worth that.
func TestStatusIsSilentWithoutAnIndex(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nothing.db")
	out := runCmd(t, "status", "--db", missing)
	if strings.TrimSpace(out) != "" {
		t.Errorf("status spoke without an index: %q", out)
	}
	if err := run([]string{"status", "--db", missing}, os.Stdout); err != nil {
		t.Errorf("status without an index returned an error: %v", err)
	}
}

// The JSON says whether the number can be believed at all, because a count of
// zero from a machine with no index means something different from a count of
// zero from a machine with one.
func TestStatusJSONSaysWhetherItIsReady(t *testing.T) {
	doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e2": "hello"})
	db := filepath.Join(t.TempDir(), "index.db")

	var before struct {
		Ready   bool `json:"ready"`
		Waiting int  `json:"waiting"`
	}
	if err := json.Unmarshal([]byte(runCmd(t, "status", "--db", db, "--json")), &before); err != nil {
		t.Fatal(err)
	}
	if before.Ready {
		t.Error("reported ready with no index")
	}

	runCmd(t, "index", "--db", db)
	var after struct {
		Ready         bool `json:"ready"`
		Conversations int  `json:"conversations"`
	}
	if err := json.Unmarshal([]byte(runCmd(t, "status", "--db", db, "--json")), &after); err != nil {
		t.Fatal(err)
	}
	if !after.Ready || after.Conversations != 1 {
		t.Errorf("after indexing: ready=%v conversations=%d", after.Ready, after.Conversations)
	}
}

// "waiting" describes the conversations rather than naming them, so it takes
// no s. Written down because "15 waitings" reached a terminal.
func TestStatusWordsTheCountCorrectly(t *testing.T) {
	doctorHome(t, map[string]string{"a1b2c3d4-0000-4000-8000-0000000000e3": "hello"})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)
	if out := runCmd(t, "status", "--db", db, "--within", "0", "--always"); strings.Contains(out, "waitings") {
		t.Errorf("status said: %q", strings.TrimSpace(out))
	}
}
