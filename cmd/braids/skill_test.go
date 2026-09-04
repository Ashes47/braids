package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The skill tells Claude which commands braids has and which flags they take.
// A skill is copied into somebody's ~/.claude/skills, where nothing here can
// fix it, so it must not describe a surface that does not exist. This reads it
// and checks every command and flag against the program itself.
func TestSkillOnlyTeachesCommandsThatExist(t *testing.T) {
	path := filepath.Join("..", "..", "skills", "braids", "SKILL.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the skill: %v", err)
	}
	// Checked out on Windows a text file arrives with CRLF line endings, so
	// anything matching on \n finds nothing. .gitattributes asks git not to do
	// that; this does not depend on it.
	text := strings.ReplaceAll(string(body), "\r\n", "\n")

	commands := map[string]bool{}
	for _, name := range known {
		commands[name] = true
	}

	// Only what is inside a shell block: the prose says "braids indexes every
	// conversation", which is a sentence rather than an invocation.
	var shell strings.Builder
	for _, block := range regexp.MustCompile("(?s)```sh\n(.*?)```").FindAllStringSubmatch(text, -1) {
		shell.WriteString(block[1])
	}
	if shell.Len() == 0 {
		t.Fatal("the skill has no shell blocks, so it teaches no commands")
	}

	invocation := regexp.MustCompile(`(?m)^braids ([a-z]+)([^\n]*)`)
	seen := 0
	for _, m := range invocation.FindAllStringSubmatch(shell.String(), -1) {
		command, rest := m[1], m[2]
		seen++
		if !commands[command] {
			t.Errorf("the skill uses `braids %s`, which is not a command", command)
			continue
		}
		for _, flag := range regexp.MustCompile(`--[a-z-]+`).FindAllString(rest, -1) {
			if !commandTakes(t, command, flag) {
				t.Errorf("the skill passes %s to `braids %s`, which does not take it",
					flag, command)
			}
		}
	}
	if seen < 8 {
		t.Errorf("only %d invocations found; the regexp is probably not matching", seen)
	}
}

// commandTakes asks the program itself, so this cannot drift from the flags a
// command actually has.
func commandTakes(t *testing.T, command, flag string) bool {
	t.Helper()
	if flag == "--json" || flag == "--help" {
		return true
	}
	var buf bytes.Buffer
	// --help writes the flag list and returns a sentinel error, which is the
	// same path a mistyped flag takes.
	_ = run([]string{command, "--help"}, &buf)
	return strings.Contains(buf.String(), flag+" ")
}

// And the reverse: a command worth teaching should be taught. This is a
// reminder rather than a rule, so it names what is missing without failing.
func TestSkillMentionsTheCommandsWorthTeaching(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "skills", "braids", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, command := range []string{"search", "branch", "explain", "lanes", "memories", "work"} {
		if !strings.Contains(text, "braids "+command) {
			t.Errorf("the skill never mentions `braids %s`", command)
		}
	}
}
