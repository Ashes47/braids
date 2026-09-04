package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
)

// explain answers "where did this file come from" with evidence and nothing
// else.
//
// It knows two things and joins them: git knows when a file changed, and
// braids knows what was being said in that directory at that moment. It does
// not know that the conversation caused the commit, and it never says so. What
// it says is which conversations were live when the change landed, how close
// they were, and how to open them. The reader draws the conclusion.
//
// That restraint is also what makes it cheap. Claiming causation would need a
// model reading both sides; claiming coincidence in time needs a git log and
// two columns braids already stores.

// defaultWindow is how long before a commit a turn can be and still count as
// context for it. Long enough to cover the session that led to a change,
// short enough that an unrelated conversation the same morning does not.
const defaultWindow = 3 * time.Hour

// change is one commit that touched the file.
type change struct {
	Hash    string    `json:"commit"`
	At      time.Time `json:"at"`
	Subject string    `json:"subject"`
	Near    []nearby  `json:"conversations"`
}

// nearby is a conversation that was being written in the window before a
// commit, with the evidence for saying so.
type nearby struct {
	Lane    string    `json:"lane"`
	Title   string    `json:"title"`
	Project string    `json:"project"`
	Turns   int       `json:"turns_in_window"`
	Gap     string    `json:"nearest_turn_before"`
	Seq     int       `json:"turn"`
	Preview string    `json:"preview"`
	At      time.Time `json:"at"`
	Resume  string    `json:"open"`
}

func cmdExplain(args []string, out *printer) error {
	fs := newFlagSet("explain")
	fs.SetOutput(out)
	db := fs.String("db", "", "index location")
	limit := fs.Int("limit", 5, "how many commits to look back over")
	window := fs.Duration("window", defaultWindow,
		"how long before a commit a turn still counts as context")
	asJSON := jsonFlag(fs)
	rest, err := parseArgs(fs, args, out)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("explain takes one file (try: braids explain internal/core/index/index.go)")
	}

	path, err := filepath.Abs(rest[0])
	if err != nil {
		return err
	}
	repo, err := repoOf(filepath.Dir(path))
	if err != nil {
		return err
	}
	commits, err := commitsTouching(repo, path, *limit)
	if err != nil {
		return err
	}
	if len(commits) == 0 {
		return fmt.Errorf("git has no commits touching %s", rest[0])
	}

	ix, err := openIndex(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	lanes, err := ix.LanesIn(ctx, repoNames(repo)...)
	if err != nil {
		return err
	}
	for i := range commits {
		commits[i].Near, err = conversationsAround(ctx, ix, lanes, commits[i].At, *window)
		if err != nil {
			return err
		}
	}

	if *asJSON {
		return out.emit(struct {
			File    string   `json:"file"`
			Repo    string   `json:"repo"`
			Commits []change `json:"commits"`
		}{path, repo, commits})
	}
	printExplanation(rest[0], lanes, commits, *window, out)
	return out.Err()
}

// repoOf finds the repository a directory belongs to.
func repoOf(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	top, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository, so there are no commits to explain", dir)
	}
	return strings.TrimSpace(string(top)), nil
}

// repoNames is every path this repository might be recorded under.
//
// git resolves symlinks when it reports a root and a shell does not, so the
// same checkout is /private/tmp/x to one and /tmp/x to the other. Both are
// offered, because a conversation is filed under whichever the session saw.
func repoNames(repo string) []string {
	names := []string{repo}
	if resolved, err := filepath.EvalSymlinks(repo); err == nil && resolved != repo {
		names = append(names, resolved)
	}
	// And the other direction: git already resolved, so walk back up looking
	// for a parent that is a symlink to what we hold.
	if unresolved := unresolve(repo); unresolved != "" && unresolved != repo {
		names = append(names, unresolved)
	}
	return names
}

// unresolve finds a path that resolves to repo but is spelled differently, by
// checking the well-known places a system symlinks. It is a best effort: a
// transcript recorded under a name nothing can rediscover simply will not
// match, and explain says it found nothing rather than guessing.
func unresolve(repo string) string {
	for from, to := range map[string]string{"/private/var": "/var", "/private/tmp": "/tmp"} {
		if rest, ok := strings.CutPrefix(repo, from); ok {
			return to + rest
		}
	}
	return ""
}

// commitsTouching lists the commits that changed a file, newest first, and
// follows it through renames because a file that was moved still has a past.
func commitsTouching(repo, path string, limit int) ([]change, error) {
	cmd := exec.Command("git", "-C", repo, "log", "--follow",
		"--max-count="+strconv.Itoa(limit), "--format=%H%x1f%at%x1f%s", "--", path)
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log %s: %w", path, err)
	}
	var out []change
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Split(line, "\x1f")
		if len(fields) != 3 {
			continue
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, change{Hash: fields[0], At: time.Unix(seconds, 0), Subject: fields[2]})
	}
	return out, nil
}

// conversationsAround finds which conversations were being written in the
// window before a commit, best evidence first.
//
// Better evidence is more turns close to the moment, because a session that
// wrote forty turns in the hour before a commit is a likelier account of it
// than one that wrote a single line three hours earlier. Ties go to whichever
// spoke last.
func conversationsAround(ctx context.Context, ix *index.Index, lanes []index.LaneInfo,
	at time.Time, window time.Duration) ([]nearby, error) {
	var found []nearby
	for _, lane := range lanes {
		around, err := ix.Around(ctx, lane.ID, at.Add(-window), at)
		if err != nil {
			return nil, err
		}
		if around.Turns == 0 {
			continue
		}
		n := nearby{
			Lane:    lane.ID,
			Title:   orUnnamed(lane.Title),
			Project: lane.Project,
			Turns:   around.Turns,
			Resume:  "braids --lane " + idPrefix(lane.ID),
		}
		if around.Spoken {
			n.Gap = humanGap(at.Sub(around.Spoke.At))
			n.Seq = around.Spoke.Seq
			n.Preview = around.Spoke.Preview
			n.At = around.Spoke.At
		}
		found = append(found, n)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Turns != found[j].Turns {
			return found[i].Turns > found[j].Turns
		}
		return found[i].At.After(found[j].At)
	})
	return found, nil
}

// idPrefix is the short form of an ID, without the ellipsis the tables use.
// This one goes into a command somebody is meant to run.
func idPrefix(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// collapse puts a preview on one line, since a turn's first words can wrap.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// humanGap says how far apart two moments are, roundly.
func humanGap(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute before"
	case d < 2*time.Minute:
		return "a minute before"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes before", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour before"
	default:
		return fmt.Sprintf("%.1f hours before", d.Hours())
	}
}

func printExplanation(name string, lanes []index.LaneInfo, commits []change,
	window time.Duration, out *printer) {
	out.printf("%s\n", name)
	out.printf("%d commits, against %d conversations that ran in this repository\n",
		len(commits), len(lanes))
	if len(lanes) == 0 {
		out.printf("\nbraids has indexed no conversations with this repository as their\n" +
			"working directory, so it has nothing to offer about this file.\n")
		return
	}
	for _, c := range commits {
		out.printf("\n%s  %s  %s\n", c.Hash[:8], c.At.Format("2006-01-02 15:04"), c.Subject)
		if len(c.Near) == 0 {
			out.printf("    nothing was being written here in the %s before it\n",
				window.String())
			continue
		}
		for i, n := range c.Near {
			if i == 2 {
				out.printf("    ... and %d more\n", len(c.Near)-2)
				break
			}
			out.printf("    %s  ·  %s  ·  %d turns in the window\n", n.Title, n.Project, n.Turns)
			if n.Preview != "" {
				out.printf("      last said %s, at turn %d:\n", n.Gap, n.Seq)
				out.printf("        %s\n", truncate(collapse(n.Preview), 66))
			} else {
				out.printf("      nothing but tool calls in the window\n")
			}
			out.printf("      %s\n", n.Resume)
		}
	}
	out.printf("\nThese conversations were live when those commits landed. braids does not\n")
	out.printf("know that they caused them; it knows when things happened and where.\n")
}
