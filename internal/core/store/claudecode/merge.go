package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ashes47/braids/internal/core/model"
	"github.com/Ashes47/braids/internal/core/store"
)

// PlanMerge reports what joining a branch back would carry over.
func (s *Source) PlanMerge(_ context.Context, req store.MergeRequest) (store.MergePlan, error) {
	base, incoming, err := readBoth(req)
	if err != nil {
		return store.MergePlan{}, err
	}
	shared, unique := split(base, incoming)
	return store.MergePlan{
		Shared:        len(shared),
		Incoming:      len(unique),
		BaseTurns:     countTurns(base),
		IncomingTurns: countTurns(unique),
	}, nil
}

// Merge writes a conversation holding the base in full and the branch's own
// turns after it.
//
// This is a splice of real messages, not a summary of them: the turns that
// happened on the branch are carried over as they were written. Records are
// given fresh IDs, because two conversations sharing a message ID is what
// braids reads as a fork — reusing them would leave a lane that looks like a
// branch of the thing it was merged from.
//
// Neither original is touched, and the result is a third conversation.
func (s *Source) Merge(ctx context.Context, req store.MergeRequest) (model.Lane, error) {
	base, incoming, err := readBoth(req)
	if err != nil {
		return model.Lane{}, err
	}
	_, unique := split(base, incoming)
	if len(unique) == 0 {
		return model.Lane{}, errors.New("merge: that branch has nothing the conversation does not already have")
	}

	newID, err := newSessionID()
	if err != nil {
		return model.Lane{}, err
	}
	path := filepath.Join(filepath.Dir(req.Base.Path), newID+".jsonl")
	if err := writeMerged(ctx, path, newID, req.Name, base, unique); err != nil {
		return model.Lane{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return model.Lane{}, fmt.Errorf("stat merged lane: %w", err)
	}
	return model.Lane{
		ID:      newID,
		Source:  s.Name(),
		Project: req.Base.Project,
		Path:    path,
		Title:   req.Name,
		Cwd:     req.Base.Cwd,
		Created: birthTime(info),
		Updated: info.ModTime(),
		Size:    info.Size(),
	}, nil
}

func readBoth(req store.MergeRequest) (base, incoming []line, err error) {
	if req.Base.Path == "" || req.Incoming.Path == "" {
		return nil, nil, errors.New("merge: both conversations are needed")
	}
	if req.Base.ID == req.Incoming.ID {
		return nil, nil, errors.New("merge: a conversation cannot be merged into itself")
	}
	if base, err = readLines(req.Base.Path); err != nil {
		return nil, nil, err
	}
	if incoming, err = readLines(req.Incoming.Path); err != nil {
		return nil, nil, err
	}
	return base, incoming, nil
}

// split separates the records a branch shares with its base from the ones only
// it has. Shared records are recognised by ID, which a fork copies verbatim.
func split(base, incoming []line) (shared, unique []line) {
	inBase := make(map[string]bool, len(base))
	for _, l := range base {
		if l.uuid != "" {
			inBase[l.uuid] = true
		}
	}
	for _, l := range incoming {
		switch {
		case l.uuid == "":
			continue // bookkeeping with no identity: it belongs to its own file
		case inBase[l.uuid]:
			shared = append(shared, l)
		default:
			unique = append(unique, l)
		}
	}
	return shared, unique
}

// countTurns counts the conversational records among a set, which is what a
// person means by the size of a merge.
func countTurns(lines []line) int {
	n := 0
	for _, l := range lines {
		var r record
		if json.Unmarshal(l.raw, &r) != nil {
			continue
		}
		if _, ok := r.toMessage(""); ok {
			n++
		}
	}
	return n
}

func writeMerged(ctx context.Context, path, newID, name string, base, unique []line) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".braids-merge-*")
	if err != nil {
		return fmt.Errorf("create temp lane: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // best effort once renamed

	err = func() error {
		if name != "" {
			header, err := json.Marshal(map[string]string{
				"type": "custom-title", "customTitle": name, "sessionId": newID,
			})
			if err != nil {
				return fmt.Errorf("encode title: %w", err)
			}
			if _, err := tmp.Write(append(header, '\n')); err != nil {
				return err
			}
		}

		leaf := ""
		for _, l := range base {
			if err := ctx.Err(); err != nil {
				return err
			}
			// Records with no identity are the file's own bookkeeping — its
			// title, its mode, the leaf it was left on — and belong to the file
			// rather than to the conversation. Copying them would let the
			// base's title overwrite the merged one, since a reader takes the
			// last title it sees.
			if l.uuid == "" {
				continue
			}
			rewritten, err := retarget(l.raw, newID)
			if err != nil {
				return err
			}
			if _, err := tmp.Write(append(rewritten, '\n')); err != nil {
				return err
			}
			if l.uuid != "" {
				leaf = l.uuid
			}
		}
		if leaf == "" {
			return errors.New("merge: the conversation has no records to carry on from")
		}

		// The branch's own turns are re-parented onto the end of the base, in
		// the order they happened, each with an identity of its own.
		renamed := make(map[string]string, len(unique))
		for _, l := range unique {
			if err := ctx.Err(); err != nil {
				return err
			}
			fresh, err := newSessionID()
			if err != nil {
				return err
			}
			renamed[l.uuid] = fresh

			parent := leaf
			if mapped, ok := renamed[l.parent]; ok {
				parent = mapped
			}
			rewritten, err := regraft(l.raw, newID, fresh, parent)
			if err != nil {
				return err
			}
			if _, err := tmp.Write(append(rewritten, '\n')); err != nil {
				return err
			}
			leaf = fresh
		}

		pin, err := json.Marshal(map[string]string{
			"type": "last-prompt", "leafUuid": leaf, "sessionId": newID,
		})
		if err != nil {
			return fmt.Errorf("encode leaf: %w", err)
		}
		_, err = tmp.Write(append(pin, '\n'))
		return err
	}()
	if err != nil {
		return errors.Join(fmt.Errorf("write merged lane: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close merged lane: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("place merged lane: %w", err)
	}
	return nil
}

// regraft gives a carried-over record a new identity and a new parent.
func regraft(raw []byte, sessionID, uuid, parent string) ([]byte, error) {
	fields, ok := decodeObject(raw)
	if !ok {
		return raw, nil
	}
	for key, value := range map[string]string{
		"sessionId": sessionID, "session_id": sessionID,
		"uuid": uuid, "parentUuid": parent,
	} {
		if key == "session_id" {
			if _, present := fields[key]; !present {
				continue
			}
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", key, err)
		}
		fields[key] = encoded
	}
	// A carried-over record no longer sits after whatever it followed in its
	// own file, so a stitched-over compaction link would point nowhere.
	delete(fields, "logicalParentUuid")

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("re-encode record: %w", err)
	}
	return out, nil
}

var _ store.Merger = (*Source)(nil)
