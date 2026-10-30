package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/pkg/codechanges"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type codeChangeRun struct {
	TaskID     string `json:"task_id"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	BranchName string `json:"branch_name,omitempty"`
	FileCount  int32  `json:"file_count"`
}
type codeChangesResponse struct {
	Available    bool                  `json:"available"`
	Reason       string                `json:"reason,omitempty"`
	Runs         []codeChangeRun       `json:"runs"`
	SourceTaskID string                `json:"source_task_id,omitempty"`
	Snapshot     *codechanges.Snapshot `json:"snapshot,omitempty"`
}

// GetIssueCodeChanges inherits the existing issue/workspace reader boundary.
// Repository access stays on the runtime; this endpoint reads saved artifacts.
func (h *Handler) GetIssueCodeChanges(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, 500, "failed to load code settings")
		return
	}
	response := codeChangesResponse{Runs: []codeChangeRun{}}
	if !codechanges.Enabled(workspace.Settings) {
		response.Reason = "disabled"
		writeJSON(w, 200, response)
		return
	}
	requested := r.URL.Query().Get("task_id")
	if requested != "" {
		id, ok := parseUUIDOrBadRequest(w, requested, "task id")
		if !ok {
			return
		}
		requested = uuidToString(id)
	}
	rows, err := h.Queries.ListIssueCodeChangeTasks(r.Context(), issue.ID)
	if err != nil {
		writeError(w, 500, "failed to load code changes history")
		return
	}
	var selected *db.AgentTaskQueue
	var count int32
	for _, row := range rows {
		task := row.AgentTaskQueue
		if !row.SnapshotRuntimeID.Valid || !row.SnapshotDispatchedAt.Valid || !task.DispatchedAt.Valid || !row.SnapshotDispatchedAt.Time.Equal(task.DispatchedAt.Time) || (task.Status != "completed" && task.Status != "failed") || (task.RuntimeID.Valid && row.SnapshotRuntimeID != task.RuntimeID) {
			continue
		}
		response.Runs = append(response.Runs, codeChangeRun{TaskID: uuidToString(task.ID), Status: task.Status, CreatedAt: timestampToString(task.CreatedAt), BranchName: task.BranchName.String, FileCount: row.FileCount})
		if requested != "" {
			if requested == uuidToString(task.ID) {
				selected = &task
			}
		} else if selected == nil || (count == 0 && row.FileCount > 0) {
			selected = &task
			count = row.FileCount
		}
	}
	if selected == nil {
		if requested != "" {
			writeError(w, 404, "code changes source unavailable")
			return
		}
		response.Reason = "not_captured"
		writeJSON(w, 200, response)
		return
	}
	raw, err := h.Queries.GetTaskCodeChangesSnapshot(r.Context(), selected.ID)
	if err != nil {
		writeError(w, 500, "saved code changes unavailable")
		return
	}
	snapshot, err := codechanges.Decode(raw)
	if err != nil {
		writeError(w, 500, "saved code changes unavailable")
		return
	}
	response.Available = true
	response.SourceTaskID = uuidToString(selected.ID)
	response.Snapshot = snapshot
	writeJSON(w, 200, response)
}

func (h *Handler) UpdateWorkspaceCodeChanges(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace id")
	if !ok {
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	member, err := h.getWorkspaceMember(r.Context(), user, uuidToString(id))
	if err != nil {
		writeError(w, 404, "workspace not found")
		return
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, 403, "insufficient permissions")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Enabled == nil {
		writeError(w, 400, "enabled must be a boolean")
		return
	}
	workspace, err := h.Queries.UpdateWorkspaceCodeChanges(r.Context(), db.UpdateWorkspaceCodeChangesParams{ID: id, Enabled: *req.Enabled})
	if err != nil {
		writeError(w, 500, "failed to update code settings")
		return
	}
	h.publish(protocol.EventWorkspaceUpdated, uuidToString(id), "member", user, map[string]any{"workspace": h.workspaceToResponse(workspace)})
	if members, err := h.Queries.ListMembers(r.Context(), id); err == nil {
		ids := make([]string, 0, len(members))
		for _, m := range members {
			ids = append(ids, uuidToString(m.UserID))
		}
		h.notifyDaemonWorkspacesChanged(ids...)
	}
	writeJSON(w, 200, map[string]any{"workspace_id": uuidToString(id), "enabled": codechanges.Enabled(workspace.Settings)})
}

func (h *Handler) setCodeChangesReceipt(w http.ResponseWriter, r *http.Request, workspaceID string, task db.AgentTaskQueue, snapshot *codechanges.Snapshot) {
	if snapshot == nil {
		return
	}
	if task.Status == "cancelled" {
		w.Header().Set(codechanges.ReceiptHeader, "discarded")
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		return
	}
	if !codechanges.Enabled(workspace.Settings) {
		w.Header().Set(codechanges.ReceiptHeader, "discarded")
		return
	}
	encoded, err := codechanges.Encode(snapshot)
	if err != nil {
		return
	}
	digest, err := h.Queries.GetTaskCodeChangesReceipt(r.Context(), task.ID)
	if err == nil && digest == codechanges.DigestEncoded(encoded) {
		w.Header().Set(codechanges.ReceiptHeader, "saved:"+digest)
	}
}
