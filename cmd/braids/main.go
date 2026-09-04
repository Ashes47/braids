// Command braids manages Claude Code conversations as a graph. It indexes every
// transcript under ~/.claude, draws them as a forest of branches, searches all
// of them, and cuts a new conversation from any turn of any of them.
//
// It never talks to a model. Everything it shows is derived from files the
// harness already wrote, and the only files it writes are its own.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/Ashes47/braids/internal/brand"
	"github.com/Ashes47/braids/internal/core/artifacts"
	"github.com/Ashes47/braids/internal/core/graph"
	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/release"
	"github.com/Ashes47/braids/internal/core/sidecar"
	"github.com/Ashes47/braids/internal/core/store"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
	"github.com/Ashes47/braids/internal/core/trash"
	"github.com/Ashes47/braids/internal/core/watch"
	"github.com/Ashes47/braids/internal/format"
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
  braids work [--lane ID] [--path SUB]   browse the work products a session left
              [--orphans] [--reclaim]    ...or find and reclaim ownerless ones
  braids memories [--project NAME]       what a project remembers, and whether
                  [--root DIR]           the index still agrees with the files
  braids promote --lane ID --agent ID    turn a subagent into its own conversation
  braids merge --lane ID --from ID       join a branch back, as a new conversation
  braids hooks [--install|--remove]      let sessions report when they block
  braids version

map keys (the keys that open another screen; each screen lists its own):
  ↵                the conversation's spine, turn by turn; there b branches
                   at the turn under the cursor and m merges one back
  /                search every conversation, memory and work product
  w                the work products a session left behind — heaviest first,
                   ↵ to descend, d to move one to the bin
  M                what the project remembers, and whether the index still
                   agrees with the files on disk
  u                what braids has binned, to restore or delete for good

map flags:
  --ascii          use narrow ASCII glyphs (for terminals that draw
                   box characters double-wide)
  --print          render one frame to stdout instead of opening the map
  --lane ID        with --print, render that lane's spine instead of the map
  --width N        frame width when printing (default 92)

search flags:
  --lane ID        restrict to one conversation
  --type LIST      conversation,memory,artifact (comma separated). Search
                   covers all three unless narrowed; each result says which
                   it is, and work products are matched by name, not contents
  --kind LIST      text,thinking,tool_use,tool_result (comma separated)
  --limit N        maximum hits (default 20)

common flags:
  --db PATH        index location (default $BRAIDS_DB or ~/.braids/index.db)
  --json           machine-readable output, with whole IDs. Every command
                   that reports something takes it.
  --help           the flags that command takes

environment:
  BRAIDS_SPAWN     command template for 'o' (open a terminal), understanding
                   {cmd} {name} {dir}. tmux and iTerm2 are driven directly
                   without one; elsewhere 'o' copies the command instead.
                   e.g. tmux new-window -c {dir} -n {name} {cmd}
                   Each value is shell-quoted when substituted, so do not put
                   quotes around a placeholder yourself.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, errShown) {
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
		return cmdHook(args[1:], out)
	case "agents":
		return cmdAgents(args[1:], out)
	case "work":
		return cmdWork(args[1:], out)
	case "memories":
		return cmdMemories(args[1:], out)
	case "version", "-v", "--version":
		out.mark(brand.Small())
		out.printf("braids %s (%s)\n", version, commit)
		// Only for a person. The installer reads this command to find out what
		// is installed, and a program parsing output does not want a nudge in
		// the middle of it.
		if stdoutIsTerminal() {
			exe, _ := os.Executable()
			if notice := updateNotice(release.Read(braidsHome(), exe), time.Now()); notice != "" {
				out.printf("\n%s\n", notice)
			}
		}
		return out.Err()
	case "help", "-h", "--help":
		out.mark(brand.Full())
		out.printf("%s", usage)
		return out.Err()
	default:
		// A flag where a command would go belongs to the map, which is what
		// braids does when told to do nothing in particular. `braids --print`
		// is documented that way, and reading it as a command name would
		// answer a documented invocation with "unknown command".
		if strings.HasPrefix(args[0], "-") {
			return cmdMap(args, out)
		}
		if guess := nearest(args[0]); guess != "" {
			return fmt.Errorf("unknown command %q, did you mean %q?", args[0], guess)
		}
		return fmt.Errorf("unknown command %q (try: braids help)", args[0])
	}
}

// known is every command name braids answers to. A test walks it against the
// dispatch above, so a command cannot be added without being offered here.
var known = []string{
	"map", "index", "search", "lanes", "branch", "agents", "work", "memories",
	"promote", "merge", "hooks", "hook", "version", "help",
}

// nearest returns the command a mistyped name most likely meant, or an empty
// string when nothing is close enough to be worth offering. A transposition or
// a dropped letter is a typo worth guessing at; three edits is a different
// word, and guessing at that wastes the reader's attention.
func nearest(word string) string {
	best, distance := "", 0
	for _, name := range known {
		if d := editDistance(word, name); best == "" || d < distance {
			best, distance = name, d
		}
	}
	if distance > 2 || distance >= len(word) {
		return ""
	}
	return best
}

// editDistance is the number of single-character insertions, deletions and
// substitutions between two words.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// errShown says a command has already printed everything it had to say, so
// main exits cleanly instead of adding an error line underneath it.
var errShown = errors.New("nothing further to report")

// newFlagSet builds a flag set that keeps quiet. Go's own output prints before
// braids can say anything, in a voice that is not braids', under a heading
// naming the flag set rather than the command as it was typed.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parse reads a command's flags, answering a mistake with the flags that
// command actually takes. The list is drawn from the flag set itself rather
// than written out again in the usage text, so it cannot drift from what the
// command accepts.
func parse(fs *flag.FlagSet, args []string, out *printer) error {
	err := fs.Parse(args)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		out.printf("braids %s\n\n%s\nsee 'braids help' for every command\n", fs.Name(), flagList(fs))
		return errShown
	}
	return fmt.Errorf("%w\n\nbraids %s takes:\n%s", err, fs.Name(), flagList(fs))
}

// flagList renders a flag set's own documentation, spelled the way braids
// spells flags everywhere else. Go writes them with one dash; the usage text,
// and every example in it, uses two.
func flagList(fs *flag.FlagSet) string {
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
	return strings.ReplaceAll("\n"+buf.String(), "\n  -", "\n  --")[1:]
}

// parseArgs parses flags that may appear before, after or between positional
// arguments. Go's flag package stops at the first non-flag argument, which
// would silently ignore "braids search foo --limit 5" — the way people
// actually type it.
func parseArgs(fs *flag.FlagSet, args []string, out *printer) ([]string, error) {
	var positional []string
	for {
		if err := parse(fs, args, out); err != nil {
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
// braidsHome is where braids keeps its own files. The stamp the installer
// leaves lives here rather than beside the index, because BRAIDS_DB can point
// the index anywhere and the two have to agree on one place to look.
func braidsHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".braids")
}

// installCommand is the one line that updates braids. It is printed rather
// than run, except by the map's update key, which prints it first.
const installCommand = "curl -fsSL https://braids.chat/install.sh | sh"

// updateNotice says how old this build is, and offers the command only when
// nobody has checked in a while. It never claims a newer version exists:
// braids cannot know that without asking, and it does not ask.
//
// The two halves are separate on purpose. Checking and finding yourself
// current resets the offer but not the age, and reporting the age as though
// the check had rebuilt the binary would be a lie told by arithmetic.
func updateNotice(state release.State, now time.Time) string {
	age, ok := state.BuildAge(now)
	if !ok {
		return ""
	}
	line := fmt.Sprintf("this build is %s old", release.Age(age))
	if !state.Due(now) {
		return line
	}
	return fmt.Sprintf("%s. To check for a newer one:\n  %s", line, installCommand)
}

// updateCommand builds the line the map's update key runs.
//
// BRAIDS_BIN_DIR points the installer at the directory this binary is running
// from, so it replaces braids where it already lives. Without it the script
// picks /usr/local/bin or ~/.local/bin, which for anyone who installed another
// way means a second braids on disk and PATH deciding which one they get.
func updateCommand() *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	script := fmt.Sprintf("echo '$ %s'; echo; %s", installCommand, installCommand)
	command := exec.Command("sh", "-c", script)
	command.Env = append(os.Environ(), "BRAIDS_BIN_DIR="+filepath.Dir(exe))
	return command
}

func defaultDB() (string, error) {
	if p := os.Getenv("BRAIDS_DB"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	dir := filepath.Join(home, ".braids")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	// MkdirAll leaves an existing directory's mode alone, so a braids
	// directory made by an older build stays as wide as it was. Everything
	// under it is conversation data; tighten it on the way past.
	if info, err := os.Stat(dir); err == nil && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", fmt.Errorf("restrict %s: %w", dir, err)
		}
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
	return openExisting(path)
}

// createIndex opens the index, making it if this is the first run. Only
// `braids index` may do this.
func createIndex(dbFlag string) (*index.Index, error) {
	path, err := resolveDB(dbFlag)
	if err != nil {
		return nil, err
	}
	return index.Open(path)
}

// openExisting opens an index that has to be there already. Only `braids index`
// creates one: everything else reads, and a read that quietly creates an empty
// database answers a typo in --db with "nothing found" — a wrong answer wearing
// the shape of a right one — while leaving a stray file where the typo pointed.
func openExisting(path string) (*index.Index, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no index at %s (run: braids index)", path)
	}
	return index.Open(path)
}

func cmdMap(args []string, out *printer) error {
	fs := newFlagSet("map")
	ascii := fs.Bool("ascii", os.Getenv("BRAIDS_ASCII") != "", "use narrow ASCII glyphs")
	db := fs.String("db", "", "index location")
	print := fs.Bool("print", false, "render one frame to stdout instead of opening the map")
	lane := fs.String("lane", "", "with --print, render this lane's spine instead of the map")
	query := fs.String("query", "", "with --print, render the search screen for this query")
	width := fs.Int("width", 92, "frame width when printing")
	height := fs.Int("height", 24, "frame height when printing")
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
	provenance, err := sidecar.Load[model.Origin](sidecarPath(dbPath, "origins.json"))
	if err != nil {
		return err
	}
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return err
	}
	src := claudecode.New(root)
	// Watching is a convenience, not a requirement: if it cannot start, the map
	// still opens as a snapshot.
	var changes <-chan struct{}
	if !*print {
		if w, err := watch.New(watched(src, root, dbPath)...); err == nil {
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
	bin := binAt(dbPath)
	spawn, terminal := spawner(ctx, ix, kinds)
	opts := tui.Options{
		ASCII:     *ascii,
		Source:    "claudecode",
		IndexPath: dbPath,
		Version:   version,
		Release: func() release.State {
			exe, _ := os.Executable()
			return release.Read(braidsHome(), exe)
		},
		Update:    updateCommand,
		LoadSpine: tui.SpineLoader(ctx, ix),
		PlanMerge: func(base, incoming string) (int, int, error) {
			req, err := mergeRequest(ctx, ix, base, incoming, "")
			if err != nil {
				return 0, 0, err
			}
			plan, err := src.PlanMerge(ctx, req)
			return plan.IncomingTurns, plan.BaseOnlyTurns, err
		},
		Merge: func(base, incoming, name string) (string, error) {
			req, err := mergeRequest(ctx, ix, base, incoming, name)
			if err != nil {
				return "", err
			}
			lane, err := src.Merge(ctx, req)
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
		Origins:   provenance.All(),
		Names:     names.All(),
		Changes:   changes,
		Reporting: hooksReporting(),
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
			bytes, err := discardWork(ctx, ix, bin, laneID)
			if err != nil {
				return bytes, err
			}
			return bytes, ix.RefreshArtifacts(ctx, src)
		},
		LoadMemories: func() ([]memory.Set, error) {
			return memorySets(src, "")
		},
		RemoveMemory: func(dir, name string) error {
			return memory.Remove(memory.Location{Dir: dir}, name, func(path string) error {
				// To the bin like everything else: being wrong about a memory
				// should cost nothing.
				_, err := bin.Discard("memory: "+name, []string{path})
				return err
			})
		},
		RenameMemory: func(dir, from, to string) (int, error) {
			return memory.Rename(memory.Location{Dir: dir}, from, to)
		},
		RepairMemoryIndex: func(dir string) (int, int, error) {
			return memory.Repair(memory.Location{Dir: dir})
		},
		LoadWork: func(laneID, dir string) (tui.WorkLevel, error) {
			lane, err := findLane(ctx, ix, laneID)
			if err != nil {
				return tui.WorkLevel{}, err
			}
			if lane.ArtifactPath == "" {
				return tui.WorkLevel{}, fmt.Errorf("%s left no work products", shortID(lane.ID))
			}
			at, err := within(lane.ArtifactPath, dir)
			if err != nil {
				return tui.WorkLevel{}, err
			}
			entries, err := artifacts.Read(at, claudecode.ReservedArtifact)
			if err != nil {
				return tui.WorkLevel{}, err
			}
			return tui.WorkLevel{Root: lane.ArtifactPath, Dir: at, Entries: entries}, nil
		},
		DiscardPaths: func(label string, paths []string) (int64, error) {
			entry, err := bin.Discard(label, paths)
			if err != nil {
				return 0, err
			}
			// The conversation's transcript did not move, so nothing else will
			// notice that its work products shrank.
			if err := ix.RefreshArtifacts(ctx, src); err != nil {
				return entry.Bytes, err
			}
			return entry.Bytes, nil
		},
		LoadBin: func() ([]trash.Entry, error) {
			// Expire on open, so the count and the deadlines shown are true.
			if _, _, err := bin.Expire(time.Now()); err != nil {
				return nil, err
			}
			return bin.List()
		},
		Restore: func(id string) error {
			if _, err := bin.RestoreByID(id); err != nil {
				return err
			}
			// Work products can come back as well as go, and the same
			// blindness applies: nothing about the conversation moved.
			return ix.RefreshArtifacts(ctx, src)
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
			if _, err := ix.Sync(ctx, src); err != nil {
				return nil, err
			}
			// Memories are what you write and then immediately search for, so
			// they are kept current here. Deciding whether to bother is a
			// directory listing; work products are not, so they wait for
			// `braids index`.
			if _, err := ix.SyncMemories(ctx, src); err != nil {
				return nil, err
			}
			return tui.Forest(ctx, ix, provenance.All(), names.All())
		},
	}
	// An upgrade that changes the schema drops what the index held, and until
	// this it opened a map with nothing on it and no word about why. The
	// transcripts are still there, so read them again before drawing anything.
	if ix.Recreated() {
		out.printf("the index format changed, so braids is reading your transcripts again\n")
		if _, err := ix.Sync(ctx, src); err != nil {
			return err
		}
		if _, err := ix.SyncMemories(ctx, src); err != nil {
			return err
		}
		forest, err := tui.Forest(ctx, ix, provenance.All(), names.All())
		if err != nil {
			return err
		}
		out.printf("read %d conversations\n", len(forest.ByID))
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
	fs := newFlagSet("index")
	fs.SetOutput(out)
	root := fs.String("root", "", "transcript root (default ~/.claude/projects)")
	db := fs.String("db", "", "index location")
	full := fs.Bool("full", false, "re-read every transcript instead of only what changed")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}

	dir := *root
	if dir == "" {
		var err error
		if dir, err = claudecode.DefaultRoot(); err != nil {
			return err
		}
	}
	ix, err := createIndex(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // reported by Rebuild failure instead

	src := claudecode.New(dir)
	sync := ix.Sync
	if *full {
		sync = ix.Rebuild
	}
	ctx := context.Background()
	stats, err := sync(ctx, src)
	if err != nil {
		return err
	}
	// Memories and work-product names are indexed here rather than on every
	// refresh: measuring the work tree walks it, and the map refreshes on
	// every transcript write.
	docsStart := time.Now()
	if err := ix.SyncDocs(ctx, src); err != nil {
		return err
	}
	// Counted in what is reported: indexing memories and work-product names
	// is part of indexing, and a duration that leaves it out is a duration
	// that does not match the wait.
	stats.Duration += time.Since(docsStart)
	if *asJSON {
		return out.emit(struct {
			Lanes    int     `json:"lanes"`
			Messages int     `json:"messages"`
			Parts    int     `json:"parts"`
			Took     float64 `json:"took_ms"`
		}{stats.Lanes, stats.Messages, stats.Parts, float64(stats.Duration.Microseconds()) / 1000})
	}
	out.printf("indexed %d lanes · %d messages · %d searchable parts in %s\n",
		stats.Lanes, stats.Messages, stats.Parts, stats.Duration.Round(time.Millisecond))
	return out.Err()
}

func cmdSearch(args []string, out *printer) error {
	fs := newFlagSet("search")
	fs.SetOutput(out)
	lane := fs.String("lane", "", "restrict to one conversation")
	kinds := fs.String("kind", "", "comma-separated part kinds")
	types := fs.String("type", "", "conversation,memory,artifact (default all three)")
	limit := fs.Int("limit", 20, "maximum hits")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	words, err := parseArgs(fs, args, out)
	if err != nil {
		return err
	}
	query := strings.Join(words, " ")
	if strings.TrimSpace(query) == "" {
		return errors.New("search needs a query (try: braids search blobstore)")
	}
	parsed, err := parseKinds(*kinds)
	if err != nil {
		return err
	}
	wanted, err := parseTypes(*types)
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
		index.Query{Text: query, Lane: *lane, Kinds: parsed, Types: wanted, Limit: *limit})
	if err != nil {
		return err
	}
	if *asJSON {
		rows := make([]hitOut, 0, len(hits))
		for _, h := range hits {
			rows = append(rows, hitOf(h))
		}
		return out.emit(struct {
			Query string   `json:"query"`
			Hits  []hitOut `json:"hits"`
			Count int      `json:"count"`
			Took  float64  `json:"took_ms"`
		}{query, rows, len(rows), float64(time.Since(start).Microseconds()) / 1000})
	}
	if len(hits) == 0 {
		out.printf("no matches for %q\n", query)
		return out.Err()
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, h := range hits {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			typeLabel(h), foundLabel(h), h.At.Format("01-02 15:04"),
			kindLabel(h), oneLine(h.Snippet)); err != nil {
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
	fs := newFlagSet("branch")
	laneRef := fs.String("lane", "", "conversation to branch from (ID prefix)")
	at := fs.Int("at", 0, "turn number to branch at")
	name := fs.String("name", "", "name for the new conversation")
	workspace := fs.Bool("workspace", false, "give the branch a git worktree of its own")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	if *laneRef == "" || *at <= 0 {
		return errors.New("branch needs --lane and --at (try: braids branch --lane abc123 --at 12)")
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
	provenance, err := sidecar.Load[model.Origin](sidecarPath(dbPath, "origins.json"))
	if err != nil {
		return err
	}
	kinds, err := sidecar.Load[string](sidecarPath(dbPath, "kinds.json"))
	if err != nil {
		return err
	}
	// Resolved once, so the prefix the user typed is turned into a whole ID
	// before anything acts on it.
	source, err := findLane(ctx, ix, *laneRef)
	if err != nil {
		return err
	}
	// Check before cutting: a branch that cannot be the kind it was asked for
	// should not exist at all. The source lane's directory is the one the
	// branch will run in.
	if *workspace {
		if err := worktreeOK(source.Cwd); err != nil {
			return err
		}
	}
	newID, err := branchAt(ctx, ix, provenance, source.ID, *at, *name)
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
	if *asJSON {
		return out.emit(struct {
			Lane   string `json:"lane"`
			From   string `json:"from"`
			At     int    `json:"at"`
			Kind   string `json:"kind"`
			Resume string `json:"resume"`
		}{newID, source.ID, *at, branchKindName(*workspace), command})
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
	fs := newFlagSet("promote")
	laneRef := fs.String("lane", "", "conversation the subagent belongs to")
	agentRef := fs.String("agent", "", "subagent to promote (ID prefix)")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	if *laneRef == "" {
		return errors.New("promote needs --lane (try: braids agents --lane abc123)")
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
	if *asJSON {
		return out.emit(struct {
			Lane   string `json:"lane"`
			From   string `json:"from"`
			Agent  string `json:"agent"`
			Type   string `json:"type"`
			Task   string `json:"task"`
			At     int    `json:"at"`
			Resume string `json:"resume"`
		}{newID, lane.ID, agent.ID, agent.Type, agent.Task, agent.ParentSeq, "claude --resume " + newID})
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
	fs := newFlagSet("agents")
	laneRef := fs.String("lane", "", "conversation to list subagents of")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
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
	if *asJSON {
		rows := make([]agentOut, 0, len(agents))
		for _, a := range agents {
			rows = append(rows, agentOf(a))
		}
		return out.emit(struct {
			Lane   string     `json:"lane"`
			Agents []agentOut `json:"agents"`
		}{lane.ID, rows})
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
	fs := newFlagSet("merge")
	baseRef := fs.String("lane", "", "conversation to carry on from")
	fromRef := fs.String("from", "", "branch whose turns are brought over")
	name := fs.String("name", "", "name for the merged conversation")
	dry := fs.Bool("plan", false, "report what would be carried over, and stop")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	if *baseRef == "" || *fromRef == "" {
		return errors.New("merge needs --lane and --from")
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
	if *dry && *asJSON {
		return out.emit(mergePlanOut(base.ID, incoming.ID, plan, ""))
	}
	if !*asJSON {
		out.printf("%s: %d turns, %d of them not in %s\n%s: %d turns not in %s\n",
			orUnnamed(base.Title), plan.BaseTurns, plan.BaseOnlyTurns, orUnnamed(incoming.Title),
			orUnnamed(incoming.Title), plan.IncomingTurns, orUnnamed(base.Title))
		if !plan.Worthwhile() {
			out.printf("\nnothing to join: one already contains the other\n")
		}
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
	if *asJSON {
		return out.emit(mergePlanOut(base.ID, incoming.ID, plan, merged.ID))
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
// orUnnamed labels a conversation that has no title. It exists, it just has
// not been named.
func orUnnamed(s string) string {
	if s == "" {
		return "(unnamed)"
	}
	return s
}

// orNone marks something absent. A memory with no recorded origin was not
// written by an unnamed conversation — no conversation was recorded at all,
// and "(unnamed)" would claim one existed.
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// cmdHook records one hook payload. It is what the harness runs, so it must
// never fail loudly: an error here is printed in the middle of someone's
// session, and nothing braids observes is worth interrupting work for.
// stdinIsTerminal is a variable so the guard below can be tested without a
// pseudo-terminal to hand.
var stdinIsTerminal = func() bool { return term.IsTerminal(os.Stdin.Fd()) }

func cmdHook(args []string, out *printer) error {
	// hook takes no arguments; the harness passes none. Anything here is a
	// person looking for what this is, so answer that rather than reading a
	// payload they were never going to send.
	if len(args) > 0 {
		out.printf("braids hook records one hook payload, read on stdin.\n")
		out.printf("The harness runs it, not you.\n\n")
		out.printf("to turn reporting on or off:  braids hooks\n")
		return errShown
	}

	// The harness pipes a payload in. A person typing this by hand gets an
	// explanation rather than a process that appears to have hung, waiting on
	// a terminal that is never going to send it anything. Checked with an
	// ioctl rather than by file mode, so a redirect from /dev/null — which is
	// a character device too — still reads as the harness and records nothing
	// quietly. A hook must never fail loudly: an error here would surface as a
	// broken hook in the middle of somebody's session.
	if stdinIsTerminal() {
		return errors.New("hook reads a hook payload on stdin, and the harness runs it, not you\n" +
			"       to turn reporting on or off, use: braids hooks")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil //nolint:nilerr // a hook that cannot find home simply records nothing
	}
	_ = hooks.Record(filepath.Join(home, ".braids", "events.jsonl"), os.Stdin)
	return nil
}

func cmdHooks(args []string, out *printer) error {
	fs := newFlagSet("hooks")
	install := fs.Bool("install", false, "ask sessions to report when they block")
	remove := fs.Bool("remove", false, "stop asking")
	settings := fs.String("settings", "", "settings file (default ~/.claude/settings.json)")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
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
		out.printf("  any braids hook left by another build now points here\n")
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

	status, err := hooks.Inspect(path, command)
	if err != nil {
		return err
	}
	if *asJSON {
		return out.emit(struct {
			Settings  string   `json:"settings"`
			Command   string   `json:"command"`
			Reporting bool     `json:"reporting"`
			Events    []string `json:"events"`
			Elsewhere []string `json:"elsewhere"`
		}{path, command, len(status.Events) > 0, orEmpty(status.Events), orEmpty(status.Elsewhere)})
	}
	out.printf("settings: %s\ncommand:  %s\n", path, command)
	switch {
	case len(status.Events) == 0:
		out.printf("status:   not installed, so braids cannot tell a running tool from one\n")
		out.printf("          waiting on you. `braids hooks --install` changes that.\n")
	default:
		out.printf("status:   reporting %s\n", strings.Join(status.Events, ", "))
	}
	for _, other := range status.Elsewhere {
		// A hook pointing at a binary that has moved fails on every event, so
		// say which one and how to take it over.
		out.printf("also:     %s\n", other)
		out.printf("          another braids owns these hooks; `braids hooks --install`\n")
		out.printf("          points them here\n")
	}
	return out.Err()
}

// accent is the orange braids uses everywhere else on screen. Written straight
// rather than through a styling library: the command line has one coloured
// thing in it, and that is not worth a dependency.
const accent, reset = "\x1b[38;2;240;136;62m", "\x1b[0m"

// stdoutIsTerminal is a variable so the colour decision can be tested.
var stdoutIsTerminal = func() bool { return term.IsTerminal(os.Stdout.Fd()) }

// mark prints braids' mark above whatever follows it. Colour only on a
// terminal: escape codes in a pipe are noise in somebody's grep.
func (p *printer) mark(art []string) {
	colour := stdoutIsTerminal()
	for _, line := range art {
		if colour {
			p.printf("%s%s%s\n", accent, line, reset)
		} else {
			p.printf("%s\n", strings.TrimRight(line, " "))
		}
	}
	p.printf("%s\n\n", strings.Repeat(" ", 4)+brand.Tagline)
}

// hooksReporting reports whether the harness has been asked to report. Hooks
// are optional, so a settings file that cannot be read is not an error here —
// it means the same thing as one with no braids hook in it.
func hooksReporting() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	command, err := hookCommand()
	if err != nil {
		return false
	}
	on, err := hooks.Installed(filepath.Join(home, ".claude", "settings.json"), command)
	return err == nil && len(on) > 0
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
	// The tables shorten an ID with an ellipsis, so the obvious thing to do
	// with one — copy it off the screen and paste it back — arrives with the
	// ellipsis attached. Accept it: refusing an ID braids itself printed
	// teaches nothing.
	ref = strings.TrimRight(ref, "…")
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
	fs := newFlagSet("lanes")
	fs.SetOutput(out)
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
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
	if *asJSON {
		// An empty result is an empty list, not a message: a caller should
		// not have to tell "nothing indexed" from a parse failure.
		rows := make([]laneOut, 0, len(lanes))
		for _, l := range lanes {
			rows = append(rows, laneOf(l))
		}
		return out.emit(struct {
			Lanes []laneOut `json:"lanes"`
		}{rows})
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

// cmdWork browses work products, and reclaims the ones nothing owns any more.
func cmdWork(args []string, out *printer) error {
	fs := newFlagSet("work")
	fs.SetOutput(out)
	laneRef := fs.String("lane", "", "conversation whose work products to list")
	sub := fs.String("path", "", "directory within them to list, relative to the top")
	orphans := fs.Bool("orphans", false, "list work products whose conversation is gone")
	reclaim := fs.Bool("reclaim", false, "with --orphans, move them to the bin")
	db := fs.String("db", "", "index location")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	if (*laneRef == "") == !*orphans {
		return errors.New("choose one of --lane or --orphans")
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
	root, err := claudecode.DefaultRoot()
	if err != nil {
		return err
	}
	src := claudecode.New(root)

	if *orphans {
		return workOrphans(ctx, ix, src, binAt(dbPath), *reclaim, *asJSON, out)
	}
	lane, err := findLane(ctx, ix, *laneRef)
	if err != nil {
		return err
	}
	if lane.ArtifactPath == "" {
		return fmt.Errorf("%s left no work products", shortID(lane.ID))
	}
	dir, err := within(lane.ArtifactPath, *sub)
	if err != nil {
		return err
	}
	entries, err := artifacts.Read(dir, claudecode.ReservedArtifact)
	if err != nil {
		return err
	}
	if *asJSON {
		return out.emit(workOut(lane.ID, dir, entries))
	}
	return printWork(dir, entries, out)
}

// within resolves a path inside a job directory, refusing to leave it. The
// argument comes from a command line, and "../.." would otherwise walk out of
// the tree the caller asked about.
func within(root, sub string) (string, error) {
	if sub == "" {
		return root, nil
	}
	dir := filepath.Clean(filepath.Join(root, sub))
	if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is outside the work products", sub)
	}
	return dir, nil
}

func printWork(dir string, entries []artifacts.Entry, out *printer) error {
	if len(entries) == 0 {
		out.printf("%s is empty\n", dir)
		return out.Err()
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	rows := []string{"SIZE\tFILES\tNAME"}
	var total int64
	for _, e := range entries {
		name := e.Name
		switch {
		case e.Dir:
			name += "/"
		case e.Reserved:
			name += "   (the harness's own record)"
		}
		rows = append(rows, fmt.Sprintf("%s\t%d\t%s", format.Bytes(e.Bytes), e.Files, name))
		total += e.Bytes
	}
	for _, r := range rows {
		if _, err := fmt.Fprintln(tw, r); err != nil {
			return fmt.Errorf("write work products: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write work products: %w", err)
	}
	out.printf("\n%s in %d entries · %s\n", format.Bytes(total), len(entries), dir)
	return out.Err()
}

// workOrphans reports the work products of conversations braids no longer
// knows about, and with --reclaim moves them to the bin. Nothing will ever look
// at them again, and nothing else will ever clear them up.
func workOrphans(ctx context.Context, ix *index.Index, src *claudecode.Source,
	bin *trash.Bin, reclaim, asJSON bool, out *printer,
) error {
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return err
	}
	known := make([]string, 0, len(lanes))
	for _, l := range lanes {
		known = append(known, l.ID)
	}
	jobs, err := artifacts.Jobs(src.JobsRoot())
	if err != nil {
		return err
	}
	orphans := artifacts.Orphans(jobs, known)

	var reclaimed int64
	if reclaim && len(orphans) > 0 {
		paths := make([]string, 0, len(orphans))
		for _, o := range orphans {
			paths = append(paths, o.Path)
		}
		entry, err := bin.Discard("work products with no conversation", paths)
		if err != nil {
			return err
		}
		reclaimed = entry.Bytes
	}

	if asJSON {
		rows := make([]orphanOut, 0, len(orphans))
		for _, o := range orphans {
			rows = append(rows, orphanOut{o.ID, o.Path, o.Bytes, o.Files, o.At})
		}
		return out.emit(struct {
			Orphans   []orphanOut `json:"orphans"`
			Bytes     int64       `json:"bytes"`
			Reclaimed int64       `json:"reclaimed_bytes"`
		}{rows, totalBytes(orphans), reclaimed})
	}
	if len(orphans) == 0 {
		out.printf("every set of work products still has its conversation\n")
		return out.Err()
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	rows := []string{"JOB\tSIZE\tFILES\tLAST WRITTEN"}
	for _, o := range orphans {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%s",
			o.ID, format.Bytes(o.Bytes), o.Files, lastWritten(o.At)))
	}
	for _, r := range rows {
		if _, err := fmt.Fprintln(tw, r); err != nil {
			return fmt.Errorf("write orphans: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write orphans: %w", err)
	}
	switch {
	case reclaim:
		out.printf("\nmoved %s to the bin · recover with `braids` and `u`\n", format.Bytes(reclaimed))
	default:
		out.printf("\n%s in %d sets · `braids work --orphans --reclaim` bins them\n",
			format.Bytes(totalBytes(orphans)), len(orphans))
	}
	return out.Err()
}

func totalBytes(jobs []artifacts.Job) int64 {
	var total int64
	for _, j := range jobs {
		total += j.Bytes
	}
	return total
}

// binAt is where deleted things wait: beside the index, so one braids
// directory holds everything braids owns.
func binAt(dbPath string) *trash.Bin {
	return trash.New(filepath.Join(filepath.Dir(dbPath), "trash"))
}

// lastWritten dates a set of work products, or says nothing for an empty one:
// a zero time printed as 0001-01-01 reads as a fault rather than as absence.
func lastWritten(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Format("2006-01-02")
}

// cmdMemories reports what a project remembers.
//
// Read-only. The index is what a session actually loads, so the two things
// worth saying are what is remembered and where the index and the files have
// drifted apart — a memory absent from the index is loaded by nothing.
func cmdMemories(args []string, out *printer) error {
	fs := newFlagSet("memories")
	fs.SetOutput(out)
	project := fs.String("project", "", "only this project")
	root := fs.String("root", "", "transcript root (default ~/.claude/projects)")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}
	dir := *root
	if dir == "" {
		var err error
		if dir, err = claudecode.DefaultRoot(); err != nil {
			return err
		}
	}
	sets, err := memorySets(claudecode.New(dir), *project)
	if err != nil {
		return err
	}
	if *asJSON {
		return out.emit(memoriesOut(sets))
	}
	return printMemories(sets, out)
}

// memorySets reads every project's memories, or one project's.
func memorySets(src store.Source, project string) ([]memory.Set, error) {
	rememberer, ok := src.(store.Rememberer)
	if !ok {
		return nil, errors.New("this source keeps no memories")
	}
	locations, err := rememberer.MemoryDirs()
	if err != nil {
		return nil, err
	}
	var sets []memory.Set
	for _, location := range locations {
		// Match the name braids shows, or anything in the directory it came
		// from. The shown name is the last dash-separated part of a slug that
		// encodes a path with dashes, so a directory called cluster-lab shows
		// as "Cluster" — consistent with the map, and surprising to type.
		if project != "" && !strings.EqualFold(location.Project, project) &&
			!strings.Contains(strings.ToLower(location.Dir), strings.ToLower(project)) {
			continue
		}
		set, err := memory.Read(location)
		if err != nil {
			return nil, err
		}
		if len(set.Memories) == 0 && len(set.Orphaned) == 0 {
			continue
		}
		sets = append(sets, set)
	}
	return sets, nil
}

func printMemories(sets []memory.Set, out *printer) error {
	if len(sets) == 0 {
		out.printf("nothing is remembered yet\n")
		return out.Err()
	}
	for i, set := range sets {
		if i > 0 {
			out.printf("\n")
		}
		out.printf("%s · %d memories · %s\n", set.Project, len(set.Memories), format.Bytes(set.Bytes()))
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		rows := []string{"MEMORY\tKIND\tLINKS\tCHANGED\tFROM"}
		for _, m := range set.Memories {
			name := m.Name
			if !m.Listed {
				name += " ⚠"
			}
			rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%s\t%s",
				truncate(name, 34), m.Kind, len(m.Links),
				m.Modified.Format("01-02"), orNone(shortID(m.Origin))))
		}
		for _, r := range rows {
			if _, err := fmt.Fprintln(tw, r); err != nil {
				return fmt.Errorf("write memories: %w", err)
			}
		}
		if err := tw.Flush(); err != nil {
			return fmt.Errorf("write memories: %w", err)
		}
		// The index is what a session loads, so a disagreement with it is the
		// one thing here that is actually broken rather than merely listed.
		for _, m := range set.Unlisted() {
			out.printf("  ⚠ %s is not in %s, so nothing ever loads it\n", m.Name, memory.IndexFile)
		}
		for _, slug := range set.Orphaned {
			out.printf("  ⚠ %s names %s, which is not there\n", memory.IndexFile, slug)
		}
		// Separated from the two warnings above on purpose: a link to a memory
		// that does not exist yet is how the convention marks something worth
		// writing later, so it is reported without being called a fault.
		for _, link := range set.Dangling() {
			out.printf("  · %s is waiting on [[%s]], not written yet\n", link.From, link.To)
		}
	}
	return out.Err()
}

// parseTypes reads the --type list: what sort of thing to look in. Empty means
// all of them, so a plain search stays global.
func parseTypes(s string) ([]index.Found, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	valid := map[string]index.Found{
		string(index.FoundTurn):     index.FoundTurn,
		string(index.FoundMemory):   index.FoundMemory,
		string(index.FoundArtifact): index.FoundArtifact,
	}
	var types []index.Found
	for _, raw := range strings.Split(s, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		t, ok := valid[name]
		if !ok {
			return nil, fmt.Errorf("unknown type %q (want conversation, memory or artifact)", name)
		}
		types = append(types, t)
	}
	return types, nil
}

// typeLabel is the one-word column saying what a hit is.
func typeLabel(h index.Hit) string {
	switch h.Of {
	case index.FoundMemory:
		return "memory"
	case index.FoundArtifact:
		return "work"
	default:
		return "convo"
	}
}

// foundLabel names the thing found: the conversation for a turn, the memory or
// the file for the others.
func foundLabel(h index.Hit) string {
	if h.Of == index.FoundTurn {
		return laneLabel(h)
	}
	return truncate(h.Name, 34)
}

// watched is everywhere a change should wake the map: where the harness writes
// transcripts, where sessions report what they are waiting on, and where each
// project keeps its memories.
//
// Memory directories are named individually because the watcher covers a root
// and one level below it, and they sit two levels down — inside the project
// directory, beside the transcripts. Work products are deliberately not
// watched: that is a tree of thousands of files a tool writes into constantly,
// and the only thing braids would do with the news is re-measure it.
func watched(src *claudecode.Source, root, dbPath string) []string {
	roots := []string{root, filepath.Dir(dbPath)}
	locations, err := src.MemoryDirs()
	if err != nil {
		return roots
	}
	for _, location := range locations {
		roots = append(roots, location.Dir)
	}
	return roots
}
