package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cross-workspace grants are separate from invocation permissions: only the
// agent's human owner may allow its running task to address another workspace.
func (h *Handler) agentForWorkspaceGrants(w http.ResponseWriter, r *http.Request) (db.Agent, bool) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, false
	}
	if !agent.OwnerID.Valid || uuidToString(agent.OwnerID) != requestUserID(r) || agent.Kind != "user" || agent.ArchivedAt.Valid {
		writeError(w, http.StatusForbidden, "only the active agent's owner can manage workspace grants")
		return db.Agent{}, false
	}
	return agent, true
}

func (h *Handler) ListAgentWorkspaceGrants(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.agentForWorkspaceGrants(w, r)
	if !ok {
		return
	}
	ids, err := h.Queries.ListAgentWorkspaceGrants(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace grants")
		return
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, uuidToString(id))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace_ids": result})
}

func (h *Handler) AddAgentWorkspaceGrant(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.agentForWorkspaceGrants(w, r)
	if !ok {
		return
	}
	target, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspace_id")
	if !ok {
		return
	}
	if target == agent.WorkspaceID {
		writeError(w, http.StatusBadRequest, "the agent already has access to its home workspace")
		return
	}
	if _, err := h.Queries.GetWorkspace(r.Context(), target); err != nil {
		writeError(w, http.StatusNotFound, "target workspace not found")
		return
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: agent.OwnerID, WorkspaceID: target})
	if err != nil || member.Role == "guest" {
		writeError(w, http.StatusForbidden, "agent owner must be a member of the target workspace")
		return
	}
	if err := h.Queries.AddAgentWorkspaceGrant(r.Context(), db.AddAgentWorkspaceGrantParams{AgentID: agent.ID, WorkspaceID: target, CreatedBy: agent.OwnerID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to grant workspace access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "workspace_id": uuidToString(target)})
}

func (h *Handler) RemoveAgentWorkspaceGrant(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.agentForWorkspaceGrants(w, r)
	if !ok {
		return
	}
	target, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspace_id")
	if !ok {
		return
	}
	if err := h.Queries.RemoveAgentWorkspaceGrant(r.Context(), db.RemoveAgentWorkspaceGrantParams{AgentID: agent.ID, WorkspaceID: target}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke workspace access")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
