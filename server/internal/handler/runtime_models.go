package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// In-memory model-list request store
// ---------------------------------------------------------------------------
//
// The server cannot call the daemon directly (the daemon is behind the user's
// NAT and only polls the server). So "list models for this runtime" uses the
// same pattern as PingStore: server creates a pending request, daemon pops it
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

// ModelListStore is a thread-safe in-memory store. Entries expire after 2 min
// to bound memory use; the UI polls /requests/:id until status is terminal.
type ModelListStore struct {
	mu       sync.Mutex
	requests map[string]*ModelListRequest
}

func NewModelListStore() *ModelListStore {
	return &ModelListStore{requests: make(map[string]*ModelListRequest)}
}

func (s *ModelListStore) Create(runtimeID string) *ModelListRequest {
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
		// Default to true; the daemon overrides this in the report
		// for providers that don't support per-agent model selection.
		Supported: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.requests[req.ID] = req
	return req
}

func (s *ModelListStore) Get(id string) *ModelListRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	if req.Status == ModelListPending && time.Since(req.CreatedAt) > 30*time.Second {
		req.Status = ModelListTimeout
		req.Error = "daemon did not respond within 30 seconds"
		req.UpdatedAt = time.Now()
	}
	return req
}

// PopPending returns and marks-running the oldest pending request for a runtime.
func (s *ModelListStore) PopPending(runtimeID string) *ModelListRequest {
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
	return oldest
}

func (s *ModelListStore) Complete(id string, models []ModelEntry, supported bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListCompleted
		req.Models = models
		req.Supported = supported
		req.UpdatedAt = time.Now()
	}
}

func (s *ModelListStore) Fail(id string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiateListModels creates a pending model list request for a runtime.
// Called by the frontend; the daemon picks it up on its next heartbeat.
func (h *Handler) InitiateListModels(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
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

	req := h.ModelListStore.Create(runtimeID)
	writeJSON(w, http.StatusOK, req)
}

// GetModelListRequest returns the status of a model list request.
func (h *Handler) GetModelListRequest(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")

	req := h.ModelListStore.Get(requestID)
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
		h.ModelListStore.Complete(requestID, body.Models, supported)
	} else {
		h.ModelListStore.Fail(requestID, body.Error)
	}

	slog.Debug("model list report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status, "count", len(body.Models))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
