package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ClaimCuratorDraftTask lets a daemon runtime claim the next queued curator
// draft task for its workspace.
func (h *Handler) ClaimCuratorDraftTask(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	if h.CuratorDraftService == nil {
		writeError(w, http.StatusServiceUnavailable, "curator draft service is not configured")
		return
	}

	task, err := h.CuratorDraftService.ClaimNextDraftTask(r.Context(), runtime.ID, runtime.WorkspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"task": nil})
			return
		}
		slog.Error("claim curator draft task failed",
			append(logger.RequestAttrs(r), "error", err, "runtime_id", runtimeID)...)
		writeError(w, http.StatusInternalServerError, "failed to claim curator draft task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"task": curatorDraftTaskToResponse(task)})
}

// CompleteCuratorDraftTask receives a completed draft from a daemon runtime
// and creates the corresponding knowledge item.
func (h *Handler) CompleteCuratorDraftTask(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	_, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	taskIDStr := chi.URLParam(r, "taskId")
	taskID := parseUUID(taskIDStr)
	if !taskID.Valid {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	var req struct {
		Draft service.CuratorDraft `json:"draft"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if h.CuratorDraftService == nil {
		writeError(w, http.StatusServiceUnavailable, "curator draft service is not configured")
		return
	}

	detail, err := h.CuratorDraftService.CompleteDraftTask(r.Context(), taskID, req.Draft)
	if err != nil {
		slog.Error("complete curator draft task failed",
			append(logger.RequestAttrs(r), "error", err, "task_id", taskIDStr)...)
		writeError(w, http.StatusInternalServerError, "failed to complete curator draft task")
		return
	}

	// Broadcast the draft-ready event so connected clients can refresh.
	task, _ := h.CuratorDraftService.GetCuratorDraftTask(r.Context(), taskID)
	if task.ID.Valid {
		h.publish(protocol.EventKnowledgeDraftReady, uuidToString(task.WorkspaceID), "agent", runtimeID, map[string]any{
			"task_id":      taskIDStr,
			"knowledge_id": uuidToString(detail.Item.ID),
			"draft_kind":   task.DraftKind,
		})
	}

	writeJSON(w, http.StatusOK, knowledgeDetailToResponse(detail))
}

// FailCuratorDraftTask marks a curator draft task as failed.
func (h *Handler) FailCuratorDraftTask(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	_, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	taskIDStr := chi.URLParam(r, "taskId")
	taskID := parseUUID(taskIDStr)
	if !taskID.Valid {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	var req struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if h.CuratorDraftService == nil {
		writeError(w, http.StatusServiceUnavailable, "curator draft service is not configured")
		return
	}

	if err := h.CuratorDraftService.FailDraftTask(r.Context(), taskID, req.Error); err != nil {
		slog.Error("fail curator draft task failed",
			append(logger.RequestAttrs(r), "error", err, "task_id", taskIDStr)...)
		writeError(w, http.StatusInternalServerError, "failed to fail curator draft task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "failed"})
}

// GetCuratorDraftStatus returns the status of a curator draft task. Used
// by the frontend for polling after receiving a 202 Accepted.
func (h *Handler) GetCuratorDraftStatus(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskId")
	taskID := parseUUID(taskIDStr)
	if !taskID.Valid {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	if h.CuratorDraftService == nil {
		writeError(w, http.StatusServiceUnavailable, "curator draft service is not configured")
		return
	}

	task, err := h.CuratorDraftService.GetCuratorDraftTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "curator draft task not found")
		return
	}

	resp := map[string]any{
		"id":         uuidToString(task.ID),
		"status":     task.Status,
		"draft_kind": task.DraftKind,
	}
	if task.Status == "completed" && len(task.Result) > 0 {
		resp["result"] = json.RawMessage(task.Result)
	}
	if task.Status == "failed" && task.Error.Valid {
		resp["error"] = task.Error.String
	}

	writeJSON(w, http.StatusOK, resp)
}

func curatorDraftTaskToResponse(task db.CuratorDraftTask) map[string]any {
	resp := map[string]any{
		"id":         uuidToString(task.ID),
		"draft_kind": task.DraftKind,
		"status":     task.Status,
	}
	if len(task.InputData) > 0 {
		resp["input_data"] = json.RawMessage(task.InputData)
	}
	return resp
}
