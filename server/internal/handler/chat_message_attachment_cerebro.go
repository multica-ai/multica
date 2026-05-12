// CEREBRO-PATCH(chat-message-id-claim): JEH-1083 cerebro-only helper that
// pre-creates the assistant chat_message row at chat-task claim time so the
// MCP add_attachment tool has a target UUID it can return via
// MULTICA_CHAT_MESSAGE_ID. Lives in a cerebro-prefixed file so the upstream
// chat handler stays untouched; the complete/cancel/fail paths upsert this
// same row by task_id, keeping exactly one assistant row per task.
package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// ensureAssistantChatMessageForTask returns the UUID string of the assistant
// chat_message row that belongs to this chat task, creating an empty stub if
// none exists yet. Idempotent: subsequent claims (e.g. retries) reuse the
// existing row. Errors are logged and swallowed — the agent simply runs
// without an attachment target rather than failing the claim.
func (h *Handler) ensureAssistantChatMessageForTask(ctx context.Context, taskID pgtype.UUID, chatSessionID pgtype.UUID) string {
	if !taskID.Valid || !chatSessionID.Valid {
		return ""
	}
	if existing, err := h.Queries.GetAssistantChatMessageByTaskID(ctx, taskID); err == nil {
		return util.UUIDToString(existing.ID)
	}
	created, err := h.Queries.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: chatSessionID,
		Role:          "assistant",
		Content:       "",
		TaskID:        taskID,
	})
	if err != nil {
		slog.Warn("ensure assistant chat_message: create failed",
			"task_id", util.UUIDToString(taskID),
			"chat_session_id", util.UUIDToString(chatSessionID),
			"error", err)
		return ""
	}
	return util.UUIDToString(created.ID)
}
