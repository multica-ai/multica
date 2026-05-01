package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Model-list request store
// ---------------------------------------------------------------------------
//
// The server cannot call the daemon directly (the daemon is behind the user's
// NAT and only polls the server). So "list models for this runtime" uses a
// pending-request pattern: server creates a pending request, daemon pops it
// on the next heartbeat, executes locally, and reports the result back.

// ModelListStatus represents the lifecycle of a model list request.
type ModelListStatus string

const (
	ModelListPending   ModelListStatus = "pending"
	ModelListRunning   ModelListStatus = "running"
	ModelListCompleted ModelListStatus = "completed"
	ModelListFailed    ModelListStatus = "failed"
	ModelListTimeout   ModelListStatus = "timeout"
)

// ModelListRequest represents a pending or completed model list request.
// Supported is false when the provider ignores per-agent model
// selection entirely (currently: hermes). The UI uses this to
// disable its dropdown rather than silently accepting a value the
// backend will drop.
type ModelListRequest struct {
	ID        string          `json:"id"`
	RuntimeID string          `json:"runtime_id"`
	Status    ModelListStatus `json:"status"`
	Models    []ModelEntry    `json:"models,omitempty"`
	Supported bool            `json:"supported"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ModelEntry mirrors agent.Model for the wire. `Default` tags the
// model the runtime advertises as its preferred pick (e.g. Claude
// Code's shipped default, or hermes' currentModelId) so the UI can
// badge it — don't drop it when marshalling.
type ModelEntry struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

const (
	// modelListPendingTimeout bounds how long a pending request can sit in
	// the store before the UI is told "daemon didn't pick this up".
	modelListPendingTimeout = 30 * time.Second
	// modelListRunningTimeout bounds how long a claimed (running) request
	// can stay claimed before the UI is told "daemon picked this up but
	// never reported a result". This matters when the heartbeat response
	// carrying `pending_model_list` is lost in transit (e.g. HTTP client
	// timeout after PopPending already mutated store state): without this
	// transition the UI would keep polling a record that is stuck in
	// `running` until the 2-minute memory GC sweeps it.
	modelListRunningTimeout = 60 * time.Second
)

// ModelListStorer abstracts the model-list request store so handlers work
// identically whether backed by an in-memory map (single-instance / tests)
// or a shared database (multi-instance production).
type ModelListStorer interface {
	Create(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Get(ctx context.Context, id string) (*ModelListRequest, error)
	PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Complete(ctx context.Context, id string, models []ModelEntry, supported bool) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// ---------------------------------------------------------------------------
// In-memory implementation (tests / single-instance fallback)
// ---------------------------------------------------------------------------

// InMemoryModelListStore is a thread-safe in-memory store. Entries expire
// after 2 min to bound memory use.
type InMemoryModelListStore struct {
	mu       sync.Mutex
	requests map[string]*ModelListRequest
}

func NewInMemoryModelListStore() *InMemoryModelListStore {
	return &InMemoryModelListStore{requests: make(map[string]*ModelListRequest)}
}

func (s *InMemoryModelListStore) Create(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Garbage-collect stale entries so the map can't grow unbounded.
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > 2*time.Minute {
			delete(s.requests, id)
		}
	}

	req := &ModelListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    ModelListPending,
		Supported: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryModelListStore) Get(_ context.Context, id string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyModelListTimeout(req, time.Now())
	return req, nil
}

// applyModelListTimeout transitions a request to ModelListTimeout when it has
// been stuck in a non-terminal state past its threshold. The pending threshold
// catches "daemon never picked this up"; the running threshold catches
// "daemon picked it up but the result report was lost" — previously the only
// escape from running was the 2-minute memory GC, which exceeded the UI's
// polling window and surfaced as a silent discovery failure.
func applyModelListTimeout(req *ModelListRequest, now time.Time) {
	switch req.Status {
	case ModelListPending:
		if now.Sub(req.CreatedAt) > modelListPendingTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not respond within 30 seconds"
			req.UpdatedAt = now
		}
	case ModelListRunning:
		if now.Sub(req.UpdatedAt) > modelListRunningTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
		}
	}
}

func (s *InMemoryModelListStore) PopPending(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *ModelListRequest
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = ModelListRunning
		oldest.UpdatedAt = time.Now()
	}
	return oldest, nil
}

func (s *InMemoryModelListStore) Complete(_ context.Context, id string, models []ModelEntry, supported bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListCompleted
		req.Models = models
		req.Supported = supported
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryModelListStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Database-backed implementation (multi-instance production)
// ---------------------------------------------------------------------------

// DBModelListStore persists model-list requests in PostgreSQL so that
// POST (create) and GET (poll) work correctly across multiple server
// instances — the root cause of #1958.
type DBModelListStore struct {
	q *db.Queries
}

func NewDBModelListStore(q *db.Queries) *DBModelListStore {
	return &DBModelListStore{q: q}
}

func (s *DBModelListStore) Create(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	// Best-effort GC of stale rows; ignore errors.
	_ = s.q.DeleteStaleModelListRequests(ctx)

	row, err := s.q.CreateModelListRequest(ctx, db.CreateModelListRequestParams{
		ID:        randomID(),
		RuntimeID: parseUUID(runtimeID),
	})
	if err != nil {
		return nil, err
	}
	return dbRowToModelListRequest(row), nil
}

func (s *DBModelListStore) Get(ctx context.Context, id string) (*ModelListRequest, error) {
	row, err := s.q.GetModelListRequest(ctx, id)
	if err != nil {
		return nil, nil // not found → nil, matching in-memory semantics
	}
	req := dbRowToModelListRequest(row)
	// Apply timeout transitions in Go, then persist if status changed.
	oldStatus := req.Status
	applyModelListTimeout(req, time.Now())
	if req.Status != oldStatus {
		_ = s.q.TimeoutModelListRequest(ctx, db.TimeoutModelListRequestParams{
			ID:    req.ID,
			Error: req.Error,
		})
	}
	return req, nil
}

func (s *DBModelListStore) PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error) {
	row, err := s.q.PopPendingModelListRequest(ctx, parseUUID(runtimeID))
	if err != nil {
		return nil, nil // no pending row → nil
	}
	return dbRowToModelListRequest(row), nil
}

func (s *DBModelListStore) Complete(ctx context.Context, id string, models []ModelEntry, supported bool) error {
	modelsJSON, err := json.Marshal(models)
	if err != nil {
		return err
	}
	return s.q.CompleteModelListRequest(ctx, db.CompleteModelListRequestParams{
		ID:        id,
		Models:    modelsJSON,
		Supported: supported,
	})
}

func (s *DBModelListStore) Fail(ctx context.Context, id string, errMsg string) error {
	return s.q.FailModelListRequest(ctx, db.FailModelListRequestParams{
		ID:    id,
		Error: errMsg,
	})
}

// dbRowToModelListRequest converts a sqlc-generated DB row into the handler's
// ModelListRequest type used on the wire.
func dbRowToModelListRequest(row db.ModelListRequest) *ModelListRequest {
	req := &ModelListRequest{
		ID:        row.ID,
		RuntimeID: uuidToString(row.RuntimeID),
		Status:    ModelListStatus(row.Status),
		Supported: row.Supported,
		Error:     row.Error,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if len(row.Models) > 0 {
		_ = json.Unmarshal(row.Models, &req.Models)
	}
	return req
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiateListModels creates a pending model list request for a runtime.
// Called by the frontend; the daemon picks it up on its next heartbeat.
func (h *Handler) InitiateListModels(w http.ResponseWriter, r *http.Request) {
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
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}

	req, err := h.ModelListStore.Create(r.Context(), uuidToString(rt.ID))
	if err != nil {
		slog.Error("model list create failed", "error", err, "runtime_id", runtimeID)
		writeError(w, http.StatusInternalServerError, "failed to initiate model discovery")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// GetModelListRequest returns the status of a model list request.
func (h *Handler) GetModelListRequest(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")

	req, _ := h.ModelListStore.Get(r.Context(), requestID)
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ReportModelListResult receives the list result from the daemon.
func (h *Handler) ReportModelListResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")

	var body struct {
		Status    string       `json:"status"` // "completed" or "failed"
		Models    []ModelEntry `json:"models"`
		Supported *bool        `json:"supported"`
		Error     string       `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status == "completed" {
		// Older daemons may omit `supported`; default to true to keep
		// the UI usable while they haven't been redeployed yet.
		supported := true
		if body.Supported != nil {
			supported = *body.Supported
		}
		h.ModelListStore.Complete(r.Context(), requestID, body.Models, supported)
	} else {
		h.ModelListStore.Fail(r.Context(), requestID, body.Error)
	}

	slog.Debug("model list report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status, "count", len(body.Models))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
