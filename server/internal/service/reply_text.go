package service

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
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
	for _, msg := range msgs {
		if msg.Type != "text" {
			sawNonText = true
			final.Reset()
			continue
		}
		if !sawNonText {
			preface.WriteString(msg.Content.String)
		}
		final.WriteString(msg.Content.String)
	}
	reply := strings.TrimSpace(final.String())
	if reply == "" && sawNonText {
		reply = strings.TrimSpace(preface.String())
	}
	// Each task-message row is redacted independently when it is persisted,
	// but a credential may span two daemon flushes and become recognizable
	// only after the rows are joined. Unescape first, then redact the complete
	// delivery body so ReplyText cannot bypass the whole-output redaction
	// applied to ChatDonePayload.Content.
	return redact.Text(util.UnescapeBackslashEscapes(reply))
}

// replyTextForTask loads the persisted transcript and returns the deliverable
// reply, decoded like chat_message content. fallback is used for legacy tasks,
// a transcript with no deliverable text, or a final message batch that has not
// landed yet.
func (s *TaskService) replyTextForTask(ctx context.Context, taskID pgtype.UUID, fallback string) string {
	msgs, err := s.Queries.ListTaskMessages(ctx, taskID)
	if err != nil || len(msgs) == 0 {
		return fallback
	}
	reply := deliverableReplyText(msgs)
	if reply == "" {
		return fallback
	}
	return reply
}
