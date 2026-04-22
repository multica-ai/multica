package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// computeNextRun delegates to the shared cron helper in the service package.
func computeNextRun(cronExpr, timezone string) (time.Time, error) {
	return service.ComputeNextRun(cronExpr, timezone)
}

// ── Response types ──────────────────────────────────────────────────────────

type AutopilotResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	ProjectID          *string `json:"project_id"`
	Title              string  `json:"title"`
	Description        *string `json:"description"`
	AssigneeID         string  `json:"assignee_id"`
	Priority           string  `json:"priority"`
	Status             string  `json:"status"`
	ExecutionMode      string  `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
	CreatedByType      string  `json:"created_by_type"`
	CreatedByID        string  `json:"created_by_id"`
	LastRunAt          *string `json:"last_run_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type AutopilotTriggerResponse struct {
	ID             string  `json:"id"`
	AutopilotID    string  `json:"autopilot_id"`
	Kind           string  `json:"kind"`
	Enabled        bool    `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	NextRunAt      *string `json:"next_run_at"`
	WebhookToken   *string `json:"webhook_token"`
	Label          *string `json:"label"`
	LastFiredAt    *string `json:"last_fired_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type AutopilotRunResponse struct {
	ID             string  `json:"id"`
	AutopilotID    string  `json:"autopilot_id"`
	TriggerID      *string `json:"trigger_id"`
	Source         string  `json:"source"`
	Status         string  `json:"status"`
	IssueID        *string `json:"issue_id"`
	TaskID         *string `json:"task_id"`
	TriggeredAt    string  `json:"triggered_at"`
	CompletedAt    *string `json:"completed_at"`
	FailureReason  *string `json:"failure_reason"`
	TriggerPayload any     `json:"trigger_payload"`
	Result         any     `json:"result"`
	CreatedAt      string  `json:"created_at"`
}

// ── Converters ──────────────────────────────────────────────────────────────

func autopilotToResponse(a db.Autopilot) AutopilotResponse {
	return AutopilotResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		ProjectID:          uuidToPtr(a.ProjectID),
		Title:              a.Title,
		Description:        textToPtr(a.Description),
		AssigneeID:         uuidToString(a.AssigneeID),
		Priority:           a.Priority,
		Status:             a.Status,
		ExecutionMode:      a.ExecutionMode,
		IssueTitleTemplate: textToPtr(a.IssueTitleTemplate),
		CreatedByType:      a.CreatedByType,
		CreatedByID:        uuidToString(a.CreatedByID),
		LastRunAt:          timestampToPtr(a.LastRunAt),
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
	}
}

func triggerToResponse(t db.AutopilotTrigger) AutopilotTriggerResponse {
	return AutopilotTriggerResponse{
		ID:             uuidToString(t.ID),
		AutopilotID:    uuidToString(t.AutopilotID),
		Kind:           t.Kind,
		Enabled:        t.Enabled,
		CronExpression: textToPtr(t.CronExpression),
		Timezone:       textToPtr(t.Timezone),
		NextRunAt:      timestampToPtr(t.NextRunAt),
		WebhookToken:   textToPtr(t.WebhookToken),
		Label:          textToPtr(t.Label),
		LastFiredAt:    timestampToPtr(t.LastFiredAt),
		CreatedAt:      timestampToString(t.CreatedAt),
		UpdatedAt:      timestampToString(t.UpdatedAt),
	}
}

func runToResponse(r db.AutopilotRun) AutopilotRunResponse {
	var payload any
	if r.TriggerPayload != nil {
		json.Unmarshal(r.TriggerPayload, &payload)
	}
	var result any
	if r.Result != nil {
		json.Unmarshal(r.Result, &result)
	}
	return AutopilotRunResponse{
		ID:             uuidToString(r.ID),
		AutopilotID:    uuidToString(r.AutopilotID),
		TriggerID:      uuidToPtr(r.TriggerID),
		Source:         r.Source,
		Status:         r.Status,
		IssueID:        uuidToPtr(r.IssueID),
		TaskID:         uuidToPtr(r.TaskID),
		TriggeredAt:    timestampToString(r.TriggeredAt),
		CompletedAt:    timestampToPtr(r.CompletedAt),
		FailureReason:  textToPtr(r.FailureReason),
		TriggerPayload: payload,
		Result:         result,
		CreatedAt:      timestampToString(r.CreatedAt),
	}
}

// ── Request types ───────────────────────────────────────────────────────────

type CreateAutopilotRequest struct {
	Title              string  `json:"title"`
	Description        *string `json:"description"`
	AssigneeID         string  `json:"assignee_id"`
	ProjectID          *string `json:"project_id"`
	Priority           string  `json:"priority"`
	ExecutionMode      string  `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
}

type UpdateAutopilotRequest struct {
	Title              *string `json:"title"`
	Description        *string `json:"description"`
	AssigneeID         *string `json:"assignee_id"`
	ProjectID          *string `json:"project_id"`
	Priority           *string `json:"priority"`
	Status             *string `json:"status"`
	ExecutionMode      *string `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
}

type CreateAutopilotTriggerRequest struct {
	Kind           string  `json:"kind"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	Label          *string `json:"label"`
}

type UpdateAutopilotTriggerRequest struct {
	Enabled        *bool   `json:"enabled"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	Label          *string `json:"label"`
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListAutopilots(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	autopilots, err := h.Queries.ListAutopilots(r.Context(), db.ListAutopilotsParams{
		WorkspaceID: parseUUID(workspaceID),
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list autopilots")
		return
	}

	resp := make([]AutopilotResponse, len(autopilots))
	for i, a := range autopilots {
		resp[i] = autopilotToResponse(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"autopilots": resp, "total": len(resp)})
}

func (h *Handler) GetAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	resp := autopilotToResponse(autopilot)

	// Include triggers.
	triggers, err := h.Queries.ListAutopilotTriggers(r.Context(), autopilot.ID)
	if err != nil {
		triggers = nil
	}
	triggerResp := make([]AutopilotTriggerResponse, len(triggers))
	for i, t := range triggers {
		triggerResp[i] = triggerToResponse(t)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"autopilot": resp,
		"triggers":  triggerResp,
	})
}

func (h *Handler) CreateAutopilot(w http.ResponseWriter, r *http.Request) {
	var req CreateAutopilotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.AssigneeID == "" {
		writeError(w, http.StatusBadRequest, "assignee_id is required")
		return
	}
	if req.ExecutionMode == "" {
		writeError(w, http.StatusBadRequest, "execution_mode is required")
		return
	}
	if req.ExecutionMode != "create_issue" && req.ExecutionMode != "run_only" {
		writeError(w, http.StatusBadRequest, "execution_mode must be create_issue or run_only")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Validate assignee is an agent in the workspace.
	_, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          parseUUID(req.AssigneeID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "assignee must be a valid agent in this workspace")
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = "none"
	}

	var projectID pgtype.UUID
	if req.ProjectID != nil {
		projectID = parseUUID(*req.ProjectID)
	}

	autopilot, err := h.Queries.CreateAutopilot(r.Context(), db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(workspaceID),
		Title:              req.Title,
		AssigneeID:         parseUUID(req.AssigneeID),
		Priority:           priority,
		Status:             "active",
		ExecutionMode:      req.ExecutionMode,
		CreatedByType:      "member",
		CreatedByID:        parseUUID(userID),
		ProjectID:          projectID,
		Description:        ptrToText(req.Description),
		IssueTitleTemplate: ptrToText(req.IssueTitleTemplate),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create autopilot")
		return
	}

	resp := autopilotToResponse(autopilot)
	h.publish(protocol.EventAutopilotCreated, workspaceID, "member", userID, map[string]any{"autopilot": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	prev, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateAutopilotRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateAutopilotParams{
		ID:                 prev.ID,
		Description:        prev.Description,
		AssigneeID:         prev.AssigneeID,
		ProjectID:          prev.ProjectID,
		IssueTitleTemplate: prev.IssueTitleTemplate,
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.ExecutionMode != nil {
		params.ExecutionMode = pgtype.Text{String: *req.ExecutionMode, Valid: true}
	}
	if _, ok := rawFields["description"]; ok {
		params.Description = ptrToText(req.Description)
	}
	if _, ok := rawFields["issue_title_template"]; ok {
		params.IssueTitleTemplate = ptrToText(req.IssueTitleTemplate)
	}
	if _, ok := rawFields["assignee_id"]; ok {
		if req.AssigneeID != nil {
			if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
				ID:          parseUUID(*req.AssigneeID),
				WorkspaceID: parseUUID(workspaceID),
			}); err != nil {
				writeError(w, http.StatusBadRequest, "assignee must be a valid agent in this workspace")
				return
			}
			params.AssigneeID = parseUUID(*req.AssigneeID)
		}
	}
	if _, ok := rawFields["project_id"]; ok {
		if req.ProjectID != nil {
			params.ProjectID = parseUUID(*req.ProjectID)
		} else {
			params.ProjectID = pgtype.UUID{Valid: false}
		}
	}

	autopilot, err := h.Queries.UpdateAutopilot(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update autopilot")
		return
	}

	resp := autopilotToResponse(autopilot)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{"autopilot": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	if _, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteAutopilot(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot")
		return
	}

	h.publish(protocol.EventAutopilotDeleted, workspaceID, "member", userID, map[string]any{"autopilot_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// ── Trigger management ──────────────────────────────────────────────────────

func (h *Handler) CreateAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	ap, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(autopilotID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	var req CreateAutopilotTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusBadRequest, "kind is required")
		return
	}
	if req.Kind != "schedule" && req.Kind != "webhook" && req.Kind != "api" {
		writeError(w, http.StatusBadRequest, "kind must be schedule, webhook, or api")
		return
	}
	if req.Kind == "schedule" && (req.CronExpression == nil || *req.CronExpression == "") {
		writeError(w, http.StatusBadRequest, "cron_expression is required for schedule triggers")
		return
	}

	if req.Timezone != nil && *req.Timezone != "" {
		if err := service.ValidateTimezone(*req.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	var nextRunAt pgtype.Timestamptz
	if req.Kind == "schedule" && req.CronExpression != nil {
		tz := "UTC"
		if req.Timezone != nil && *req.Timezone != "" {
			tz = *req.Timezone
		}
		t, err := computeNextRun(*req.CronExpression, tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		nextRunAt = pgtype.Timestamptz{Time: t, Valid: true}
	}

	trigger, err := h.Queries.CreateAutopilotTrigger(r.Context(), db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           req.Kind,
		Enabled:        true,
		CronExpression: ptrToText(req.CronExpression),
		Timezone:       ptrToText(req.Timezone),
		NextRunAt:      nextRunAt,
		Label:          ptrToText(req.Label),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create trigger")
		return
	}

	resp := triggerToResponse(trigger)
	userID, _ := requireUserID(w, r)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": autopilotID,
		"trigger":      resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	workspaceID := h.resolveWorkspaceID(r)

	// Verify autopilot belongs to workspace.
	if _, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(autopilotID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	prev, err := h.Queries.GetAutopilotTrigger(r.Context(), parseUUID(triggerID))
	if err != nil || uuidToString(prev.AutopilotID) != autopilotID {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}

	var req UpdateAutopilotTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateAutopilotTriggerParams{
		ID:             prev.ID,
		CronExpression: prev.CronExpression,
		Timezone:       prev.Timezone,
		NextRunAt:      prev.NextRunAt,
		Label:          prev.Label,
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}
	if req.CronExpression != nil {
		params.CronExpression = pgtype.Text{String: *req.CronExpression, Valid: true}
	}
	if req.Timezone != nil {
		if *req.Timezone != "" {
			if err := service.ValidateTimezone(*req.Timezone); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		params.Timezone = pgtype.Text{String: *req.Timezone, Valid: true}
	}
	if req.Label != nil {
		params.Label = pgtype.Text{String: *req.Label, Valid: true}
	}

	// Recompute next_run_at if cron or timezone changed.
	cronExpr := prev.CronExpression.String
	if req.CronExpression != nil {
		cronExpr = *req.CronExpression
	}
	tz := "UTC"
	if prev.Timezone.Valid {
		tz = prev.Timezone.String
	}
	if req.Timezone != nil {
		tz = *req.Timezone
	}
	if prev.Kind == "schedule" && cronExpr != "" {
		t, err := computeNextRun(cronExpr, tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.NextRunAt = pgtype.Timestamptz{Time: t, Valid: true}
	}

	trigger, err := h.Queries.UpdateAutopilotTrigger(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update trigger")
		return
	}

	resp := triggerToResponse(trigger)
	userID, _ := requireUserID(w, r)
	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": autopilotID,
		"trigger":      resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	workspaceID := h.resolveWorkspaceID(r)

	if _, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(autopilotID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	trigger, err := h.Queries.GetAutopilotTrigger(r.Context(), parseUUID(triggerID))
	if err != nil || uuidToString(trigger.AutopilotID) != autopilotID {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteAutopilotTrigger(r.Context(), parseUUID(triggerID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete trigger")
		return
	}

	h.publish(protocol.EventAutopilotUpdated, workspaceID, "member", userID, map[string]any{
		"autopilot_id": autopilotID,
		"trigger_id":   triggerID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Runs ────────────────────────────────────────────────────────────────────

func (h *Handler) ListAutopilotRuns(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	if _, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(autopilotID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}

	limit := int32(20)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = int32(v)
		}
	}
	if limit > 100 {
		limit = 100
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = int32(v)
		}
	}

	runs, err := h.Queries.ListAutopilotRuns(r.Context(), db.ListAutopilotRunsParams{
		AutopilotID: parseUUID(autopilotID),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	resp := make([]AutopilotRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = runToResponse(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": resp, "total": len(resp)})
}

// ── Manual trigger ──────────────────────────────────────────────────────────

func (h *Handler) TriggerAutopilot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return
	}
	if autopilot.Status != "active" {
		writeError(w, http.StatusBadRequest, "autopilot is not active")
		return
	}

	run, err := h.AutopilotService.DispatchAutopilot(r.Context(), autopilot, pgtype.UUID{}, "manual", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to trigger autopilot: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, runToResponse(*run))
}
