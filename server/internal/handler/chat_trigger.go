package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChatTriggerResponse is the sealed input and reply anchor of the authenticated task.
// ChannelID is resolved for Slack and opaque for other platforms.
// An empty ThreadID lets callers choose a root reply. Native input has no channel fields.
type ChatTriggerResponse struct {
	TaskID           string `json:"task_id"`
	Input            string `json:"input"`
	ChannelType      string `json:"channel_type"`
	ChannelID        string `json:"channel_id"`
	TriggerMessageID string `json:"trigger_message_id"`
	ThreadID         string `json:"thread_id"`
}

func (h *Handler) GetChatTrigger(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.chatHistorySession(w, r)
	if !ok {
		return
	}
	if ctxWorkspaceID(r.Context()) == "" {
		writeError(w, http.StatusForbidden, "missing workspace context")
		return
	}
	task := scope.task
	if !task.ChatInputTaskID.Valid {
		writeError(w, http.StatusConflict, "task has no sealed chat input")
		return
	}
	messages, err := h.Queries.ListChatInputMessages(r.Context(), task.ChatInputTaskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read sealed chat input")
		return
	}
	for _, m := range messages {
		if m.ChatSessionID != scope.sessionID {
			writeError(w, http.StatusConflict, "sealed input does not belong to this chat")
			return
		}
	}
	input := sealedChatInput(messages)
	if strings.TrimSpace(input) == "" {
		writeError(w, http.StatusConflict, "task has no sealed chat input")
		return
	}
	ingested, err := engine.TaskInputIsChannelIngested(r.Context(), h.Queries, task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read chat input provenance")
		return
	}
	delivery, err := h.Queries.GetChannelTaskDelivery(r.Context(), task.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		if !ingested {
			writeJSON(w, http.StatusOK, ChatTriggerResponse{TaskID: uuidToString(task.ID), Input: input})
			return
		}
		writeError(w, http.StatusConflict, "task has no channel delivery snapshot")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read channel delivery snapshot")
		return
	}
	if !ingested {
		writeError(w, http.StatusConflict, "task input did not originate from a channel")
		return
	}
	channelID, threadID := delivery.ChannelChatID, delivery.ChannelThreadID.String
	if delivery.ChannelType == string(slack.TypeSlack) {
		channelID, threadID = slack.TaskDeliveryTarget(delivery)
	}
	messageID := delivery.ChannelMessageID.String
	if strings.TrimSpace(delivery.ChannelType) == "" || strings.TrimSpace(channelID) == "" || strings.TrimSpace(messageID) == "" {
		writeError(w, http.StatusConflict, "task has incomplete channel delivery metadata")
		return
	}
	writeJSON(w, http.StatusOK, ChatTriggerResponse{TaskID: uuidToString(task.ID), Input: input, ChannelType: delivery.ChannelType, ChannelID: channelID, TriggerMessageID: messageID, ThreadID: threadID})
}

func sealedChatInput(messages []db.ChatMessage) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}
