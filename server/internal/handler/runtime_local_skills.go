package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type RuntimeLocalSkillRequestStatus string

const (
	RuntimeLocalSkillPending   RuntimeLocalSkillRequestStatus = "pending"
	RuntimeLocalSkillRunning   RuntimeLocalSkillRequestStatus = "running"
	RuntimeLocalSkillCompleted RuntimeLocalSkillRequestStatus = "completed"
	RuntimeLocalSkillFailed    RuntimeLocalSkillRequestStatus = "failed"
	RuntimeLocalSkillTimeout   RuntimeLocalSkillRequestStatus = "timeout"
	// RuntimeLocalSkillConflict is a terminal state set when a fresh import
	// hits an existing same-name skill. It is not an error: the request carries
	// structured Conflict metadata so the caller (Desktop UI / CLI) can offer
	// overwrite / rename / skip instead of silently failing. See MUL-2800.
	RuntimeLocalSkillConflict RuntimeLocalSkillRequestStatus = "conflict"
)

// LocalSkillImportAction selects how a runtime-local-skill import resolves when
// a skill with the same name already exists in the workspace.
type LocalSkillImportAction string

const (
	// LocalSkillImportActionCreate is the default: create a new skill, and
	// surface a structured `conflict` if the name is already taken.
	LocalSkillImportActionCreate LocalSkillImportAction = ""
	// LocalSkillImportActionOverwrite re-imports onto an existing skill,
	// identified by TargetSkillID. Only the skill's creator may overwrite.
	LocalSkillImportActionOverwrite LocalSkillImportAction = "overwrite"
)

// LocalSkillImportConflict is the structured result attached to a request that
// terminated in RuntimeLocalSkillConflict. CanOverwrite reflects the
// creator-only re-import policy (canOverwriteSkillByLocalImport).
type LocalSkillImportConflict struct {
	ExistingSkillID   string `json:"existing_skill_id"`
	ExistingCreatedBy string `json:"existing_created_by,omitempty"`
	CanOverwrite      bool   `json:"can_overwrite"`
}

const (
	// runtimeLocalSkillPendingTimeout bounds how long a request can sit in
	// pending before the server marks it timed out. The value must accommodate
	// old daemons that don't support batch import: they pop only one import
	// per heartbeat cycle (~15s). With maxLocalSkillImportBatch=10, the 10th
	// queued import waits up to 10×15s = 150s before being claimed. 3 minutes
	// gives a comfortable margin.
	//
	// Timeout invariant: IMPORT_CONCURRENCY (views/.../runtime-local-skill-import-panel.tsx)
	// × heartbeat period (~15s) ≤ runtimeLocalSkillPendingTimeout, and
	// IMPORT_POLL_TIMEOUT_MS (core/runtimes/local-skills.ts) must exceed
	// runtimeLocalSkillPendingTimeout + runtimeLocalSkillRunningTimeout.
	// See also maxLocalSkillImportBatch in daemon.go.
	runtimeLocalSkillPendingTimeout = 3 * time.Minute
	runtimeLocalSkillRunningTimeout = 60 * time.Second
	runtimeLocalSkillStoreRetention = 5 * time.Minute
)

// LocalSkillListStore tracks pending / running / completed runtime-local-skill
// inventory requests. The server MUST stay stateless — any state that needs to
// outlive a single request has to live in shared storage so multi-node deploys
// can have POST, heartbeat and poll land on different nodes and still agree
// on the request's state.
type LocalSkillListStore interface {
	Create(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error)
	Get(ctx context.Context, id string) (*RuntimeLocalSkillListRequest, error)
	// HasPending is a cheap read-only probe that reports whether the runtime
	// has at least one pending request. Callers on the hot path (e.g. the
	// heartbeat handler) use it to gate the side-effecting PopPending so they
	// never start a claim they might have to abort.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error)
	Complete(ctx context.Context, id string, skills []RuntimeLocalSkillSummary, supported bool, mcpServers []RuntimeLocalMcpServerSummary, mcpSupported bool) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// LocalSkillImportRequestInput carries the fields needed to enqueue a
// runtime-local-skill import. SupportsConflict gates the structured-conflict
// contract: only clients that opt in receive the `conflict` terminal status;
// older clients keep the legacy `failed` ("a skill with this name already
// exists") behavior so an already-installed Desktop build doesn't regress when
// it talks to an upgraded backend. See MUL-2800.
type LocalSkillImportRequestInput struct {
	RuntimeID        string
	CreatorID        string
	SkillKey         string
	Name             *string
	Description      *string
	Action           LocalSkillImportAction
	TargetSkillID    string
	SupportsConflict bool
}

// LocalSkillImportStore is the same contract as LocalSkillListStore but for
// runtime-local-skill import requests. Kept as a separate interface because the
// Create signature carries import-specific fields (skill_key, optional rename).
type LocalSkillImportStore interface {
	Create(ctx context.Context, input LocalSkillImportRequestInput) (*RuntimeLocalSkillImportRequest, error)
	Get(ctx context.Context, id string) (*RuntimeLocalSkillImportRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error)
	// PopPendingBatch claims up to limit pending requests atomically and
	// transitions them to running. Used by the heartbeat handler to deliver
	// multiple imports per heartbeat cycle.
	PopPendingBatch(ctx context.Context, runtimeID string, limit int) ([]*RuntimeLocalSkillImportRequest, error)
	Complete(ctx context.Context, id string, skill SkillWithFilesResponse) error
	// Conflict transitions a request to the terminal RuntimeLocalSkillConflict
	// state, attaching structured conflict metadata for the caller to act on.
	Conflict(ctx context.Context, id string, info LocalSkillImportConflict) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// applyLocalSkillListTimeout transitions a request into the timeout terminal
// state if it has been pending / running past the configured thresholds.
// Returns true when the record was modified so callers can persist the change.
func applyLocalSkillListTimeout(req *RuntimeLocalSkillListRequest, now time.Time) bool {
	switch req.Status {
	case RuntimeLocalSkillPending:
		if now.Sub(req.CreatedAt) > runtimeLocalSkillPendingTimeout {
			req.Status = RuntimeLocalSkillTimeout
			req.Error = "daemon did not respond within 3 minutes"
			req.UpdatedAt = now
			return true
		}
	case RuntimeLocalSkillRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > runtimeLocalSkillRunningTimeout {
			req.Status = RuntimeLocalSkillTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

func applyLocalSkillImportTimeout(req *RuntimeLocalSkillImportRequest, now time.Time) bool {
	switch req.Status {
	case RuntimeLocalSkillPending:
		if now.Sub(req.CreatedAt) > runtimeLocalSkillPendingTimeout {
			req.Status = RuntimeLocalSkillTimeout
			req.Error = "daemon did not respond within 3 minutes"
			req.UpdatedAt = now
			return true
		}
	case RuntimeLocalSkillRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > runtimeLocalSkillRunningTimeout {
			req.Status = RuntimeLocalSkillTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

type RuntimeLocalSkillSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourcePath  string `json:"source_path"`
	Provider    string `json:"provider"`
	// Root classifies the discovery root the daemon found this skill under:
	// "provider" (the runtime's own skill directory, e.g. ~/.claude/skills),
	// "universal" (the cross-tool ~/.agents/skills fallback), or "plugin"
	// for a skill contributed by an enabled runtime plugin. Daemons
	// that predate multi-root discovery omit it; an empty value means
	// "unknown" and the UI should not assert either origin.
	Root      string `json:"root,omitempty"`
	Plugin    string `json:"plugin,omitempty"`
	FileCount int    `json:"file_count"`
}

// RuntimeLocalMcpServerSummary is deliberately non-secret. The daemon only
// reports the configured server name, transport, and config scope; command
// arguments, URLs, headers, and environment values never leave the machine.
type RuntimeLocalMcpServerSummary struct {
	Name      string `json:"name"`
	Transport string `json:"transport,omitempty"`
	Source    string `json:"source,omitempty"`
	Enabled   bool   `json:"enabled"`
}

type RuntimeLocalSkillListRequest struct {
	ID           string                         `json:"id"`
	RuntimeID    string                         `json:"runtime_id"`
	Status       RuntimeLocalSkillRequestStatus `json:"status"`
	Skills       []RuntimeLocalSkillSummary     `json:"skills,omitempty"`
	Supported    bool                           `json:"supported"`
	McpServers   []RuntimeLocalMcpServerSummary `json:"mcp_servers,omitempty"`
	McpSupported bool                           `json:"mcp_supported"`
	Error        string                         `json:"error,omitempty"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	RunStartedAt *time.Time                     `json:"-"`
}

type RuntimeLocalSkillImportRequest struct {
	ID            string                 `json:"id"`
	RuntimeID     string                 `json:"runtime_id"`
	SkillKey      string                 `json:"skill_key"`
	Name          *string                `json:"name,omitempty"`
	Description   *string                `json:"description,omitempty"`
	Action        LocalSkillImportAction `json:"action,omitempty"`
	TargetSkillID string                 `json:"target_skill_id,omitempty"`
	// SupportsConflict records whether the initiating client opted into the
	// structured-conflict contract; consulted at report time to decide between
	// the new `conflict` status and the legacy `failed` behavior.
	SupportsConflict bool                           `json:"supports_conflict,omitempty"`
	Status           RuntimeLocalSkillRequestStatus `json:"status"`
	Skill            *SkillWithFilesResponse        `json:"skill,omitempty"`
	Conflict         *LocalSkillImportConflict      `json:"conflict,omitempty"`
	Error            string                         `json:"error,omitempty"`
	CreatedAt        time.Time                      `json:"created_at"`
	UpdatedAt        time.Time                      `json:"updated_at"`
	CreatorID        string                         `json:"-"`
	RunStartedAt     *time.Time                     `json:"-"`
}

// InMemoryLocalSkillListStore is the single-node implementation — good enough
// for local dev and the in-process test suite. Production (multi-node) must
// use RedisLocalSkillListStore so every API node agrees on the same pending
// set.
type InMemoryLocalSkillListStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeLocalSkillListRequest
}

func NewInMemoryLocalSkillListStore() *InMemoryLocalSkillListStore {
	return &InMemoryLocalSkillListStore{requests: make(map[string]*RuntimeLocalSkillListRequest)}
}

func (s *InMemoryLocalSkillListStore) Create(_ context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > runtimeLocalSkillStoreRetention {
			delete(s.requests, id)
		}
	}

	req := &RuntimeLocalSkillListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    RuntimeLocalSkillPending,
		Supported: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryLocalSkillListStore) Get(_ context.Context, id string) (*RuntimeLocalSkillListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyLocalSkillListTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryLocalSkillListStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyLocalSkillListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryLocalSkillListStore) PopPending(_ context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *RuntimeLocalSkillListRequest
	now := time.Now()
	for _, req := range s.requests {
		applyLocalSkillListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = RuntimeLocalSkillRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryLocalSkillListStore) Complete(_ context.Context, id string, skills []RuntimeLocalSkillSummary, supported bool, mcpServers []RuntimeLocalMcpServerSummary, mcpSupported bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillCompleted
		req.Skills = skills
		req.Supported = supported
		req.McpServers = mcpServers
		req.McpSupported = mcpSupported
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryLocalSkillListStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

// InMemoryLocalSkillImportStore mirrors InMemoryLocalSkillListStore for import
// requests. Same single-node vs. multi-node caveat.
type InMemoryLocalSkillImportStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeLocalSkillImportRequest
}

func NewInMemoryLocalSkillImportStore() *InMemoryLocalSkillImportStore {
	return &InMemoryLocalSkillImportStore{requests: make(map[string]*RuntimeLocalSkillImportRequest)}
}

func (s *InMemoryLocalSkillImportStore) Create(_ context.Context, input LocalSkillImportRequestInput) (*RuntimeLocalSkillImportRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > runtimeLocalSkillStoreRetention {
			delete(s.requests, id)
		}
	}

	req := &RuntimeLocalSkillImportRequest{
		ID:               randomID(),
		RuntimeID:        input.RuntimeID,
		SkillKey:         input.SkillKey,
		Name:             input.Name,
		Description:      input.Description,
		Action:           input.Action,
		TargetSkillID:    input.TargetSkillID,
		SupportsConflict: input.SupportsConflict,
		Status:           RuntimeLocalSkillPending,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		CreatorID:        input.CreatorID,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryLocalSkillImportStore) Get(_ context.Context, id string) (*RuntimeLocalSkillImportRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyLocalSkillImportTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryLocalSkillImportStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyLocalSkillImportTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryLocalSkillImportStore) PopPending(_ context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *RuntimeLocalSkillImportRequest
	now := time.Now()
	for _, req := range s.requests {
		applyLocalSkillImportTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = RuntimeLocalSkillRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryLocalSkillImportStore) PopPendingBatch(_ context.Context, runtimeID string, limit int) ([]*RuntimeLocalSkillImportRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Collect all pending requests for this runtime, sorted by creation time.
	var pending []*RuntimeLocalSkillImportRequest
	for _, req := range s.requests {
		applyLocalSkillImportTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			pending = append(pending, req)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})

	if limit > len(pending) {
		limit = len(pending)
	}

	result := make([]*RuntimeLocalSkillImportRequest, 0, limit)
	for _, req := range pending[:limit] {
		req.Status = RuntimeLocalSkillRunning
		startedAt := now
		req.RunStartedAt = &startedAt
		req.UpdatedAt = now
		result = append(result, req)
	}
	return result, nil
}

func (s *InMemoryLocalSkillImportStore) Complete(_ context.Context, id string, skill SkillWithFilesResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillCompleted
		req.Skill = &skill
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryLocalSkillImportStore) Conflict(_ context.Context, id string, info LocalSkillImportConflict) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillConflict
		conflict := info
		req.Conflict = &conflict
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryLocalSkillImportStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

type CreateRuntimeLocalSkillImportRequest struct {
	SkillKey    string  `json:"skill_key"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	// Action selects create (default) vs overwrite. When overwrite,
	// TargetSkillID must reference the existing same-name skill.
	Action        LocalSkillImportAction `json:"action,omitempty"`
	TargetSkillID string                 `json:"target_skill_id,omitempty"`
	// SupportsConflict opts the client into the structured-conflict contract.
	// Omit it (older clients) to keep the legacy `failed` behavior on a
	// same-name collision. An overwrite request implies the new contract.
	SupportsConflict bool `json:"supports_conflict,omitempty"`
}

type reportedRuntimeLocalSkill struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Content     string                   `json:"content"`
	SourcePath  string                   `json:"source_path"`
	Provider    string                   `json:"provider"`
	Files       []CreateSkillFileRequest `json:"files,omitempty"`
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func runtimeLocalSkillRequestTerminal(status RuntimeLocalSkillRequestStatus) bool {
	return status == RuntimeLocalSkillCompleted || status == RuntimeLocalSkillFailed ||
		status == RuntimeLocalSkillTimeout || status == RuntimeLocalSkillConflict
}

func (h *Handler) requireRuntimeLocalSkillAccess(w http.ResponseWriter, r *http.Request, runtimeID string) (runtimeIDAndWorkspace, bool) {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return runtimeIDAndWorkspace{}, false
	}

	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return runtimeIDAndWorkspace{}, false
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "runtime not found")
	if !ok {
		return runtimeIDAndWorkspace{}, false
	}

	if rt.OwnerID.Valid && uuidToString(rt.OwnerID) == uuidToString(member.UserID) {
		return runtimeIDAndWorkspace{
			runtimeID:   uuidToString(rt.ID),
			workspaceID: wsID,
			provider:    rt.Provider,
			status:      rt.Status,
		}, true
	}

	writeError(w, http.StatusForbidden, "insufficient permissions")
	return runtimeIDAndWorkspace{}, false
}

type runtimeIDAndWorkspace struct {
	runtimeID   string
	workspaceID string
	provider    string
	status      string
}

func (h *Handler) InitiateListLocalSkills(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireRuntimeLocalSkillAccess(w, r, runtimeID)
	if !ok {
		return
	}
	if rt.status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}

	req, err := h.LocalSkillListStore.Create(r.Context(), rt.runtimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue local skills request: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) GetLocalSkillListRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireRuntimeLocalSkillAccess(w, r, runtimeID)
	if !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.LocalSkillListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != rt.runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) InitiateImportLocalSkill(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireRuntimeLocalSkillAccess(w, r, runtimeID)
	if !ok {
		return
	}
	if rt.status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}

	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateRuntimeLocalSkillImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.SkillKey) == "" {
		writeError(w, http.StatusBadRequest, "skill_key is required")
		return
	}

	targetSkillID := ""
	switch req.Action {
	case LocalSkillImportActionCreate:
		// nothing extra
	case LocalSkillImportActionOverwrite:
		// Existence + creator permission are re-verified authoritatively at
		// report time (the skill may change between confirm and write); here we
		// only require a well-formed target so we never enqueue a doomed write.
		uuid, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.TargetSkillID), "target_skill_id")
		if !ok {
			return
		}
		targetSkillID = uuidToString(uuid)
	default:
		writeError(w, http.StatusBadRequest, "invalid action")
		return
	}

	importReq, err := h.LocalSkillImportStore.Create(r.Context(), LocalSkillImportRequestInput{
		RuntimeID:     rt.runtimeID,
		CreatorID:     creatorID,
		SkillKey:      strings.TrimSpace(req.SkillKey),
		Name:          cleanOptionalString(req.Name),
		Description:   cleanOptionalString(req.Description),
		Action:        req.Action,
		TargetSkillID: targetSkillID,
		// An overwrite request is inherently a new-client action, so it implies
		// the structured-conflict contract even if the flag is omitted.
		SupportsConflict: req.SupportsConflict || req.Action == LocalSkillImportActionOverwrite,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue local skill import: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, importReq)
}

func (h *Handler) GetLocalSkillImportRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireRuntimeLocalSkillAccess(w, r, runtimeID)
	if !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.LocalSkillImportStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != rt.runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}

	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) ReportLocalSkillListResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.LocalSkillListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if runtimeLocalSkillRequestTerminal(req.Status) {
		slog.Debug("ignoring stale runtime local skills report", "runtime_id", runtimeID, "request_id", requestID, "status", req.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status       string                         `json:"status"`
		Skills       []RuntimeLocalSkillSummary     `json:"skills"`
		Supported    *bool                          `json:"supported"`
		McpServers   []RuntimeLocalMcpServerSummary `json:"mcp_servers"`
		McpSupported *bool                          `json:"mcp_supported"`
		Error        string                         `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status == "completed" {
		supported := true
		if body.Supported != nil {
			supported = *body.Supported
		}
		mcpSupported := false
		if body.McpSupported != nil {
			mcpSupported = *body.McpSupported
		}
		if err := h.LocalSkillListStore.Complete(r.Context(), requestID, body.Skills, supported, body.McpServers, mcpSupported); err != nil {
			// Surface the store failure as 5xx so the daemon can retry instead
			// of swallowing the report (leaves the request stuck in running
			// until the server-side timeout, which is exactly the "looks OK but
			// nothing happens" class of bug we're trying to avoid).
			slog.Error("local skills Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
	} else {
		if err := h.LocalSkillListStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("local skills Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}

	slog.Debug("runtime local skills report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status, "count", len(body.Skills))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ReportLocalSkillImportResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req, err := h.LocalSkillImportStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if runtimeLocalSkillRequestTerminal(req.Status) {
		slog.Debug("ignoring stale runtime local skill import report", "runtime_id", runtimeID, "request_id", requestID, "status", req.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status string                     `json:"status"`
		Skill  *reportedRuntimeLocalSkill `json:"skill"`
		Error  string                     `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status != "completed" {
		h.failLocalSkillImport(w, r, requestID, body.Error)
		return
	}
	if body.Skill == nil {
		h.failLocalSkillImport(w, r, requestID, "daemon returned an empty skill bundle")
		return
	}
	creatorUUID, err := util.ParseUUID(req.CreatorID)
	if err != nil {
		failMsg := "stored local skill import creator_id is invalid"
		if ferr := h.LocalSkillImportStore.Fail(r.Context(), requestID, failMsg); ferr != nil {
			slog.Error("local skill import Fail failed", "error", ferr, "request_id", requestID)
		}
		writeError(w, http.StatusInternalServerError, failMsg)
		return
	}

	name := body.Skill.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := body.Skill.Description
	if req.Description != nil {
		description = *req.Description
	}

	files := make([]CreateSkillFileRequest, 0, len(body.Skill.Files))
	for _, f := range body.Skill.Files {
		if !validateFilePath(f.Path) {
			continue
		}
		files = append(files, f)
	}

	config := map[string]any{
		"origin": map[string]any{
			"type":        "runtime_local",
			"runtime_id":  runtimeID,
			"provider":    body.Skill.Provider,
			"source_path": body.Skill.SourcePath,
		},
	}

	// Overwrite path: re-import onto an existing skill. Existence and creator
	// permission are re-verified inside overwriteSkillWithFiles, in the same tx
	// as the write, so a target deleted (or a creator change) between the user's
	// confirm and this report fails cleanly without falling back to create.
	if req.Action == LocalSkillImportActionOverwrite {
		targetUUID, perr := util.ParseUUID(req.TargetSkillID)
		if perr != nil {
			failMsg := "stored target_skill_id is invalid"
			if ferr := h.LocalSkillImportStore.Fail(r.Context(), requestID, failMsg); ferr != nil {
				slog.Error("local skill import Fail failed", "error", ferr, "request_id", requestID)
			}
			writeError(w, http.StatusInternalServerError, failMsg)
			return
		}
		resp, oerr := h.overwriteSkillWithFiles(r.Context(), skillOverwriteInput{
			WorkspaceID:   rt.WorkspaceID,
			TargetSkillID: targetUUID,
			UserID:        req.CreatorID,
			ExpectedName:  sanitizeNullBytes(name),
			Description:   description,
			Content:       body.Skill.Content,
			Config:        config,
			Files:         files,
		})
		if oerr != nil {
			failMsg := oerr.Error()
			switch {
			case errors.Is(oerr, errSkillOverwriteNotFound):
				failMsg = "target skill no longer exists"
			case errors.Is(oerr, errSkillOverwriteForbidden):
				failMsg = "you no longer have permission to overwrite this skill"
			case errors.Is(oerr, errSkillOverwriteNameMismatch):
				failMsg = "target skill name no longer matches the imported skill"
			}
			h.failLocalSkillImport(w, r, requestID, failMsg)
			return
		}
		if err := h.LocalSkillImportStore.Complete(r.Context(), requestID, resp); err != nil {
			// The overwrite already committed; unlike the create path we must
			// NOT delete the skill to "roll back" (that would destroy a
			// pre-existing skill and its agent bindings). Surface 5xx so the
			// daemon retries — the retry re-applies the same UPDATE idempotently.
			slog.Error("local skill import overwrite Complete failed",
				"error", err, "request_id", requestID, "skill_id", resp.ID)
			writeError(w, http.StatusInternalServerError, "failed to persist import completion")
			return
		}
		h.publish(protocol.EventSkillUpdated, uuidToString(rt.WorkspaceID), "member", req.CreatorID, map[string]any{"skill": resp})
		slog.Debug("runtime local skill overwritten", "runtime_id", runtimeID, "request_id", requestID, "skill_id", resp.ID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Create path: detect a same-name conflict before writing. For opted-in
	// clients this is a structured terminal state (not a failure) so the caller
	// can offer overwrite / rename / skip; older clients keep the legacy
	// `failed` behavior (see resolveLocalSkillConflict).
	if existing, found, lerr := h.lookupSkillByName(r.Context(), rt.WorkspaceID, sanitizeNullBytes(name)); lerr != nil {
		h.failLocalSkillImport(w, r, requestID, "failed to check for existing skill: "+lerr.Error())
		return
	} else if found {
		h.resolveLocalSkillConflict(w, r, req, existing)
		return
	}

	resp, err := h.createSkillWithFiles(r.Context(), skillCreateInput{
		WorkspaceID: rt.WorkspaceID,
		CreatorID:   creatorUUID,
		Name:        name,
		Description: description,
		Content:     body.Skill.Content,
		Config:      config,
		Files:       files,
	})
	if err != nil {
		// A unique-violation here means another import won the race between our
		// lookup and the insert — surface it as a conflict, not a hard failure.
		if isUniqueViolation(err) {
			if existing, found, lerr := h.lookupSkillByName(r.Context(), rt.WorkspaceID, sanitizeNullBytes(name)); lerr == nil && found {
				h.resolveLocalSkillConflict(w, r, req, existing)
				return
			}
			// Lost the row again (deleted between insert-fail and re-lookup):
			// fall through to the legacy unique-violation message.
			h.failLocalSkillImport(w, r, requestID, "a skill with this name already exists")
			return
		}
		h.failLocalSkillImport(w, r, requestID, err.Error())
		return
	}

	if err := h.LocalSkillImportStore.Complete(r.Context(), requestID, resp); err != nil {
		// We already wrote the Skill to Postgres. If the store-side Complete
		// fails we can't leave that Skill orphaned: the daemon will retry on
		// 5xx and re-create it, which blows up on the unique-name constraint
		// and looks to the user like "import keeps failing". Roll back our
		// side-effects so the retry lands on a clean slate.
		slog.Error("local skill import Complete failed — rolling back created skill",
			"error", err, "request_id", requestID, "skill_id", resp.ID)
		if delErr := h.Queries.DeleteSkill(r.Context(), db.DeleteSkillParams{
			ID:          parseUUID(resp.ID),
			WorkspaceID: rt.WorkspaceID,
		}); delErr != nil {
			slog.Warn("orphan skill rollback failed", "error", delErr, "skill_id", resp.ID)
		}
		writeError(w, http.StatusInternalServerError, "failed to persist import completion")
		return
	}
	h.publish(protocol.EventSkillCreated, uuidToString(rt.WorkspaceID), "member", req.CreatorID, map[string]any{"skill": resp})
	slog.Debug("runtime local skill imported", "runtime_id", runtimeID, "request_id", requestID, "skill_id", resp.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// failLocalSkillImport marks the request failed and writes the standard daemon
// response (200 ok). If the store write itself fails it returns 500 so the
// daemon retries.
func (h *Handler) failLocalSkillImport(w http.ResponseWriter, r *http.Request, requestID, failMsg string) {
	if err := h.LocalSkillImportStore.Fail(r.Context(), requestID, failMsg); err != nil {
		slog.Error("local skill import Fail failed", "error", err, "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "failed to persist failure")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resolveLocalSkillConflict terminates a same-name create import. Clients that
// opted into the structured-conflict contract (SupportsConflict) receive the
// `conflict` status plus metadata so they can offer overwrite / rename / skip;
// older clients keep the legacy `failed` ("a skill with this name already
// exists") behavior so an installed Desktop build that predates the contract
// doesn't regress when it hits an upgraded backend.
func (h *Handler) resolveLocalSkillConflict(w http.ResponseWriter, r *http.Request, req *RuntimeLocalSkillImportRequest, existing db.Skill) {
	if req.SupportsConflict {
		h.reportLocalSkillConflict(w, r, req.ID, req.CreatorID, existing)
		return
	}
	h.failLocalSkillImport(w, r, req.ID, "a skill with this name already exists")
}

// reportLocalSkillConflict records a same-name conflict as the terminal
// RuntimeLocalSkillConflict state with structured metadata the caller uses to
// offer overwrite / rename / skip.
func (h *Handler) reportLocalSkillConflict(w http.ResponseWriter, r *http.Request, requestID, creatorID string, existing db.Skill) {
	info := LocalSkillImportConflict{
		ExistingSkillID: uuidToString(existing.ID),
		CanOverwrite:    canOverwriteSkillByLocalImport(creatorID, existing),
	}
	if existing.CreatedBy.Valid {
		info.ExistingCreatedBy = uuidToString(existing.CreatedBy)
	}
	if err := h.LocalSkillImportStore.Conflict(r.Context(), requestID, info); err != nil {
		slog.Error("local skill import Conflict failed", "error", err, "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "failed to persist conflict")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// lookupSkillByName resolves a skill by (workspace, name). found=false with a
// nil error means there is no such skill — i.e. no conflict.
func (h *Handler) lookupSkillByName(ctx context.Context, workspaceID pgtype.UUID, name string) (db.Skill, bool, error) {
	skill, err := h.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Skill{}, false, nil
		}
		return db.Skill{}, false, err
	}
	return skill, true, nil
}
