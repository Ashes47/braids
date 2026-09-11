package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/release"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
	"github.com/Ashes47/braids/internal/skill"
)

// doctor answers one question: can you trust what braids is about to tell you.
//
// Every check here already existed and each was somewhere else. Whether the
// index is current came out of `braids index`, whether the hook reports out of
// `braids hooks`, lost memories out of `braids memories`, ownerless work
// products out of `braids work --orphans`, the build's age out of `braids
// version`. Five commands to find out whether the sixth can be believed.
//
// It never writes. A check that fixed things would be a check nobody could run
// safely on a machine they were unsure about, and the point of running it is
// that you are unsure.

// liveWrite is how recently a transcript must have moved to be treated as one
// being written now rather than one the index has fallen behind. Long enough
// to cover the gap between a turn landing and the check running, short enough
// that a session somebody left an hour ago still counts as unindexed.
const liveWrite = 2 * time.Minute

// checkup is one answer, and what to do when it is not the good one.
type checkup struct {
	Name   string `json:"check"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func cmdDoctor(args []string, out *printer) error {
	fs := newFlagSet("doctor")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}

	ctx := context.Background()
	checks := []checkup{}
	add := func(c checkup) { checks = append(checks, c) }

	path, err := resolveDB(*db)
	if err != nil {
		return err
	}
	root, rootErr := claudecode.DefaultRoot()
	var src *claudecode.Source
	if rootErr == nil {
		src = claudecode.New(root)
	}

	add(indexCheck(ctx, path, src))
	if ix, err := openExisting(path); err == nil {
		defer ix.Close() //nolint:errcheck // read-only
		add(readableCheck(ctx, ix))
		if src != nil {
			add(memoryCheck(src))
		}
	}
	add(hookCheck())
	add(skillCheck())
	add(buildCheck())

	if *asJSON {
		bad := 0
		for _, c := range checks {
			if !c.OK {
				bad++
			}
		}
		return out.emit(struct {
			Version string    `json:"version"`
			Checks  []checkup `json:"checks"`
			Healthy bool      `json:"healthy"`
			Notes   int       `json:"notes"`
		}{version, checks, bad == 0, bad})
	}
	printCheckups(checks, out)
	return out.Err()
}

// indexCheck asks whether the index exists and still matches the transcripts.
//
// Comparing size and modification time is what Sync does to decide whether to
// re-read a lane, so this is that decision without the reading: it says how
// many conversations would be picked up, and never picks them up.
func indexCheck(ctx context.Context, path string, src *claudecode.Source) checkup {
	ix, err := openExisting(path)
	if err != nil {
		return checkup{"index", false, err.Error(), "braids index"}
	}
	defer ix.Close() //nolint:errcheck // read-only

	known, err := ix.Lanes(ctx)
	if err != nil {
		return checkup{"index", false, err.Error(), "braids index"}
	}
	messages := 0
	stored := make(map[string]index.LaneInfo, len(known))
	for _, l := range known {
		messages += l.Messages
		stored[l.ID] = l
	}
	held := fmt.Sprintf("%s, %s", plural(len(known), "conversation"), plural(messages, "message"))
	if src == nil {
		return checkup{"index", true, held, ""}
	}

	onDisk, err := src.Lanes(ctx)
	if err != nil {
		return checkup{"index", true, held + ", and the transcripts could not be listed", ""}
	}
	// A transcript touched in the last couple of minutes is one being written
	// right now, which is almost certainly the session running this. Counting
	// it would make the check say "behind" every single time it was run, and a
	// check that can never pass is a check nobody reads.
	behind, live := 0, 0
	for _, lane := range onDisk {
		was, seen := stored[lane.ID]
		if seen && was.Size == lane.Size && was.Updated.Unix() == lane.Updated.Unix() {
			continue
		}
		if time.Since(lane.Updated) < liveWrite {
			live++
			continue
		}
		// A file that holds no turns is session state, and Sync skips it on
		// purpose. Counting it here would report an index that can never be
		// made current, because indexing will never store it. Asked only
		// about files that look unindexed, which is rare.
		if turns, err := src.HasTurns(ctx, lane); err == nil && !turns {
			continue
		}
		behind++
	}
	switch {
	case behind == 0 && live == 0:
		return checkup{"index", true, held + ", current", ""}
	case behind == 0:
		return checkup{"index", true,
			held + ", current apart from a conversation being written right now", ""}
	default:
		return checkup{"index", false,
			fmt.Sprintf("%s, and %s changed since the last index",
				held, plural(behind, "conversation")),
			"braids index"}
	}
}

// readableCheck is the one thing braids cannot otherwise notice about itself.
func readableCheck(ctx context.Context, ix *index.Index) checkup {
	unreadable, err := ix.Unreadable(ctx)
	if err != nil {
		return checkup{"transcripts", false, err.Error(), ""}
	}
	if len(unreadable) == 0 {
		return checkup{"transcripts", true, "all readable", ""}
	}
	return checkup{"transcripts", false,
		fmt.Sprintf("%s hold bytes braids reads nothing from", plural(len(unreadable), "conversation")),
		"report it: https://github.com/Ashes47/braids/issues"}
}

// memoryCheck reports what a project remembers and has lost track of.
func memoryCheck(src *claudecode.Source) checkup {
	locations, err := src.MemoryDirs()
	if err != nil {
		return checkup{"memories", true, "no memory directories", ""}
	}
	total, orphaned, dangling := 0, 0, 0
	for _, location := range locations {
		set, err := memory.Read(location)
		if err != nil {
			continue
		}
		total += len(set.Memories)
		orphaned += len(set.Orphaned)
		dangling += len(set.Dangling())
	}
	// Only the problems that are actually there. "0 rows listed but missing"
	// is a line that makes somebody go and look for nothing.
	var wrong []string
	if orphaned > 0 {
		wrong = append(wrong, plural(orphaned, "row")+" listed but missing")
	}
	if dangling > 0 {
		wrong = append(wrong, plural(dangling, "link")+" pointing nowhere")
	}
	if len(wrong) == 0 {
		return checkup{"memories", true, plural(total, "memory") + ", all accounted for", ""}
	}
	return checkup{"memories", false,
		plural(total, "memory") + ", " + strings.Join(wrong, " and "), "braids memories"}
}

// hookCheck says whether waiting states can be believed.
func hookCheck() checkup {
	home, err := os.UserHomeDir()
	if err != nil {
		return checkup{"hook", false, "cannot find your home directory", ""}
	}
	command, err := hookCommand()
	if err != nil {
		return checkup{"hook", false, err.Error(), ""}
	}
	status, err := hooks.Inspect(filepath.Join(home, ".claude", "settings.json"), command)
	if err != nil {
		return checkup{"hook", false, err.Error(), "braids hooks --install"}
	}
	switch {
	case len(status.Events) == 0:
		return checkup{"hook", false,
			"not installed, so braids cannot tell a running tool from one waiting on you",
			"braids hooks --install"}
	case len(status.Elsewhere) > 0:
		return checkup{"hook", false,
			fmt.Sprintf("reporting %d events, with %s pointing at another braids",
				len(status.Events), plural(len(status.Elsewhere), "hook")),
			"braids hooks --install"}
	default:
		return checkup{"hook", true, fmt.Sprintf("reporting %d events", len(status.Events)), ""}
	}
}

// skillCheck catches the skill an older braids left in place. One that
// describes flags a command no longer takes is worse than none at all.
func skillCheck() checkup {
	home, err := os.UserHomeDir()
	if err != nil {
		return checkup{"skill", false, "cannot find your home directory", ""}
	}
	state, err := skill.Read(filepath.Join(home, ".claude", "skills"))
	if err != nil {
		return checkup{"skill", false, err.Error(), "braids skill --install"}
	}
	plugged := skill.PluginPath(filepath.Join(home, ".claude", "plugins"))
	switch {
	case plugged != "" && state.Installed:
		// Both. Claude loads the same instructions twice, under `braids` and
		// `braids:braids`, and nothing else says so because from Claude
		// Code's side they are two skills that happen to agree.
		return checkup{"skill", false,
			"installed twice, by hand and by the plugin, so Claude loads it under two names",
			"braids skill --remove, and keep the plugin"}
	case plugged != "":
		return checkup{"skill", true, "installed by the plugin", ""}
	case !state.Installed:
		return checkup{"skill", false, "not installed", "braids skill --install"}
	case !state.Current:
		return checkup{"skill", false, "installed, but written by another version of braids",
			"braids skill --install"}
	default:
		return checkup{"skill", true, "installed and current", ""}
	}
}

// buildCheck reads this binary's age off the disk, and makes no network call
// to do it. Old is not broken, so this is a note and never a failure.
func buildCheck() checkup {
	home, err := os.UserHomeDir()
	if err != nil {
		return checkup{"build", true, version, ""}
	}
	exe, err := os.Executable()
	if err != nil {
		return checkup{"build", true, version, ""}
	}
	state := release.Read(home, exe)
	age, known := state.BuildAge(time.Now())
	if !known {
		return checkup{"build", true, version, ""}
	}
	// Age says "today" and "yesterday" for a new build and "3 days" for an
	// old one, so only the second kind takes an "ago".
	when := release.Age(age)
	if when != "today" && when != "yesterday" {
		when += " ago"
	}
	detail := fmt.Sprintf("%s, built %s", version, when)
	if state.Due(time.Now()) {
		return checkup{"build", true, detail + ", worth checking for a newer one",
			installCommand}
	}
	return checkup{"build", true, detail, ""}
}

func printCheckups(checks []checkup, out *printer) {
	notes := 0
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, c := range checks {
		mark := "ok"
		if !c.OK {
			mark = "note"
			notes++
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", mark, c.Name, c.Detail) //nolint:errcheck // tabwriter, flushed below
		if c.Fix != "" {
			fmt.Fprintf(tw, "  \t\t%s\n", c.Fix) //nolint:errcheck // tabwriter, flushed below
		}
	}
	_ = tw.Flush()
	if notes == 0 {
		out.printf("\nNothing to do.\n")
		return
	}
	out.printf("\n%s above. braids still answers; each line says what it is short of.\n",
		plural(notes, "note"))
}
