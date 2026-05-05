package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type RuntimeLocalSkillRequestStatus string

const (
	RuntimeLocalSkillPending   RuntimeLocalSkillRequestStatus = "pending"
	RuntimeLocalSkillRunning   RuntimeLocalSkillRequestStatus = "running"
	RuntimeLocalSkillCompleted RuntimeLocalSkillRequestStatus = "completed"
	RuntimeLocalSkillFailed    RuntimeLocalSkillRequestStatus = "failed"
	RuntimeLocalSkillTimeout   RuntimeLocalSkillRequestStatus = "timeout"
)

const (
	runtimeLocalSkillPendingTimeout = 30 * time.Second
	runtimeLocalSkillRunningTimeout = 60 * time.Second
	runtimeLocalSkillStoreRetention = 2 * time.Minute
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
	Complete(ctx context.Context, id string, skills []RuntimeLocalSkillSummary, supported bool) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// LocalSkillImportStore is the same contract as LocalSkillListStore but for
// runtime-local-skill import requests. Kept as a separate interface because the
// Create signature carries import-specific fields (skill_key, optional rename).
type LocalSkillImportStore interface {
	Create(ctx context.Context, runtimeID, creatorID, skillKey string, name, description *string) (*RuntimeLocalSkillImportRequest, error)
	Get(ctx context.Context, id string) (*RuntimeLocalSkillImportRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error)
	Complete(ctx context.Context, id string, skill SkillResponse) error
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
			req.Error = "daemon did not respond within 30 seconds"
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
			req.Error = "daemon did not respond within 30 seconds"
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
	FileCount   int    `json:"file_count"`
}

type RuntimeLocalSkillListRequest struct {
	ID           string                         `json:"id"`
	RuntimeID    string                         `json:"runtime_id"`
	Status       RuntimeLocalSkillRequestStatus `json:"status"`
	Skills       []RuntimeLocalSkillSummary     `json:"skills,omitempty"`
	Supported    bool                           `json:"supported"`
	Error        string                         `json:"error,omitempty"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	RunStartedAt *time.Time                     `json:"-"`
}

type RuntimeLocalSkillImportRequest struct {
	ID           string                         `json:"id"`
	RuntimeID    string                         `json:"runtime_id"`
	SkillKey     string                         `json:"skill_key"`
	Name         *string                        `json:"name,omitempty"`
	Description  *string                        `json:"description,omitempty"`
	Status       RuntimeLocalSkillRequestStatus `json:"status"`
	Skill        *SkillResponse                 `json:"skill,omitempty"`
	Error        string                         `json:"error,omitempty"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	CreatorID    string                         `json:"-"`
	RunStartedAt *time.Time                     `json:"-"`
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

func (s *InMemoryLocalSkillListStore) Complete(_ context.Context, id string, skills []RuntimeLocalSkillSummary, supported bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillCompleted
		req.Skills = skills
		req.Supported = supported
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

func (s *InMemoryLocalSkillImportStore) Create(_ context.Context, runtimeID, creatorID, skillKey string, name, description *string) (*RuntimeLocalSkillImportRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > runtimeLocalSkillStoreRetention {
			delete(s.requests, id)
		}
	}

	req := &RuntimeLocalSkillImportRequest{
		ID:          randomID(),
		RuntimeID:   runtimeID,
		SkillKey:    skillKey,
		Name:        name,
		Description: description,
		Status:      RuntimeLocalSkillPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		CreatorID:   creatorID,
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

func (s *InMemoryLocalSkillImportStore) Complete(_ context.Context, id string, skill SkillResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillCompleted
		req.Skill = &skill
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
	return status == RuntimeLocalSkillCompleted || status == RuntimeLocalSkillFailed || status == RuntimeLocalSkillTimeout
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

	importReq, err := h.LocalSkillImportStore.Create(
		r.Context(),
		rt.runtimeID,
		creatorID,
		strings.TrimSpace(req.SkillKey),
		cleanOptionalString(req.Name),
		cleanOptionalString(req.Description),
	)
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
		Status    string                     `json:"status"`
		Skills    []RuntimeLocalSkillSummary `json:"skills"`
		Supported *bool                      `json:"supported"`
		Error     string                     `json:"error"`
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
		if err := h.LocalSkillListStore.Complete(r.Context(), requestID, body.Skills, supported); err != nil {
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
		if err := h.LocalSkillImportStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("local skill import Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if body.Skill == nil {
		if err := h.LocalSkillImportStore.Fail(r.Context(), requestID, "daemon returned an empty skill bundle"); err != nil {
			slog.Error("local skill import Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

	resp, err := h.createSkillWithFiles(r.Context(), skillCreateInput{
		WorkspaceID: rt.WorkspaceID,
		CreatorID:   creatorUUID,
		Name:        name,
		Description: description,
		Content:     body.Skill.Content,
		Config: map[string]any{
			"origin": map[string]any{
				"type":        "runtime_local",
				"runtime_id":  runtimeID,
				"provider":    body.Skill.Provider,
				"source_path": body.Skill.SourcePath,
			},
		},
		Files: files,
	})
	if err != nil {
		failMsg := err.Error()
		if isUniqueViolation(err) {
			failMsg = "a skill with this name already exists"
		}
		if ferr := h.LocalSkillImportStore.Fail(r.Context(), requestID, failMsg); ferr != nil {
			slog.Error("local skill import Fail failed", "error", ferr, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if err := h.LocalSkillImportStore.Complete(r.Context(), requestID, resp.SkillResponse); err != nil {
		// We already wrote the Skill to Postgres. If the store-side Complete
		// fails we can't leave that Skill orphaned: the daemon will retry on
		// 5xx and re-create it, which blows up on the unique-name constraint
		// and looks to the user like "import keeps failing". Roll back our
		// side-effects so the retry lands on a clean slate.
		slog.Error("local skill import Complete failed — rolling back created skill",
			"error", err, "request_id", requestID, "skill_id", resp.ID)
		if delErr := h.Queries.DeleteSkill(r.Context(), parseUUID(resp.ID)); delErr != nil {
			slog.Warn("orphan skill rollback failed", "error", delErr, "skill_id", resp.ID)
		}
		writeError(w, http.StatusInternalServerError, "failed to persist import completion")
		return
	}
	h.publish(protocol.EventSkillCreated, uuidToString(rt.WorkspaceID), "member", req.CreatorID, map[string]any{"skill": resp})
	slog.Debug("runtime local skill imported", "runtime_id", runtimeID, "request_id", requestID, "skill_id", resp.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
