package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebra"
	"github.com/multica-ai/multica/server/internal/middleware"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
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

	rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeUUID)
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

// ReportTaskRoutingLog stores a routing decision record submitted by the daemon.
// Route: POST /api/daemon/tasks/{taskId}/routing-log
func (h *Handler) ReportTaskRoutingLog(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if _, ok := parseUUIDOrBadRequest(w, taskID, "task_id"); !ok {
		return
	}

	var req cerebra.RoutingLogEntry
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	id := uuid.New().String()
	if err := h.Queries.InsertRoutingLog(r.Context(), db.InsertRoutingLogParams{
		ID:                id,
		TaskID:            strToText(taskID),
		IssueID:           strToText(req.IssueID),
		SessionID:         strToText(req.SessionID),
		RuntimeID:         req.RuntimeID,
		ChosenModel:       req.ChosenModel,
		Tier:              req.Tier,
		MatchedRule:       req.MatchedRule,
		ToolChainExpected: req.ToolChainExpected,
		FallbackUsed:      req.FallbackUsed,
		LatencyMs:         int32(req.LatencyMs),
		Status:            req.Status,
		PolicyReason:      strToText(req.PolicyReason),
		CandidateCount:    pgtype.Int4{Int32: int32(req.CandidateCount), Valid: req.CandidateCount > 0},
		ClassifierVersion: strToText(req.ClassifierVersion),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert routing log")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}
