//go:build !windows

package launch

import "context"

// shellTemplates says whether BRAIDS_SPAWN can be honoured here.
//
// A template is only useful if it can use shell syntax, so braids runs it
// through one and shell-quotes every value it substitutes. Those quoting rules
// are POSIX rules. cmd.exe and PowerShell each have their own, and getting
// them subtly wrong is how a conversation titled `x; rm -rf ~ #` becomes a
// command rather than a name, which is a bug this code has already had once.
// So on Windows braids does not offer the template at all and copies the
// command instead, which is what it already does for a terminal it cannot
// drive.
const shellTemplates = true

func runTemplate(ctx context.Context, script string) error {
	return run(ctx, "sh", "-c", script)
}
