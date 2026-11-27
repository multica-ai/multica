package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) GetAgentFullDelegation(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.agentForWorkspaceGrants(w, r)
	if !ok {
		return
	}
	grant, err := h.Queries.GetAgentFullDelegation(r.Context(), agent.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to read full delegation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": err == nil && grant.GrantorUserID == agent.OwnerID, "grantor_user_id": uuidToString(agent.OwnerID)})
}

func (h *Handler) SetAgentFullDelegation(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.agentForWorkspaceGrants(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	if *body.Enabled {
		if err := h.Queries.EnableAgentFullDelegation(r.Context(), db.EnableAgentFullDelegationParams{AgentID: agent.ID, GrantorUserID: agent.OwnerID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to enable full delegation")
			return
		}
	} else {
		if err := h.Queries.RevokeAgentFullDelegation(r.Context(), db.RevokeAgentFullDelegationParams{AgentID: agent.ID, GrantorUserID: agent.OwnerID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke full delegation")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": *body.Enabled, "grantor_user_id": uuidToString(agent.OwnerID)})
}

func (h *Handler) ListMyAgentFullDelegations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListUserFullDelegations(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list full delegations")
		return
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"agent_id": uuidToString(row.AgentID), "agent_name": row.AgentName, "workspace_id": uuidToString(row.WorkspaceID),
			"workspace_name": row.WorkspaceName, "created_at": timestampToString(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RevokeMyAgentFullDelegation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "agentId"), "agent_id")
	if !ok {
		return
	}
	if err := h.Queries.RevokeAgentFullDelegation(r.Context(), db.RevokeAgentFullDelegationParams{AgentID: agentID, GrantorUserID: parseUUID(userID)}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke full delegation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
