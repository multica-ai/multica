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
// Quota check store
// ---------------------------------------------------------------------------
//
// Uses the same pending-request pattern as ModelListStore: the frontend POSTs
// to initiate a check, the daemon pops the request on its next heartbeat,
// probes the provider API for rate-limit headers, and reports the result back.

// QuotaCheckStatus tracks the lifecycle of a quota check request.
type QuotaCheckStatus string

const (
	QuotaCheckPending   QuotaCheckStatus = "pending"
	QuotaCheckRunning   QuotaCheckStatus = "running"
	QuotaCheckCompleted QuotaCheckStatus = "completed"
	QuotaCheckFailed    QuotaCheckStatus = "failed"
	QuotaCheckTimeout   QuotaCheckStatus = "timeout"
)

// QuotaWindow describes the remaining capacity in a single rate-limit window.
type QuotaWindow struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetsAt  time.Time `json:"resets_at"`
}

// QuotaCheckRequest holds the lifecycle state of a quota check for one runtime.
type QuotaCheckRequest struct {
	ID           string           `json:"id"`
	RuntimeID    string           `json:"runtime_id"`
	Provider     string           `json:"provider"`
	Status       QuotaCheckStatus `json:"status"`
	RateRequests *QuotaWindow     `json:"rate_requests,omitempty"`
	RateTokens   *QuotaWindow     `json:"rate_tokens,omitempty"`
	CreditsLimit *int             `json:"credits_limit,omitempty"`
	Error        string           `json:"error,omitempty"`
	FetchedAt    *time.Time       `json:"fetched_at,omitempty"`
	Stale        bool             `json:"stale"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	RunStartedAt *time.Time       `json:"-"`
}

const (
	quotaCheckPendingTimeout = 30 * time.Second
	quotaCheckRunningTimeout = 30 * time.Second
	quotaCheckStoreRetention = 10 * time.Minute
	quotaCheckStaleThreshold = 5 * time.Minute
)

// QuotaCheckStore is the contract for storing quota check requests.
type QuotaCheckStore interface {
	Create(ctx context.Context, runtimeID string) (*QuotaCheckRequest, error)
	Get(ctx context.Context, id string) (*QuotaCheckRequest, error)
	GetLastByRuntime(ctx context.Context, runtimeID string) (*QuotaCheckRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*QuotaCheckRequest, error)
	Complete(ctx context.Context, id string, result QuotaCheckResult) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// QuotaCheckResult is the payload reported by the daemon.
type QuotaCheckResult struct {
	Provider     string       `json:"provider"`
	RateRequests *QuotaWindow `json:"rate_requests,omitempty"`
	RateTokens   *QuotaWindow `json:"rate_tokens,omitempty"`
	CreditsLimit *int         `json:"credits_limit,omitempty"`
	Error        string       `json:"error,omitempty"`
}

func applyQuotaCheckTimeout(req *QuotaCheckRequest, now time.Time) bool {
	switch req.Status {
	case QuotaCheckPending:
		if now.Sub(req.CreatedAt) > quotaCheckPendingTimeout {
			req.Status = QuotaCheckTimeout
			req.Error = "daemon did not respond within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	case QuotaCheckRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > quotaCheckRunningTimeout {
			req.Status = QuotaCheckTimeout
			req.Error = "daemon did not finish within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

func quotaCheckTerminal(s QuotaCheckStatus) bool {
	return s == QuotaCheckCompleted || s == QuotaCheckFailed || s == QuotaCheckTimeout
}

// ---------------------------------------------------------------------------
// InMemoryQuotaCheckStore
// ---------------------------------------------------------------------------

type InMemoryQuotaCheckStore struct {
	mu       sync.Mutex
	requests map[string]*QuotaCheckRequest
	// last completed result per runtimeID so GetLastByRuntime can return
	// stale data even after the request is GC'd.
	lastByRuntime map[string]*QuotaCheckRequest
}

func NewInMemoryQuotaCheckStore() *InMemoryQuotaCheckStore {
	return &InMemoryQuotaCheckStore{
		requests:      make(map[string]*QuotaCheckRequest),
		lastByRuntime: make(map[string]*QuotaCheckRequest),
	}
}

func (s *InMemoryQuotaCheckStore) Create(_ context.Context, runtimeID string) (*QuotaCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > quotaCheckStoreRetention {
			delete(s.requests, id)
		}
	}

	now := time.Now()
	req := &QuotaCheckRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    QuotaCheckPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryQuotaCheckStore) Get(_ context.Context, id string) (*QuotaCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyQuotaCheckTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryQuotaCheckStore) GetLastByRuntime(_ context.Context, runtimeID string) (*QuotaCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prefer an in-flight (or recently completed) request from the main map.
	var best *QuotaCheckRequest
	now := time.Now()
	for _, req := range s.requests {
		if req.RuntimeID != runtimeID {
			continue
		}
		applyQuotaCheckTimeout(req, now)
		if best == nil || req.CreatedAt.After(best.CreatedAt) {
			best = req
		}
	}
	if best != nil {
		return withStaleFlag(best, now), nil
	}

	// Fall back to the last completed snapshot.
	if last, ok := s.lastByRuntime[runtimeID]; ok {
		return withStaleFlag(last, now), nil
	}
	return nil, nil
}

func withStaleFlag(req *QuotaCheckRequest, now time.Time) *QuotaCheckRequest {
	copy := *req
	if req.FetchedAt != nil {
		copy.Stale = now.Sub(*req.FetchedAt) > quotaCheckStaleThreshold
	}
	return &copy
}

func (s *InMemoryQuotaCheckStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyQuotaCheckTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == QuotaCheckPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryQuotaCheckStore) PopPending(_ context.Context, runtimeID string) (*QuotaCheckRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *QuotaCheckRequest
	now := time.Now()
	for _, req := range s.requests {
		applyQuotaCheckTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == QuotaCheckPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = QuotaCheckRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryQuotaCheckStore) Complete(_ context.Context, id string, result QuotaCheckResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	now := time.Now()
	req.Status = QuotaCheckCompleted
	req.Provider = result.Provider
	req.RateRequests = result.RateRequests
	req.RateTokens = result.RateTokens
	req.CreditsLimit = result.CreditsLimit
	req.Error = result.Error
	req.FetchedAt = &now
	req.UpdatedAt = now
	// Keep a snapshot so GetLastByRuntime can serve it after GC.
	snapshot := *req
	s.lastByRuntime[req.RuntimeID] = &snapshot
	return nil
}

func (s *InMemoryQuotaCheckStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = QuotaCheckFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

// InitiateQuotaCheck creates a pending quota check request for a runtime.
func (h *Handler) InitiateQuotaCheck(w http.ResponseWriter, r *http.Request) {
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

	req, err := h.QuotaCheckStore.Create(r.Context(), uuidToString(rt.ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue quota check: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// GetRuntimeQuota returns the latest known quota state for a runtime.
func (h *Handler) GetRuntimeQuota(w http.ResponseWriter, r *http.Request) {
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

	result, err := h.QuotaCheckStore.GetLastByRuntime(r.Context(), uuidToString(rt.ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load quota: "+err.Error())
		return
	}
	if result == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "no_data"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ReportQuotaCheckResult receives the quota probe result from the daemon.
func (h *Handler) ReportQuotaCheckResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")

	existing, err := h.QuotaCheckStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if quotaCheckTerminal(existing.Status) {
		slog.Debug("ignoring stale quota check report", "runtime_id", runtimeID, "request_id", requestID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status string          `json:"status"` // "completed" or "failed"
		Result QuotaCheckResult `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status == "completed" {
		if err := h.QuotaCheckStore.Complete(r.Context(), requestID, body.Result); err != nil {
			slog.Error("QuotaCheckStore Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	} else {
		if err := h.QuotaCheckStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("QuotaCheckStore Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}

	slog.Debug("quota check report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
