package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

// Branch writes a new transcript containing the source lane's records from the
// root up to and including the chosen turn, and nothing after it.
//
// The source file is opened read-only and never written to. The new transcript
// is built in a temporary file and renamed into place, so an interrupted branch
// leaves either a complete lane or none at all.
//
// Records are copied verbatim apart from their session ID. That includes the
// environment they captured — working directory, git branch, file snapshots —
// so a branch resumes believing in the world as it was. That is deliberate: the
// harness already refuses an edit built on a stale read, which is the only
// point at which the difference can do harm.
func (s *Source) Branch(ctx context.Context, req store.BranchRequest) (model.Lane, error) {
	if req.AtMessage == "" {
		return model.Lane{}, errors.New("branch: no turn chosen to branch at")
	}
	lines, err := readLines(req.Lane.Path)
	if err != nil {
		return model.Lane{}, err
	}
	keep, err := ancestry(lines, req.AtMessage)
	if err != nil {
		return model.Lane{}, err
	}

	newID, err := newSessionID()
	if err != nil {
		return model.Lane{}, err
	}
	path := filepath.Join(filepath.Dir(req.Lane.Path), newID+".jsonl")
	if err := writeBranch(ctx, path, newID, req, lines, keep); err != nil {
		return model.Lane{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return model.Lane{}, fmt.Errorf("stat new lane: %w", err)
	}
	return model.Lane{
		ID:      newID,
		Source:  s.Name(),
		Project: req.Lane.Project,
		Path:    path,
		Title:   req.Name,
		Created: birthTime(info),
		Updated: info.ModTime(),
		Size:    info.Size(),
	}, nil
}

// line is one raw record plus the identity braids needs to thread it.
type line struct {
	raw    []byte
	uuid   string
	parent string
}

func readLines(path string) ([]line, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open lane: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only

	var out []line
	sc := newScanner(f)
	for sc.Scan() {
		raw := append([]byte(nil), sc.Bytes()...)
		var r record
		if err := json.Unmarshal(raw, &r); err != nil {
			out = append(out, line{raw: raw})
			continue
		}
		out = append(out, line{raw: raw, uuid: r.UUID, parent: r.parentID()})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read lane: %w", err)
	}
	return out, nil
}

// ancestry returns the set of record IDs from the root up to target. It walks
// the raw chain, bookkeeping records included, because the resumed transcript
// needs every record the harness wrote — not only the conversational ones.
func ancestry(lines []line, target string) (map[string]bool, error) {
	parents := make(map[string]string, len(lines))
	found := false
	for _, l := range lines {
		if l.uuid == "" {
			continue
		}
		parents[l.uuid] = l.parent
		if l.uuid == target {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("branch: turn %s is not in this conversation", short(target))
	}

	keep := make(map[string]bool)
	for cur := target; cur != "" && !keep[cur]; cur = parents[cur] {
		keep[cur] = true
	}
	return keep, nil
}

func writeBranch(ctx context.Context, path, newID string, req store.BranchRequest, lines []line, keep map[string]bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".braids-branch-*")
	if err != nil {
		return fmt.Errorf("create temp lane: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // best effort once renamed

	write := func(v any) error {
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encode record: %w", err)
		}
		_, err = tmp.Write(append(encoded, '\n'))
		return err
	}

	err = func() error {
		if req.Name != "" {
			if err := write(map[string]string{
				"type": "custom-title", "customTitle": req.Name, "sessionId": newID,
			}); err != nil {
				return err
			}
		}
		for _, l := range lines {
			if err := ctx.Err(); err != nil {
				return err
			}
			if l.uuid == "" || !keep[l.uuid] {
				continue
			}
			rewritten, err := retarget(l.raw, newID)
			if err != nil {
				return err
			}
			if _, err := tmp.Write(append(rewritten, '\n')); err != nil {
				return err
			}
		}
		// The harness resumes from the last record's chain; this pins the leaf.
		return write(map[string]string{
			"type": "last-prompt", "leafUuid": req.AtMessage, "sessionId": newID,
		})
	}()
	if err != nil {
		return errors.Join(fmt.Errorf("write branch: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close branch: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("place branch: %w", err)
	}
	return nil
}

// retarget rewrites a record's session ID, leaving every other field untouched.
func retarget(raw []byte, newID string) ([]byte, error) {
	fields, ok := decodeObject(raw)
	if !ok {
		// Not a JSON object: copy it through rather than dropping a record.
		return raw, nil
	}
	encoded, err := json.Marshal(newID)
	if err != nil {
		return nil, fmt.Errorf("encode session id: %w", err)
	}
	for _, key := range []string{"sessionId", "session_id"} {
		if _, ok := fields[key]; ok {
			fields[key] = encoded
		}
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("re-encode record: %w", err)
	}
	return out, nil
}

// decodeObject parses a record's fields, reporting false for anything that is
// not a JSON object.
func decodeObject(raw []byte) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	return fields, true
}

// newSessionID returns a random UUIDv4, the shape Claude Code expects.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

var _ store.Brancher = (*Source)(nil)
