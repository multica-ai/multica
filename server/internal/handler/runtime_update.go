package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Daemon CLI update request store
// ---------------------------------------------------------------------------
//
// The frontend asks the server "tell daemon X to update its CLI to version Y";
// the daemon then picks up the request on its next heartbeat. POST (create),
// GET (poll), heartbeat (pop-pending), and daemon report (complete/fail) can
// all land on different API pods, so the store MUST be shared across nodes.
// Production uses RedisUpdateStore; local dev / tests use InMemoryUpdateStore.

type UpdateStatus string

const (
	UpdatePending   UpdateStatus = "pending"
	UpdateRunning   UpdateStatus = "running"
	UpdateCompleted UpdateStatus = "completed"
	UpdateFailed    UpdateStatus = "failed"
	UpdateTimeout   UpdateStatus = "timeout"
)

const (
	// updateTimeout bounds how long a request can sit in pending or running
	// before the UI is told "this never finished". Single-tier (vs the model
	// list store's two-tier) because daemon updates take much longer than
	// model discovery — the daemon downloads a binary, swaps it, and restarts.
	updateTimeout = 120 * time.Second
	// updateStoreRetention is the TTL used by the Redis-backed store. The
	// in-memory store uses the same duration for its lazy GC sweep.
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

// UpdateStore tracks pending / running / completed daemon CLI update requests.
// The server MUST stay stateless — any state that needs to outlive a single
// request has to live in shared storage so multi-node deploys can have POST,
// heartbeat and poll land on different nodes and still agree on the request's
// state. See PR #51 for the equivalent fix on ModelListStore.
type UpdateStore interface {
	Create(ctx context.Context, runtimeID, targetVersion string) (*UpdateRequest, error)
	Get(ctx context.Context, id string) (*UpdateRequest, error)
	PopPending(ctx context.Context, runtimeID string) (*UpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// errUpdateInProgress is returned by Create when an update is already pending
// or running for the same runtime. The InitiateUpdate handler maps this to a
// 409 — any other error becomes a 500.
var errUpdateInProgress = errors.New("an update is already in progress for this runtime")

// applyUpdateTimeout transitions a request to UpdateTimeout when it has been
// stuck pending or running past the threshold. Returns true when the record
// was modified so callers can persist the change.
func applyUpdateTimeout(req *UpdateRequest, now time.Time) bool {
	if req.Status != UpdatePending && req.Status != UpdateRunning {
		return false
	}
	if now.Sub(req.CreatedAt) <= updateTimeout {
		return false
	}
	req.Status = UpdateTimeout
	req.Error = "update did not complete within 120 seconds"
	req.UpdatedAt = now
	return true
}

// ---------------------------------------------------------------------------
// InMemoryUpdateStore — single-node implementation
// ---------------------------------------------------------------------------

type InMemoryUpdateStore struct {
	mu       sync.Mutex
	requests map[string]*UpdateRequest // keyed by update ID
}

func NewInMemoryUpdateStore() *InMemoryUpdateStore {
	return &InMemoryUpdateStore{
		requests: make(map[string]*UpdateRequest),
	}
}

func (s *InMemoryUpdateStore) Create(_ context.Context, runtimeID, targetVersion string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Garbage-collect stale entries so the map can't grow unbounded.
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > updateStoreRetention {
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

func (s *InMemoryUpdateStore) Get(_ context.Context, id string) (*UpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyUpdateTimeout(req, time.Now())
	return req, nil
}

// PopPending returns and marks-running the pending update for a runtime.
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
	if errors.Is(err, errUpdateInProgress) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue update: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, update)
}

// GetUpdate returns the status of an update request (protected route, called by frontend).
func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) {
	updateID := chi.URLParam(r, "updateId")

	update, err := h.UpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load update: "+err.Error())
		return
	}
	if update == nil {
		writeError(w, http.StatusNotFound, "update not found")
		return
	}
	if update.Status == UpdateTimeout {
		slog.Warn("update timed out", "update_id", updateID, "runtime_id", update.RuntimeID, "error", update.Error)
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
			slog.Error("update Complete failed", "error", err, "update_id", updateID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	case "failed":
		if err := h.UpdateStore.Fail(r.Context(), updateID, req.Error); err != nil {
			slog.Error("update Fail failed", "error", err, "update_id", updateID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
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
