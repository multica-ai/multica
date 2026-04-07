package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Response types ──────────────────────────────────────────────────────

type AgentflowResponse struct {
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	Title             string `json:"title"`
	Description       *string `json:"description"`
	AgentID           string `json:"agent_id"`
	Status            string `json:"status"`
	ConcurrencyPolicy string `json:"concurrency_policy"`
	Variables         json.RawMessage `json:"variables"`
	CreatedBy         string `json:"created_by"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type AgentflowTriggerResponse struct {
	ID             string  `json:"id"`
	AgentflowID    string  `json:"agentflow_id"`
	Kind           string  `json:"kind"`
	Enabled        bool    `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	NextRunAt      *string `json:"next_run_at"`
	PublicID       *string `json:"public_id,omitempty"`
	LastFiredAt    *string `json:"last_fired_at"`
	CreatedAt      string  `json:"created_at"`
}

type AgentflowRunResponse struct {
	ID            string  `json:"id"`
	AgentflowID   string  `json:"agentflow_id"`
	TriggerID     *string `json:"trigger_id"`
	SourceKind    string  `json:"source_kind"`
	Status        string  `json:"status"`
	LinkedIssueID *string `json:"linked_issue_id"`
	Payload       json.RawMessage `json:"payload"`
	AgentOutput   *string `json:"agent_output"`
	StartedAt     *string `json:"started_at"`
	CompletedAt   *string `json:"completed_at"`
	CreatedAt     string  `json:"created_at"`
}

// ── Converters ──────────────────────────────────────────────────────────

func agentflowToResponse(a db.Agentflow) AgentflowResponse {
	vars := json.RawMessage(a.Variables)
	if len(vars) == 0 {
		vars = json.RawMessage("[]")
	}
	return AgentflowResponse{
		ID:                uuidToString(a.ID),
		WorkspaceID:       uuidToString(a.WorkspaceID),
		Title:             a.Title,
		Description:       textToPtr(a.Description),
		AgentID:           uuidToString(a.AgentID),
		Status:            a.Status,
		ConcurrencyPolicy: a.ConcurrencyPolicy,
		Variables:         vars,
		CreatedBy:         uuidToString(a.CreatedBy),
		CreatedAt:         timestampToString(a.CreatedAt),
		UpdatedAt:         timestampToString(a.UpdatedAt),
	}
}

func triggerToResponse(t db.AgentflowTrigger) AgentflowTriggerResponse {
	return AgentflowTriggerResponse{
		ID:             uuidToString(t.ID),
		AgentflowID:    uuidToString(t.AgentflowID),
		Kind:           t.Kind,
		Enabled:        t.Enabled,
		CronExpression: textToPtr(t.CronExpression),
		Timezone:       textToPtr(t.Timezone),
		NextRunAt:      timestampToPtr(t.NextRunAt),
		PublicID:       textToPtr(t.PublicID),
		LastFiredAt:    timestampToPtr(t.LastFiredAt),
		CreatedAt:      timestampToString(t.CreatedAt),
	}
}

func runToResponse(r db.AgentflowRun) AgentflowRunResponse {
	payload := json.RawMessage(r.Payload)
	if len(payload) == 0 {
		payload = nil
	}
	return AgentflowRunResponse{
		ID:            uuidToString(r.ID),
		AgentflowID:   uuidToString(r.AgentflowID),
		TriggerID:     uuidToPtr(r.TriggerID),
		SourceKind:    r.SourceKind,
		Status:        r.Status,
		LinkedIssueID: uuidToPtr(r.LinkedIssueID),
		Payload:       payload,
		AgentOutput:   textToPtr(r.AgentOutput),
		StartedAt:     timestampToPtr(r.StartedAt),
		CompletedAt:   timestampToPtr(r.CompletedAt),
		CreatedAt:     timestampToString(r.CreatedAt),
	}
}

// ── Agentflow CRUD ──────────────────────────────────────────────────────

type CreateAgentflowRequest struct {
	Title             string           `json:"title"`
	Description       *string          `json:"description"`
	AgentID           string           `json:"agent_id"`
	Status            string           `json:"status"`
	ConcurrencyPolicy string           `json:"concurrency_policy"`
	Variables         json.RawMessage  `json:"variables"`
}

func (h *Handler) CreateAgentflow(w http.ResponseWriter, r *http.Request) {
	var req CreateAgentflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	status := req.Status
	if status == "" {
		status = "active"
	}
	policy := req.ConcurrencyPolicy
	if policy == "" {
		policy = "skip_if_active"
	}
	vars := req.Variables
	if len(vars) == 0 {
		vars = json.RawMessage("[]")
	}

	af, err := h.Queries.CreateAgentflow(r.Context(), db.CreateAgentflowParams{
		WorkspaceID:       parseUUID(workspaceID),
		Title:             req.Title,
		Description:       ptrToText(req.Description),
		AgentID:           parseUUID(req.AgentID),
		Status:            status,
		ConcurrencyPolicy: policy,
		Variables:         vars,
		CreatedBy:         parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agentflow")
		return
	}

	writeJSON(w, http.StatusCreated, agentflowToResponse(af))
}

func (h *Handler) ListAgentflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	flows, err := h.Queries.ListAgentflows(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agentflows")
		return
	}

	resp := make([]AgentflowResponse, len(flows))
	for i, f := range flows {
		resp[i] = agentflowToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetAgentflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	af, err := h.Queries.GetAgentflowInWorkspace(r.Context(), db.GetAgentflowInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agentflow not found")
		return
	}

	writeJSON(w, http.StatusOK, agentflowToResponse(af))
}

type UpdateAgentflowRequest struct {
	Title             *string          `json:"title"`
	Description       *string          `json:"description"`
	AgentID           *string          `json:"agent_id"`
	Status            *string          `json:"status"`
	ConcurrencyPolicy *string          `json:"concurrency_policy"`
	Variables         json.RawMessage  `json:"variables"`
}

func (h *Handler) UpdateAgentflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateAgentflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateAgentflowParams{ID: parseUUID(id)}
	if req.Title != nil {
		params.Title = ptrToText(req.Title)
	}
	if req.Description != nil {
		params.Description = ptrToText(req.Description)
	}
	if req.AgentID != nil {
		params.AgentID = parseUUID(*req.AgentID)
	}
	if req.Status != nil {
		params.Status = ptrToText(req.Status)
	}
	if req.ConcurrencyPolicy != nil {
		params.ConcurrencyPolicy = ptrToText(req.ConcurrencyPolicy)
	}
	if len(req.Variables) > 0 {
		params.Variables = req.Variables
	}

	af, err := h.Queries.UpdateAgentflow(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update agentflow")
		return
	}

	writeJSON(w, http.StatusOK, agentflowToResponse(af))
}

func (h *Handler) ArchiveAgentflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	af, err := h.Queries.ArchiveAgentflow(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive agentflow")
		return
	}

	writeJSON(w, http.StatusOK, agentflowToResponse(af))
}

// ── Triggers ────────────────────────────────────────────────────────────

type CreateTriggerRequest struct {
	Kind           string  `json:"kind"`
	Enabled        *bool   `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	NextRunAt      *string `json:"next_run_at"`
}

func (h *Handler) CreateAgentflowTrigger(w http.ResponseWriter, r *http.Request) {
	agentflowID := chi.URLParam(r, "id")

	var req CreateTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind is required")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var nextRunAt pgtype.Timestamptz
	if req.NextRunAt != nil {
		nextRunAt = parseTimestamp(*req.NextRunAt)
	}

	t, err := h.Queries.CreateAgentflowTrigger(r.Context(), db.CreateAgentflowTriggerParams{
		AgentflowID:    parseUUID(agentflowID),
		Kind:           req.Kind,
		Enabled:        enabled,
		CronExpression: ptrToText(req.CronExpression),
		Timezone:       ptrToText(req.Timezone),
		NextRunAt:      nextRunAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create trigger")
		return
	}

	writeJSON(w, http.StatusCreated, triggerToResponse(t))
}

func (h *Handler) ListAgentflowTriggers(w http.ResponseWriter, r *http.Request) {
	agentflowID := chi.URLParam(r, "id")

	triggers, err := h.Queries.ListAgentflowTriggers(r.Context(), parseUUID(agentflowID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list triggers")
		return
	}

	resp := make([]AgentflowTriggerResponse, len(triggers))
	for i, t := range triggers {
		resp[i] = triggerToResponse(t)
	}
	writeJSON(w, http.StatusOK, resp)
}

type UpdateTriggerRequest struct {
	Enabled        *bool   `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	NextRunAt      *string `json:"next_run_at"`
}

func (h *Handler) UpdateAgentflowTrigger(w http.ResponseWriter, r *http.Request) {
	triggerID := chi.URLParam(r, "triggerId")

	var req UpdateTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateAgentflowTriggerParams{ID: parseUUID(triggerID)}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}
	if req.CronExpression != nil {
		params.CronExpression = ptrToText(req.CronExpression)
	}
	if req.Timezone != nil {
		params.Timezone = ptrToText(req.Timezone)
	}
	if req.NextRunAt != nil {
		params.NextRunAt = parseTimestamp(*req.NextRunAt)
	}

	t, err := h.Queries.UpdateAgentflowTrigger(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update trigger")
		return
	}

	writeJSON(w, http.StatusOK, triggerToResponse(t))
}

func (h *Handler) DeleteAgentflowTrigger(w http.ResponseWriter, r *http.Request) {
	triggerID := chi.URLParam(r, "triggerId")

	err := h.Queries.DeleteAgentflowTrigger(r.Context(), parseUUID(triggerID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete trigger")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Runs ────────────────────────────────────────────────────────────────

func (h *Handler) ListAgentflowRuns(w http.ResponseWriter, r *http.Request) {
	agentflowID := chi.URLParam(r, "id")

	limit := int32(50)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = int32(v)
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = int32(v)
		}
	}

	runs, err := h.Queries.ListAgentflowRuns(r.Context(), db.ListAgentflowRunsParams{
		AgentflowID: parseUUID(agentflowID),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	resp := make([]AgentflowRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = runToResponse(run)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RunAgentflow(w http.ResponseWriter, r *http.Request) {
	agentflowID := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	af, err := h.Queries.GetAgentflowInWorkspace(r.Context(), db.GetAgentflowInWorkspaceParams{
		ID:          parseUUID(agentflowID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agentflow not found")
		return
	}

	// Check concurrency policy
	if af.ConcurrencyPolicy == "skip_if_active" || af.ConcurrencyPolicy == "coalesce" {
		hasActive, err := h.Queries.HasActiveAgentflowRun(r.Context(), af.ID)
		if err == nil && hasActive {
			if af.ConcurrencyPolicy == "skip_if_active" {
				writeError(w, http.StatusConflict, "agentflow already has an active run")
				return
			}
			// coalesce: skip silently
			run, _ := h.Queries.CreateAgentflowRun(r.Context(), db.CreateAgentflowRunParams{
				AgentflowID: af.ID,
				SourceKind:  "manual",
				Status:      "coalesced",
			})
			writeJSON(w, http.StatusOK, runToResponse(run))
			return
		}
	}

	// Create run
	run, err := h.Queries.CreateAgentflowRun(r.Context(), db.CreateAgentflowRunParams{
		AgentflowID: af.ID,
		SourceKind:  "manual",
		Status:      "received",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create run")
		return
	}

	// Enqueue task for the agent
	err = h.TaskService.EnqueueTaskForAgentflow(r.Context(), af, run)
	if err != nil {
		// Mark run as failed
		h.Queries.UpdateAgentflowRunStatus(r.Context(), db.UpdateAgentflowRunStatusParams{
			ID:     run.ID,
			Status: "failed",
		})
		writeError(w, http.StatusInternalServerError, "failed to enqueue agentflow task")
		return
	}

	writeJSON(w, http.StatusCreated, runToResponse(run))
}

// ── Helpers ─────────────────────────────────────────────────────────────

func parseTimestamp(s string) pgtype.Timestamptz {
	var ts pgtype.Timestamptz
	if err := ts.Scan(s); err != nil {
		return pgtype.Timestamptz{}
	}
	return ts
}
