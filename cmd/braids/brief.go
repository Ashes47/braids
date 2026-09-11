package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
	"github.com/Ashes47/braids/internal/tui"
)

// brief is the state of play where you are standing.
//
// Everything else braids offers answers a question somebody already has:
// search needs words, show needs a turn, explain needs a file. Nothing
// answered the question you have before you have any of those, which is what
// was happening here and where it was left.
//
// It is bounded on purpose and by construction. A caller gets the most
// recently touched conversations and one clipped line of what each was last
// saying, never a transcript. The point is to spend a few hundred words
// instead of reading a history.

// briefLanes is how many conversations to describe. Enough to see the shape of
// a week, few enough to read.
const briefLanes = 8

// briefSince is how far back a brief looks by default. A fortnight covers
// coming back from a week away, which is when this is worth running.
const briefSince = 14 * 24 * time.Hour

// briefLookBack is how far into a conversation to reach for the last thing
// actually said. Two thirds of a real conversation is tool calls and their
// results, so a short window often lands in the middle of a run of them.
const briefLookBack = 24 * time.Hour

// briefSaid is how many of the conversations get a line of what they were
// saying. The list is a glance; eight quotes is a wall.
const briefSaid = 3

type briefLane struct {
	ID      string `json:"lane"`
	Title   string `json:"title"`
	Turns   int    `json:"turns"`
	Age     string `json:"age"`
	Waiting bool   `json:"waiting"`
	Said    string `json:"last_said,omitempty"`
	Resume  string `json:"open"`
}

func cmdBrief(args []string, out *printer) error {
	fs := newFlagSet("brief")
	db := fs.String("db", "", "index location")
	project := fs.String("project", "", "which project, default the one you are standing in")
	since := fs.Duration("since", briefSince, "how far back to look")
	limit := fs.Int("limit", briefLanes, "how many conversations to describe")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}

	dbPath, err := resolveDB(*db)
	if err != nil {
		return err
	}
	ix, err := openExisting(dbPath)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	all, err := ix.Lanes(ctx)
	if err != nil {
		return err
	}
	name, lanes := narrowToProject(all, *project)
	now := time.Now()

	recent := make([]index.LaneInfo, 0, len(lanes))
	for _, l := range lanes {
		if *since <= 0 || now.Sub(l.Updated) <= *since {
			recent = append(recent, l)
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].Updated.After(recent[j].Updated) })

	live := map[string]hooks.Event{}
	if events, err := hooks.Read(filepath.Join(filepath.Dir(dbPath), "events.jsonl")); err == nil {
		live = hooks.Latest(events)
	}
	owed := tui.Waiting(recent, live, now, 0)

	shown := recent
	if len(shown) > *limit {
		shown = shown[:*limit]
	}
	rows := make([]briefLane, 0, len(shown))
	for _, l := range shown {
		row := briefLane{
			ID: l.ID, Title: orUnnamed(l.Title), Turns: l.Messages,
			Age:     tui.HumanAge(now.Sub(l.Updated)),
			Waiting: tui.Waiting([]index.LaneInfo{l}, live, now, 0) == 1,
			Resume:  "braids show --lane " + idPrefix(l.ID),
		}
		// One line of what it was last actually saying, which is the only
		// thing here that needs the transcript rather than the index. Two
		// thirds of any conversation is tool calls, so the window has to be
		// wide enough to reach past a run of them to the last thing anybody
		// said.
		if around, err := ix.Around(ctx, l.ID, l.Updated.Add(-briefLookBack), l.Updated); err == nil && around.Spoken {
			row.Said = truncate(collapse(around.Spoke.Preview), 96)
		}
		rows = append(rows, row)
	}

	remembered, lost := projectMemories(name)

	if *asJSON {
		return out.emit(struct {
			Project       string      `json:"project"`
			Conversations int         `json:"conversations"`
			Waiting       int         `json:"waiting"`
			Memories      int         `json:"memories"`
			LostMemories  int         `json:"memories_lost,omitempty"`
			Recent        []briefLane `json:"recent"`
		}{name, len(recent), owed, remembered, lost, rows})
	}
	printBrief(name, recent, owed, remembered, lost, rows, *since, out)
	return out.Err()
}

// narrowToProject picks the conversations worth briefing on.
//
// Named explicitly, or the project of the directory you are standing in, which
// is what somebody means by "here". Falling back to everything is right when
// braids is run somewhere it has never seen: a brief of nothing would be
// technically correct and useless.
func narrowToProject(all []index.LaneInfo, want string) (string, []index.LaneInfo) {
	if want != "" {
		var kept []index.LaneInfo
		for _, l := range all {
			if strings.EqualFold(l.Project, want) {
				kept = append(kept, l)
			}
		}
		return want, kept
	}
	here, err := os.Getwd()
	if err != nil {
		return "everything", all
	}
	var kept []index.LaneInfo
	name := ""
	for _, l := range all {
		if l.Cwd == "" || (!inTree(l.Cwd, here) && !inTree(here, l.Cwd)) {
			continue
		}
		if name == "" {
			name = l.Project
		}
		kept = append(kept, l)
	}
	if len(kept) == 0 {
		return "everything", all
	}
	return name, kept
}

// projectMemories counts what a project remembers, and what it has lost track
// of. Failures are silent: a brief that refused to print because a memory
// directory could not be read would be less useful than one without the line.
func projectMemories(project string) (remembered, lost int) {
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return 0, 0
	}
	locations, err := claudecode.New(root).MemoryDirs()
	if err != nil {
		return 0, 0
	}
	for _, location := range locations {
		if project != "everything" && !strings.EqualFold(location.Project, project) {
			continue
		}
		set, err := memory.Read(location)
		if err != nil {
			continue
		}
		remembered += len(set.Memories)
		lost += len(set.Orphaned) + len(set.Dangling())
	}
	return remembered, lost
}

func printBrief(project string, recent []index.LaneInfo, owed, remembered, lost int,
	rows []briefLane, since time.Duration, out *printer,
) {
	if len(recent) == 0 {
		out.printf("%s: nothing in the last %s\n", project, tui.HumanAge(since))
		return
	}
	out.printf("%s: %s in the last %s", project,
		plural(len(recent), "conversation"), tui.HumanAge(since))
	if owed > 0 {
		out.printf(", %d waiting on you", owed)
	}
	out.printf("\n\n")

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		mark := " "
		if r.Waiting {
			mark = "*"
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%s\t%s\n", //nolint:errcheck // flushed below
			mark, truncate(r.Title, 44), r.Age, plural(r.Turns, "turn"), idPrefix(r.ID))
	}
	_ = tw.Flush()
	said := 0
	for _, r := range rows {
		if r.Said == "" || said >= briefSaid {
			continue
		}
		said++
		out.printf("\n  %s\n    %s\n", truncate(r.Title, 60), r.Said)
	}
	if remembered > 0 {
		out.printf("\n%s remembered", plural(remembered, "memory"))
		if lost > 0 {
			out.printf(", %d of them lost", lost)
		}
		out.printf("\n")
	}
}
