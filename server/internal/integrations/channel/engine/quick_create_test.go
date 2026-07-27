package engine

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestQuickCreatePrompt(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "title", text: "/issue fix login", want: "fix login"},
		{name: "description", text: "/issue fix login\nsteps to reproduce", want: "fix login\nsteps to reproduce"},
		{name: "bare", text: "/issue", want: ""},
		{name: "newline boundary", text: "/issue\nsteps to reproduce", want: "steps to reproduce"},
		{name: "leading blank lines", text: "\n  \n\t/issue fix login\nsteps", want: "fix login\nsteps"},
		{name: "token boundary", text: "/issuetracker foo", want: "/issuetracker foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quickCreatePrompt(channel.InboundMessage{Text: tt.text}); got != tt.want {
				t.Fatalf("quickCreatePrompt(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestQuickCreatePromptUsesUnenrichedCommandText(t *testing.T) {
	msg := channel.InboundMessage{
		Text:        "<quoted_message>prior context</quoted_message>\n/issue fix login",
		CommandText: "/issue fix login",
	}
	if got := quickCreatePrompt(msg); got != "fix login" {
		t.Fatalf("quickCreatePrompt() = %q, want command-only prompt", got)
	}
}
