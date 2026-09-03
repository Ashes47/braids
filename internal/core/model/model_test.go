package model

import "testing"

func msg() Message {
	return Message{Parts: []Part{
		{Kind: PartThinking, Text: "weighing it up"},
		{Kind: PartToolUse, Tool: "Bash", Text: `{"command":"ls"}`},
		{Kind: PartText, Text: "here is the answer"},
		{Kind: PartText, Text: ""}, // empty parts must not add blank lines
	}}
}

func TestMessageText(t *testing.T) {
	tests := []struct {
		name  string
		kinds []PartKind
		want  string
	}{
		{"no filter returns every part", nil, "weighing it up\n{\"command\":\"ls\"}\nhere is the answer"},
		{"single kind", []PartKind{PartText}, "here is the answer"},
		{"multiple kinds keep part order", []PartKind{PartText, PartThinking}, "weighing it up\nhere is the answer"},
		{"kind that is absent", []PartKind{PartToolResult}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := msg().Text(tt.kinds...); got != tt.want {
				t.Errorf("Text(%v) = %q, want %q", tt.kinds, got, tt.want)
			}
		})
	}
}

func TestMessageTextOnEmptyMessage(t *testing.T) {
	if got := (Message{}).Text(); got != "" {
		t.Errorf("Text() on empty message = %q", got)
	}
}
