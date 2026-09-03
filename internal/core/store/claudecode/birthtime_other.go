//go:build !darwin

package claudecode

import (
	"os"
	"time"
)

// birthTime returns the zero time on platforms that do not report file
// creation, leaving the graph to fall back on weaker evidence.
func birthTime(os.FileInfo) time.Time { return time.Time{} }
