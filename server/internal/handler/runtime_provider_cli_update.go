package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type ProviderCLIUpdateRequest struct {
	ID              string       `json:"id"`
	RuntimeID       string       `json:"runtime_id"`
	Status          UpdateStatus `json:"status"`
	Provider        string       `json:"provider"`
	Mode            string       `json:"mode"`
	TargetVersion   string       `json:"target_version,omitempty"`
	RollbackVersion string       `json:"rollback_version,omitempty"`
	Output          string       `json:"output,omitempty"`
	Error           string       `json:"error,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	RunStartedAt    *time.Time   `json:"-"`
}

type ProviderCLIUpdateStore interface {
	Create(ctx context.Context, runtimeID, provider, mode, targetVersion, rollbackVersion string) (*ProviderCLIUpdateRequest, error)
	Get(ctx context.Context, id string) (*ProviderCLIUpdateRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*ProviderCLIUpdateRequest, error)
	Complete(ctx context.Context, id string, output string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// InMemoryProviderCLIUpdateStore is single-node only. Before provider CLI
// apply smoke in a multi-node server deployment, wire a Redis-backed store
// equivalent to RedisUpdateStore so POST, heartbeat, poll, and result reports
// cannot land on different API nodes with divergent state.
type InMemoryProviderCLIUpdateStore struct {
	mu       sync.Mutex
	requests map[string]*ProviderCLIUpdateRequest
}

func NewInMemoryProviderCLIUpdateStore() *InMemoryProviderCLIUpdateStore {
	return &InMemoryProviderCLIUpdateStore{requests: make(map[string]*ProviderCLIUpdateRequest)}
}

func (s *InMemoryProviderCLIUpdateStore) Create(_ context.Context, runtimeID, provider, mode, targetVersion, rollbackVersion string) (*ProviderCLIUpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, req := range s.requests {
		if now.Sub(req.CreatedAt) > updateStoreRetention {
			delete(s.requests, id)
		}
	}
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && (req.Status == UpdatePending || req.Status == UpdateRunning) {
			return nil, errUpdateInProgress
		}
	}
	req := &ProviderCLIUpdateRequest{
		ID:              randomID(),
		RuntimeID:       runtimeID,
		Status:          UpdatePending,
		Provider:        provider,
		Mode:            mode,
		TargetVersion:   targetVersion,
		RollbackVersion: rollbackVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryProviderCLIUpdateStore) Get(_ context.Context, id string) (*ProviderCLIUpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	if applyProviderCLIUpdateTimeout(req, time.Now()) {
		// State already mutated under lock.
	}
	return req, nil
}

func (s *InMemoryProviderCLIUpdateStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, req := range s.requests {
		applyProviderCLIUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryProviderCLIUpdateStore) PopPending(_ context.Context, runtimeID string) (*ProviderCLIUpdateRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldest *ProviderCLIUpdateRequest
	now := time.Now()
	for _, req := range s.requests {
		applyProviderCLIUpdateTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == UpdatePending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = UpdateRunning
		started := now
		oldest.RunStartedAt = &started
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryProviderCLIUpdateStore) Complete(_ context.Context, id string, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[id]; ok {
		req.Status = UpdateCompleted
		req.Output = output
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryProviderCLIUpdateStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[id]; ok {
		req.Status = UpdateFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

func applyProviderCLIUpdateTimeout(req *ProviderCLIUpdateRequest, now time.Time) bool {
	switch req.Status {
	case UpdatePending:
		if now.Sub(req.CreatedAt) > updatePendingTimeout {
			req.Status = UpdateTimeout
			req.Error = "daemon did not respond within 120 seconds"
			req.UpdatedAt = now
			return true
		}
	case UpdateRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > updateRunningTimeout {
			req.Status = UpdateTimeout
			req.Error = "provider CLI update did not complete within 150 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

func normalizeProviderCLIUpdateMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "dry-run", "dry_run", "dryrun", "plan":
		return "dry-run", true
	case "apply":
		return "apply", true
	default:
		return "", false
	}
}

func (h *Handler) InitiateProviderCLIUpdate(w http.ResponseWriter, r *http.Request) {
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
	if !h.cfg.ProviderCLIUpdateControlEnabled {
		writeError(w, http.StatusForbidden, "provider CLI update control requires MULTICA_PROVIDER_CLI_UPDATE_CONTROL_SINGLE_NODE=true until a Redis-backed store is implemented")
		return
	}
	var req struct {
		Provider        string `json:"provider"`
		Mode            string `json:"mode"`
		TargetVersion   string `json:"target_version"`
		RollbackVersion string `json:"rollback_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if runtimeProvider := strings.ToLower(strings.TrimSpace(rt.Provider)); provider != runtimeProvider {
		writeError(w, http.StatusBadRequest, "provider must match runtime provider")
		return
	}
	mode, ok := normalizeProviderCLIUpdateMode(req.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "mode must be dry-run or apply")
		return
	}
	update, err := h.ProviderCLIUpdateStore.Create(r.Context(), uuidToString(rt.ID), provider, mode, strings.TrimSpace(req.TargetVersion), strings.TrimSpace(req.RollbackVersion))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, update)
}

func (h *Handler) GetProviderCLIUpdate(w http.ResponseWriter, r *http.Request) {
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
	updateID := chi.URLParam(r, "updateId")
	update, err := h.ProviderCLIUpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider CLI update: "+err.Error())
		return
	}
	if update == nil || update.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "provider CLI update not found")
		return
	}
	writeJSON(w, http.StatusOK, update)
}

func (h *Handler) ReportProviderCLIUpdateResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}
	updateID := chi.URLParam(r, "updateId")
	existing, err := h.ProviderCLIUpdateStore.Get(r.Context(), updateID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load provider CLI update: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "provider CLI update not found")
		return
	}
	if updateRequestTerminal(existing.Status) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	var req struct {
		Status string `json:"status"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Status {
	case "completed":
		if err := h.ProviderCLIUpdateStore.Complete(r.Context(), updateID, req.Output); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	case "failed":
		if err := h.ProviderCLIUpdateStore.Fail(r.Context(), updateID, req.Error); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	case "running":
		// Status is already running after heartbeat PopPending.
	default:
		writeError(w, http.StatusBadRequest, "invalid status: "+req.Status)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
