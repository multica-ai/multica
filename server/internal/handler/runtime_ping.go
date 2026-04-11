package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// In-memory ping store
// ---------------------------------------------------------------------------

// PingStatus represents the lifecycle of a runtime ping test.
type PingStatus string

const (
	PingPending   PingStatus = "pending"
	PingRunning   PingStatus = "running"
	PingCompleted PingStatus = "completed"
	PingFailed    PingStatus = "failed"
	PingTimeout   PingStatus = "timeout"
)

// PingRequest represents a pending or completed ping test.
type PingRequest struct {
	ID        string     `json:"id"`
	RuntimeID string     `json:"runtime_id"`
	Status    PingStatus `json:"status"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
	DurationMs int64    `json:"duration_ms,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// PingStore is a thread-safe in-memory store for ping requests.
// Pings expire after 2 minutes.
type PingStore struct {
	mu    sync.Mutex
	pings map[string]*PingRequest // keyed by ping ID
}

func NewPingStore() *PingStore {
	return &PingStore{
		pings: make(map[string]*PingRequest),
	}
}

func (s *PingStore) Create(runtimeID string) *PingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up old pings for this runtime
	for id, p := range s.pings {
		if time.Since(p.CreatedAt) > 2*time.Minute {
			delete(s.pings, id)
		}
	}

	ping := &PingRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    PingPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.pings[ping.ID] = ping
	return ping
}

func (s *PingStore) Get(id string) *PingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pings[id]
	if !ok {
		return nil
	}
	// Check for timeout
	if p.Status == PingPending && time.Since(p.CreatedAt) > 60*time.Second {
		p.Status = PingTimeout
		p.Error = "daemon did not respond within 60 seconds"
		p.UpdatedAt = time.Now()
	}
	return p
}

// PopPending returns and removes the oldest pending ping for a runtime.
func (s *PingStore) PopPending(runtimeID string) *PingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *PingRequest
	for _, p := range s.pings {
		if p.RuntimeID == runtimeID && p.Status == PingPending {
			if oldest == nil || p.CreatedAt.Before(oldest.CreatedAt) {
				oldest = p
			}
		}
	}
	if oldest != nil {
		oldest.Status = PingRunning
		oldest.UpdatedAt = time.Now()
	}
	return oldest
}

func (s *PingStore) Complete(id string, output string, durationMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pings[id]; ok {
		p.Status = PingCompleted
		p.Output = output
		p.DurationMs = durationMs
		p.UpdatedAt = time.Now()
	}
}

func (s *PingStore) Fail(id string, errMsg string, durationMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pings[id]; ok {
		p.Status = PingFailed
		p.Error = errMsg
		p.DurationMs = durationMs
		p.UpdatedAt = time.Now()
	}
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiatePing creates a new ping request (protected route, called by frontend).
func (h *Handler) InitiatePing(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}

	ping := h.PingStore.Create(runtimeID)
	writeJSON(w, http.StatusOK, ping)
}

// GetPing returns the status of a ping request (protected route, called by frontend).
func (h *Handler) GetPing(w http.ResponseWriter, r *http.Request) {
	pingID := chi.URLParam(r, "pingId")

	ping := h.PingStore.Get(pingID)
	if ping == nil {
		writeError(w, http.StatusNotFound, "ping not found")
		return
	}

	writeJSON(w, http.StatusOK, ping)
}

// ReportPingResult receives the ping result from the daemon.
func (h *Handler) ReportPingResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	// Verify the caller owns this runtime's workspace.
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	pingID := chi.URLParam(r, "pingId")

	var req struct {
		Status     string `json:"status"` // "completed" or "failed"
		Output     string `json:"output"`
		Error      string `json:"error"`
		DurationMs int64  `json:"duration_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Status == "completed" {
		h.PingStore.Complete(pingID, req.Output, req.DurationMs)
	} else {
		h.PingStore.Fail(pingID, req.Error, req.DurationMs)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
