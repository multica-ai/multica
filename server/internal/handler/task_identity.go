package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetCurrentTaskIdentity exposes only the live task bound by auth middleware.
// No caller-provided task/user/workspace selectors participate in resolution.
func (h *Handler) GetCurrentTaskIdentity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	deny := func() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "active task identity unavailable"})
	}
	if r.Header.Get("X-Actor-Source") != "task_token" {
		deny()
		return
	}
	taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
	if err != nil {
		deny()
		return
	}
	workspaceID, err := util.ParseUUID(r.Header.Get("X-Workspace-ID"))
	if err != nil {
		deny()
		return
	}
	lookup := func(id pgtype.UUID) (db.AgentTaskQueue, error) {
		return h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
	}
	task, err := lookup(taskID)
	if err != nil || uuidToString(task.AgentID) != r.Header.Get("X-Agent-ID") || (task.Status != "running" && task.Status != "dispatched") {
		deny()
		return
	}
	identity, err := attribution.ResolveExecutionIdentity(task, lookup)
	if err != nil {
		deny()
		return
	}
	userID := uuidToString(identity.UserID)
	if identity.Kind == "human" {
		if _, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(workspaceID)); err != nil {
			deny()
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"task_id": uuidToString(task.ID), "agent_id": uuidToString(task.AgentID),
		"workspace_id": uuidToString(workspaceID), "kind": identity.Kind, "user_id": userID,
	})
}
