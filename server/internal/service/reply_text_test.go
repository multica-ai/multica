package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func taskMsg(seq int32, typ, content string) db.TaskMessage {
	return db.TaskMessage{
		Seq:     seq,
		Type:    typ,
		Content: pgtype.Text{String: content, Valid: content != ""},
	}
}

func TestDeliverableReplyText(t *testing.T) {
	cases := []struct {
		name string
		msgs []db.TaskMessage
		want string
	}{
		{name: "empty transcript", msgs: nil, want: ""},
		{
			name: "no tools: whole text is the reply",
			msgs: []db.TaskMessage{
				taskMsg(1, "text", "Hello, "),
				taskMsg(2, "text", "world."),
			},
			want: "Hello, world.",
		},
		{
			name: "narration between tools is dropped, final kept",
			msgs: []db.TaskMessage{
				taskMsg(1, "text", "Let me check the code."),
				taskMsg(2, "tool_use", ""),
				taskMsg(3, "tool_result", ""),
				taskMsg(4, "text", "Found it, now verifying."),
				taskMsg(5, "tool_use", ""),
				taskMsg(6, "tool_result", ""),
				taskMsg(7, "text", "The bug is in foo()."),
			},
			want: "The bug is in foo().",
		},
		{
			name: "final split across flush batches is rejoined",
			msgs: []db.TaskMessage{
				taskMsg(1, "tool_use", ""),
				taskMsg(2, "tool_result", ""),
				taskMsg(3, "text", "First half, "),
				taskMsg(4, "text", "second half."),
			},
			want: "First half, second half.",
		},
		{
			name: "thinking counts as non-text",
			msgs: []db.TaskMessage{
				taskMsg(1, "thinking", "hmm"),
				taskMsg(2, "text", "The answer."),
			},
			want: "The answer.",
		},
		{
			name: "tool-terminated run falls back to preface",
			msgs: []db.TaskMessage{
				taskMsg(1, "text", "Posting the reply via CLI."),
				taskMsg(2, "tool_use", ""),
				taskMsg(3, "tool_result", ""),
			},
			want: "Posting the reply via CLI.",
		},
		{
			name: "tools only, no text at all",
			msgs: []db.TaskMessage{
				taskMsg(1, "tool_use", ""),
				taskMsg(2, "tool_result", ""),
			},
			want: "",
		},
		{
			name: "whitespace-only final falls back to preface",
			msgs: []db.TaskMessage{
				taskMsg(1, "text", "Preface."),
				taskMsg(2, "tool_use", ""),
				taskMsg(3, "text", "\n\n"),
			},
			want: "Preface.",
		},
		{
			name: "narration between tools is not a preface",
			msgs: []db.TaskMessage{
				taskMsg(1, "tool_use", ""),
				taskMsg(2, "text", "middle narration"),
				taskMsg(3, "tool_use", ""),
			},
			want: "",
		},
		{
			name: "whitespace-only preface yields no reply",
			msgs: []db.TaskMessage{
				taskMsg(1, "text", "   "),
				taskMsg(2, "tool_use", ""),
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliverableReplyText(tc.msgs); got != tc.want {
				t.Fatalf("deliverableReplyText() = %q, want %q", got, tc.want)
			}
		})
	}
}
