package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestDetectPrefersAnExplicitTemplate(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"explicit template wins", map[string]string{
			"BRAIDS_SPAWN": "echo {cmd}", "TMUX": "/tmp/x", "TERM_PROGRAM": "iTerm.app",
		}, "BRAIDS_SPAWN"},
		{"tmux next", map[string]string{"TMUX": "/tmp/x", "TERM_PROGRAM": "iTerm.app"}, "tmux"},
		{"iterm next", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iTerm2"},
		{"a terminal that cannot be driven", map[string]string{"TERM_PROGRAM": "WarpTerminal"}, ""},
		{"whitespace is not a template", map[string]string{"BRAIDS_SPAWN": "   "}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Where a template cannot be run through a shell braids does not
			// offer one, so the next launcher wins instead.
			if !shellTemplates && tt.want == "BRAIDS_SPAWN" {
				t.Skip("BRAIDS_SPAWN needs a POSIX shell, which braids does not use here")
			}
			open, name := Detect(envOf(tt.env))
			if name != tt.want {
				t.Errorf("terminal = %q, want %q", name, tt.want)
			}
			if (open == nil) != (tt.want == "") {
				t.Errorf("launcher presence does not match %q", tt.want)
			}
		})
	}
}

func TestTemplateExpansion(t *testing.T) {
	if !shellTemplates {
		t.Skip("BRAIDS_SPAWN needs a POSIX shell, which braids does not use here")
	}
	open, _ := Detect(envOf(map[string]string{
		"BRAIDS_SPAWN": "sh -c 'test {name} = mine && test {dir} = /tmp && echo {cmd}'",
	}))
	if err := open(context.Background(), "/tmp", "mine", "true"); err != nil {
		t.Errorf("template did not expand as expected: %v", err)
	}
}

func TestRunReportsWhatWentWrong(t *testing.T) {
	open, _ := Detect(envOf(map[string]string{"BRAIDS_SPAWN": "echo nope >&2; exit 3"}))
	err := open(context.Background(), "", "", "")
	if err == nil {
		t.Fatal("a launcher that fails must report it, not claim success")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v, want it to carry the launcher's own message", err)
	}
}

// itermBundleID is what AppleScript resolves `tell application "iTerm2"` to.
const itermBundleID = "com.googlecode.iterm2"

func TestQuotingCannotBreakTheScript(t *testing.T) {
	// A resume command carries a quoted title; a directory can contain one too.
	script := itermScript(`/tmp/it's here`, `claude --resume abc --name "say \"hi\""`)
	if strings.Contains(script, "\n\"") {
		t.Error("an unescaped quote closed the AppleScript literal early")
	}
	// Shell-quoted first, then escaped for AppleScript: the shell's `'\''`
	// reaches the script as `'\\''` and unescapes back on the way out.
	if !strings.Contains(script, `'\\''`) {
		t.Errorf("directory quoting did not survive both layers:\n%s", script)
	}

	if runtime.GOOS != "darwin" {
		t.Skip("AppleScript is macOS only")
	}
	if _, err := exec.LookPath("osacompile"); err != nil {
		t.Skip("osacompile unavailable")
	}
	// Compiling resolves iTerm2's own terminology — `tab`, `session`,
	// `default profile` are its words, not AppleScript's. Without the app
	// installed they are unresolvable class names and every script fails
	// alike, which tests the runner rather than the quoting.
	// Bounded: a lookup that hangs on a headless runner would be worse than
	// one that fails.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "osascript", "-e",
		`path to application id "`+itermBundleID+`"`).Run(); err != nil {
		t.Skip("iTerm2 not installed: its terminology cannot be resolved")
	}
	// Compile without running: proves the script is valid, opens nothing.
	cmd := exec.Command("osacompile", "-o", t.TempDir()+"/out.scpt", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("generated AppleScript does not compile: %v\n%s", err, out)
	}
}

func TestAppleScriptQuote(t *testing.T) {
	if got := appleScriptQuote(`a "b" c\d`); got != `a \"b\" c\\d` {
		t.Errorf("appleScriptQuote = %q", got)
	}
}

// A conversation's title and directory are data read out of a transcript. They
// reach a template through `sh -c`, so they are quoted before they get there:
// a title of `x; touch pwned #` is a name, never a command.
func TestTemplateValuesCannotBecomeCommands(t *testing.T) {
	for _, tc := range []struct{ name, template string }{
		{"name", "true {name}"},
		{"dir", "true {dir}"},
		{"cmd", "true {cmd}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mark := filepath.Join(t.TempDir(), "pwned")
			hostile := "x; touch " + mark + " #"
			launcher, _ := Detect(func(k string) string {
				if k == "BRAIDS_SPAWN" {
					return tc.template
				}
				return ""
			})
			if launcher == nil {
				t.Fatal("no launcher for BRAIDS_SPAWN")
			}
			// Whichever placeholder is under test carries the hostile string.
			_ = launcher(context.Background(), hostile, hostile, hostile)
			if _, err := os.Stat(mark); err == nil {
				t.Errorf("%s reached the shell unquoted: it ran a command", tc.name)
			}
		})
	}
}
