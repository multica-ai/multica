package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Agent conversation monitoring.
//
// Every chat session is private between its creator and the agent. These two
// endpoints are the deliberate exception: the agent's owner (or a workspace
// owner/admin) may review every member's conversation with that agent. The
// ordinary /api/chat/sessions routes stay creator-only — see
// gateChatSessionForUser in chat.go — so the privilege lives here rather than
// by widening those gates.

// authorizeAgentConversations resolves the {id} agent and admits only the
// agent owner or a workspace owner/admin. It also rejects agent actors before
// anything else: an agent process runs with its owner's identity stamped
// (X-Actor-Source: task_token), so without this guard the agent itself would
// inherit the owner's monitoring privilege and could read every member's
// conversation. Mirrors authorizeAgentEnv (agent_env.go).
func (h *Handler) authorizeAgentConversations(w http.ResponseWriter, r *http.Request) (db.Agent, bool) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access conversation monitoring endpoints")
		return db.Agent{}, false
	}
	if !h.canManageAgent(w, r, agent) {
		return db.Agent{}, false
	}
	return agent, true
}

// ListAgentChatSessions returns every member's chat sessions with one agent.
// ?status=all includes archived sessions; the default is active only.
func (h *Handler) ListAgentChatSessions(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.authorizeAgentConversations(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListChatSessionsByAgent(r.Context(), db.ListChatSessionsByAgentParams{
		WorkspaceID:     agent.WorkspaceID,
		AgentID:         agent.ID,
		IncludeArchived: r.URL.Query().Get("status") == "all",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
		return
	}

	resp := make([]ChatSessionResponse, 0, len(rows))
	for _, s := range rows {
		resp = append(resp, ChatSessionResponse{
			ID:          uuidToString(s.ID),
			WorkspaceID: uuidToString(s.WorkspaceID),
			AgentID:     uuidToString(s.AgentID),
			CreatorID:   uuidToString(s.CreatorID),
			ProjectID:   uuidToPtr(s.ProjectID),
			Title:       s.Title,
			Status:      s.Status,
			HasUnread:   s.UnreadCount > 0,
			UnreadCount: int(s.UnreadCount),
			LastMessage: buildChatLastMessage(s.LastMessageAt, s.LastMessageContent, s.LastMessageRole, s.LastMessageFailureReason, s.LastMessageKind),
			Pinned:      s.PinnedAt.Valid,
			CreatedAt:   timestampToString(s.CreatedAt),
			UpdatedAt:   timestampToString(s.UpdatedAt),
		})
	}
	if err := h.hydrateChatSessionChannelMetadata(r.Context(), resp); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat channel metadata")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListAgentChatSessionMessages returns one page of a monitored conversation's
// transcript. The session must belong to the agent named in the URL: the
// management privilege is per-agent, so it must not become a key to every
// session in the workspace. Same page shape and paging contract as
// GET /api/chat/sessions/{sessionId}/messages/page.
func (h *Handler) ListAgentChatSessionMessages(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.authorizeAgentConversations(w, r)
	if !ok {
		return
	}

	sessionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "chat session id")
	if !ok {
		return
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          sessionUUID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if session.AgentID != agent.ID {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}

	h.respondChatMessagesPage(w, r, session)
}
