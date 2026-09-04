// Command shots draws one frame of any braids screen and prints it.
//
// braids itself prints the map, a spine and search, which is what --print is
// for. The work browser, the memory list and reader, and the bin are reached
// with keys, and adding a flag to the program for the sake of a screenshot
// would put a command in every user's --help for the benefit of the website.
// So this lives here, in the tools, and drives the same screens the same way.
//
// It is not part of the braids binary and is never installed.
//
//	go run ./scripts/shots --db /tmp/braids-demo/braids/index.db \
//	    --root /tmp/braids-demo/projects --screen memories --width 195
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ashes47/braids/internal/core/artifacts"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/sidecar"
	"github.com/Ashes47/braids/internal/core/store/claudecode"
	"github.com/Ashes47/braids/internal/core/trash"
	"github.com/Ashes47/braids/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "shots: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	db := flag.String("db", "", "index to read (required)")
	root := flag.String("root", "", "transcript root (required)")
	screen := flag.String("screen", "", "named screen: "+strings.Join(named(), ", "))
	lane := flag.String("lane", "", "conversation to land on")
	query := flag.String("query", "", "with no screen, the search screen for this query")
	keys := flag.String("keys", "", "keys to press, space separated, instead of --screen")
	bare := flag.Bool("no-mark", false, "leave the ASCII mark out, for a page that has it already")
	discard := flag.String("discard", "", "move this path to the bin first, so the bin has something in it")
	width := flag.Int("width", 195, "frame width")
	height := flag.Int("height", 24, "frame height")
	flag.Parse()
	if *db == "" || *root == "" {
		return fmt.Errorf("--db and --root are both required")
	}

	ctx := context.Background()
	ix, err := index.Open(*db)
	if err != nil {
		return err
	}
	defer ix.Close() //nolint:errcheck // read only

	src := claudecode.New(*root)
	dir := filepath.Dir(*db)
	origins, err := sidecar.Load[model.Origin](filepath.Join(dir, "origins.json"))
	if err != nil {
		return err
	}
	names, err := sidecar.Load[string](filepath.Join(dir, "names.json"))
	if err != nil {
		return err
	}
	forest, err := tui.Forest(ctx, ix, origins.All(), names.All())
	if err != nil {
		return err
	}

	bin := trash.New(filepath.Join(dir, "trash"))
	opts := tui.Options{
		Source:    "claudecode",
		IndexPath: "~/.braids/index.db",
		// The frames go on a page that carries the mark in its own header, and
		// the mark costs the map 87 columns it would rather spend on titles.
		HideMark: *bare,
		// So the header says what it says on a machine with the hook
		// installed, which is what the facts block is describing.
		Reporting: true,
		LoadSpine: tui.SpineLoader(ctx, ix),
		Search: func(query, scope string) ([]index.Hit, error) {
			return ix.Search(ctx, index.Query{Text: query, Lane: scope, Limit: 200})
		},
		LoadBin: bin.List,
		LoadMemories: func() ([]memory.Set, error) {
			locations, err := src.MemoryDirs()
			if err != nil {
				return nil, err
			}
			var sets []memory.Set
			for _, location := range locations {
				set, err := memory.Read(location)
				if err != nil {
					return nil, err
				}
				if len(set.Memories) > 0 || len(set.Orphaned) > 0 {
					sets = append(sets, set)
				}
			}
			return sets, nil
		},
		LoadWork: func(laneID, at string) (tui.WorkLevel, error) {
			lanes, err := ix.Lanes(ctx)
			if err != nil {
				return tui.WorkLevel{}, err
			}
			for _, l := range lanes {
				if !strings.HasPrefix(l.ID, laneID) || l.ArtifactPath == "" {
					continue
				}
				// The screen hands back a path relative to the job root, so
				// it is joined and checked rather than trusted.
				where := filepath.Clean(filepath.Join(l.ArtifactPath, at))
				if rel, err := filepath.Rel(l.ArtifactPath, where); err != nil ||
					strings.HasPrefix(rel, "..") {
					return tui.WorkLevel{}, fmt.Errorf("%s is outside %s", at, l.ArtifactPath)
				}
				entries, err := artifacts.Read(where, claudecode.ReservedArtifact)
				if err != nil {
					return tui.WorkLevel{}, err
				}
				return tui.WorkLevel{Root: l.ArtifactPath, Dir: where, Entries: entries}, nil
			}
			return tui.WorkLevel{}, fmt.Errorf("%.8s left no work products", laneID)
		},
	}

	if *discard != "" {
		if _, err := bin.Discard(filepath.Base(*discard), []string{*discard}); err != nil {
			return err
		}
	}

	press := strings.Fields(*keys)
	if *screen != "" {
		script, ok := screens[*screen]
		if !ok {
			return fmt.Errorf("no screen %q; try one of %s", *screen, strings.Join(named(), ", "))
		}
		press = script
	}

	fmt.Println(tui.RenderShot(forest, opts, tui.Shot{
		Lane: *lane, Query: *query, Keys: press,
		Width: *width, Height: *height,
	}))
	return nil
}

// screens are the keys that reach each screen, so the tool that takes the
// screenshots says "memories" rather than spelling out a keystroke sequence.
// A file has to be descended to: the top of a job directory is one directory
// and the harness's own record of the job, and neither is worth a picture.
var screens = map[string][]string{
	"spine":    {"enter"},
	"work":     {"w"},
	"file":     {"w", "enter", "enter"},
	"memories": {"M"},
	"memory":   {"M", "j", "j", "j", "enter"},
	"bin":      {"u"},
}

func named() []string {
	out := make([]string, 0, len(screens))
	for name := range screens {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
