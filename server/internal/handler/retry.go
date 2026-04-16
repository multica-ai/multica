package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
)

// RetryTask creates a new queued task as a retry of a failed or cancelled task.
// POST /api/tasks/{taskId}/retry
func (h *Handler) RetryTask(w http.ResponseWriter, r *http.Request) {
	_, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	taskID := chi.URLParam(r, "taskId")

	task, err := h.TaskService.RetryTask(r.Context(), parseUUID(taskID), workspaceID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTaskNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrTaskNotRetryable),
			errors.Is(err, service.ErrIssueClosed):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrAlreadyRetried),
			errors.Is(err, service.ErrActiveTaskExists):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "retry failed")
		}
		return
	}

	writeJSON(w, http.StatusCreated, taskToResponse(*task))
}
