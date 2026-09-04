//go:build windows

package launch

import (
	"context"
	"errors"
)

// shellTemplates is false on Windows: see the note in shell_unix.go. braids
// copies the resume command instead of guessing at cmd.exe quoting rules it
// cannot test.
const shellTemplates = false

func runTemplate(context.Context, string) error {
	return errors.New("BRAIDS_SPAWN runs through a POSIX shell, which braids does not use on Windows")
}
