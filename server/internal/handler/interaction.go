package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type ReportInteractionRequest struct {
	ID            string                       `json:"id,omitempty"`
	Type          string                       `json:"type"`
	Title         string                       `json:"title"`
	Detail        string                       `json:"detail,omitempty"`
	Options       []protocol.InteractionOption `json:"options"`
	DefaultOption string                       `json:"default_option,omitempty"`
	ExpiresIn     int                          `json:"expires_in,omitempty"` // seconds; 0 = default 5min
	Provider      string                       `json:"provider,omitempty"`
}

type RespondInteractionRequest struct {
	ChosenOption string `json:"chosen_option"`
}

type InteractionDTO struct {
	ID            string                       `json:"id"`
	TaskID        string                       `json:"task_id"`
	Provider      string                       `json:"provider"`
	Type          string                       `json:"type"`
	Title         string                       `json:"title"`
	Detail        string                       `json:"detail,omitempty"`
	Options       []protocol.InteractionOption `json:"options"`
	DefaultOption string                       `json:"default_option,omitempty"`
	Status        string                       `json:"status"`
	CreatedAt     string                       `json:"created_at"`
	ExpiresAt     string                       `json:"expires_at"`
	RespondedAt   *string                      `json:"responded_at,omitempty"`
	ChosenOption  string                       `json:"chosen_option,omitempty"`
}

func interactionToDTO(req protocol.InteractionRequest) InteractionDTO {
	dto := InteractionDTO{
		ID:            req.ID,
		TaskID:        req.TaskID,
		Provider:      req.Provider,
		Type:          req.Type,
		Title:         req.Title,
		Detail:        req.Detail,
		Options:       req.Options,
		DefaultOption: req.DefaultOption,
		Status:        req.Status,
		CreatedAt:     req.CreatedAt.Format(time.RFC3339),
		ExpiresAt:     req.ExpiresAt.Format(time.RFC3339),
		ChosenOption:  req.ChosenOption,
	}
	if req.RespondedAt != nil {
		s := req.RespondedAt.Format(time.RFC3339)
		dto.RespondedAt = &s
	}
	return dto
}

// ---------------------------------------------------------------------------
// Daemon-side: report a pending interaction
// POST /api/daemon/tasks/{taskId}/interactions
// ---------------------------------------------------------------------------

func (h *Handler) ReportInteraction(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	task, ok := h.requireDaemonTaskAccess(w, r, taskID)
	if !ok {
		return
	}

	var body ReportInteractionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Type == "" || body.Title == "" {
		writeError(w, http.StatusBadRequest, "type and title are required")
		return
	}

	now := time.Now()
	expiresIn := 5 * time.Minute
	if body.ExpiresIn > 0 {
		expiresIn = time.Duration(body.ExpiresIn) * time.Second
	}

	req := protocol.InteractionRequest{
		ID:            body.ID,
		TaskID:        uuidToString(task.ID),
		Provider:      body.Provider,
		Type:          body.Type,
		Title:         body.Title,
		Detail:        body.Detail,
		Options:       body.Options,
		DefaultOption: body.DefaultOption,
		Status:        protocol.InteractionStatusPending,
		CreatedAt:     now,
		ExpiresAt:     now.Add(expiresIn),
	}

	id := h.InteractionStore.Create(req)
	req.ID = id

	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	h.publish(protocol.EventInteractionCreated, wsID, "system", "", protocol.InteractionCreatedPayload{
		InteractionRequest: req,
	})

	writeJSON(w, http.StatusCreated, interactionToDTO(req))
}

// ---------------------------------------------------------------------------
// Daemon-side: poll for interaction response
// GET /api/daemon/tasks/{taskId}/interactions/{interactionId}
// ---------------------------------------------------------------------------

func (h *Handler) GetInteractionResult(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
		return
	}

	interactionID := chi.URLParam(r, "interactionId")
	item, err := h.InteractionStore.Get(interactionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}
	writeJSON(w, http.StatusOK, interactionToDTO(item))
}

// ---------------------------------------------------------------------------
// User-side: list interactions for a task
// GET /api/tasks/{taskId}/interactions
// ---------------------------------------------------------------------------

func (h *Handler) ListTaskInteractions(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if !h.requireUserTaskAccess(w, r, taskID) {
		return
	}

	status := r.URL.Query().Get("status")
	items := h.InteractionStore.ListByTask(taskID, status)
	dtos := make([]InteractionDTO, len(items))
	for i, item := range items {
		dtos[i] = interactionToDTO(item)
	}
	writeJSON(w, http.StatusOK, dtos)
}

// ---------------------------------------------------------------------------
// User-side: respond to an interaction
// POST /api/tasks/{taskId}/interactions/{interactionId}/respond
// ---------------------------------------------------------------------------

func (h *Handler) RespondInteraction(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if !h.requireUserTaskAccess(w, r, taskID) {
		return
	}

	interactionID := chi.URLParam(r, "interactionId")

	var body RespondInteractionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.ChosenOption == "" {
		writeError(w, http.StatusBadRequest, "chosen_option is required")
		return
	}

	if err := h.InteractionStore.Respond(interactionID, body.ChosenOption); err != nil {
		if err.Error() == "interaction not found" {
			writeError(w, http.StatusNotFound, "interaction not found")
		} else {
			writeError(w, http.StatusConflict, err.Error())
		}
		return
	}

	item, _ := h.InteractionStore.Get(interactionID)

	wsID := middleware.WorkspaceIDFromContext(r.Context())
	h.publish(protocol.EventInteractionResolved, wsID, "member", requestUserID(r), protocol.InteractionResolvedPayload{
		RequestID:    interactionID,
		TaskID:       taskID,
		Status:       item.Status,
		ChosenOption: body.ChosenOption,
	})

	writeJSON(w, http.StatusOK, interactionToDTO(item))
}

// ---------------------------------------------------------------------------
// requireUserTaskAccess verifies the caller is a workspace member with access
// to the given task. Used for user-side interaction endpoints.
// ---------------------------------------------------------------------------

func (h *Handler) requireUserTaskAccess(w http.ResponseWriter, r *http.Request, taskID string) bool {
	task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return false
	}
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" {
		writeError(w, http.StatusNotFound, "task not found")
		return false
	}
	ctxWsID := middleware.WorkspaceIDFromContext(r.Context())
	if ctxWsID == "" || ctxWsID != wsID {
		writeError(w, http.StatusForbidden, "no access to this task")
		return false
	}
	return true
}
