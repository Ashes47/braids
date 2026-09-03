package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

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
	}
}

// Lanes enumerates every transcript under the root. Subagent transcripts, which
// live one directory deeper, are deliberately excluded: they are attached to
// their parent lane rather than listed alongside it.
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
			if title, err := readTitle(path); err == nil {
				lane.Title = title
			}
			lanes = append(lanes, lane)
		}
	}
	return lanes, nil
}

// Messages streams a lane's messages in file order, skipping the bookkeeping
// records Claude Code interleaves with the conversation.
func (s *Source) Messages(ctx context.Context, lane model.Lane, visit store.Visit) error {
	f, err := os.Open(lane.Path)
	if err != nil {
		return fmt.Errorf("open lane %s: %w", lane.ID, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	sc := newScanner(f)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue // a malformed line must not abort an otherwise good lane
		}
		msg, ok := r.toMessage(lane.ID)
		if !ok {
			continue
		}
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

// readTitle returns the lane's display title, preferring a title the user set
// over one the model generated.
func readTitle(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	var custom, ai, agent string
	sc := newScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
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
		return "", fmt.Errorf("scan %s: %w", path, err)
	}
	return firstNonEmpty(custom, ai, agent), nil
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

type record struct {
	Type              string      `json:"type"`
	UUID              string      `json:"uuid"`
	ParentUUID        *string     `json:"parentUuid"`
	LogicalParentUUID *string     `json:"logicalParentUuid"`
	Timestamp         string      `json:"timestamp"`
	IsMeta            bool        `json:"isMeta"`
	Message           *rawMessage `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
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
		ID:       r.UUID,
		ParentID: r.parentID(),
		LaneID:   laneID,
		Role:     role,
		At:       at,
		Parts:    parts,
	}, true
}

// parentID stitches over compaction. A compact boundary is written with a nil
// parentUuid and a logicalParentUuid pointing at the pre-compaction record;
// without this, one conversation reads as one lane per compaction.
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
		return model.Part{Kind: model.PartToolUse, Tool: b.Name, Text: string(b.Input)}, true
	case "tool_result":
		text := flatten(b.Content)
		if text == "" {
			return model.Part{}, false
		}
		return model.Part{Kind: model.PartToolResult, Text: text}, true
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
var _ store.Source = (*Source)(nil)
