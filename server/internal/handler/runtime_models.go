package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Model list request store
// ---------------------------------------------------------------------------
//
// The server cannot call the daemon directly (the daemon is behind the user's
// NAT and only polls the server). So "list models for this runtime" uses a
// pending-request pattern: a frontend POST creates a pending request, the
// daemon pops it on the next heartbeat, executes locally, and reports the
// result back.
//
// The store is the cross-cutting state for that flow. It MUST stay coherent
// across API replicas — POST, heartbeat and poll can each land on a different
// node, and they all need to see the same request lifecycle. The single-node
// in-memory implementation is fine for self-hosted dev; multi-node deploys
// (Multica Cloud) MUST use the Redis-backed implementation, otherwise the
// pending request is invisible to whichever replica receives the next call
// and the picker shows "No models available" (regression: see issue
// review on multica-ai/multica#2009).

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
//
// RunStartedAt is set when PopPending claims the request. It is
// `json:"-"` because it's a server-side bookkeeping field — the UI only
// needs Status / UpdatedAt to drive the polling loop.
type ModelListRequest struct {
	ID           string          `json:"id"`
	RuntimeID    string          `json:"runtime_id"`
	Status       ModelListStatus `json:"status"`
	Models       []ModelEntry    `json:"models,omitempty"`
	Supported    bool            `json:"supported"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	RunStartedAt *time.Time      `json:"-"`
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
	// `running` until retention sweeps it.
	modelListRunningTimeout = 60 * time.Second
	// modelListStoreRetention bounds how long any stored request lives in
	// the backing store. The Redis backend uses it as a TTL; the in-memory
	// backend GCs on Create. The window is deliberately wider than the
	// running/pending timeouts so terminal records are still readable when
	// the UI's last poll arrives.
	modelListStoreRetention = 2 * time.Minute
)

// ModelListStore is the contract every backend (in-memory single-node,
// Redis multi-node) must satisfy. Methods take a context so the Redis
// implementation can honour the heartbeat-side timeout that gates a
// slow shared store from stalling the rest of the heartbeat.
type ModelListStore interface {
	Create(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Get(ctx context.Context, id string) (*ModelListRequest, error)
	// HasPending is a cheap read-only probe used by the heartbeat hot path
	// to gate the side-effecting PopPending. A spurious "true" is fine —
	// PopPending handles "queue empty after probe" by returning nil.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Complete(ctx context.Context, id string, models []ModelEntry, supported bool) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// applyModelListTimeout transitions a request to ModelListTimeout when it has
// been stuck in a non-terminal state past its threshold. Returns true when
// the record was modified so callers can persist the change. The pending
// threshold catches "daemon never picked this up"; the running threshold
// catches "daemon picked it up but the result report was lost" — without
// the running escape, only retention sweep ends the polling loop.
func applyModelListTimeout(req *ModelListRequest, now time.Time) bool {
	switch req.Status {
	case ModelListPending:
		if now.Sub(req.CreatedAt) > modelListPendingTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not respond within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	case ModelListRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > modelListRunningTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

// InMemoryModelListStore is the single-node implementation. Adequate for
// self-hosted dev and the test suite, but unsafe in multi-node deploys
// (each replica gets its own map and the pending request is invisible to
// every replica that didn't receive the POST).
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
		if time.Since(req.CreatedAt) > modelListStoreRetention {
			delete(s.requests, id)
		}
	}

	now := time.Now()
	req := &ModelListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    ModelListPending,
		// Default to true; the daemon overrides this in the report
		// for providers that don't support per-agent model selection.
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
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

func (s *InMemoryModelListStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyModelListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryModelListStore) PopPending(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *ModelListRequest
	now := time.Now()
	for _, req := range s.requests {
		applyModelListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = ModelListRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
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

func modelListRequestTerminal(status ModelListStatus) bool {
	return status == ModelListCompleted || status == ModelListFailed || status == ModelListTimeout
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
		writeError(w, http.StatusInternalServerError, "failed to enqueue model list request: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// GetModelListRequest returns the status of a model list request.
func (h *Handler) GetModelListRequest(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")

	req, err := h.ModelListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
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

	// Fetch first so we can ignore stale reports for already-terminal
	// requests (e.g. the heartbeat response that triggered the daemon
	// run was a retry, and the original report already landed).
	existing, err := h.ModelListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if modelListRequestTerminal(existing.Status) {
		slog.Debug("ignoring stale model list report", "runtime_id", runtimeID, "request_id", requestID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

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
		if err := h.ModelListStore.Complete(r.Context(), requestID, body.Models, supported); err != nil {
			// Surface the store failure as 5xx so the daemon can retry instead
			// of swallowing the report (leaves the request stuck in running
			// until the server-side timeout, which is exactly the "looks OK
			// but nothing happens" class of bug we're trying to avoid).
			slog.Error("ModelListStore Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	} else {
		if err := h.ModelListStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("ModelListStore Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}

	slog.Debug("model list report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status, "count", len(body.Models))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
