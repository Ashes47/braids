package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// laneWith writes one conversation with the turns given, in order, and returns
// the root to index and the session id. A turn whose text is empty is a tool
// call, which is what two thirds of a real conversation is.
func laneWith(t *testing.T, said []string) (root, session string) {
	t.Helper()
	root = t.TempDir()
	projects := filepath.Join(root, "projects", "-p")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	session = "a1b2c3d4-0000-4000-8000-00000000aaaa"
	at := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	lines := []string{`{"type":"ai-title","aiTitle":"the lock","sessionId":"` + session + `"}`}
	prev := ""
	for i, text := range said {
		uid := "u" + strings.Repeat("x", i%3) + itoa(i+1)
		parent := "null"
		if prev != "" {
			parent = `"` + prev + `"`
		}
		when := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		if text == "" {
			lines = append(lines, `{"type":"assistant","uuid":"`+uid+`","parentUuid":`+parent+
				`,"timestamp":"`+when+`","message":{"role":"assistant","content":`+
				`[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`)
		} else {
			body, err := json.Marshal(text)
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, `{"type":"user","uuid":"`+uid+`","parentUuid":`+parent+
				`,"timestamp":"`+when+`","cwd":"/tmp/x","message":{"role":"user","content":`+
				string(body)+`}}`)
		}
		prev = uid
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projects, session+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, session
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func showable(t *testing.T, said []string) (db, session string) {
	t.Helper()
	root, session := laneWith(t, said)
	db = filepath.Join(t.TempDir(), "index.db")
	runCmd(t, "index", "--root", filepath.Join(root, "projects"), "--db", db)
	return db, session
}

// The point of the command: a search hit names a turn, and this reads what was
// said around it. Without it a search leaves the reader at a number.
func TestShowReadsTheTurnsAroundOne(t *testing.T) {
	db, session := showable(t, []string{
		"first", "second", "third", "fourth", "fifth", "sixth", "seventh",
	})
	out := runCmd(t, "show", "--db", db, "--lane", session, "--at", "4", "--around", "1")
	for _, want := range []string{"third", "fourth", "fifth"} {
		if !strings.Contains(out, want) {
			t.Errorf("the window is missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"first", "seventh"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the window reached past its bounds and included %q:\n%s", unwanted, out)
		}
	}
}

// A window at the start of a conversation must not ask for turn zero, and one
// past the end must say so rather than printing an empty frame.
func TestShowClampsToTheConversation(t *testing.T) {
	db, session := showable(t, []string{"first", "second", "third"})

	out := runCmd(t, "show", "--db", db, "--lane", session, "--at", "1", "--around", "5")
	if !strings.Contains(out, "first") {
		t.Errorf("a window over the start lost the first turn:\n%s", out)
	}
	out = runCmd(t, "show", "--db", db, "--lane", session, "--at", "900")
	if !strings.Contains(out, "no turns between") {
		t.Errorf("a window past the end said:\n%s", out)
	}
}

// Two thirds of a real conversation is tool calls. Asking for text is asking
// for the turns that have some, not for a list of the ones that do not.
func TestShowFilteringDropsTheTurnsThatHaveNothing(t *testing.T) {
	db, session := showable(t, []string{"spoken", "", "", "also spoken"})
	raw := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "1", "--to", "4", "--kind", "text", "--json")
	var got struct {
		Shown []struct {
			Seq   int `json:"turn"`
			Parts []struct {
				Body string `json:"body"`
			} `json:"parts"`
		} `json:"shown"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	// Turns 2 and 3 are tool calls and must not come back at all, as opposed
	// to coming back carrying nothing.
	var seen []int
	for _, turn := range got.Shown {
		seen = append(seen, turn.Seq)
	}
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 4 {
		t.Fatalf("filtering to text returned turns %v, want [1 4]", seen)
	}
	if got.Shown[0].Parts[0].Body != "spoken" || got.Shown[1].Parts[0].Body != "also spoken" {
		t.Errorf("filtering to text lost what was said: %+v", got.Shown)
	}
}

// A tool result can be a whole file. What is printed is bounded, and it says
// how much it took off rather than letting the reader believe they have it all.
func TestShowSaysWhatItCutOff(t *testing.T) {
	long := strings.Repeat("abcdefghij", 200) // 2000 characters
	db, session := showable(t, []string{long})
	out := runCmd(t, "show", "--db", db, "--lane", session, "--from", "1", "--to", "1", "--chars", "100")
	if strings.Contains(out, long) {
		t.Error("the whole block was printed despite --chars")
	}
	if !strings.Contains(out, "more") {
		t.Errorf("nothing said that the block had been cut:\n%s", out)
	}
	// And with the limit off, all of it.
	whole := runCmd(t, "show", "--db", db, "--lane", session, "--from", "1", "--to", "1", "--chars", "0")
	if !strings.Contains(whole, long) {
		t.Error("--chars 0 did not print the whole block")
	}
}

// The JSON is what an agent reads: whole ids, and a list where there is
// nothing rather than a null it has to guard against.
func TestShowJSONCarriesWholeIDsAndEmptyLists(t *testing.T) {
	db, session := showable(t, []string{"first", "second", "third"})
	raw := runCmd(t, "show", "--db", db, "--lane", session[:8], "--at", "2", "--around", "0", "--json")

	var got struct {
		Lane  string `json:"lane"`
		Turns int    `json:"turns_in_conversation"`
		Shown []struct {
			Seq   int    `json:"turn"`
			ID    string `json:"message"`
			Parts []struct {
				Kind string `json:"kind"`
				Body string `json:"body"`
			} `json:"parts"`
		} `json:"shown"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Lane != session {
		t.Errorf("lane came back as %q, not the whole id", got.Lane)
	}
	if got.Turns != 3 {
		t.Errorf("the conversation is %d turns, want 3", got.Turns)
	}
	if len(got.Shown) != 1 || got.Shown[0].Seq != 2 {
		t.Fatalf("--around 0 showed %d turns: %+v", len(got.Shown), got.Shown)
	}
	if got.Shown[0].ID == "" {
		t.Error("no message id came back, so the turn cannot be pointed at again")
	}
	if body := got.Shown[0].Parts[0].Body; body != "second" {
		t.Errorf("the turn's text came back as %q", body)
	}
}

// The two ways of asking for a window are alternatives, and asking for both is
// a mistake worth naming rather than silently resolving.
func TestShowRefusesContradictoryBounds(t *testing.T) {
	db, session := showable(t, []string{"first"})
	err := run([]string{"show", "--db", db, "--lane", session, "--at", "2", "--from", "1"}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("asking for both bounds said: %v", err)
	}
	err = run([]string{"show", "--db", db}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "--lane") {
		t.Errorf("show with no lane said: %v", err)
	}
}

// Two different nothings. A window past the end of a conversation and a window
// full of turns that the filter emptied are not the same thing, and saying the
// first when it is the second sends the reader looking for a conversation that
// is right in front of them. Found by running the command against a real
// index, where a search hit landed on a run of tool calls.
func TestShowTellsAnEmptyWindowFromAnEmptyFilter(t *testing.T) {
	db, session := showable(t, []string{"spoken", "", "", "", "also spoken"})

	// Turns 2 to 4 exist and are all tool calls.
	filtered := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "2", "--to", "4", "--kind", "text")
	if !strings.Contains(filtered, "nothing of the kinds asked for") {
		t.Errorf("a window emptied by the filter said:\n%s", filtered)
	}
	if strings.Contains(filtered, "no turns between") {
		t.Errorf("a window emptied by the filter claimed the turns do not exist:\n%s", filtered)
	}

	// Turns 900 to 910 genuinely are not there.
	beyond := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "900", "--to", "910", "--kind", "text")
	if !strings.Contains(beyond, "no turns between") {
		t.Errorf("a window past the end said:\n%s", beyond)
	}

	// And the JSON says which, without the reader having to parse prose.
	raw := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "2", "--to", "4", "--kind", "text", "--json")
	var got struct {
		InView int `json:"turns_in_window"`
		Shown  []struct {
			Seq int `json:"turn"`
		} `json:"shown"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.InView != 3 {
		t.Errorf("turns_in_window is %d, want the 3 turns that are there", got.InView)
	}
	if len(got.Shown) != 0 {
		t.Errorf("shown has %d turns, want none to survive the filter", len(got.Shown))
	}
}

// A program that finds a terminal on the other end writes colour, and the
// harness records the bytes it wrote. braids reports what was written, so the
// codes are there by default and go only when asked for.
func TestShowLeavesTerminalColourAloneUntilAsked(t *testing.T) {
	coloured := "\x1b[2m Test Files \x1b[22m \x1b[1m\x1b[31m1 failed\x1b[39m\x1b[22m | 64 passed"
	db, session := showable(t, []string{coloured})

	raw := runCmd(t, "show", "--db", db, "--lane", session, "--from", "1", "--to", "1", "--chars", "0")
	if !strings.Contains(raw, "\x1b[31m") {
		t.Error("the default edited the transcript, which it must not do")
	}

	plain := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "1", "--to", "1", "--chars", "0", "--plain")
	if strings.Contains(plain, "\x1b") {
		t.Errorf("--plain left an escape behind: %q", plain)
	}
	// What was said has to survive, including the words the codes were wrapped
	// around: "1 failed" has eleven escape bytes sitting inside that phrase.
	if !strings.Contains(plain, "1 failed | 64 passed") {
		t.Errorf("--plain took the words with the colour:\n%s", plain)
	}
}

// The stripping runs before the block is shortened. A budget of characters
// spent on codes for a terminal that no longer exists is spent on nothing, and
// the whole reason to ask for plain text is to fit more of the words.
func TestShowStripsBeforeItTruncates(t *testing.T) {
	// 40 characters of words, wrapped in 20 characters of escapes.
	body := "\x1b[1m\x1b[31m" + strings.Repeat("word ", 8) + "\x1b[39m\x1b[22m" + "TAIL"
	db, session := showable(t, []string{body})

	raw := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "1", "--to", "1", "--chars", "44")
	plain := runCmd(t, "show", "--db", db, "--lane", session,
		"--from", "1", "--to", "1", "--chars", "44", "--plain")

	// With the codes counted, 44 characters cannot reach the end. Without
	// them, they can.
	if strings.Contains(raw, "TAIL") {
		t.Error("the raw form reached the end of the block, so the budget is not being counted")
	}
	if !strings.Contains(plain, "TAIL") {
		t.Errorf("--plain still spent its budget on escape codes:\n%s", plain)
	}
}

// Text that was never coloured must come back exactly as it was.
func TestPlainTextLeavesOrdinaryTextAlone(t *testing.T) {
	for _, body := range []string{
		"", "just words", "a [bracket] and a ] and a \\ backslash",
		"one\ntwo\nthree", "0x1b is not an escape",
	} {
		if got := plainText(body); got != body {
			t.Errorf("plainText(%q) = %q", body, got)
		}
	}
	// And the shapes that are escapes do go.
	for _, c := range []struct{ in, want string }{
		{"\x1b[0ma", "a"},
		{"\x1b[38;2;240;136;62mb", "b"},
		{"\x1b]8;;https://example.invalid\x07link\x1b]8;;\x07", "link"},
		{"\x1b[2K\x1b[1Gc", "c"},
	} {
		if got := plainText(c.in); got != c.want {
			t.Errorf("plainText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
