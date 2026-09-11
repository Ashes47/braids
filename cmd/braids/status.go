package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/Ashes47/braids/internal/core/hooks"
	"github.com/Ashes47/braids/internal/tui"
)

// status is braids in one line, for a status line.
//
// The map's header carries six facts and the only one worth a permanent place
// on somebody's screen is how many conversations are owed a reply. It is the
// one thing braids knows that nothing else on the machine does: a session that
// asked a question and stopped looks exactly like a session that finished,
// unless something was watching the hooks.
//
// It prints nothing when the answer is none. A status line that always says
// something is a status line people stop reading, and zero is the answer most
// of the time for most people.
//
// Speed is the constraint nothing else here has: a status line runs on every
// render, so this opens the index, counts, and gets out.

// statusWindow is how far back a status line looks. A day covers the thread
// somebody put down before lunch and leaves out the one they abandoned in
// August, which is the difference between a number worth glancing at and one
// that never changes.
const statusWindow = 24 * time.Hour

func cmdStatus(args []string, out *printer) error {
	fs := newFlagSet("status")
	db := fs.String("db", "", "index location")
	always := fs.Bool("always", false, "print a line even when nothing is waiting")
	within := fs.Duration("within", statusWindow,
		"only conversations touched this recently, 0 for all of them")
	asJSON := jsonFlag(fs)
	if err := parse(fs, args, out); err != nil {
		return err
	}

	path, err := resolveDB(*db)
	if err != nil {
		return err
	}
	ix, err := openExisting(path)
	if err != nil {
		// A status line must never be the thing that breaks. No index is not
		// an error here, it is nothing to say.
		if *asJSON {
			return out.emit(struct {
				Waiting int    `json:"waiting"`
				Ready   bool   `json:"ready"`
				Reason  string `json:"reason,omitempty"`
			}{0, false, err.Error()})
		}
		return nil
	}
	defer ix.Close() //nolint:errcheck // read-only

	ctx := context.Background()
	lanes, err := ix.Lanes(ctx)
	if err != nil {
		return err
	}
	// Waiting states are only meaningful when the hook is reporting. Without
	// it every stopped session looks the same, so the count would be a guess
	// dressed as a number.
	events, err := hooks.Read(filepath.Join(filepath.Dir(path), "events.jsonl"))
	if err != nil {
		events = nil
	}
	n := tui.Waiting(lanes, hooks.Latest(events), time.Now(), *within)

	if *asJSON {
		return out.emit(struct {
			Waiting       int  `json:"waiting"`
			Ready         bool `json:"ready"`
			Conversations int  `json:"conversations"`
		}{n, true, len(lanes)})
	}
	if n == 0 && !*always {
		return nil
	}
	// "waiting" is describing the conversations, not naming them, so it does
	// not take an s however many there are.
	out.printf("braids %d waiting\n", n)
	return out.Err()
}
