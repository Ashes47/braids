// Package launch opens a terminal already running a command.
//
// Terminals differ in whether they can be driven at all, so braids detects the
// ones it can and hands the command over otherwise. Nothing here goes through a
// shell unless the user asked for one: a resume command carries a quoted title,
// and every layer of quoting is a way to get it wrong.
package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// timeout bounds a launcher. tmux and osascript return as soon as the window
// exists; a custom template that keeps running is reported as launched.
const timeout = 5 * time.Second

// Launcher opens a terminal in dir, named name, running command.
type Launcher func(ctx context.Context, dir, name, command string) error

// Detect returns a launcher for the current terminal, and the name it goes by.
// A nil Launcher means this terminal cannot be told to run a command, which is
// a fact about the terminal rather than a failure.
func Detect(env func(string) string) (Launcher, string) {
	if template := strings.TrimSpace(env("BRAIDS_SPAWN")); template != "" {
		return fromTemplate(template), "BRAIDS_SPAWN"
	}
	if env("TMUX") != "" {
		return tmux, "tmux"
	}
	if env("TERM_PROGRAM") == "iTerm.app" {
		return iterm, "iTerm2"
	}
	return nil, ""
}

// tmux opens a new window. Arguments are passed as argv, so a title containing
// spaces or quotes cannot break the command.
func tmux(ctx context.Context, dir, name, command string) error {
	args := []string{"new-window"}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	if name != "" {
		args = append(args, "-n", name)
	}
	return run(ctx, "tmux", append(args, command)...)
}

// iterm opens a tab through AppleScript. The script is built in Go and passed
// as one argument, so only AppleScript's own escaping applies — there is no
// shell in the way.
func iterm(ctx context.Context, dir, _, command string) error {
	return run(ctx, "osascript", "-e", itermScript(dir, command))
}

// itermScript builds the AppleScript that opens a tab and runs the command.
func itermScript(dir, command string) string {
	line := command
	if dir != "" {
		line = "cd " + shellQuote(dir) + " && " + command
	}
	return `tell application "iTerm2"
	activate
	if (count of windows) = 0 then
		set targetWindow to (create window with default profile)
	else
		set targetWindow to current window
	end if
	tell targetWindow
		set newTab to (create tab with default profile)
		tell current session of newTab
			write text "` + appleScriptQuote(line) + `"
		end tell
	end tell
end tell`
}

// fromTemplate runs a user-supplied command through a shell, because a template
// is only useful if it can use shell syntax.
func fromTemplate(template string) Launcher {
	return func(ctx context.Context, dir, name, command string) error {
		expanded := strings.NewReplacer(
			"{cmd}", command,
			"{name}", name,
			"{dir}", dir,
		).Replace(template)
		return run(ctx, "sh", "-c", expanded)
	}
}

// run executes a launcher and reports what went wrong, rather than reporting
// success because the process started.
func run(parent context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		// Still running after the timeout: it is a foreground launcher, which
		// means it worked.
		return nil
	case err != nil:
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return fmt.Errorf("%s: %s", name, firstLine(detail))
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// appleScriptQuote escapes a string for an AppleScript literal.
func appleScriptQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// shellQuote wraps a value for the shell that iTerm's session will run.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Env is the default environment lookup.
func Env(key string) string { return os.Getenv(key) }
