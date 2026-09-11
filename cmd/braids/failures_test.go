package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// laneWithFailures writes a conversation of n turns where the turns listed in
// failed came back a tool error, so a merge plan has something real to read.
func laneWithFailures(t *testing.T, session string, turns int, failed map[int]bool) string {
	return lanesWithFailures(t, session, turns, failed)
}

// base is the conversation a branch is merged back into. Every test here needs
// one, because braids refuses to merge a conversation into itself.
const baseLane = "a1b2c3d4-0000-4000-8000-00000000ba5e"

func lanesWithFailures(t *testing.T, session string, turns int, failed map[int]bool) string {
	t.Helper()
	home := t.TempDir()
	setHome(t, home)
	projects := filepath.Join(home, ".claude", "projects", "-p")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	lines := []string{`{"type":"ai-title","aiTitle":"the branch","sessionId":"` + session + `"}`}
	prev := ""
	for i := 1; i <= turns; i++ {
		uid := "u" + itoa(i)
		parent := "null"
		if prev != "" {
			parent = `"` + prev + `"`
		}
		when := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		if failed[i] {
			// A tool result marked as an error is what braids records as a
			// failed turn.
			lines = append(lines, `{"type":"user","uuid":"`+uid+`","parentUuid":`+parent+
				`,"timestamp":"`+when+`","cwd":"/tmp/x","message":{"role":"user","content":`+
				`[{"type":"tool_result","is_error":true,"content":"exit status 1"}]}}`)
		} else {
			lines = append(lines, `{"type":"user","uuid":"`+uid+`","parentUuid":`+parent+
				`,"timestamp":"`+when+`","cwd":"/tmp/x","message":{"role":"user","content":"turn `+
				itoa(i)+`"}}`)
		}
		prev = uid
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// The conversation the branch would be joined back into.
	base := `{"type":"ai-title","aiTitle":"the base","sessionId":"` + baseLane + `"}` + "\n" +
		`{"type":"user","uuid":"b1","parentUuid":null,"timestamp":"` +
		at.Format(time.RFC3339) + `","cwd":"/tmp/x","message":{"role":"user","content":"base"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, baseLane+".jsonl"), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// braids has recorded a failed turn on every message since the beginning and
// never read one back. A merge decided on counts alone is a merge decided
// without knowing whether the work succeeded.
func TestMergePlanSaysWhenTheBranchEndsBadly(t *testing.T) {
	const branch = "a1b2c3d4-0000-4000-8000-0000000000f1"
	laneWithFailures(t, branch, 10, map[int]bool{9: true})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	var got struct {
		Failed     int `json:"incoming_failed_turns"`
		LastFailed int `json:"incoming_last_failed_turn"`
	}
	raw := runCmd(t, "merge", "--db", db, "--lane", baseLane, "--from", branch, "--plan", "--json")
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Failed != 1 || got.LastFailed != 9 {
		t.Errorf("the plan reported %d failures, last at turn %d; want 1 at turn 9",
			got.Failed, got.LastFailed)
	}
}

// Silence when nothing failed. A line saying "0 turns failed" on every merge
// is a line nobody reads by the third time.
func TestMergePlanSaysNothingWhenNothingFailed(t *testing.T) {
	const branch = "a1b2c3d4-0000-4000-8000-0000000000f2"
	laneWithFailures(t, branch, 6, nil)
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)

	out := runCmd(t, "merge", "--db", db, "--lane", baseLane, "--from", branch, "--plan")
	if strings.Contains(out, "failed") {
		t.Errorf("a clean branch was reported as having failures:\n%s", out)
	}
}

// How near the end the failure was is the part that decides whether it is
// unfinished work or something recovered from.
func TestMergePlanTellsRecoveredFromUnfinished(t *testing.T) {
	const early = "a1b2c3d4-0000-4000-8000-0000000000f3"
	laneWithFailures(t, early, 40, map[int]bool{2: true})
	db := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db)
	out := runCmd(t, "merge", "--db", db, "--lane", baseLane, "--from", early, "--plan")
	if strings.Contains(out, "ends badly") {
		t.Errorf("a failure at turn 2 of 40 was called an ending:\n%s", out)
	}
	if !strings.Contains(out, "failed") {
		t.Errorf("an early failure went unmentioned entirely:\n%s", out)
	}

	const late = "a1b2c3d4-0000-4000-8000-0000000000f4"
	laneWithFailures(t, late, 40, map[int]bool{39: true})
	db2 := filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--db", db2)
	out = runCmd(t, "merge", "--db", db2, "--lane", baseLane, "--from", late, "--plan")
	if !strings.Contains(out, "ends badly") {
		t.Errorf("a failure at turn 39 of 40 was not called an ending:\n%s", out)
	}
}
