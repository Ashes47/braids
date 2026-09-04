package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/Ashes47/braids/internal/core/artifacts"
	"github.com/Ashes47/braids/internal/core/index"
	"github.com/Ashes47/braids/internal/core/memory"
	"github.com/Ashes47/braids/internal/core/store"
)

// The machine surface is declared here as its own types rather than by tagging
// the ones braids works with internally. What it prints for a program is a
// promise that has to keep its shape; what it holds in memory should stay free
// to change without breaking a caller.
//
// IDs are whole here, always. The tables shorten them with an ellipsis to fit a
// terminal, which is right for a person glancing at a list and useless to
// anything that has to hand the ID back.

type laneOut struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project"`
	Path     string    `json:"path"`
	Cwd      string    `json:"cwd,omitempty"`
	Messages int       `json:"messages"`
	Parts    int       `json:"parts"`
	Bytes    int64     `json:"bytes"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Resume   string    `json:"resume"`
}

func laneOf(l index.LaneInfo) laneOut {
	return laneOut{
		ID: l.ID, Title: l.Title, Project: l.Project, Path: l.Path, Cwd: l.Cwd,
		Messages: l.Messages, Parts: l.Parts, Bytes: l.Size,
		Created: l.Created, Updated: l.Updated,
		Resume: "claude --resume " + l.ID,
	}
}

type hitOut struct {
	// Of says what was found: a conversation's turn, a memory, or a work
	// product. A result whose kind you cannot tell is one you have to open
	// before you understand it.
	Of string `json:"of"`
	// Name is a memory's slug or a work product's path, empty for a turn.
	Name string `json:"name,omitempty"`
	// Path is the file it lives in, for the things that are files.
	Path      string `json:"path,omitempty"`
	Lane      string `json:"lane"`
	LaneTitle string `json:"lane_title"`
	Project   string `json:"project"`
	Message   string `json:"message"`
	// Turn is the position in the conversation, and what --at takes to branch
	// at this exact point.
	Turn    int       `json:"turn"`
	Kind    string    `json:"kind"`
	Role    string    `json:"role"`
	Tool    string    `json:"tool,omitempty"`
	Snippet string    `json:"snippet"`
	At      time.Time `json:"at"`
}

func hitOf(h index.Hit) hitOut {
	return hitOut{
		Of: found(h), Name: h.Name, Path: h.Path,
		Lane: h.LaneID, LaneTitle: h.LaneTitle, Project: h.Project,
		Message: h.MessageID, Turn: h.Seq, Kind: string(h.Kind),
		Role: string(h.Role), Tool: h.Tool, Snippet: h.Snippet, At: h.At,
	}
}

type agentOut struct {
	ID string `json:"id"`
	// Lane is the conversation that spawned it.
	Lane     string `json:"lane"`
	Type     string `json:"type"`
	Task     string `json:"task"`
	Turn     int    `json:"turn"`
	Depth    int    `json:"depth"`
	Messages int    `json:"messages"`
	Path     string `json:"path"`
}

func agentOf(a index.SubagentRow) agentOut {
	return agentOut{
		ID: a.ID, Lane: a.LaneID, Type: a.Type, Task: a.Task,
		Turn: a.ParentSeq, Depth: a.Depth, Messages: a.Messages, Path: a.Path,
	}
}

// emit writes one JSON document. Indented, because these are read by a person
// debugging at least as often as by a program, and the size never makes that
// a cost worth saving.
func (p *printer) emit(v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	p.printf("%s\n", body)
	return p.Err()
}

// jsonFlag declares --json on a command. Declared in one place so every
// command spells it and describes it the same way.
func jsonFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("json", false, "machine-readable output, with whole IDs")
}

// mergePlanOut reports both sides of a join. A caller deciding whether to merge
// needs the same two numbers a person does: what each side has that the other
// does not. Lane is empty for a plan, and the new conversation once joined.
func mergePlanOut(base, incoming string, plan store.MergePlan, lane string) any {
	return struct {
		Lane          string `json:"lane,omitempty"`
		Base          string `json:"base"`
		Incoming      string `json:"incoming"`
		BaseTurns     int    `json:"base_turns"`
		BaseOnlyTurns int    `json:"base_only_turns"`
		IncomingTurns int    `json:"incoming_turns"`
		Worthwhile    bool   `json:"worthwhile"`
	}{lane, base, incoming, plan.BaseTurns, plan.BaseOnlyTurns, plan.IncomingTurns, plan.Worthwhile()}
}

// orEmpty renders a nil slice as [] rather than null, so a caller can iterate
// what comes back without checking for it first.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type entryOut struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Dir   bool      `json:"dir"`
	Bytes int64     `json:"bytes"`
	Files int       `json:"files"`
	At    time.Time `json:"at"`
	// Reserved marks the harness's own record of a job. braids shows it so the
	// sizes add up, and refuses to delete it.
	Reserved bool `json:"reserved"`
}

type orphanOut struct {
	Job   string    `json:"job"`
	Path  string    `json:"path"`
	Bytes int64     `json:"bytes"`
	Files int       `json:"files"`
	At    time.Time `json:"at"`
}

// workOut is one level of a job directory, with the totals a caller would
// otherwise have to add up itself.
func workOut(lane, dir string, entries []artifacts.Entry) any {
	rows := make([]entryOut, 0, len(entries))
	var bytes int64
	var files int
	for _, e := range entries {
		rows = append(rows, entryOut{e.Name, e.Path, e.Dir, e.Bytes, e.Files, e.At, e.Reserved})
		bytes += e.Bytes
		files += e.Files
	}
	return struct {
		Lane    string     `json:"lane"`
		Path    string     `json:"path"`
		Entries []entryOut `json:"entries"`
		Bytes   int64      `json:"bytes"`
		Files   int        `json:"files"`
	}{lane, dir, rows, bytes, files}
}

type memoryOut struct {
	Name        string    `json:"name"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Path        string    `json:"path"`
	Bytes       int64     `json:"bytes"`
	Modified    time.Time `json:"modified"`
	// Origin is the conversation that wrote it, when one was recorded. It is
	// the edge worth having: it leads back to where the decision was made.
	Origin string   `json:"origin,omitempty"`
	Links  []string `json:"links"`
	// Listed is whether the index mentions it. False means nothing loads it.
	Listed bool `json:"listed"`
}

type setOut struct {
	Project  string         `json:"project"`
	Dir      string         `json:"dir"`
	Memories []memoryOut    `json:"memories"`
	Bytes    int64          `json:"bytes"`
	Kinds    map[string]int `json:"kinds"`
	// Unlisted, Orphaned and Dangling are what the index and the files
	// disagree about, which is the part a person cannot see from inside a
	// session.
	Unlisted []string      `json:"unlisted"`
	Orphaned []string      `json:"orphaned"`
	Dangling []danglingOut `json:"dangling"`
}

type danglingOut struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func memoriesOut(sets []memory.Set) any {
	rows := make([]setOut, 0, len(sets))
	for _, set := range sets {
		memories := make([]memoryOut, 0, len(set.Memories))
		for _, m := range set.Memories {
			links := m.Links
			if links == nil {
				links = []string{}
			}
			memories = append(memories, memoryOut{
				m.Name, m.Title, m.Description, m.Kind, m.Path, m.Bytes,
				m.Modified, m.Origin, links, m.Listed,
			})
		}
		unlisted := make([]string, 0)
		for _, m := range set.Unlisted() {
			unlisted = append(unlisted, m.Name)
		}
		dangling := make([]danglingOut, 0)
		for _, l := range set.Dangling() {
			dangling = append(dangling, danglingOut{l.From, l.To})
		}
		orphaned := set.Orphaned
		if orphaned == nil {
			orphaned = []string{}
		}
		rows = append(rows, setOut{
			Project: set.Project, Dir: set.Dir, Memories: memories,
			Bytes: set.Bytes(), Kinds: set.ByKind(),
			Unlisted: unlisted, Orphaned: orphaned, Dangling: dangling,
		})
	}
	return struct {
		Projects []setOut `json:"projects"`
	}{rows}
}

// found is the kind of thing a hit is, defaulting to a conversation turn: the
// zero value is the common case and the original one.
func found(h index.Hit) string {
	if h.IsTurn() {
		return string(index.FoundTurn)
	}
	return string(h.Of)
}
