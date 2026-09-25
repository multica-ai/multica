package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxChatPinnedAgents caps the quick-agent bar so it stays compact.
const maxChatPinnedAgents = 5

// ChatPinnedAgentResponse is one entry in a user's "quick agent" bar.
type ChatPinnedAgentResponse struct {
	AgentID  string  `json:"agent_id"`
	Position float64 `json:"position"`
}

// resolveChatAgentAccess returns the set of agent ids the caller may use, or
// writes an error and returns ok=false. Mirrors ListChatSessions so pins can't
// surface or target agents the caller has lost access to.
func (h *Handler) resolveChatAgentAccess(w http.ResponseWriter, r *http.Request, userID, workspaceID string) (map[string]struct{}, bool) {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return nil, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return nil, false
	}
	return allowed, true
}

// ListChatPinnedAgents returns the caller's pinned quick-chat agents, ordered.
// Pins for agents the caller can no longer access (archived / permissions
// changed) are dropped from the response.
func (h *Handler) ListChatPinnedAgents(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	allowed, ok := h.resolveChatAgentAccess(w, r, userID, workspaceID)
	if !ok {
		return
	}

	rows, err := h.Queries.ListChatPinnedAgents(r.Context(), db.ListChatPinnedAgentsParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pinned agents")
		return
	}

	resp := make([]ChatPinnedAgentResponse, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		resp = append(resp, ChatPinnedAgentResponse{AgentID: agentID, Position: row.Position})
	}
	writeJSON(w, http.StatusOK, resp)
}

// PinChatAgent adds an agent to the caller's quick-chat bar (append at end).
// Idempotent — pinning an already-pinned agent returns 200.
func (h *Handler) PinChatAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}

	allowed, ok := h.resolveChatAgentAccess(w, r, userID, workspaceID)
	if !ok {
		return
	}
	if _, ok := allowed[req.AgentID]; !ok {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Cap the quick-agent bar at a small, curated set. Re-pinning an already
	// pinned agent stays idempotent and is allowed even at the cap.
	existing, err := h.Queries.ListChatPinnedAgents(r.Context(), db.ListChatPinnedAgentsParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pinned agents")
		return
	}
	alreadyPinned := false
	for _, e := range existing {
		if uuidToString(e.AgentID) == req.AgentID {
			alreadyPinned = true
			break
		}
	}
	if !alreadyPinned && len(existing) >= maxChatPinnedAgents {
		writeError(w, http.StatusBadRequest, "pinned agent limit reached")
		return
	}

	maxPos, err := h.Queries.GetMaxChatPinnedAgentPosition(r.Context(), db.GetMaxChatPinnedAgentPositionParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get position")
		return
	}

	row, err := h.Queries.CreateChatPinnedAgent(r.Context(), db.CreateChatPinnedAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		AgentID:     agentUUID,
		Position:    maxPos + 1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pin agent")
		return
	}
	writeJSON(w, http.StatusOK, ChatPinnedAgentResponse{
		AgentID:  uuidToString(row.AgentID),
		Position: row.Position,
	})
}

// UnpinChatAgent removes an agent from the caller's quick-chat bar.
func (h *Handler) UnpinChatAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agentId")
	if !ok {
		return
	}

	if err := h.Queries.DeleteChatPinnedAgent(r.Context(), db.DeleteChatPinnedAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		AgentID:     agentUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unpin agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
