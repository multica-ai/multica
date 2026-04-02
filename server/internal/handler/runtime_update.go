package handler

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// In-memory update store
// ---------------------------------------------------------------------------

type UpdateStatus string

const (
	UpdatePending   UpdateStatus = "pending"
	UpdateRunning   UpdateStatus = "running"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
	UpdateTimeout   UpdateStatus = "timeout"
)

// UpdateRequest represents a pending or completed CLI update request.
type UpdateRequest struct {
	ID            string       `json:"id"`
	RuntimeID     string       `json:"runtime_id"`
	Status        UpdateStatus `json:"status"`
	TargetVersion string       `json:"target_version"`
	Output        string       `json:"output,omitempty"`
	Error         string       `json:"error,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// UpdateStore is a thread-safe in-memory store for CLI update requests.
type UpdateStore struct {
	mu       sync.Mutex
	requests map[string]*UpdateRequest // keyed by update ID
}

func NewUpdateStore() *UpdateStore {
	return &UpdateStore{
		requests: make(map[string]*UpdateRequest),
	}
}

func (s *UpdateStore) Create(runtimeID, targetVersion string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up old requests (>5 minutes).
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > 5*time.Minute {
			delete(s.requests, id)
		}
	}

	// Reject if there is already a pending or running update for this runtime.
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && (req.Status == UpdatePending || req.Status == UpdateRunning) {
			return nil, errUpdateInProgress
		}
	}

	req := &UpdateRequest{
		ID:            randomID(),
		RuntimeID:     runtimeID,
		Status:        UpdatePending,
		TargetVersion: targetVersion,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

var errUpdateInProgress = &updateError{msg: "an update is already in progress for this runtime"}

type updateError struct{ msg string }

func (e *updateError) Error() string { return e.msg }

func (s *UpdateStore) Get(id string) *UpdateRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	// Check for timeout (both pending and running states).
	if (req.Status == UpdatePending || req.Status == UpdateRunning) && time.Since(req.CreatedAt) > 120*time.Second {
		req.Status = UpdateTimeout
		req.Error = "update did not complete within 120 seconds"
		req.UpdatedAt = time.Now()
	}
	return req
}

// PopPending returns and marks as running the pending update for a runtime.
func (s *UpdateStore) PopPending(runtimeID string) *UpdateRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			req.Status = UpdateRunning
			req.UpdatedAt = time.Now()
			return req
		}
	}
	return nil
}

func (s *UpdateStore) Complete(id string, output string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateCompleted
		req.Output = output
		req.UpdatedAt = time.Now()
	}
}

func (s *UpdateStore) Fail(id string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiateUpdate creates a new CLI update request (protected route, called by frontend).
func (h *Handler) InitiateUpdate(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	var req struct {
		TargetVersion string `json:"target_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetVersion == "" {
		writeError(w, http.StatusBadRequest, "target_version is required")
		return
	}

	update, err := h.UpdateStore.Create(runtimeID, req.TargetVersion)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// GetUpdate returns the status of an update request (protected route, called by frontend).
func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) {
	updateID := chi.URLParam(r, "updateId")

	update := h.UpdateStore.Get(updateID)
	if update == nil {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// ReportUpdateResult receives the update result from the daemon.
func (h *Handler) ReportUpdateResult(w http.ResponseWriter, r *http.Request) {
	updateID := chi.URLParam(r, "updateId")

	var req struct {
		Status string `json:"status"` // "running", "completed", or "failed"
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Status {
	case "completed":
		h.UpdateStore.Complete(updateID, req.Output)
	case "failed":
		h.UpdateStore.Fail(updateID, req.Error)
	case "running":
		// No-op: status is already "running" from PopPending. This call is
		// just a progress signal from the daemon to confirm it received the
		// update command and is executing it.
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
