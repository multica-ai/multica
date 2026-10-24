package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// McpProbeStatus is the lifecycle of one on-demand probe request.
// The result lives only as long as this request. It is not written back
// onto the library row (GH #7166: persisted last_probe is a later slice).
type McpProbeStatus string

const (
	McpProbePending   McpProbeStatus = "pending"
	McpProbeRunning   McpProbeStatus = "running"
	McpProbeCompleted McpProbeStatus = "completed"
	McpProbeFailed    McpProbeStatus = "failed"
	McpProbeTimeout   McpProbeStatus = "timeout"
)

const (
	McpProbeCodeNoRuntime            = protocol.McpProbeCodeNoRuntime
	McpProbeCodeUnsupportedDaemon    = protocol.McpProbeCodeUnsupportedDaemon
	McpProbeCodeTimeout              = protocol.McpProbeCodeTimeout
	McpProbeCodeCommandNotFound      = protocol.McpProbeCodeCommandNotFound
	McpProbeCodeUnauthorized         = protocol.McpProbeCodeUnauthorized
	McpProbeCodeTLS                  = protocol.McpProbeCodeTLS
	McpProbeCodeUnsupportedTransport = protocol.McpProbeCodeUnsupportedTransport
	McpProbeCodeHandshake            = protocol.McpProbeCodeHandshake
	McpProbeCodeInternal             = protocol.McpProbeCodeInternal
)

const (
	mcpProbePendingTimeout = 30 * time.Second
	mcpProbeRunningTimeout = 60 * time.Second
	mcpProbeStoreRetention = 2 * time.Minute
)

// McpProbeRequest is the pollable in-flight (or just-finished) probe.
type McpProbeRequest struct {
	ID           string         `json:"id"`
	WorkspaceID  string         `json:"workspace_id"`
	ServerID     string         `json:"server_id"`
	RuntimeID    string         `json:"runtime_id"`
	RuntimeName  string         `json:"runtime_name"`
	Status       McpProbeStatus `json:"status"`
	ErrorCode    string         `json:"error_code,omitempty"`
	Error        string         `json:"error,omitempty"`
	ElapsedMs    int64          `json:"elapsed_ms,omitempty"`
	Tools        []string       `json:"tools,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	RunStartedAt *time.Time     `json:"-"`
}

// McpProbeCreate is the store input for a newly queued probe.
type McpProbeCreate struct {
	WorkspaceID string
	ServerID    string
	RuntimeID   string
	RuntimeName string
}

// McpProbeReport is what the daemon posts back.
type McpProbeReport struct {
	Status    string   `json:"status"` // "completed" or "failed"
	ErrorCode string   `json:"error_code,omitempty"`
	Error     string   `json:"error,omitempty"`
	ElapsedMs int64    `json:"elapsed_ms,omitempty"`
	Tools     []string `json:"tools,omitempty"`
}

// McpProbeStore is the multi-replica contract for in-flight probes.
type McpProbeStore interface {
	Create(ctx context.Context, in McpProbeCreate) (*McpProbeRequest, error)
	Get(ctx context.Context, id string) (*McpProbeRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*McpProbeRequest, error)
	Complete(ctx context.Context, id string, tools []string, elapsedMs int64) error
	Fail(ctx context.Context, id string, code, errMsg string, elapsedMs int64) error
}

func applyMcpProbeTimeout(req *McpProbeRequest, now time.Time) bool {
	switch req.Status {
	case McpProbePending:
		if now.Sub(req.CreatedAt) > mcpProbePendingTimeout {
			req.Status = McpProbeTimeout
			req.ErrorCode = McpProbeCodeTimeout
			req.Error = "daemon did not respond within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	case McpProbeRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > mcpProbeRunningTimeout {
			req.Status = McpProbeTimeout
			req.ErrorCode = McpProbeCodeTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

func mcpProbeRequestTerminal(status McpProbeStatus) bool {
	return status == McpProbeCompleted || status == McpProbeFailed || status == McpProbeTimeout
}

// InMemoryMcpProbeStore is the single-node implementation.
type InMemoryMcpProbeStore struct {
	mu       sync.Mutex
	requests map[string]*McpProbeRequest
}

func NewInMemoryMcpProbeStore() *InMemoryMcpProbeStore {
	return &InMemoryMcpProbeStore{requests: make(map[string]*McpProbeRequest)}
}

func (s *InMemoryMcpProbeStore) Create(_ context.Context, in McpProbeCreate) (*McpProbeRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > mcpProbeStoreRetention {
			delete(s.requests, id)
		}
	}
	now := time.Now()
	req := &McpProbeRequest{
		ID:          randomID(),
		WorkspaceID: in.WorkspaceID,
		ServerID:    in.ServerID,
		RuntimeID:   in.RuntimeID,
		RuntimeName: in.RuntimeName,
		Status:      McpProbePending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.requests[req.ID] = req
	return cloneMcpProbeRequest(req), nil
}

func (s *InMemoryMcpProbeStore) Get(_ context.Context, id string) (*McpProbeRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	if time.Since(req.CreatedAt) > mcpProbeStoreRetention {
		delete(s.requests, id)
		return nil, nil
	}
	applyMcpProbeTimeout(req, time.Now())
	return cloneMcpProbeRequest(req), nil
}

func (s *InMemoryMcpProbeStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, req := range s.requests {
		applyMcpProbeTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == McpProbePending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryMcpProbeStore) PopPending(_ context.Context, runtimeID string) (*McpProbeRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldest *McpProbeRequest
	now := time.Now()
	for _, req := range s.requests {
		applyMcpProbeTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == McpProbePending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = McpProbeRunning
		started := now
		oldest.RunStartedAt = &started
		oldest.UpdatedAt = now
	}
	return cloneMcpProbeRequest(oldest), nil
}

func cloneMcpProbeRequest(req *McpProbeRequest) *McpProbeRequest {
	if req == nil {
		return nil
	}
	copy := *req
	copy.Tools = append([]string(nil), req.Tools...)
	if req.RunStartedAt != nil {
		started := *req.RunStartedAt
		copy.RunStartedAt = &started
	}
	return &copy
}

func (s *InMemoryMcpProbeStore) Complete(_ context.Context, id string, tools []string, elapsedMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[id]; ok {
		applyMcpProbeTimeout(req, time.Now())
		if mcpProbeRequestTerminal(req.Status) {
			return nil
		}
		req.Status = McpProbeCompleted
		req.Tools = append([]string(nil), tools...)
		req.ElapsedMs = elapsedMs
		req.ErrorCode = ""
		req.Error = ""
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryMcpProbeStore) Fail(_ context.Context, id string, code, errMsg string, elapsedMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[id]; ok {
		applyMcpProbeTimeout(req, time.Now())
		if mcpProbeRequestTerminal(req.Status) {
			return nil
		}
		req.Status = McpProbeFailed
		req.ErrorCode = sanitizeMcpProbeErrorCode(code)
		req.Error = sanitizeMcpProbeError(errMsg)
		req.ElapsedMs = elapsedMs
		req.UpdatedAt = time.Now()
	}
	return nil
}

func sanitizeMcpProbeErrorCode(code string) string {
	code = strings.TrimSpace(code)
	switch code {
	case McpProbeCodeNoRuntime, McpProbeCodeUnsupportedDaemon, McpProbeCodeTimeout,
		McpProbeCodeCommandNotFound, McpProbeCodeUnauthorized, McpProbeCodeTLS,
		McpProbeCodeUnsupportedTransport, McpProbeCodeHandshake, McpProbeCodeInternal:
		return code
	default:
		return McpProbeCodeInternal
	}
}

func sanitizeMcpProbeError(msg string) string {
	// Daemon-supplied diagnostics are untrusted even when they fit on one line.
	switch msg {
	case "probe timed out", "command not found", "unauthorized", "tls handshake failed",
		"unsupported transport", "unsupported protocol version", "initialize failed",
		"initialization failed", "tools/list failed", "failed to start server",
		"failed to load probe job", "daemon client is not configured", "server cleanup failed":
		return msg
	default:
		return "probe failed"
	}
}

// ProbeWorkspaceMcpServer queues a daemon-side handshake of one library entry.
// The caller must be a workspace owner/admin and the target must be an online
// daemon they own. A public runtime owned by someone else is not probeable
// in this slice (GH #7166).
func (h *Handler) ProbeWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	if h.McpProbeStore == nil {
		writeError(w, http.StatusInternalServerError, "probe store is not configured")
		return
	}

	server, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{
		ID:          serverUUID,
		WorkspaceID: idUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}

	var body struct {
		RuntimeID string `json:"runtime_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	callerID := requestUserID(r)
	rt, code, msg := h.resolveMcpProbeRuntime(r.Context(), idUUID, callerID, strings.TrimSpace(body.RuntimeID))
	if msg != "" {
		writeError(w, code, msg)
		return
	}
	if !runtimeHasClientCapability(rt, protocol.DaemonCapabilityMcpProbeV1) {
		writeError(w, http.StatusConflict, "this runtime's daemon does not support MCP probes; update the daemon")
		return
	}

	req, err := h.McpProbeStore.Create(r.Context(), McpProbeCreate{
		WorkspaceID: workspaceID,
		ServerID:    uuidToString(server.ID),
		RuntimeID:   uuidToString(rt.ID),
		RuntimeName: runtimeDisplayName(rt),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue MCP probe")
		return
	}
	h.requestDaemonPendingWork(req.RuntimeID, protocol.PendingWorkKindMcpProbe)
	writeJSON(w, http.StatusOK, req)
}

// GetWorkspaceMcpProbe returns the in-flight probe the caller started.
func (h *Handler) GetWorkspaceMcpProbe(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireWorkspaceMcpWriter(w, r, workspaceID) {
		return
	}
	serverID := chi.URLParam(r, "serverId")
	serverUUID, ok := parseUUIDOrBadRequest(w, serverID, "server id")
	if !ok {
		return
	}
	requestID := chi.URLParam(r, "requestId")
	if h.McpProbeStore == nil {
		writeError(w, http.StatusInternalServerError, "probe store is not configured")
		return
	}
	req, err := h.McpProbeStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request")
		return
	}
	if req == nil || req.WorkspaceID != workspaceID || req.ServerID != serverID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime id")
	if !ok {
		return
	}
	rt, err := h.getAgentRuntime(r.Context(), obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeUUID)
	if err != nil || uuidToString(rt.WorkspaceID) != workspaceID || !runtimeOwnedBy(rt, requestUserID(r)) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	server, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{
		ID: serverUUID, WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if server.UpdatedAt.Time.After(req.CreatedAt) {
		writeError(w, http.StatusConflict, "MCP configuration changed; run the test again")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// requireDaemonMcpProbeCaller allows mdt_ and PAT (how daemons auth today)
// and 404s browser JWT / direct handler calls so write-only config never
// leaves on a user session.
func (h *Handler) requireDaemonMcpProbeCaller(w http.ResponseWriter, r *http.Request) bool {
	if middleware.DaemonIDFromContext(r.Context()) != "" {
		return true
	}
	switch middleware.DaemonAuthPathFromContext(r.Context()) {
	case middleware.DaemonAuthPathPAT, middleware.DaemonAuthPathCloudPAT, middleware.DaemonAuthPathDaemonToken:
		return true
	default:
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
}

// GetDaemonMcpProbeJob returns the write-only config to the daemon that will
// run the handshake. Browser JWT is rejected here; live daemons still
// authenticate with PAT or mdt_ on /api/daemon.
func (h *Handler) GetDaemonMcpProbeJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireDaemonMcpProbeCaller(w, r) {
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok || !mcpProbeDaemonOwnsRuntime(r, rt) {
		if ok {
			writeError(w, http.StatusNotFound, "runtime not found")
		}
		return
	}
	requestID := chi.URLParam(r, "requestId")
	if h.McpProbeStore == nil {
		writeError(w, http.StatusInternalServerError, "probe store is not configured")
		return
	}
	req, err := h.McpProbeStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request")
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if mcpProbeRequestTerminal(req.Status) {
		writeError(w, http.StatusConflict, "probe has already finished")
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, req.ServerID, "server id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace id")
	if !ok {
		return
	}
	server, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{
		ID:          serverUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if server.UpdatedAt.Time.After(req.CreatedAt) {
		writeError(w, http.StatusConflict, "MCP configuration changed; run the test again")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":  req.ID,
		"server_name": server.Name,
		"config":      json.RawMessage(server.Config),
	})
}

// ReportDaemonMcpProbeResult receives the handshake outcome from the daemon.
func (h *Handler) ReportDaemonMcpProbeResult(w http.ResponseWriter, r *http.Request) {
	if !h.requireDaemonMcpProbeCaller(w, r) {
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok || !mcpProbeDaemonOwnsRuntime(r, rt) {
		if ok {
			writeError(w, http.StatusNotFound, "runtime not found")
		}
		return
	}
	requestID := chi.URLParam(r, "requestId")
	if h.McpProbeStore == nil {
		writeError(w, http.StatusInternalServerError, "probe store is not configured")
		return
	}
	existing, err := h.McpProbeStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request")
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if mcpProbeRequestTerminal(existing.Status) {
		slog.Debug("ignoring stale mcp probe report", "runtime_id", runtimeID, "request_id", requestID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body McpProbeReport
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Status != "completed" && body.Status != "failed" {
		writeError(w, http.StatusBadRequest, "invalid probe status")
		return
	}
	if body.ElapsedMs < 0 {
		writeError(w, http.StatusBadRequest, "invalid elapsed_ms")
		return
	}

	if body.Status == "completed" {
		if err := h.McpProbeStore.Complete(r.Context(), requestID, sanitizeMcpProbeTools(body.Tools), body.ElapsedMs); err != nil {
			slog.Error("McpProbeStore Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	} else {
		if err := h.McpProbeStore.Fail(r.Context(), requestID, body.ErrorCode, body.Error, body.ElapsedMs); err != nil {
			slog.Error("McpProbeStore Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func mcpProbeDaemonOwnsRuntime(r *http.Request, rt db.AgentRuntime) bool {
	if daemonID := middleware.DaemonIDFromContext(r.Context()); daemonID != "" {
		return rt.DaemonID.Valid && rt.DaemonID.String == daemonID
	}
	return runtimeOwnedBy(rt, requestUserID(r))
}

var mcpProbeToolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func sanitizeMcpProbeTools(tools []string) []string {
	out := make([]string, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, name := range tools {
		name = strings.TrimSpace(name)
		if !mcpProbeToolName.MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
		if len(out) >= 1000 {
			break
		}
	}
	return out
}

func (h *Handler) resolveMcpProbeRuntime(ctx context.Context, workspaceID pgtype.UUID, callerID, runtimeID string) (db.AgentRuntime, int, string) {
	if runtimeID != "" {
		runtimeUUID, ok := parseUUIDSoft(runtimeID)
		if !ok {
			return db.AgentRuntime{}, http.StatusBadRequest, "invalid runtime_id"
		}
		rt, loadErr := h.getAgentRuntime(ctx, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeUUID)
		if loadErr != nil || uuidToString(rt.WorkspaceID) != uuidToString(workspaceID) || !runtimeOwnedBy(rt, callerID) {
			return db.AgentRuntime{}, http.StatusNotFound, "runtime not found"
		}
		if rt.Status != "online" {
			return db.AgentRuntime{}, http.StatusServiceUnavailable, "runtime is offline"
		}
		return rt, 0, ""
	}

	runtimes, listErr := h.Queries.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     parseUUID(callerID),
	})
	if listErr != nil {
		return db.AgentRuntime{}, http.StatusInternalServerError, "failed to list runtimes"
	}
	online := make([]db.AgentRuntime, 0, 1)
	for _, rt := range runtimes {
		if rt.Status == "online" && runtimeOwnedBy(rt, callerID) {
			online = append(online, rt)
		}
	}
	switch len(online) {
	case 0:
		return db.AgentRuntime{}, http.StatusServiceUnavailable, "no online runtime you own"
	case 1:
		return online[0], 0, ""
	default:
		return db.AgentRuntime{}, http.StatusConflict, "runtime_id is required when more than one of your runtimes is online"
	}
}

func parseUUIDSoft(s string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return id, true
}

func runtimeOwnedBy(rt db.AgentRuntime, userID string) bool {
	return userID != "" && rt.OwnerID.Valid && uuidToString(rt.OwnerID) == userID
}

func runtimeDisplayName(rt db.AgentRuntime) string {
	if rt.CustomName.Valid && strings.TrimSpace(rt.CustomName.String) != "" {
		return rt.CustomName.String
	}
	return rt.Name
}

func runtimeHasClientCapability(rt db.AgentRuntime, cap string) bool {
	if len(rt.Metadata) == 0 {
		return false
	}
	var meta struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rt.Metadata, &meta); err != nil {
		return false
	}
	for _, c := range meta.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
