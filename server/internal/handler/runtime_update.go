package handler

import (
	"context"
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

	// updateTimeout is how long an update request can stay non-terminal
	// before being auto-transitioned to UpdateTimeout.
	updateTimeout = 120 * time.Second
	// updateStoreRetention is the Redis TTL for individual update request
	// records. It must be longer than updateTimeout so the record survives
	// long enough for the UI to poll it.
	updateStoreRetention = 5 * time.Minute
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

// applyUpdateTimeout transitions a request to UpdateTimeout when it has been
// stuck in a non-terminal state past its threshold. Returns true when the
// transition was applied so callers (e.g. the Redis store) can persist the
// change.
func applyUpdateTimeout(req *UpdateRequest, now time.Time) bool {
	if (req.Status == UpdatePending || req.Status == UpdateRunning) && now.Sub(req.CreatedAt) > updateTimeout {
		req.Status = UpdateTimeout
		req.Error = "update did not complete within 120 seconds"
		req.UpdatedAt = now
		return true
	}
	return false
}

// UpdateStore abstracts the persistence of CLI update requests. Both the
// in-memory implementation (single-node) and the Redis-backed one (multi-node)
// satisfy this interface.
type UpdateStore interface {
	Create(ctx context.Context, runtimeID, targetVersion string) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	PopPending(ctx context.Context, runtimeID string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// InMemoryUpdateStore is a thread-safe in-memory store for CLI update requests.
type InMemoryUpdateStore struct {
	mu       sync.Mutex
	requests map[string]*UpdateRequest // keyed by update ID
}

func NewUpdateStore() *InMemoryUpdateStore {
	return &InMemoryUpdateStore{
		requests: make(map[string]*UpdateRequest),
	}
}

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, targetVersion string) (*UpdateRequest, error) {
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

func (s *InMemoryUpdateStore) Get(_ context.Context, id string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	// Check for timeout (both pending and running states).
	applyUpdateTimeout(req, time.Now())
	return req, nil
}

// PopPending returns and marks as running the pending update for a runtime.
func (s *InMemoryUpdateStore) PopPending(_ context.Context, runtimeID string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			req.Status = UpdateRunning
			req.UpdatedAt = time.Now()
			return req, nil
		}
	}
	return nil, nil
}

func (s *InMemoryUpdateStore) Complete(_ context.Context, id string, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateCompleted
		req.Output = output
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryUpdateStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = UpdateFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiateUpdate creates a new CLI update request (protected route, called by frontend).
func (h *Handler) InitiateUpdate(w http.ResponseWriter, r *http.Request) {
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

	update, err := h.UpdateStore.Create(r.Context(), uuidToString(rt.ID), req.TargetVersion)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// GetUpdate returns the status of an update request (protected route, called by frontend).
func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) {
	updateID := chi.URLParam(r, "updateId")

	update, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get update")
		return
	}
	if update == nil {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// ReportUpdateResult receives the update result from the daemon.
func (h *Handler) ReportUpdateResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	// Verify the caller owns this runtime's workspace.
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

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
		if err := h.UpdateStore.Complete(r.Context(), updateID, req.Output); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to complete update")
			return
		}
	case "failed":
		if err := h.UpdateStore.Fail(r.Context(), updateID, req.Error); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to fail update")
			return
		}
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
