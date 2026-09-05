package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
)

// show reads a run of turns out of a conversation.
//
// It exists because everything else braids tells you about history ends in a
// pointer to it. A search hit names a conversation and a turn and gives back
// twelve words of snippet; explain names a conversation and the last thing
// said in it. Both then leave you at a turn number with no way to read the
// turns around it, unless you are a person who can open the map. Something
// reading history rather than browsing it could find where an answer was and
// never read the answer.
//
// Bounded by construction. A conversation runs to tens of thousands of turns
// and tool output to megabytes, so this asks for a window and truncates what
// it prints, and says so both times rather than quietly handing back less
// than it found.

// aroundBy is how many turns either side of one turn make useful context: far
// enough back for the question, far enough forward for the answer.
const aroundBy = 6

// bodyChars is how much of one block to print before cutting it. Tool results
// carry whole files and whole test runs, and a reader that wanted those would
// open the file.
const bodyChars = 600

type showPart struct {
	Kind string `json:"kind"`
	Tool string `json:"tool,omitempty"`
	Body string `json:"body"`
	Cut  int    `json:"characters_cut,omitempty"`
}

type showTurn struct {
	Seq    int        `json:"turn"`
	ID     string     `json:"message"`
	Role   string     `json:"role"`
	At     string     `json:"at"`
	Failed bool       `json:"failed,omitempty"`
	Parts  []showPart `json:"parts"`
}

func cmdShow(args []string, out *printer) error {
	fs := newFlagSet("show")
	laneRef := fs.String("lane", "", "conversation to read")
	db := fs.String("db", "", "index location")
	at := fs.Int("at", 0, "centre the window on this turn")
	around := fs.Int("around", aroundBy, "turns either side of --at")
	from := fs.Int("from", 0, "first turn")
	to := fs.Int("to", 0, "last turn")
	kinds := fs.String("kind", "", "comma-separated part kinds: text, thinking, tool_use, tool_result")
	chars := fs.Int("chars", bodyChars, "characters of each block to print, 0 for all of it")
	plain := fs.Bool("plain", false, "strip the terminal colour codes recorded in tool output")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	if *laneRef == "" {
		return errors.New("show needs --lane (try: braids search QUERY --json)")
	}
	if *at != 0 && (*from != 0 || *to != 0) {
		return errors.New("show takes --at or --from/--to, not both")
	}

	ix, err := openIndex(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	lane, err := findLane(ctx, ix, *laneRef)
	if err != nil {
		return err
	}

	first, last := windowOf(*at, *around, *from, *to, lane.Messages)
	turns, err := ix.Window(ctx, lane.ID, first, last)
	if err != nil {
		return err
	}
	wanted, err := index.ParseKinds(*kinds)
	if err != nil {
		return err
	}

	rows := make([]showTurn, 0, len(turns))
	for _, t := range turns {
		row := showTurn{
			Seq: t.Seq, ID: t.ID, Role: string(t.Role),
			At: t.At.Format("2006-01-02 15:04"), Failed: t.Failed,
			Parts: []showPart{},
		}
		for _, p := range t.Parts {
			if !keeps(wanted, p.Kind) {
				continue
			}
			body := p.Body
			if *plain {
				body = plainText(body)
			}
			body, cut := clip(body, *chars)
			row.Parts = append(row.Parts, showPart{
				Kind: string(p.Kind), Tool: p.Tool, Body: body, Cut: cut,
			})
		}
		// Asking for one kind of block is asking for the turns that have one.
		// Two thirds of a real conversation is tool calls, so listing them as
		// empty would bury the three turns that were an answer to the question.
		if len(row.Parts) == 0 && len(wanted) > 0 {
			continue
		}
		rows = append(rows, row)
	}

	if *asJSON {
		return out.emit(struct {
			Lane   string     `json:"lane"`
			Title  string     `json:"title"`
			Turns  int        `json:"turns_in_conversation"`
			From   int        `json:"from"`
			To     int        `json:"to"`
			InView int        `json:"turns_in_window"`
			Shown  []showTurn `json:"shown"`
		}{lane.ID, lane.Title, lane.Messages, first, last, len(turns), rows})
	}
	printTurns(lane, first, last, len(turns), rows, out)
	return out.Err()
}

// windowOf works out which turns to read.
//
// With --at it is a window around one turn, which is what a search hit or an
// explanation leaves you holding. With --from and --to it is what was asked
// for. With neither it is the end of the conversation, because the question
// somebody asks about a conversation they have not opened is how it went.
func windowOf(at, around, from, to, turns int) (first, last int) {
	switch {
	case at > 0:
		return at - around, at + around
	case from > 0 || to > 0:
		if from == 0 {
			from = 1
		}
		if to == 0 {
			to = turns
		}
		return from, to
	default:
		return turns - 2*around, turns
	}
}

// keeps reports whether a block is one the reader asked for. No kinds named
// means all of them: narrowing is a choice, not a default.
func keeps(wanted []model.PartKind, kind model.PartKind) bool {
	if len(wanted) == 0 {
		return true
	}
	for _, w := range wanted {
		if w == kind {
			return true
		}
	}
	return false
}

// escapes matches what a program writes to colour a terminal: the CSI
// sequences that carry colour and cursor movement, and the OSC sequences that
// carry titles and hyperlinks, each to its own terminator.
var escapes = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]" + // CSI
	"|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" + // OSC
	"|\x1b[@-Z\\\\-_]") // the short two-byte ones

// plainText takes the colour out of what a tool wrote.
//
// A program that finds a terminal on the other end writes colour, and the
// harness records the bytes it wrote, so a test run that printed "1 failed" in
// red is stored with eleven escape bytes sitting inside that phrase. braids
// reports what was written and does not quietly tidy it, so this happens only
// when it is asked for. It runs before the block is shortened, because a
// budget of characters spent on codes for a terminal that no longer exists is
// a budget spent on nothing.
func plainText(body string) string {
	if !strings.Contains(body, "\x1b") {
		return body
	}
	return escapes.ReplaceAllString(body, "")
}

// clip shortens a block, reporting how much it took off so the reader knows
// there was more rather than believing they have all of it.
func clip(body string, limit int) (string, int) {
	body = strings.TrimRight(body, "\n")
	if limit <= 0 || utf8.RuneCountInString(body) <= limit {
		return body, 0
	}
	kept := 0
	for i := range body {
		if kept == limit {
			return body[:i], utf8.RuneCountInString(body[i:])
		}
		kept++
	}
	return body, 0
}

func printTurns(lane index.LaneInfo, first, last, found int, turns []showTurn, out *printer) {
	title := lane.Title
	if title == "" {
		title = "(unnamed)"
	}
	out.printf("%s  %s\n", shortID(lane.ID), title)
	// Two different nothings, and saying the wrong one sends the reader to
	// look for a conversation that is in front of them. Either the window is
	// past the end of the conversation, or the window is full of turns and
	// every one of them was filtered out.
	if len(turns) == 0 {
		if found > 0 {
			out.printf("turns %d to %d hold nothing of the kinds asked for, out of %s\n",
				first, last, plural(found, "turn"))
			return
		}
		out.printf("no turns between %d and %d; this conversation has %s\n",
			first, last, plural(lane.Messages, "turn"))
		return
	}
	out.printf("turns %d to %d of %d\n", first, last, lane.Messages)

	for _, t := range turns {
		mark := ""
		if t.Failed {
			mark = "  (failed)"
		}
		out.printf("\n%d  %s  %s%s\n", t.Seq, t.Role, t.At, mark)
		for _, p := range t.Parts {
			label := p.Kind
			if p.Tool != "" {
				label = p.Kind + " " + p.Tool
			}
			out.printf("  [%s]\n", label)
			for _, line := range strings.Split(p.Body, "\n") {
				out.printf("    %s\n", line)
			}
			if p.Cut > 0 {
				out.printf("    ... %s more\n", plural(p.Cut, "character"))
			}
		}
	}
}

// plural says a count with its unit, in the one shape English needs.
func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
