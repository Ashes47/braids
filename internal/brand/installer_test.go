package brand

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The installer prints the same mark braids does, and carries its own copy
// because it is a shell script that runs before there is a braids to ask.
// Two copies of anything drift: this one did, silently, the moment the letters
// were respaced.
func TestTheInstallerPrintsTheSameMark(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "site", "install.sh"))
	if err != nil {
		t.Fatalf("read the installer: %v", err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")

	// The heredoc the installer draws it from.
	_, rest, ok := strings.Cut(script, "cat <<'ART'\n")
	if !ok {
		t.Fatal("the installer no longer draws the mark from a heredoc named ART")
	}
	drawn, _, ok := strings.Cut(rest, "\nART\n")
	if !ok {
		t.Fatal("the installer's mark heredoc is not closed")
	}

	// Written in a shell heredoc, so every backslash is doubled.
	drawn = strings.ReplaceAll(drawn, `\\`, `\`)
	for i, line := range strings.Split(drawn, "\n") {
		if i >= len(Full()) {
			t.Fatalf("the installer draws %d lines, the mark has %d",
				i+1, len(Full()))
		}
		if want := strings.TrimRight(Full()[i], " "); line != want {
			t.Errorf("installer line %d is\n  %q\nand the mark is\n  %q\n"+
				"(run: python3 - <<'EOF' to regenerate, or copy `braids help`)",
				i+1, line, want)
		}
	}
}
