package handler

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// validateHostAgentForAITask applies the same agent dispatch boundary used by
// issue assignment and quick-create before a no-parent AI task is queued.
func (h *Handler) validateHostAgentForAITask(w http.ResponseWriter, r *http.Request, workspaceID, agentID string) (db.Agent, pgtype.UUID, bool) {
	agentUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(agentID), "agent_id")
	if !ok {
		return db.Agent{}, pgtype.UUID{}, false
	}
	if status, msg := h.validateAssigneePair(
		r.Context(), r, workspaceID,
		pgtype.Text{String: "agent", Valid: true},
		agentUUID,
	); status != 0 {
		writeError(w, status, msg)
		return db.Agent{}, pgtype.UUID{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Agent{}, pgtype.UUID{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.Agent{}, pgtype.UUID{}, false
	}
	if !agent.RuntimeID.Valid {
		writeAgentUnavailable(w, "agent has no runtime")
		return db.Agent{}, pgtype.UUID{}, false
	}
	if !h.isRuntimeOnline(r.Context(), agent.RuntimeID) {
		writeAgentUnavailable(w, "agent's runtime is offline")
		return db.Agent{}, pgtype.UUID{}, false
	}
	return agent, agentUUID, true
}
