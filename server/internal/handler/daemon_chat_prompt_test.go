package handler

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func msg(role, content string) db.ChatMessage {
	return db.ChatMessage{Role: role, Content: content}
}

func contents(msgs []db.ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTrailingUserMessages pins the message-selection logic behind the daemon
// chat prompt: the agent must receive every user message since its last reply
// (the MUL-2968 debounce can land several before one run fires), not just the
// most recent one.
func TestTrailingUserMessages(t *testing.T) {
	cases := []struct {
		name string
		in   []db.ChatMessage
		want []string
	}{
		{
			name: "debounced burst with no prior reply delivers all",
			in:   []db.ChatMessage{msg("user", "看上海天气"), msg("user", "还有青岛")},
			want: []string{"看上海天气", "还有青岛"},
		},
		{
			name: "only messages after the last assistant reply",
			in: []db.ChatMessage{
				msg("user", "old q"), msg("assistant", "old a"),
				msg("user", "看上海天气"), msg("user", "还有青岛"),
			},
			want: []string{"看上海天气", "还有青岛"},
		},
		{
			name: "single new message after a reply",
			in: []db.ChatMessage{
				msg("user", "看上海天气"), msg("user", "还有青岛"),
				msg("assistant", "weather…"), msg("user", "深圳呢"),
			},
			want: []string{"深圳呢"},
		},
		{
			name: "no trailing user message (last is assistant)",
			in:   []db.ChatMessage{msg("user", "hi"), msg("assistant", "done")},
			want: []string{},
		},
		{
			name: "empty history",
			in:   []db.ChatMessage{},
			want: []string{},
		},
		{
			name: "single user message",
			in:   []db.ChatMessage{msg("user", "hi")},
			want: []string{"hi"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contents(trailingUserMessages(tc.in))
			if !eq(got, tc.want) {
				t.Fatalf("trailingUserMessages = %v, want %v", got, tc.want)
			}
		})
	}
}
