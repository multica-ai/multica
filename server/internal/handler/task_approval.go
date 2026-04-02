package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ApproveTask transitions a pending_approval task to queued.
// Only the runtime owner can approve.
func (h *Handler) ApproveTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	task, err := h.TaskService.ApproveTask(r.Context(), parseUUID(taskID), userID)
	if err != nil {
		if err.Error() == "only the runtime owner can approve tasks" {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": uuidToString(task.ID),
		"status":  task.Status,
	})
}

// RejectTask cancels a pending_approval task.
// Only the runtime owner can reject.
func (h *Handler) RejectTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	task, err := h.TaskService.RejectTask(r.Context(), parseUUID(taskID), userID)
	if err != nil {
		if err.Error() == "only the runtime owner can reject tasks" {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": uuidToString(task.ID),
		"status":  task.Status,
	})
}
