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
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
)

// Set by the release build.
var (
	version = "dev"
	commit  = "none"
)

const usage = `braids — manage Claude Code conversations as a graph

usage:
  braids index [--root DIR]              rebuild the index from local transcripts
  braids search [flags] QUERY            search every indexed message
  braids lanes                           list indexed conversations
  braids version

search flags:
  --lane ID        restrict to one conversation
  --kind LIST      text,thinking,tool_use,tool_result (comma separated)
  --limit N        maximum hits (default 20)

common flags:
  --db PATH        index location (default $BRAIDS_DB or ~/.braids/index.db)
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
		out.printf("%s", usage)
		return out.Err()
	}
	switch args[0] {
	case "index":
		return cmdIndex(args[1:], out)
	case "search":
		return cmdSearch(args[1:], out)
	case "lanes":
		return cmdLanes(args[1:], out)
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

// openIndex resolves the database path and opens it.
func openIndex(dbFlag string) (*index.Index, error) {
	path := dbFlag
	if path == "" {
		var err error
		if path, err = defaultDB(); err != nil {
			return nil, err
		}
	}
	return index.Open(path)
}

func cmdIndex(args []string, out *printer) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", "", "transcript root (default ~/.claude/projects)")
	db := fs.String("db", "", "index location")
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

	stats, err := ix.Rebuild(context.Background(), claudecode.New(dir))
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
