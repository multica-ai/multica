// CEREBRO-PATCH(chat-agent-reply): TECH-3183 — agent-initiated assistant chat
// message with attachment support. Allows an agent to post a reply to a chat
// session where it is the assigned agent, without needing an active chat task.
// Lives in a cerebro-prefixed file so the upstream chat handler stays untouched.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type sendAgentChatMessageRequest struct {
	Content string `json:"content"`
}

type sendAgentChatMessageResponse struct {
	MessageID string `json:"message_id"`
	CreatedAt string `json:"created_at"`
}

// SendAgentChatMessage creates an assistant message in the given chat session on
// behalf of the calling agent. The caller must be the agent assigned to the
// session — any other principal (member, different agent) gets 403.
func (h *Handler) SendAgentChatMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only agents can send direct chat replies")
		return
	}

	var req sendAgentChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	sessionUUID, ok := parseUUIDOrBadRequest(w, sessionID, "chat session id")
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          sessionUUID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}

	// Only the agent assigned to this session may post replies.
	if uuidToString(session.AgentID) != actorID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return
	}

	msg, err := h.Queries.CreateChatMessage(r.Context(), db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "assistant",
		Content:       req.Content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chat message")
		return
	}

	if err := h.Queries.TouchChatSession(r.Context(), session.ID); err != nil {
		slog.Warn("failed to touch chat session", "session_id", sessionID, "error", err)
	}

	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatMessage, workspaceID, "agent", actorID, resolvedSessionID, protocol.ChatMessagePayload{
		ChatSessionID: resolvedSessionID,
		MessageID:     uuidToString(msg.ID),
		Role:          "assistant",
		Content:       req.Content,
		CreatedAt:     timestampToString(msg.CreatedAt),
	})

	writeJSON(w, http.StatusCreated, sendAgentChatMessageResponse{
		MessageID: uuidToString(msg.ID),
		CreatedAt: timestampToString(msg.CreatedAt),
	})
}
