// Command braids manages Claude Code conversations as a graph.
//
// This build ships the index and search; the TUI is the next slice.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/sidecar"
	"github.com/Ashes47/braids/internal/core/store"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
	"github.com/Ashes47/braids/internal/core/trash"
	"github.com/Ashes47/braids/internal/core/watch"
	"github.com/Ashes47/braids/internal/launch"
	"github.com/Ashes47/braids/internal/tui"
)

// Set by the release build.
var (
	version = "dev"
	commit  = "none"
)

const usage = `braids — manage Claude Code conversations as a graph

usage:
  braids                                 open the map
  braids index [--full]                  index new and changed transcripts
  braids search [flags] QUERY            search every indexed message
  braids lanes                           list indexed conversations
  braids branch --lane ID --at TURN      cut a new conversation at that turn
                 [--workspace]           ...with a git worktree of its own
  braids agents --lane ID                list the subagents a conversation spawned
  braids promote --lane ID --agent ID    turn a subagent into its own conversation
  braids merge --lane ID --from ID       join a branch back, as a new conversation
  braids hooks [--install|--remove]      let sessions report when they block
  braids version

map flags:
  --ascii          use narrow ASCII glyphs (for terminals that draw
                   box characters double-wide)
  --print          render one frame to stdout instead of opening the map
  --lane ID        with --print, render that lane's spine instead of the map
  --width N        frame width when printing (default 92)

search flags:
  --lane ID        restrict to one conversation
  --kind LIST      text,thinking,tool_use,tool_result (comma separated)
  --limit N        maximum hits (default 20)

common flags:
  --db PATH        index location (default $BRAIDS_DB or ~/.braids/index.db)

environment:
  BRAIDS_SPAWN     command template for 'o' (open a terminal), understanding
                   {cmd} {name} {dir}. tmux and iTerm2 are driven directly
                   without one; elsewhere 'o' copies the command instead.
                   e.g. tmux new-window -c {dir} -n {name} '{cmd}'
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "braids: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, w io.Writer) error {
	out := newPrinter(w)
	if len(args) == 0 {
		return cmdMap(nil, out)
	}
	switch args[0] {
	case "map":
		return cmdMap(args[1:], out)
	case "index":
		return cmdIndex(args[1:], out)
	case "search":
		return cmdSearch(args[1:], out)
	case "lanes":
		return cmdLanes(args[1:], out)
	case "branch":
		return cmdBranch(args[1:], out)
	case "promote":
		return cmdPromote(args[1:], out)
	case "merge":
		return cmdMerge(args[1:], out)
	case "hooks":
		return cmdHooks(args[1:], out)
	case "hook":
		return cmdHook()
	case "agents":
		return cmdAgents(args[1:], out)
	case "version":
		out.printf("braids %s (%s)\n", version, commit)
		return out.Err()
	case "help", "-h", "--help":
		out.printf("%s", usage)
		return out.Err()
	default:
		return fmt.Errorf("unknown command %q (try: braids help)", args[0])
	}
}

// parseArgs parses flags that may appear before, after or between positional
// arguments. Go's flag package stops at the first non-flag argument, which
// would silently ignore "braids search foo --limit 5" — the way people
// actually type it.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// defaultDB resolves the index location and ensures its directory exists.
func defaultDB() (string, error) {
	if p := os.Getenv("BRAIDS_DB"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	dir := filepath.Join(home, ".braids")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return filepath.Join(dir, "index.db"), nil
}

// resolveDB picks the index path: the flag if given, else the default.
func resolveDB(dbFlag string) (string, error) {
	if dbFlag != "" {
		return dbFlag, nil
	}
	return defaultDB()
}

// openIndex resolves the database path and opens it.
func openIndex(dbFlag string) (*index.Index, error) {
	path, err := resolveDB(dbFlag)
	if err != nil {
		return nil, err
	}
	return index.Open(path)
}

func cmdMap(args []string, out *printer) error {
	fs := flag.NewFlagSet("map", flag.ContinueOnError)
	ascii := fs.Bool("ascii", os.Getenv("BRAIDS_ASCII") != "", "use narrow ASCII glyphs")
	db := fs.String("db", "", "index location")
	print := fs.Bool("print", false, "render one frame to stdout instead of opening the map")
	lane := fs.String("lane", "", "with --print, render this lane's spine instead of the map")
	query := fs.String("query", "", "with --print, render the search screen for this query")
	width := fs.Int("width", 92, "frame width when printing")
	height := fs.Int("height", 24, "frame height when printing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dbPath, err := resolveDB(*db)
	if err != nil {
		return err
	}
	ix, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	provenance, err := sidecar.Load[model.Origin](sidecarPath(dbPath, "origins.json"))
	if err != nil {
		return err
	}
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return err
	}
	// Watching is a convenience, not a requirement: if it cannot start, the map
	// still opens as a snapshot.
	var changes <-chan struct{}
	if !*print {
		if w, err := watch.New(root, filepath.Dir(dbPath)); err == nil {
			defer w.Close() //nolint:errcheck // closing on exit
			changes = w.Changes()
		}
	}
	shelf, err := sidecar.Load[bool](sidecarPath(dbPath, "archived.json"))
	if err != nil {
		return err
	}
	names, err := sidecar.Load[string](sidecarPath(dbPath, "names.json"))
	if err != nil {
		return err
	}
	kinds, err := sidecar.Load[string](sidecarPath(dbPath, "kinds.json"))
	if err != nil {
		return err
	}
	bin := trash.New(filepath.Join(filepath.Dir(dbPath), "trash"))
	spawn, terminal := spawner(ctx, ix, kinds)
	opts := tui.Options{
		ASCII:     *ascii,
		Source:    "claudecode",
		IndexPath: dbPath,
		LoadSpine: tui.SpineLoader(ctx, ix),
		PlanMerge: func(base, incoming string) (int, int, error) {
			req, err := mergeRequest(ctx, ix, base, incoming, "")
			if err != nil {
				return 0, 0, err
			}
			plan, err := claudecode.New(root).PlanMerge(ctx, req)
			return plan.IncomingTurns, plan.BaseOnlyTurns, err
		},
		Merge: func(base, incoming, name string) (string, error) {
			req, err := mergeRequest(ctx, ix, base, incoming, name)
			if err != nil {
				return "", err
			}
			lane, err := claudecode.New(root).Merge(ctx, req)
			if err != nil {
				return "", err
			}
			return lane.ID, nil
		},
		WorkspaceOK: func(laneID string) error {
			lane, err := findLane(ctx, ix, laneID)
			if err != nil {
				return err
			}
			return worktreeOK(lane.Cwd)
		},
		LoadCompactions: func(laneID string) ([]index.CompactionRow, error) {
			return ix.LaneCompactions(ctx, laneID)
		},
		LoadSubagents: func(laneID string) ([]index.SubagentRow, error) {
			return ix.LaneSubagents(ctx, laneID)
		},
		Promote: func(laneID, agentID string) (string, error) {
			return promoteAgent(ctx, ix, provenance, laneID, agentID)
		},
		LoadAgentSpine: func(laneID, agentID string) ([]graph.Segment, error) {
			return agentSpine(ctx, ix, root, laneID, agentID)
		},
		Origins: provenance.All(),
		Names:   names.All(),
		Changes: changes,
		LiveEvents: func() (map[string]hooks.Event, error) {
			events, err := hooks.Read(filepath.Join(filepath.Dir(dbPath), "events.jsonl"))
			if err != nil {
				return nil, err
			}
			return hooks.Latest(events), nil
		},
		Archived: shelf.All(),
		Rename: func(laneID, name string) error {
			if name == "" {
				return names.Delete(laneID)
			}
			return names.Set(laneID, name)
		},
		Archive: func(laneID string, archived bool) error {
			if archived {
				return shelf.Set(laneID, true)
			}
			return shelf.Delete(laneID)
		},
		Delete: func(laneID string) (int64, error) {
			return discardLane(ctx, ix, bin, laneID)
		},
		DeleteWork: func(laneID string) (int64, error) {
			return discardWork(ctx, ix, bin, laneID)
		},
		LoadBin: func() ([]trash.Entry, error) {
			// Expire on open, so the count and the deadlines shown are true.
			if _, _, err := bin.Expire(time.Now()); err != nil {
				return nil, err
			}
			return bin.List()
		},
		Restore: func(id string) error {
			_, err := bin.RestoreByID(id)
			return err
		},
		Purge: bin.Purge,
		ResumeCommand: func(laneID string) (string, error) {
			return resumeCommand(ctx, ix, kinds, laneID)
		},
		Spawn:    spawn,
		Terminal: terminal,
		Search: func(query, scope string) ([]index.Hit, error) {
			return ix.Search(ctx, index.Query{Text: query, Lane: scope, Limit: 200})
		},
		Branch: func(laneID string, turn int, name string, workspace bool) (string, error) {
			id, err := branchAt(ctx, ix, provenance, laneID, turn, name)
			if err != nil {
				return "", err
			}
			if workspace {
				// Recorded rather than acted on: braids does not create the
				// worktree, it asks the harness to when the branch is resumed.
				if err := kinds.Set(id, workspaceKind); err != nil {
					return "", err
				}
			}
			return id, nil
		},
		Refresh: func() (*graph.Forest, error) {
			if _, err := ix.Sync(ctx, claudecode.New(root)); err != nil {
				return nil, err
			}
			return tui.Forest(ctx, ix, provenance.All(), names.All())
		},
	}
	if !*print {
		return tui.Run(ctx, ix, opts)
	}
	forest, err := tui.Forest(ctx, ix, provenance.All(), names.All())
	if err != nil {
		return err
	}
	out.printf("%s\n", tui.Render(forest, opts, *lane, *query, *width, *height))
	return out.Err()
}

func cmdIndex(args []string, out *printer) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", "", "transcript root (default ~/.claude/projects)")
	db := fs.String("db", "", "index location")
	full := fs.Bool("full", false, "re-read every transcript instead of only what changed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := *root
	if dir == "" {
		var err error
		if dir, err = claudecode.DefaultRoot(); err != nil {
			return err
		}
	}
	ix, err := openIndex(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // reported by Rebuild failure instead

	src := claudecode.New(dir)
	sync := ix.Sync
	if *full {
		sync = ix.Rebuild
	}
	stats, err := sync(context.Background(), src)
	if err != nil {
		return err
	}
	out.printf("indexed %d lanes · %d messages · %d searchable parts in %s\n",
		stats.Lanes, stats.Messages, stats.Parts, stats.Duration.Round(time.Millisecond))
	return out.Err()
}

func cmdSearch(args []string, out *printer) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(out)
	lane := fs.String("lane", "", "restrict to one conversation")
	kinds := fs.String("kind", "", "comma-separated part kinds")
	limit := fs.Int("limit", 20, "maximum hits")
	db := fs.String("db", "", "index location")
	words, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	query := strings.Join(words, " ")
	if strings.TrimSpace(query) == "" {
		return errors.New("search needs a query (try: braids search gcsfuse)")
	}
	parsed, err := parseKinds(*kinds)
	if err != nil {
		return err
	}

	ix, err := openIndex(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	start := time.Now()
	hits, err := ix.Search(context.Background(),
		index.Query{Text: query, Lane: *lane, Kinds: parsed, Limit: *limit})
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		out.printf("no matches for %q\n", query)
		return out.Err()
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, h := range hits {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			laneLabel(h), h.At.Format("01-02 15:04"), kindLabel(h), oneLine(h.Snippet)); err != nil {
			return fmt.Errorf("write results: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	out.printf("\n%d hits in %s\n", len(hits), time.Since(start).Round(time.Microsecond))
	return out.Err()
}

func cmdBranch(args []string, out *printer) error {
	fs := flag.NewFlagSet("branch", flag.ContinueOnError)
	laneRef := fs.String("lane", "", "conversation to branch from (ID prefix)")
	at := fs.Int("at", 0, "turn number to branch at")
	name := fs.String("name", "", "name for the new conversation")
	workspace := fs.Bool("workspace", false, "give the branch a git worktree of its own")
	db := fs.String("db", "", "index location")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *laneRef == "" || *at <= 0 {
		return errors.New("branch needs --lane and --at (try: braids branch --lane abc123 --at 12)")
	}

	dbPath, err := resolveDB(*db)
	if err != nil {
		return err
	}
	ix, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	provenance, err := sidecar.Load[model.Origin](sidecarPath(dbPath, "origins.json"))
	if err != nil {
		return err
	}
	kinds, err := sidecar.Load[string](sidecarPath(dbPath, "kinds.json"))
	if err != nil {
		return err
	}
	// Check before cutting: a branch that cannot be the kind it was asked for
	// should not exist at all. The source lane's directory is the one the
	// branch will run in.
	if *workspace {
		source, err := findLane(ctx, ix, *laneRef)
		if err != nil {
			return err
		}
		if err := worktreeOK(source.Cwd); err != nil {
			return err
		}
	}
	newID, err := branchAt(ctx, ix, provenance, *laneRef, *at, *name)
	if err != nil {
		return err
	}
	if *workspace {
		if err := kinds.Set(newID, workspaceKind); err != nil {
			return err
		}
	}
	// Index the new lane so the command reported below can be built from it.
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return err
	}
	if _, err := ix.Sync(ctx, claudecode.New(root)); err != nil {
		return err
	}
	command, err := resumeCommand(ctx, ix, kinds, newID)
	if err != nil {
		return err
	}
	out.printf("branched %s at turn %d as a %s\n  new conversation %s\n  resume with: %s\n",
		shortID(*laneRef), *at, branchKindName(*workspace), newID, command)
	out.printf("\nthe map picks it up on its own; `braids index` if you want it now\n")
	return out.Err()
}

// branchAt cuts a lane at a turn number and returns the new lane's ID. Shared
// by the CLI and the map so both take exactly the same path.
func branchAt(ctx context.Context, ix *index.Index, provenance *sidecar.Store[model.Origin], laneRef string, turn int, name string) (string, error) {
	lane, err := findLane(ctx, ix, laneRef)
	if err != nil {
		return "", err
	}
	msgs, err := ix.LaneMessages(ctx, lane.ID)
	if err != nil {
		return "", err
	}
	var target string
	for _, m := range msgs {
		if m.Seq == turn {
			target = m.ID
			break
		}
	}
	if target == "" {
		return "", fmt.Errorf("turn %d is not in %s (it has %d turns)", turn, shortID(lane.ID), len(msgs))
	}
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return "", err
	}
	branch, err := claudecode.New(root).Branch(ctx, store.BranchRequest{
		Lane: lane.Lane, AtMessage: target, Name: name,
	})
	if err != nil {
		return "", err
	}
	// Record where it came from: two lanes can hold identical prefixes, so
	// inference alone would sometimes attach it to the wrong parent.
	if err := provenance.Set(branch.ID, model.Origin{Parent: lane.ID, ForkSeq: turn}); err != nil {
		return "", err
	}
	return branch.ID, nil
}

// sidecarPath keeps braids' own small files next to the index they describe.
func sidecarPath(dbPath, name string) string {
	return filepath.Join(filepath.Dir(dbPath), name)
}

// branchKindName describes what was cut, for the line reporting it.
func branchKindName(workspace bool) string {
	if workspace {
		return "workspace"
	}
	return "thought"
}

func cmdPromote(args []string, out *printer) error {
	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	laneRef := fs.String("lane", "", "conversation the subagent belongs to")
	agentRef := fs.String("agent", "", "subagent to promote (ID prefix)")
	db := fs.String("db", "", "index location")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *laneRef == "" {
		return errors.New("promote needs --lane (try: braids agents --lane abc123)")
	}
	dbPath, err := resolveDB(*db)
	if err != nil {
		return err
	}
	ix, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	lane, err := findLane(ctx, ix, *laneRef)
	if err != nil {
		return err
	}
	agents, err := ix.LaneSubagents(ctx, lane.ID)
	if err != nil {
		return err
	}
	agent, err := pickAgent(agents, *agentRef)
	if err != nil {
		return err
	}
	provenance, err := sidecar.Load[model.Origin](sidecarPath(dbPath, "origins.json"))
	if err != nil {
		return err
	}
	newID, err := promoteAgent(ctx, ix, provenance, lane.ID, agent.ID)
	if err != nil {
		return err
	}
	out.printf("promoted %s (%s) from turn %d\n  new conversation %s\n  resume with: claude --resume %s\n",
		agent.Type, agent.Task, agent.ParentSeq, newID, newID)
	return out.Err()
}

// pickAgent resolves a subagent by ID prefix, or the only one there is.
func pickAgent(agents []index.SubagentRow, ref string) (index.SubagentRow, error) {
	if len(agents) == 0 {
		return index.SubagentRow{}, errors.New("that conversation spawned no subagents")
	}
	if ref == "" {
		if len(agents) == 1 {
			return agents[0], nil
		}
		return index.SubagentRow{}, fmt.Errorf("it spawned %d subagents; name one with --agent", len(agents))
	}
	var found []index.SubagentRow
	for _, a := range agents {
		if strings.HasPrefix(a.ID, ref) {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return index.SubagentRow{}, fmt.Errorf("no subagent starts with %q", ref)
	case 1:
		return found[0], nil
	default:
		return index.SubagentRow{}, fmt.Errorf("%q matches %d subagents", ref, len(found))
	}
}

func cmdAgents(args []string, out *printer) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	laneRef := fs.String("lane", "", "conversation to list subagents of")
	db := fs.String("db", "", "index location")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *laneRef == "" {
		return errors.New("agents needs --lane")
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
	agents, err := ix.LaneSubagents(ctx, lane.ID)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		out.printf("%s spawned no subagents\n", shortID(lane.ID))
		return out.Err()
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	rows := []string{"AGENT\tTYPE\tTURN\tTURNS\tTASK"}
	for _, a := range agents {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%d\t%s",
			truncate(a.ID, 18), truncate(a.Type, 24), a.ParentSeq, a.Messages, a.Task))
	}
	for _, r := range rows {
		if _, err := fmt.Fprintln(tw, r); err != nil {
			return fmt.Errorf("write agents: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write agents: %w", err)
	}
	return out.Err()
}

func cmdMerge(args []string, out *printer) error {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	baseRef := fs.String("lane", "", "conversation to carry on from")
	fromRef := fs.String("from", "", "branch whose turns are brought over")
	name := fs.String("name", "", "name for the merged conversation")
	dry := fs.Bool("plan", false, "report what would be carried over, and stop")
	db := fs.String("db", "", "index location")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *baseRef == "" || *fromRef == "" {
		return errors.New("merge needs --lane and --from")
	}
	dbPath, err := resolveDB(*db)
	if err != nil {
		return err
	}
	ix, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	base, incoming, err := twoLanes(ctx, ix, *baseRef, *fromRef)
	if err != nil {
		return err
	}
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return err
	}
	src := claudecode.New(root)
	req := store.MergeRequest{Base: base.Lane, Incoming: incoming.Lane, Name: *name}

	plan, err := src.PlanMerge(ctx, req)
	if err != nil {
		return err
	}
	out.printf("%s: %d turns, %d of them not in %s\n%s: %d turns not in %s\n",
		orDash(base.Title), plan.BaseTurns, plan.BaseOnlyTurns, orDash(incoming.Title),
		orDash(incoming.Title), plan.IncomingTurns, orDash(base.Title))
	if !plan.Worthwhile() {
		out.printf("\nnothing to join: one already contains the other\n")
	}
	if *dry {
		return out.Err()
	}

	merged, err := src.Merge(ctx, req)
	if err != nil {
		return err
	}
	if _, err := ix.Sync(ctx, src); err != nil {
		return err
	}
	out.printf("  new conversation %s\n  resume with: claude --resume %s\n", merged.ID, merged.ID)
	return out.Err()
}

// mergeRequest resolves both sides of a merge.
func mergeRequest(ctx context.Context, ix *index.Index, base, incoming, name string) (store.MergeRequest, error) {
	first, second, err := twoLanes(ctx, ix, base, incoming)
	if err != nil {
		return store.MergeRequest{}, err
	}
	return store.MergeRequest{Base: first.Lane, Incoming: second.Lane, Name: name}, nil
}

// twoLanes resolves a pair of conversations by ID prefix.
func twoLanes(ctx context.Context, ix *index.Index, a, b string) (index.LaneInfo, index.LaneInfo, error) {
	first, err := findLane(ctx, ix, a)
	if err != nil {
		return index.LaneInfo{}, index.LaneInfo{}, err
	}
	second, err := findLane(ctx, ix, b)
	if err != nil {
		return index.LaneInfo{}, index.LaneInfo{}, err
	}
	return first, second, nil
}

// orDash keeps an unnamed conversation readable in a sentence.
func orDash(s string) string {
	if s == "" {
		return "(unnamed)"
	}
	return s
}

// cmdHook records one hook payload. It is what the harness runs, so it must
// never fail loudly: an error here is printed in the middle of someone's
// session, and nothing braids observes is worth interrupting work for.
func cmdHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil //nolint:nilerr // a hook that cannot find home simply records nothing
	}
	_ = hooks.Record(filepath.Join(home, ".braids", "events.jsonl"), os.Stdin)
	return nil
}

func cmdHooks(args []string, out *printer) error {
	fs := flag.NewFlagSet("hooks", flag.ContinueOnError)
	install := fs.Bool("install", false, "ask sessions to report when they block")
	remove := fs.Bool("remove", false, "stop asking")
	settings := fs.String("settings", "", "settings file (default ~/.claude/settings.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := *settings
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("locate home directory: %w", err)
		}
		path = filepath.Join(home, ".claude", "settings.json")
	}
	command, err := hookCommand()
	if err != nil {
		return err
	}

	switch {
	case *install && *remove:
		return errors.New("choose one of --install or --remove")
	case *install:
		added, err := hooks.Install(path, command)
		if err != nil {
			return err
		}
		if len(added) == 0 {
			out.printf("already installed in %s\n", path)
			return out.Err()
		}
		out.printf("installed in %s for %s\n", path, strings.Join(added, ", "))
		out.printf("  a copy of the previous file is beside it\n")
		out.printf("  sessions already running will not report until restarted\n")
		return out.Err()
	case *remove:
		removed, err := hooks.Remove(path, command)
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			out.printf("not installed in %s\n", path)
			return out.Err()
		}
		out.printf("removed from %s for %s\n", path, strings.Join(removed, ", "))
		return out.Err()
	}

	on, err := hooks.Installed(path, command)
	if err != nil {
		return err
	}
	out.printf("settings: %s\ncommand:  %s\n", path, command)
	if len(on) == 0 {
		out.printf("status:   not installed — braids cannot tell a running tool from one\n")
		out.printf("          waiting on you. `braids hooks --install` changes that.\n")
		return out.Err()
	}
	out.printf("status:   reporting %s\n", strings.Join(on, ", "))
	return out.Err()
}

// hookCommand is what the harness will run. It is the binary's own path, so a
// braids that moves takes its hook with it.
func hookCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate braids: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return resolved + " hook", nil
}

// agentSpine reads a subagent's own transcript so it can be looked at before
// anything is decided about it. Nothing is written and nothing is indexed.
func agentSpine(ctx context.Context, ix *index.Index, root, laneID, agentID string) ([]graph.Segment, error) {
	agents, err := ix.LaneSubagents(ctx, laneID)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if a.ID != agentID {
			continue
		}
		rows, err := index.RowsFrom(ctx, claudecode.New(root),
			model.Lane{ID: a.ID, Path: a.Path})
		if err != nil {
			return nil, err
		}
		return graph.Spine(rows), nil
	}
	return nil, fmt.Errorf("no subagent %s in %s", shortID(agentID), shortID(laneID))
}

// promoteAgent turns a subagent into a conversation of its own and records
// where it came from, so the map hangs it under the lane that spawned it.
func promoteAgent(ctx context.Context, ix *index.Index, provenance *sidecar.Store[model.Origin], laneID, agentID string) (string, error) {
	agents, err := ix.LaneSubagents(ctx, laneID)
	if err != nil {
		return "", err
	}
	for _, a := range agents {
		if a.ID != agentID {
			continue
		}
		root, err := claudecode.DefaultRoot()
		if err != nil {
			return "", err
		}
		lane, err := claudecode.New(root).Promote(ctx, a.Subagent)
		if err != nil {
			return "", err
		}
		// A promoted agent shares no message IDs with its parent, so nothing
		// could infer where it came from; recording it is the only way it hangs
		// under the conversation that spawned it.
		if err := provenance.Set(lane.ID, model.Origin{Parent: laneID, ForkSeq: a.ParentSeq}); err != nil {
			return "", err
		}
		return lane.ID, nil
	}
	return "", fmt.Errorf("no subagent %s in %s", shortID(agentID), shortID(laneID))
}

// discardLane moves a conversation's files into the bin, returning how much was
// reclaimed. Everything a conversation owns goes together: the transcript and
// the directory beside it holding its subagents and tool output.
func discardLane(ctx context.Context, ix *index.Index, bin *trash.Bin, laneID string) (int64, error) {
	lane, err := findLane(ctx, ix, laneID)
	if err != nil {
		return 0, err
	}
	label := lane.Title
	if label == "" {
		label = shortID(lane.ID)
	}
	entry, err := bin.Discard(label, []string{
		lane.Path,
		strings.TrimSuffix(lane.Path, ".jsonl"),
	})
	if err != nil {
		return 0, err
	}
	if len(entry.Items) == 0 {
		return 0, fmt.Errorf("nothing to delete for %s", shortID(lane.ID))
	}
	return entry.Bytes, nil
}

// discardWork moves a conversation's work products to the bin, leaving the
// conversation itself in place.
func discardWork(ctx context.Context, ix *index.Index, bin *trash.Bin, laneID string) (int64, error) {
	lane, err := findLane(ctx, ix, laneID)
	if err != nil {
		return 0, err
	}
	if lane.ArtifactPath == "" {
		return 0, fmt.Errorf("%s has no work products", shortID(lane.ID))
	}
	label := lane.Title
	if label == "" {
		label = shortID(lane.ID)
	}
	entry, err := bin.Discard("work products of "+label, []string{lane.ArtifactPath})
	if err != nil {
		return 0, err
	}
	if len(entry.Items) == 0 {
		return 0, fmt.Errorf("nothing to discard for %s", shortID(lane.ID))
	}
	return entry.Bytes, nil
}

// resumeCommand is what the user would type to continue a conversation.
// workspaceKind marks a branch that should get a git worktree of its own.
const workspaceKind = "workspace"

// worktreeOK reports whether a directory can hold a git worktree, which is what
// a workspace branch needs. A directory that is not a repository cannot, and
// finding that out when the branch is resumed is far too late.
func worktreeOK(dir string) error {
	if dir == "" {
		return errors.New("this conversation has no working directory recorded")
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = out
		return fmt.Errorf("%s is not a git repository, so it cannot hold a worktree", dir)
	}
	return nil
}

func resumeCommand(ctx context.Context, ix *index.Index, kinds *sidecar.Store[string], laneID string) (string, error) {
	lane, err := findLane(ctx, ix, laneID)
	if err != nil {
		return "", err
	}
	command := "claude --resume " + lane.ID
	if lane.Title != "" {
		command += fmt.Sprintf(" --name %q", lane.Title)
	}
	// A workspace branch asks the harness for a worktree of its own, so two
	// branches that both write to the repo cannot collide. braids does not
	// create it: --worktree is the harness's own flag and its own job.
	// A flag that is known to fail is worse than one that is absent: if the
	// directory stopped being a repository, ask for a plain resume.
	if kind, _ := kinds.Get(lane.ID); kind == workspaceKind && worktreeOK(lane.Cwd) == nil {
		command += " --worktree " + slug(lane.Title, lane.ID)
	}
	return command, nil
}

// slug turns a branch name into something a worktree directory can be called.
func slug(name, fallback string) string {
	var out []rune
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case len(out) > 0 && out[len(out)-1] != '-':
			out = append(out, '-')
		}
	}
	trimmed := strings.Trim(string(out), "-")
	if trimmed != "" {
		return trimmed
	}
	// shortID is for reading, not for paths: it elides with a character no
	// directory name should carry.
	if len(fallback) > shortIDLen {
		return fallback[:shortIDLen]
	}
	return fallback
}

// shortIDLen is how much of an ID identifies it well enough for a path.
const shortIDLen = 8

// spawner opens a terminal for a lane, or reports that this terminal cannot be
// driven. The name it goes by is reported too, so the map can say what it used.
func spawner(ctx context.Context, ix *index.Index, kinds *sidecar.Store[string]) (func(string) error, string) {
	open, terminal := launch.Detect(launch.Env)
	if open == nil {
		return nil, ""
	}
	return func(laneID string) error {
		lane, err := findLane(ctx, ix, laneID)
		if err != nil {
			return err
		}
		command, err := resumeCommand(ctx, ix, kinds, laneID)
		if err != nil {
			return err
		}
		dir := lane.Cwd
		if dir == "" {
			dir = filepath.Dir(lane.Path)
		}
		name := lane.Title
		if name == "" {
			name = shortID(lane.ID)
		}
		return open(ctx, dir, name, command)
	}, terminal
}

// findLane resolves a lane by ID prefix, refusing an ambiguous one rather than
// guessing which conversation the user meant.
func findLane(ctx context.Context, ix *index.Index, ref string) (index.LaneInfo, error) {
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return index.LaneInfo{}, err
	}
	var found []index.LaneInfo
	for _, l := range lanes {
		if strings.HasPrefix(l.ID, ref) {
			found = append(found, l)
		}
	}
	switch len(found) {
	case 0:
		return index.LaneInfo{}, fmt.Errorf("no conversation starts with %q", ref)
	case 1:
		return found[0], nil
	default:
		return index.LaneInfo{}, fmt.Errorf("%q matches %d conversations; use more of the ID", ref, len(found))
	}
}

func cmdLanes(args []string, out *printer) error {
	fs := flag.NewFlagSet("lanes", flag.ContinueOnError)
	fs.SetOutput(out)
	db := fs.String("db", "", "index location")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ix, err := openIndex(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read-only

	lanes, err := ix.Lanes(context.Background())
	if err != nil {
		return err
	}
	if len(lanes) == 0 {
		out.printf("no lanes indexed yet (run: braids index)\n")
		return out.Err()
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	rows := []string{"LANE\tPROJECT\tMSGS\tUPDATED\tTITLE"}
	for _, l := range lanes {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%s\t%s",
			shortID(l.ID), l.Project, l.Messages, l.Updated.Format("01-02 15:04"), l.Title))
	}
	for _, r := range rows {
		if _, err := fmt.Fprintln(tw, r); err != nil {
			return fmt.Errorf("write lanes: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write lanes: %w", err)
	}
	out.printf("\n%d lanes\n", len(lanes))
	return out.Err()
}

// parseKinds validates the --kind flag, refusing unknown kinds rather than
// silently returning nothing.
func parseKinds(s string) ([]model.PartKind, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	valid := map[string]model.PartKind{
		string(model.PartText):       model.PartText,
		string(model.PartThinking):   model.PartThinking,
		string(model.PartToolUse):    model.PartToolUse,
		string(model.PartToolResult): model.PartToolResult,
	}
	var kinds []model.PartKind
	for _, raw := range strings.Split(s, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		k, ok := valid[name]
		if !ok {
			return nil, fmt.Errorf("unknown kind %q (want text, thinking, tool_use or tool_result)", name)
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

func laneLabel(h index.Hit) string {
	if h.LaneTitle != "" {
		return truncate(h.LaneTitle, 28)
	}
	return shortID(h.LaneID)
}

func kindLabel(h index.Hit) string {
	if h.Kind == model.PartToolUse && h.Tool != "" {
		return truncate(h.Tool, 12)
	}
	return string(h.Kind)
}

func shortID(id string) string { return truncate(id, 8) }

func oneLine(s string) string {
	return truncate(strings.Join(strings.Fields(s), " "), 78)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
