package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
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

	// Resolve secret_ref to plaintext API key for one-time delivery to the daemon.
	credentials, err := h.resolveCuratorCredentials(r.Context(), task)
	if err != nil {
		slog.Error("resolve curator draft credentials failed",
			append(logger.RequestAttrs(r), "error", err, "task_id", uuidToString(task.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to resolve curator draft credentials")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"task": curatorDraftTaskToResponse(task, credentials)})
}

// CompleteCuratorDraftTask receives a completed draft from a daemon runtime
// and creates the corresponding knowledge item.
func (h *Handler) CompleteCuratorDraftTask(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
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

	detail, err := h.CuratorDraftService.CompleteDraftTask(r.Context(), taskID, runtime.ID, runtime.WorkspaceID, req.Draft)
	if err != nil {
		slog.Error("complete curator draft task failed",
			append(logger.RequestAttrs(r), "error", err, "task_id", taskIDStr)...)
		writeError(w, http.StatusInternalServerError, "failed to complete curator draft task")
		return
	}

	// Broadcast the draft-ready event so connected clients can refresh.
	task, _ := h.CuratorDraftService.GetCuratorDraftTask(r.Context(), taskID, runtime.WorkspaceID)
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
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
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

	if err := h.CuratorDraftService.FailDraftTask(r.Context(), taskID, runtime.ID, runtime.WorkspaceID, req.Error); err != nil {
		slog.Error("fail curator draft task failed",
			append(logger.RequestAttrs(r), "error", err, "task_id", taskIDStr)...)
		writeError(w, http.StatusInternalServerError, "failed to fail curator draft task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "failed"})
}

// GetCuratorDraftStatus returns the status of a curator draft task. Used
// by the frontend for polling after receiving a 202 Accepted.
// Workspace context is resolved from the RequireWorkspaceMember middleware on the route group.
func (h *Handler) GetCuratorDraftStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

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

	task, err := h.CuratorDraftService.GetCuratorDraftTask(r.Context(), taskID, wsUUID)
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

func curatorDraftTaskToResponse(task db.CuratorDraftTask, credentials map[string]any) map[string]any {
	resp := map[string]any{
		"id":          uuidToString(task.ID),
		"draft_kind":  task.DraftKind,
		"status":      task.Status,
		"credentials": credentials,
	}
	// Include only the draft_input portion, not the full input_data (which may contain secret_ref).
	if len(task.InputData) > 0 {
		var input service.CuratorDraftTaskInput
		if err := json.Unmarshal(task.InputData, &input); err == nil {
			resp["draft_input"] = input.DraftInput
		}
	}
	return resp
}

// resolveCuratorCredentials extracts the secret_ref from the task's input_data
// and resolves it to a plaintext API key for one-time delivery to the daemon.
func (h *Handler) resolveCuratorCredentials(ctx context.Context, task db.CuratorDraftTask) (map[string]any, error) {
	var input service.CuratorDraftTaskInput
	if err := json.Unmarshal(task.InputData, &input); err != nil {
		return nil, fmt.Errorf("unmarshal task input: %w", err)
	}

	if h.WorkspaceSecretService == nil {
		return nil, errors.New("workspace secret service is not configured")
	}

	apiKey, err := h.WorkspaceSecretService.ResolveSecretRef(ctx, task.WorkspaceID, input.SecretRef)
	if err != nil {
		return nil, fmt.Errorf("resolve secret ref: %w", err)
	}

	return map[string]any{
		"base_url":        input.BaseURL,
		"api_key":         apiKey,
		"model":           input.Model,
		"embedding_model": input.EmbeddingModel,
		"provider":        input.Provider,
	}, nil
}
