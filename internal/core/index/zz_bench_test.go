package index

import (
	"context"
	"testing"
	"time"

	"github.com/Ashes47/braids/internal/core/store/claudecode"
)

// What a sync costs when nothing has changed, against the real history.
func TestBenchNoopSync(t *testing.T) {
	root, err := claudecode.DefaultRoot()
	if err != nil {
		t.Skip(err)
	}
	ix, err := Open("/Users/averma/.braids/index.db")
	if err != nil {
		t.Skip(err)
	}
	defer ix.Close() //nolint:errcheck
	ctx := context.Background()
	src := claudecode.New(root)

	// Warm: bring it fully up to date first.
	if _, err := ix.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		start := time.Now()
		st, err := ix.Sync(ctx, src)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("  no-op sync %d: %v   (%d lanes, %d messages)",
			i+1, time.Since(start).Round(time.Millisecond), st.Lanes, st.Messages)
	}
}
