package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

// shortIDLen is how much of a session ID names its job directory.
const shortIDLen = 8

// maxLine caps a single JSONL record. The largest record seen in a real
// transcript is ~1.2 MB, so 64 MB is generous headroom rather than a guess.
const maxLine = 64 << 20

// titleHints pre-filter lines before JSON decoding: title records are tiny and
// rare, so scanning bytes is far cheaper than decoding every line.
var titleHints = [][]byte{[]byte("Title"), []byte("agentName")}

// Source reads Claude Code transcripts from a projects directory.
type Source struct {
	root string
}

// New returns a Source reading transcripts from root, which is normally the
// value of DefaultRoot.
func New(root string) *Source { return &Source{root: root} }

// DefaultRoot is where Claude Code stores transcripts for the current user.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Name identifies this harness.
func (s *Source) Name() string { return "claudecode" }

// Capabilities reports that Claude Code supports every optional feature.
func (s *Source) Capabilities() store.Capabilities {
	return store.Capabilities{
		InFileBranching: true,
		Subagents:       true,
		Compaction:      true,
		StableIDs:       true,
		Branching:       true,
	}
}

// Lanes enumerates every transcript under the root, costing one directory scan
// and a stat per file. Titles are deliberately not read here — that means
// opening every transcript — and are fetched through Title when a lane is
// actually being re-read.
//
// Subagent transcripts, which live one directory deeper, are excluded: they are
// attached to their parent lane rather than listed alongside it.
func (s *Source) Lanes(ctx context.Context) ([]model.Lane, error) {
	projects, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read projects dir: %w", err)
	}
	var lanes []model.Lane
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, project.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read project %s: %w", project.Name(), err)
		}
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				return nil, fmt.Errorf("stat %s: %w", e.Name(), err)
			}
			path := filepath.Join(dir, e.Name())
			lane := model.Lane{
				ID:      strings.TrimSuffix(e.Name(), ".jsonl"),
				Source:  s.Name(),
				Project: projectName(project.Name()),
				Path:    path,
				Created: birthTime(info),
				Updated: info.ModTime(),
				Size:    info.Size(),
			}
			lanes = append(lanes, lane)
		}
	}
	return lanes, nil
}

// Enrich reads the details that need the transcript open: the display title,
// preferring one the user set over one the model generated, and the directory
// the conversation ran in.
func (s *Source) Enrich(_ context.Context, lane model.Lane) (model.Lane, error) {
	title, cwd, err := readMeta(lane.Path)
	if err != nil {
		return lane, err
	}
	lane.Title, lane.Cwd = title, cwd
	lane.ArtifactPath, lane.ArtifactBytes = s.artifacts(lane.ID)
	return lane, nil
}

// artifacts locates a conversation's work products and measures them.
//
// Claude Code keeps them outside the transcript, beside the projects directory,
// and they dwarf it: 3.2 GB against 392 MB of conversation on this machine,
// almost all of it scratch files. Measuring costs a directory walk of a few
// thousand entries, which is milliseconds.
func (s *Source) artifacts(laneID string) (path string, bytes int64) {
	// Job directories are named by the short form of the session ID, not the
	// whole UUID the transcript is filed under.
	if len(laneID) < shortIDLen {
		return "", 0
	}
	path = filepath.Join(filepath.Dir(s.root), "jobs", laneID[:shortIDLen])
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", 0
	}
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry contributes nothing
		}
		if fi, err := d.Info(); err == nil {
			bytes += fi.Size()
		}
		return nil
	})
	if err != nil || bytes == 0 {
		return "", 0
	}
	return path, bytes
}

// Messages streams a lane's messages in file order, skipping the bookkeeping
// records Claude Code interleaves with the conversation.
func (s *Source) Messages(ctx context.Context, lane model.Lane, visit store.Visit) error {
	f, err := os.Open(lane.Path)
	if err != nil {
		return fmt.Errorf("open lane %s: %w", lane.ID, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	// Claude Code threads bookkeeping records (attachments, snapshots) into the
	// same parent chain as the conversation, so a message's parentUuid usually
	// names a record that is not itself a message. nearest maps every record to
	// the closest *conversational* ancestor, which is what ParentID must be for
	// the chain to reconstruct.
	nearest := make(map[string]string)
	// A compaction is announced by a system record just before the summary that
	// replaces what it dropped, so it is carried forward one record rather than
	// costing a second pass over the transcript.
	var pending *model.Compaction

	sc := newScanner(f)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue // a malformed line must not abort an otherwise good lane
		}
		if c := r.compaction(); c != nil {
			pending = c
		}
		if r.UUID == "" {
			continue
		}
		ancestor := nearest[r.parentID()]
		msg, ok := r.toMessage(lane.ID)
		if !ok {
			nearest[r.UUID] = ancestor
			continue
		}
		nearest[r.UUID] = r.UUID
		msg.ParentID = ancestor
		msg.Compaction, pending = pending, nil
		if err := visit(msg); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan lane %s: %w", lane.ID, err)
	}
	return nil
}

func newScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	return sc
}

// projectName turns Claude Code's path slug ("-Users-me-src-app") into a short
// label ("app").
func projectName(slug string) string {
	parts := strings.Split(strings.Trim(slug, "-"), "-")
	if len(parts) == 0 {
		return slug
	}
	return parts[len(parts)-1]
}

// readMeta scans a transcript once for the details that are not in its
// filename: the titles it carries, and the directory it ran in.
//
// Titles are rewritten as a conversation goes on, so the scan cannot stop early
// for them; the working directory never changes, so the first one wins.
func readMeta(path string) (title, cwd string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	var custom, ai, agent string
	sc := newScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if cwd == "" && bytes.Contains(line, []byte(`"cwd"`)) {
			var r struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &r) == nil {
				cwd = r.Cwd
			}
		}
		if !hasTitleHint(line) {
			continue
		}
		var t titleRecord
		if err := json.Unmarshal(line, &t); err != nil {
			continue
		}
		switch {
		case t.CustomTitle != "":
			custom = t.CustomTitle
		case t.AITitle != "":
			ai = t.AITitle
		case t.AgentName != "":
			agent = t.AgentName
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", fmt.Errorf("scan %s: %w", path, err)
	}
	return firstNonEmpty(custom, ai, agent), cwd, nil
}

func hasTitleHint(line []byte) bool {
	for _, h := range titleHints {
		if bytes.Contains(line, h) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

type titleRecord struct {
	CustomTitle string `json:"customTitle"`
	AITitle     string `json:"aiTitle"`
	AgentName   string `json:"agentName"`
}

type compactMetadata struct {
	Trigger                 string `json:"trigger"`
	PreTokens               int    `json:"preTokens"`
	PostTokens              int    `json:"postTokens"`
	CumulativeDroppedTokens int    `json:"cumulativeDroppedTokens"`
	DurationMs              int    `json:"durationMs"`
}

type record struct {
	Type              string           `json:"type"`
	Subtype           string           `json:"subtype"`
	CompactMetadata   *compactMetadata `json:"compactMetadata"`
	UUID              string           `json:"uuid"`
	ParentUUID        *string          `json:"parentUuid"`
	LogicalParentUUID *string          `json:"logicalParentUuid"`
	Timestamp         string           `json:"timestamp"`
	IsMeta            bool             `json:"isMeta"`
	Message           *rawMessage      `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

// toMessage converts a raw record, reporting false for the bookkeeping records
// (attachments, titles, mode changes) that are not part of the conversation.
func (r record) toMessage(laneID string) (model.Message, bool) {
	if r.UUID == "" || r.IsMeta || r.Message == nil {
		return model.Message{}, false
	}
	var role model.Role
	switch r.Type {
	case "user":
		role = model.RoleUser
	case "assistant":
		role = model.RoleAssistant
	default:
		return model.Message{}, false
	}
	parts := parseParts(r.Message.Content)
	if len(parts) == 0 {
		return model.Message{}, false
	}
	at, _ := time.Parse(time.RFC3339, r.Timestamp)
	return model.Message{
		ID:     r.UUID,
		LaneID: laneID,
		Role:   role,
		At:     at,
		Parts:  parts,
	}, true
}

// compaction reads a compact boundary, which is a system record rather than a
// turn and so never becomes a message of its own.
func (r record) compaction() *model.Compaction {
	if r.Subtype != "compact_boundary" || r.CompactMetadata == nil {
		return nil
	}
	m := r.CompactMetadata
	return &model.Compaction{
		Trigger:    m.Trigger,
		PreTokens:  m.PreTokens,
		PostTokens: m.PostTokens,
		Dropped:    m.CumulativeDroppedTokens,
		Duration:   time.Duration(m.DurationMs) * time.Millisecond,
	}
}

// parentID is the raw predecessor of a record, stitched over compaction: a
// compact boundary is written with a nil parentUuid and a logicalParentUuid
// pointing at the pre-compaction record, and without following it one
// conversation reads as one lane per compaction.
func (r record) parentID() string {
	if r.ParentUUID != nil {
		return *r.ParentUUID
	}
	if r.LogicalParentUUID != nil {
		return *r.LogicalParentUUID
	}
	return ""
}

// parseParts handles both content shapes Claude Code writes: a bare string, or
// an array of typed blocks.
func parseParts(raw json.RawMessage) []model.Part {
	if len(raw) == 0 {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []model.Part{{Kind: model.PartText, Text: text}}
	}
	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	parts := make([]model.Part, 0, len(blocks))
	for _, b := range blocks {
		if p, ok := b.toPart(); ok {
			parts = append(parts, p)
		}
	}
	return parts
}

func (b block) toPart() (model.Part, bool) {
	switch b.Type {
	case "text":
		if strings.TrimSpace(b.Text) == "" {
			return model.Part{}, false
		}
		return model.Part{Kind: model.PartText, Text: b.Text}, true
	case "thinking":
		if strings.TrimSpace(b.Thinking) == "" {
			return model.Part{}, false
		}
		return model.Part{Kind: model.PartThinking, Text: b.Thinking}, true
	case "tool_use":
		return model.Part{Kind: model.PartToolUse, Tool: b.Name, ID: b.ID, Text: string(b.Input)}, true
	case "tool_result":
		text := flatten(b.Content)
		if text == "" {
			return model.Part{}, false
		}
		return model.Part{Kind: model.PartToolResult, ID: b.ToolUseID, Text: text}, true
	default:
		return model.Part{}, false
	}
}

// flatten renders a tool result, which may be a string or an array of blocks.
func flatten(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(blk.Text)
	}
	return b.String()
}

// compile-time check that Source satisfies the port.
var (
	_ store.Source     = (*Source)(nil)
	_ store.Enricher   = (*Source)(nil)
	_ store.Sidechains = (*Source)(nil)
)
