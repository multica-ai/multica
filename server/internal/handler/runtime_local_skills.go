package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
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

type RuntimeLocalSkillSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourcePath  string `json:"source_path"`
	Provider    string `json:"provider"`
	FileCount   int    `json:"file_count"`
}

type RuntimeLocalSkillListRequest struct {
	ID        string                         `json:"id"`
	RuntimeID string                         `json:"runtime_id"`
	Status    RuntimeLocalSkillRequestStatus `json:"status"`
	Skills    []RuntimeLocalSkillSummary     `json:"skills,omitempty"`
	Supported bool                           `json:"supported"`
	Error     string                         `json:"error,omitempty"`
	CreatedAt time.Time                      `json:"created_at"`
	UpdatedAt time.Time                      `json:"updated_at"`
}

type RuntimeLocalSkillImportRequest struct {
	ID          string                         `json:"id"`
	RuntimeID   string                         `json:"runtime_id"`
	SkillKey    string                         `json:"skill_key"`
	Name        *string                        `json:"name,omitempty"`
	Description *string                        `json:"description,omitempty"`
	Status      RuntimeLocalSkillRequestStatus `json:"status"`
	Skill       *SkillResponse                 `json:"skill,omitempty"`
	Error       string                         `json:"error,omitempty"`
	CreatedAt   time.Time                      `json:"created_at"`
	UpdatedAt   time.Time                      `json:"updated_at"`
	CreatorID   string                         `json:"-"`
}

type RuntimeLocalSkillListStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeLocalSkillListRequest
}

func NewRuntimeLocalSkillListStore() *RuntimeLocalSkillListStore {
	return &RuntimeLocalSkillListStore{requests: make(map[string]*RuntimeLocalSkillListRequest)}
}

func (s *RuntimeLocalSkillListStore) Create(runtimeID string) *RuntimeLocalSkillListRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > 2*time.Minute {
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
	return req
}

func (s *RuntimeLocalSkillListStore) Get(id string) *RuntimeLocalSkillListRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	if req.Status == RuntimeLocalSkillPending && time.Since(req.CreatedAt) > 30*time.Second {
		req.Status = RuntimeLocalSkillTimeout
		req.Error = "daemon did not respond within 30 seconds"
		req.UpdatedAt = time.Now()
	}
	return req
}

func (s *RuntimeLocalSkillListStore) PopPending(runtimeID string) *RuntimeLocalSkillListRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *RuntimeLocalSkillListRequest
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = RuntimeLocalSkillRunning
		oldest.UpdatedAt = time.Now()
	}
	return oldest
}

func (s *RuntimeLocalSkillListStore) Complete(id string, skills []RuntimeLocalSkillSummary, supported bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillCompleted
		req.Skills = skills
		req.Supported = supported
		req.UpdatedAt = time.Now()
	}
}

func (s *RuntimeLocalSkillListStore) Fail(id string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
}

type RuntimeLocalSkillImportStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeLocalSkillImportRequest
}

func NewRuntimeLocalSkillImportStore() *RuntimeLocalSkillImportStore {
	return &RuntimeLocalSkillImportStore{requests: make(map[string]*RuntimeLocalSkillImportRequest)}
}

func (s *RuntimeLocalSkillImportStore) Create(runtimeID, creatorID, skillKey string, name, description *string) *RuntimeLocalSkillImportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > 2*time.Minute {
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
	return req
}

func (s *RuntimeLocalSkillImportStore) Get(id string) *RuntimeLocalSkillImportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	if req.Status == RuntimeLocalSkillPending && time.Since(req.CreatedAt) > 30*time.Second {
		req.Status = RuntimeLocalSkillTimeout
		req.Error = "daemon did not respond within 30 seconds"
		req.UpdatedAt = time.Now()
	}
	return req
}

func (s *RuntimeLocalSkillImportStore) PopPending(runtimeID string) *RuntimeLocalSkillImportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *RuntimeLocalSkillImportRequest
	for _, req := range s.requests {
		if req.RuntimeID == runtimeID && req.Status == RuntimeLocalSkillPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = RuntimeLocalSkillRunning
		oldest.UpdatedAt = time.Now()
	}
	return oldest
}

func (s *RuntimeLocalSkillImportStore) Complete(id string, skill SkillResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillCompleted
		req.Skill = &skill
		req.UpdatedAt = time.Now()
	}
}

func (s *RuntimeLocalSkillImportStore) Fail(id string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = RuntimeLocalSkillFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
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

func (h *Handler) requireRuntimeLocalSkillAccess(w http.ResponseWriter, r *http.Request, runtimeID string) (runtimeIDAndWorkspace, bool) {
	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return runtimeIDAndWorkspace{}, false
	}

	wsID := uuidToString(rt.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, wsID, "runtime not found", "owner", "admin", "member")
	if !ok {
		return runtimeIDAndWorkspace{}, false
	}

	userID := requestUserID(r)
	if roleAllowed(member.Role, "owner", "admin") || (rt.OwnerID.Valid && uuidToString(rt.OwnerID) == userID) {
		return runtimeIDAndWorkspace{
			runtimeID:   runtimeID,
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

	req := h.LocalSkillListStore.Create(runtimeID)
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) GetLocalSkillListRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireRuntimeLocalSkillAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req := h.LocalSkillListStore.Get(requestID)
	if req == nil || req.RuntimeID != runtimeID {
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

	importReq := h.LocalSkillImportStore.Create(
		runtimeID,
		creatorID,
		strings.TrimSpace(req.SkillKey),
		cleanOptionalString(req.Name),
		cleanOptionalString(req.Description),
	)
	writeJSON(w, http.StatusOK, importReq)
}

func (h *Handler) GetLocalSkillImportRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireRuntimeLocalSkillAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")
	req := h.LocalSkillImportStore.Get(requestID)
	if req == nil || req.RuntimeID != runtimeID {
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
		h.LocalSkillListStore.Complete(requestID, body.Skills, supported)
	} else {
		h.LocalSkillListStore.Fail(requestID, body.Error)
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
	req := h.LocalSkillImportStore.Get(requestID)
	if req == nil || req.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
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
		h.LocalSkillImportStore.Fail(requestID, body.Error)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if body.Skill == nil {
		h.LocalSkillImportStore.Fail(requestID, "daemon returned an empty skill bundle")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		WorkspaceID: uuidToString(rt.WorkspaceID),
		CreatorID:   req.CreatorID,
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
		if isUniqueViolation(err) {
			h.LocalSkillImportStore.Fail(requestID, "a skill with this name already exists")
		} else {
			h.LocalSkillImportStore.Fail(requestID, err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	h.LocalSkillImportStore.Complete(requestID, resp.SkillResponse)
	h.publish(protocol.EventSkillCreated, uuidToString(rt.WorkspaceID), "member", req.CreatorID, map[string]any{"skill": resp})
	slog.Debug("runtime local skill imported", "runtime_id", runtimeID, "request_id", requestID, "skill_id", resp.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
