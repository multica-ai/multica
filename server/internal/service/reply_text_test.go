package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func taskMessageForReplyText(seq int32, typ, content string) db.TaskMessage {
	return db.TaskMessage{
		Seq:     seq,
		Type:    typ,
		Content: pgtype.Text{String: content, Valid: content != ""},
	}
}

func TestDeliverableReplyText(t *testing.T) {
	tests := []struct {
		name     string
		messages []db.TaskMessage
		want     string
	}{
		{name: "empty transcript"},
		{
			name: "text-only transcript is all reply",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", "Hello, "),
				taskMessageForReplyText(2, "text", "world."),
			},
			want: "Hello, world.",
		},
		{
			name: "drops interim narration and keeps final block",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", "Let me check."),
				taskMessageForReplyText(2, "tool_use", ""),
				taskMessageForReplyText(3, "tool_result", ""),
				taskMessageForReplyText(4, "text", "Now verifying."),
				taskMessageForReplyText(5, "tool_use", ""),
				taskMessageForReplyText(6, "tool_result", ""),
				taskMessageForReplyText(7, "text", "The bug is in foo()."),
			},
			want: "The bug is in foo().",
		},
		{
			name: "rejoins final text flushes",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "tool_use", ""),
				taskMessageForReplyText(2, "tool_result", ""),
				taskMessageForReplyText(3, "text", "First half, "),
				taskMessageForReplyText(4, "text", "second half."),
			},
			want: "First half, second half.",
		},
		{
			name: "redacts a credential split across text flushes",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", "ghp_ABCDEFGHIJKLMNO"),
				taskMessageForReplyText(2, "text", "PQRSTUVWXYZabcdefghij"),
			},
			want: "[REDACTED GITHUB TOKEN]",
		},
		{
			name: "unescapes the complete reply before final redaction",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", `First line.\nSecond line.`),
			},
			want: "First line.\nSecond line.",
		},
		{
			name: "thinking is process detail",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "thinking", "hmm"),
				taskMessageForReplyText(2, "text", "The answer."),
			},
			want: "The answer.",
		},
		{
			name: "tool-terminated run falls back to preface",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", "Posting the reply via CLI."),
				taskMessageForReplyText(2, "tool_use", ""),
				taskMessageForReplyText(3, "tool_result", ""),
			},
			want: "Posting the reply via CLI.",
		},
		{
			name: "tools only have no reply",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "tool_use", ""),
				taskMessageForReplyText(2, "tool_result", ""),
			},
		},
		{
			name: "whitespace final falls back to preface",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", "Preface."),
				taskMessageForReplyText(2, "tool_use", ""),
				taskMessageForReplyText(3, "text", "\n\n"),
			},
			want: "Preface.",
		},
		{
			name: "middle narration is never a reply",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "tool_use", ""),
				taskMessageForReplyText(2, "text", "middle narration"),
				taskMessageForReplyText(3, "tool_use", ""),
			},
		},
		{
			name: "whitespace preface is empty",
			messages: []db.TaskMessage{
				taskMessageForReplyText(1, "text", "   "),
				taskMessageForReplyText(2, "tool_use", ""),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deliverableReplyText(test.messages); got != test.want {
				t.Fatalf("deliverableReplyText() = %q, want %q", got, test.want)
			}
		})
	}
}
