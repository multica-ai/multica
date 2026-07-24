package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// deliverableReplyText derives the user-facing reply from a task's persisted
// transcript, using the web timeline's region split (splitTimeline in
// packages/views/chat/lib/copy-text.ts): adjacent "text" rows are one logical
// block (they are split only by daemon flush timing), and the reply is the
// text block after the last non-text row — interim narration between tool
// calls is process detail, not reply. A transcript with no non-text rows is
// all reply. Unlike web Copy, which joins preface + final, delivery is
// deliberately final-only; the preface block before the first non-text row
// is used only when the run ended on a tool call and left no trailing text.
// Returns "" when the transcript yields no text at all.
func deliverableReplyText(msgs []db.TaskMessage) string {
	var preface, final strings.Builder
	sawNonText := false
	for _, m := range msgs {
		if m.Type != "text" {
			sawNonText = true
			final.Reset()
			continue
		}
		if !sawNonText {
			preface.WriteString(m.Content.String)
		}
		final.WriteString(m.Content.String)
	}
	if reply := strings.TrimSpace(final.String()); reply != "" || !sawNonText {
		return reply
	}
	return strings.TrimSpace(preface.String())
}

// replyTextForTask loads the task's persisted transcript and returns the
// deliverable reply, decoded like the stored chat_message content (literal
// `\n` sequences become real newlines). Falls back to fallback when the
// transcript is missing or yields no text — e.g. legacy tasks without task
// messages, or the daemon's final message batch losing the race with task
// completion. Transcript content is already redacted at persist time.
func (s *TaskService) replyTextForTask(ctx context.Context, taskID pgtype.UUID, fallback string) string {
	msgs, err := s.Queries.ListTaskMessages(ctx, taskID)
	if err != nil || len(msgs) == 0 {
		return fallback
	}
	reply := deliverableReplyText(msgs)
	if reply == "" {
		return fallback
	}
	return util.UnescapeBackslashEscapes(reply)
}
