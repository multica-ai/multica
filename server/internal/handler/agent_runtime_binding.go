package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AgentRuntimeBindingResponse struct {
	AgentID            string  `json:"agent_id"`
	UserID             string  `json:"user_id"`
	RuntimeID          *string `json:"runtime_id"`
	EffectiveRuntimeID string  `json:"effective_runtime_id"`
	Bound              bool    `json:"bound"`
	CreatedAt          *string `json:"created_at"`
	UpdatedAt          *string `json:"updated_at"`
}

type UpsertAgentRuntimeBindingRequest struct {
	RuntimeID string `json:"runtime_id"`
}

func (h *Handler) GetMyAgentRuntimeBinding(w http.ResponseWriter, r *http.Request) {
	agent, member, userID, ok := h.loadRuntimeBindingAgent(w, r)
	if !ok {
		return
	}

	binding, err := h.Queries.GetAgentRuntimeBinding(r.Context(), db.GetAgentRuntimeBindingParams{
		AgentID: agent.ID,
		UserID:  userID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusOK, agentRuntimeBindingToResponse(agent, member.UserID, nil))
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load runtime binding")
		return
	}

	writeJSON(w, http.StatusOK, agentRuntimeBindingToResponse(agent, member.UserID, &binding))
}

func (h *Handler) UpsertMyAgentRuntimeBinding(w http.ResponseWriter, r *http.Request) {
	agent, member, userID, ok := h.loadRuntimeBindingAgent(w, r)
	if !ok {
		return
	}

	var req UpsertAgentRuntimeBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can bind agent runs to it")
		return
	}

	binding, err := h.Queries.UpsertAgentRuntimeBinding(r.Context(), db.UpsertAgentRuntimeBindingParams{
		AgentID:   agent.ID,
		UserID:    userID,
		RuntimeID: runtime.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update runtime binding")
		return
	}

	writeJSON(w, http.StatusOK, agentRuntimeBindingToResponse(agent, member.UserID, &binding))
}

func (h *Handler) DeleteMyAgentRuntimeBinding(w http.ResponseWriter, r *http.Request) {
	agent, member, userID, ok := h.loadRuntimeBindingAgent(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteAgentRuntimeBinding(r.Context(), db.DeleteAgentRuntimeBindingParams{
		AgentID: agent.ID,
		UserID:  userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear runtime binding")
		return
	}

	writeJSON(w, http.StatusOK, agentRuntimeBindingToResponse(agent, member.UserID, nil))
}

func (h *Handler) loadRuntimeBindingAgent(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, pgtype.UUID, bool) {
	userIDString, ok := requireUserID(w, r)
	if !ok {
		return db.Agent{}, db.Member{}, pgtype.UUID{}, false
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, db.Member{}, pgtype.UUID{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Agent{}, db.Member{}, pgtype.UUID{}, false
	}
	actorType, actorID := h.resolveActor(r, userIDString, workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return db.Agent{}, db.Member{}, pgtype.UUID{}, false
	}
	return agent, member, parseUUID(userIDString), true
}

func agentRuntimeBindingToResponse(agent db.Agent, userID pgtype.UUID, binding *db.AgentRuntimeBinding) AgentRuntimeBindingResponse {
	effectiveRuntimeID := uuidToString(agent.RuntimeID)
	resp := AgentRuntimeBindingResponse{
		AgentID:            uuidToString(agent.ID),
		UserID:             uuidToString(userID),
		EffectiveRuntimeID: effectiveRuntimeID,
		Bound:              false,
	}
	if binding == nil {
		return resp
	}

	runtimeID := uuidToString(binding.RuntimeID)
	resp.RuntimeID = &runtimeID
	resp.EffectiveRuntimeID = runtimeID
	resp.Bound = true
	createdAt := timestampToString(binding.CreatedAt)
	updatedAt := timestampToString(binding.UpdatedAt)
	resp.CreatedAt = &createdAt
	resp.UpdatedAt = &updatedAt
	return resp
}
