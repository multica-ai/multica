package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/middleware"
)

type taskMandateResponse struct {
	TaskID       string   `json:"task_id"`
	AgentID      string   `json:"agent_id"`
	AllowedTools []string `json:"allowed_tools"`
	IssuedAt     string   `json:"issued_at"`
	ExpiresAt    string   `json:"expires_at"`
	Status       string   `json:"status"`
}

// GetTaskMandateByUser exposes the immutable access snapshot next to the task
// transcript. It performs the same workspace ownership check as task messages;
// a caller outside the task workspace receives the same 404.
func (h *Handler) GetTaskMandateByUser(w http.ResponseWriter, r *http.Request) {
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
	workspaceID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if workspaceID == "" || workspaceID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	snapshot, err := taskmandate.NewStoreDB(h.DB).Get(r.Context(), taskUUID, workspaceUUID, task.AgentID)
	if errors.Is(err, taskmandate.ErrMissing) || errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task access snapshot not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task access snapshot")
		return
	}
	status := "active"
	if !snapshot.ExpiresAt.After(time.Now()) {
		status = "expired"
	}
	writeJSON(w, http.StatusOK, taskMandateResponse{
		TaskID:       uuidToString(snapshot.TaskID),
		AgentID:      uuidToString(snapshot.AgentID),
		AllowedTools: snapshot.AllowedTools,
		IssuedAt:     snapshot.IssuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:    snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Status:       status,
	})
}
