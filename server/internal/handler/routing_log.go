package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetTaskRoutingLog returns the Cerebra routing evidence for a task.
// Route: GET /api/tasks/{taskId}/routing-log
func (h *Handler) GetTaskRoutingLog(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// Verify the task belongs to the caller's workspace.
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	rows, err := h.Queries.GetRoutingLogsByTask(r.Context(), strToText(taskID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get routing log")
		return
	}
	if len(rows) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no routing log"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": rows})
}

// PutRuntimeTierModelMap configures the simple/standard/heavy model map for a runtime.
// Route: PUT /api/runtimes/{runtimeId}/tier-model-map
func (h *Handler) PutRuntimeTierModelMap(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only edit your own runtimes")
		return
	}

	var tierMap map[string]string
	if err := json.NewDecoder(r.Body).Decode(&tierMap); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	for k := range tierMap {
		if k != "simple" && k != "standard" && k != "heavy" {
			writeError(w, http.StatusBadRequest, "tier map keys must be 'simple', 'standard', or 'heavy'")
			return
		}
	}

	jsonBytes, err := json.Marshal(tierMap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal tier map")
		return
	}

	if err := h.Queries.SetRuntimeTierModelMap(r.Context(), db.SetRuntimeTierModelMapParams{
		ID:           runtimeUUID,
		TierModelMap: jsonBytes,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update runtime tier model map")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
