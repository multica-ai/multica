// CEREBRO-PATCH(agent-pause-cerebro): FIR-4508 — cerebro-only HTTP handlers for
// agent-scoped pause/unpause on multi-provider runtimes (Hermes).
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// pauseAgentRequest is the JSON body for POST /api/agents/{id}/pause.
type pauseAgentRequest struct {
	UnpauseAt string `json:"unpause_at"` // RFC3339; empty = no auto-unpause
	Reason    string `json:"reason"`     // free-form slug; defaults to "manual"
}

// PauseAgent puts one agent to sleep. Owner/admin or agent owner only.
// Sibling agents on the same multi-provider runtime stay online.
func (h *Handler) PauseAgent(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgentForPauseMutation(w, r)
	if !ok {
		return
	}

	var req pauseAgentRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	opts := AgentPauseOptions{Reason: req.Reason}
	if req.UnpauseAt != "" {
		t, err := time.Parse(time.RFC3339, req.UnpauseAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "unpause_at must be RFC3339 (e.g. 2026-05-04T18:50:00Z)")
			return
		}
		if t.Before(time.Now().Add(-1 * time.Minute)) {
			writeError(w, http.StatusBadRequest, "unpause_at must be in the future")
			return
		}
		opts.UnpauseAt = t
	}
	if opts.Reason == "" {
		opts.Reason = "manual"
	}

	if h.AgentPause == nil {
		writeError(w, http.StatusServiceUnavailable, "agent pause service not available")
		return
	}
	if _, err := h.AgentPause.PauseAgent(r.Context(), agent.ID, opts); err != nil {
		slog.Warn("pause agent failed", "agent_id", uuidToString(agent.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to pause agent")
		return
	}

	updated, err := h.Queries.GetAgent(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refetch agent")
		return
	}

	slog.Info("agent paused",
		"agent_id", uuidToString(agent.ID),
		"reason", opts.Reason,
		"unpause_at", req.UnpauseAt,
		"actor", uuidToString(member.UserID),
	)

	writeJSON(w, http.StatusOK, agentToResponse(updated))
}

// UnpauseAgent wakes a paused agent and resumes suspended work.
func (h *Handler) UnpauseAgent(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgentForPauseMutation(w, r)
	if !ok {
		return
	}

	if h.AgentPause == nil {
		writeError(w, http.StatusServiceUnavailable, "agent pause service not available")
		return
	}
	if _, err := h.AgentPause.UnpauseAgent(r.Context(), agent.ID); err != nil {
		slog.Warn("unpause agent failed", "agent_id", uuidToString(agent.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unpause agent")
		return
	}

	updated, err := h.Queries.GetAgent(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refetch agent")
		return
	}

	slog.Info("agent unpaused",
		"agent_id", uuidToString(agent.ID),
		"actor", uuidToString(member.UserID),
	)

	writeJSON(w, http.StatusOK, agentToResponse(updated))
}

func (h *Handler) loadAgentForPauseMutation(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool) {
	agentID := chi.URLParam(r, "id")
	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "id")
	if !ok {
		return db.Agent{}, db.Member{}, false
	}

	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.Agent{}, db.Member{}, false
	}

	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "agent not found")
	if !ok {
		return db.Agent{}, db.Member{}, false
	}

	userID := uuidToString(member.UserID)
	isAdmin := roleAllowed(member.Role, "owner", "admin")
	isOwner := agent.OwnerID.Valid && uuidToString(agent.OwnerID) == userID
	if !isAdmin && !isOwner {
		writeError(w, http.StatusForbidden, "you can only manage your own agents")
		return db.Agent{}, db.Member{}, false
	}

	return agent, member, true
}
