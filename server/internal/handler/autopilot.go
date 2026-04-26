package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type AutopilotResponse struct {
	ID                 string                     `json:"id"`
	WorkspaceID        string                     `json:"workspace_id"`
	Title              string                     `json:"title"`
	Description        *string                    `json:"description"`
	Status             string                     `json:"status"`
	Mode               string                     `json:"mode"`
	AgentID            string                     `json:"agent_id"`
	ProjectID          *string                    `json:"project_id"`
	Priority           string                     `json:"priority"`
	IssueTitleTemplate string                     `json:"issue_title_template"`
	CreatedBy          *string                    `json:"created_by"`
	CreatedAt          string                     `json:"created_at"`
	UpdatedAt          string                     `json:"updated_at"`
	Triggers           []AutopilotTriggerResponse `json:"triggers,omitempty"`
}

type AutopilotTriggerResponse struct {
	ID          string  `json:"id"`
	AutopilotID string  `json:"autopilot_id"`
	Type        string  `json:"type"`
	Label       *string `json:"label"`
	Cron        *string `json:"cron"`
	Timezone    string  `json:"timezone"`
	Status      string  `json:"status"`
	NextRunAt   *string `json:"next_run_at"`
	LastRunAt   *string `json:"last_run_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type AutopilotRunResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	AutopilotID    string  `json:"autopilot_id"`
	TriggerID      *string `json:"trigger_id"`
	Source         string  `json:"source"`
	Status         string  `json:"status"`
	ScheduledFor   *string `json:"scheduled_for"`
	StartedAt      *string `json:"started_at"`
	CompletedAt    *string `json:"completed_at"`
	CreatedIssueID *string `json:"created_issue_id"`
	CreatedTaskID  *string `json:"created_task_id"`
	Error          *string `json:"error"`
	CreatedAt      string  `json:"created_at"`
}

func autopilotToResponse(a db.Autopilot) AutopilotResponse {
	return AutopilotResponse{
		ID:                 uuidToString(a.ID),
		WorkspaceID:        uuidToString(a.WorkspaceID),
		Title:              a.Title,
		Description:        textToPtr(a.Description),
		Status:             a.Status,
		Mode:               a.Mode,
		AgentID:            uuidToString(a.AgentID),
		ProjectID:          uuidToPtr(a.ProjectID),
		Priority:           a.Priority,
		IssueTitleTemplate: a.IssueTitleTemplate,
		CreatedBy:          uuidToPtr(a.CreatedBy),
		CreatedAt:          timestampToString(a.CreatedAt),
		UpdatedAt:          timestampToString(a.UpdatedAt),
	}
}

func autopilotTriggerToResponse(t db.AutopilotTrigger) AutopilotTriggerResponse {
	return AutopilotTriggerResponse{
		ID:          uuidToString(t.ID),
		AutopilotID: uuidToString(t.AutopilotID),
		Type:        t.Type,
		Label:       textToPtr(t.Label),
		Cron:        textToPtr(t.Cron),
		Timezone:    t.Timezone,
		Status:      t.Status,
		NextRunAt:   timestampToPtr(t.NextRunAt),
		LastRunAt:   timestampToPtr(t.LastRunAt),
		CreatedAt:   timestampToString(t.CreatedAt),
		UpdatedAt:   timestampToString(t.UpdatedAt),
	}
}

func autopilotRunToResponse(run db.AutopilotRun) AutopilotRunResponse {
	return AutopilotRunResponse{
		ID:             uuidToString(run.ID),
		WorkspaceID:    uuidToString(run.WorkspaceID),
		AutopilotID:    uuidToString(run.AutopilotID),
		TriggerID:      uuidToPtr(run.TriggerID),
		Source:         run.Source,
		Status:         run.Status,
		ScheduledFor:   timestampToPtr(run.ScheduledFor),
		StartedAt:      timestampToPtr(run.StartedAt),
		CompletedAt:    timestampToPtr(run.CompletedAt),
		CreatedIssueID: uuidToPtr(run.CreatedIssueID),
		CreatedTaskID:  uuidToPtr(run.CreatedTaskID),
		Error:          textToPtr(run.Error),
		CreatedAt:      timestampToString(run.CreatedAt),
	}
}

type CreateAutopilotRequest struct {
	Title              string  `json:"title"`
	Description        *string `json:"description"`
	Status             string  `json:"status"`
	Mode               string  `json:"mode"`
	Agent              *string `json:"agent"`
	AgentID            *string `json:"agent_id"`
	Project            *string `json:"project"`
	ProjectID          *string `json:"project_id"`
	Priority           string  `json:"priority"`
	IssueTitleTemplate string  `json:"issue_title_template"`
}

type UpdateAutopilotRequest struct {
	Title              *string `json:"title"`
	Description        *string `json:"description"`
	Status             *string `json:"status"`
	Mode               *string `json:"mode"`
	Agent              *string `json:"agent"`
	AgentID            *string `json:"agent_id"`
	Project            *string `json:"project"`
	ProjectID          *string `json:"project_id"`
	Priority           *string `json:"priority"`
	IssueTitleTemplate *string `json:"issue_title_template"`
}

type CreateAutopilotTriggerRequest struct {
	Type     string  `json:"type"`
	Label    *string `json:"label"`
	Cron     string  `json:"cron"`
	Timezone string  `json:"timezone"`
	Status   string  `json:"status"`
}

type UpdateAutopilotTriggerRequest struct {
	Type     *string `json:"type"`
	Label    *string `json:"label"`
	Cron     *string `json:"cron"`
	Timezone *string `json:"timezone"`
	Status   *string `json:"status"`
}

func (h *Handler) ListAutopilots(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	var statusFilter pgtype.Text
	if s := strings.TrimSpace(r.URL.Query().Get("status")); s != "" {
		if !validAutopilotStatus(s) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	autopilots, err := h.Queries.ListAutopilots(r.Context(), db.ListAutopilotsParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       int32(limit),
		Offset:      int32(offset),
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list autopilots")
		return
	}
	total, err := h.Queries.CountAutopilots(r.Context(), db.CountAutopilotsParams{
		WorkspaceID: parseUUID(workspaceID),
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count autopilots")
		return
	}

	resp := make([]AutopilotResponse, len(autopilots))
	for i, autopilot := range autopilots {
		resp[i] = autopilotToResponse(autopilot)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"autopilots": resp,
		"total":      total,
		"has_more":   int64(offset+limit) < total,
	})
}

func (h *Handler) GetAutopilot(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	resp := autopilotToResponse(autopilot)
	triggers, err := h.Queries.ListAutopilotTriggers(r.Context(), autopilot.ID)
	if err == nil && len(triggers) > 0 {
		resp.Triggers = make([]AutopilotTriggerResponse, len(triggers))
		for i, trigger := range triggers {
			resp.Triggers[i] = autopilotTriggerToResponse(trigger)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateAutopilot(w http.ResponseWriter, r *http.Request) {
	var req CreateAutopilotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "create_issue"
	}
	if mode != "create_issue" {
		writeError(w, http.StatusBadRequest, "only create_issue mode is supported")
		return
	}
	status := req.Status
	if status == "" {
		status = "active"
	}
	if !validAutopilotStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validIssuePriority(priority) {
		writeError(w, http.StatusBadRequest, "invalid priority")
		return
	}

	agentInput := firstNonEmptyPtr(req.AgentID, req.Agent)
	agent, ok := h.validateAutopilotAgent(w, r, workspaceID, agentInput)
	if !ok {
		return
	}
	projectID, ok := h.resolveAutopilotProject(w, r, workspaceID, firstNonEmptyPtr(req.ProjectID, req.Project), false)
	if !ok {
		return
	}

	autopilot, err := h.Queries.CreateAutopilot(r.Context(), db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(workspaceID),
		Title:              strings.TrimSpace(req.Title),
		Description:        ptrToText(req.Description),
		Status:             status,
		Mode:               mode,
		AgentID:            agent.ID,
		ProjectID:          projectID,
		Priority:           priority,
		IssueTitleTemplate: strings.TrimSpace(req.IssueTitleTemplate),
		CreatedBy:          parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create autopilot")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, parseUUID(workspaceID), pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_created", map[string]any{
		"autopilot_id": uuidToString(autopilot.ID),
		"agent_id":     uuidToString(autopilot.AgentID),
		"mode":         autopilot.Mode,
	})

	writeJSON(w, http.StatusCreated, autopilotToResponse(autopilot))
}

func (h *Handler) UpdateAutopilot(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateAutopilotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(body, &raw)

	params := db.UpdateAutopilotParams{
		ID:          autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
	}
	changed := map[string]any{"autopilot_id": uuidToString(autopilot.ID)}

	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		params.Title = strToText(strings.TrimSpace(*req.Title))
		changed["title_changed"] = true
	}
	if _, exists := raw["description"]; exists {
		params.SetDescription = true
		params.Description = ptrToText(req.Description)
		changed["description_changed"] = true
	}
	if req.Status != nil {
		if !validAutopilotStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		params.Status = strToText(*req.Status)
		changed["status"] = *req.Status
	}
	if req.Mode != nil {
		if *req.Mode != "create_issue" {
			writeError(w, http.StatusBadRequest, "only create_issue mode is supported")
			return
		}
		params.Mode = strToText(*req.Mode)
	}
	if req.Priority != nil {
		if !validIssuePriority(*req.Priority) {
			writeError(w, http.StatusBadRequest, "invalid priority")
			return
		}
		params.Priority = strToText(*req.Priority)
		changed["priority"] = *req.Priority
	}
	if req.IssueTitleTemplate != nil {
		params.IssueTitleTemplate = pgtype.Text{String: strings.TrimSpace(*req.IssueTitleTemplate), Valid: true}
		changed["issue_title_template_changed"] = true
	}
	if agentInput, provided := optionalFirstPtr(req.AgentID, req.Agent); provided {
		agent, ok := h.validateAutopilotAgent(w, r, workspaceID, agentInput)
		if !ok {
			return
		}
		params.AgentID = agent.ID
		changed["agent_id"] = uuidToString(agent.ID)
	}
	if projectInput, provided := optionalFirstPtr(req.ProjectID, req.Project); provided {
		projectID, ok := h.resolveAutopilotProject(w, r, workspaceID, projectInput, true)
		if !ok {
			return
		}
		params.SetProjectID = true
		params.ProjectID = projectID
		changed["project_id"] = uuidToString(projectID)
	}

	updated, err := h.Queries.UpdateAutopilot(r.Context(), params)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "autopilot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update autopilot")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, updated.WorkspaceID, pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_updated", changed)
	writeJSON(w, http.StatusOK, autopilotToResponse(updated))
}

func (h *Handler) DeleteAutopilot(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	deleted, err := h.Queries.DeleteAutopilot(r.Context(), db.DeleteAutopilotParams{
		ID:          autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "autopilot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, deleted.WorkspaceID, pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_deleted", map[string]any{
		"autopilot_id": uuidToString(deleted.ID),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DisableAutopilot(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	updated, err := h.Queries.UpdateAutopilot(r.Context(), db.UpdateAutopilotParams{
		ID:          autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
		Status:      strToText("paused"),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "autopilot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to disable autopilot")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, updated.WorkspaceID, pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_disabled", map[string]any{
		"autopilot_id": uuidToString(updated.ID),
		"status":       updated.Status,
	})
	writeJSON(w, http.StatusOK, autopilotToResponse(updated))
}

func (h *Handler) ListAutopilotRuns(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	runs, err := h.Queries.ListAutopilotRuns(r.Context(), db.ListAutopilotRunsParams{
		WorkspaceID: autopilot.WorkspaceID,
		AutopilotID: autopilot.ID,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list autopilot runs")
		return
	}
	total, err := h.Queries.CountAutopilotRuns(r.Context(), db.CountAutopilotRunsParams{
		WorkspaceID: autopilot.WorkspaceID,
		AutopilotID: autopilot.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count autopilot runs")
		return
	}
	resp := make([]AutopilotRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = autopilotRunToResponse(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"runs":     resp,
		"total":    total,
		"has_more": int64(offset+limit) < total,
	})
}

func (h *Handler) TriggerAutopilot(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if autopilot.Mode != "create_issue" {
		writeError(w, http.StatusBadRequest, "only create_issue mode is supported")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	run, err := h.AutopilotService.TriggerCreateIssue(r.Context(), autopilot, actorType, parseUUID(actorID))
	if err != nil {
		if run.ID.Valid {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "failed to trigger autopilot",
				"run":   autopilotRunToResponse(run),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to trigger autopilot")
		return
	}

	h.recordAutopilotActivity(r, autopilot.WorkspaceID, run.CreatedIssueID, actorType, parseUUID(actorID), "autopilot_triggered", map[string]any{
		"autopilot_id": uuidToString(autopilot.ID),
		"run_id":       uuidToString(run.ID),
		"issue_id":     uuidToString(run.CreatedIssueID),
		"task_id":      uuidToString(run.CreatedTaskID),
		"source":       run.Source,
	})
	writeJSON(w, http.StatusCreated, autopilotRunToResponse(run))
}

func (h *Handler) CreateAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateAutopilotTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	triggerType := strings.TrimSpace(req.Type)
	if triggerType == "" {
		triggerType = "schedule"
	}
	if triggerType != "schedule" {
		writeError(w, http.StatusBadRequest, "only schedule triggers are supported")
		return
	}
	status := req.Status
	if status == "" {
		status = "active"
	}
	if !validAutopilotStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	cronExpr, timezone, nextRunAt, ok := normalizeScheduleForHTTP(w, req.Cron, req.Timezone, time.Now())
	if !ok {
		return
	}

	trigger, err := h.Queries.CreateAutopilotTrigger(r.Context(), db.CreateAutopilotTriggerParams{
		AutopilotID: autopilot.ID,
		Type:        triggerType,
		Label:       ptrToText(req.Label),
		Cron:        strToText(cronExpr),
		Timezone:    timezone,
		Status:      status,
		NextRunAt:   pgtype.Timestamptz{Time: nextRunAt, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create autopilot trigger")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, autopilot.WorkspaceID, pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_trigger_created", map[string]any{
		"autopilot_id": uuidToString(autopilot.ID),
		"trigger_id":   uuidToString(trigger.ID),
		"type":         trigger.Type,
		"status":       trigger.Status,
	})
	writeJSON(w, http.StatusCreated, autopilotTriggerToResponse(trigger))
}

func (h *Handler) UpdateAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	trigger, ok := h.loadAutopilotTrigger(w, r, autopilot)
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateAutopilotTriggerRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(body, &raw)

	params := db.UpdateAutopilotTriggerParams{
		ID:          trigger.ID,
		AutopilotID: autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
	}
	changed := map[string]any{
		"autopilot_id": uuidToString(autopilot.ID),
		"trigger_id":   uuidToString(trigger.ID),
	}
	recomputeNextRun := false
	cronExpr := ""
	if trigger.Cron.Valid {
		cronExpr = trigger.Cron.String
	}
	timezone := trigger.Timezone

	if req.Type != nil {
		if strings.TrimSpace(*req.Type) != "schedule" {
			writeError(w, http.StatusBadRequest, "only schedule triggers are supported")
			return
		}
	}
	if _, exists := raw["label"]; exists {
		params.SetLabel = true
		params.Label = ptrToText(req.Label)
		changed["label_changed"] = true
	}
	if req.Cron != nil {
		cronExpr = *req.Cron
		params.Cron = strToText(strings.TrimSpace(cronExpr))
		recomputeNextRun = true
		changed["cron_changed"] = true
	}
	if req.Timezone != nil {
		timezone = *req.Timezone
		params.Timezone = strToText(strings.TrimSpace(timezone))
		recomputeNextRun = true
		changed["timezone"] = strings.TrimSpace(timezone)
	}
	if req.Status != nil {
		if !validAutopilotStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		params.Status = strToText(*req.Status)
		changed["status"] = *req.Status
		if *req.Status == "active" {
			recomputeNextRun = true
		}
	}
	if recomputeNextRun {
		normalizedCron, normalizedTimezone, nextRunAt, ok := normalizeScheduleForHTTP(w, cronExpr, timezone, time.Now())
		if !ok {
			return
		}
		params.Cron = strToText(normalizedCron)
		params.Timezone = strToText(normalizedTimezone)
		params.SetNextRunAt = true
		params.NextRunAt = pgtype.Timestamptz{Time: nextRunAt, Valid: true}
	}

	updated, err := h.Queries.UpdateAutopilotTrigger(r.Context(), params)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "autopilot trigger not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update autopilot trigger")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, autopilot.WorkspaceID, pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_trigger_updated", changed)
	writeJSON(w, http.StatusOK, autopilotTriggerToResponse(updated))
}

func (h *Handler) DeleteAutopilotTrigger(w http.ResponseWriter, r *http.Request) {
	autopilot, ok := h.loadAutopilot(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(autopilot.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "autopilot not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	trigger, ok := h.loadAutopilotTrigger(w, r, autopilot)
	if !ok {
		return
	}

	deleted, err := h.Queries.DeleteAutopilotTrigger(r.Context(), db.DeleteAutopilotTriggerParams{
		ID:          trigger.ID,
		AutopilotID: autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "autopilot trigger not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete autopilot trigger")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.recordAutopilotActivity(r, autopilot.WorkspaceID, pgtype.UUID{}, actorType, parseUUID(actorID), "autopilot_trigger_deleted", map[string]any{
		"autopilot_id": uuidToString(autopilot.ID),
		"trigger_id":   uuidToString(deleted.ID),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) loadAutopilot(w http.ResponseWriter, r *http.Request) (db.Autopilot, bool) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	autopilot, err := h.Queries.GetAutopilotInWorkspace(r.Context(), db.GetAutopilotInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot not found")
		return db.Autopilot{}, false
	}
	return autopilot, true
}

func (h *Handler) loadAutopilotTrigger(w http.ResponseWriter, r *http.Request, autopilot db.Autopilot) (db.AutopilotTrigger, bool) {
	id := chi.URLParam(r, "triggerId")
	trigger, err := h.Queries.GetAutopilotTriggerInWorkspace(r.Context(), db.GetAutopilotTriggerInWorkspaceParams{
		ID:          parseUUID(id),
		AutopilotID: autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "autopilot trigger not found")
		return db.AutopilotTrigger{}, false
	}
	return trigger, true
}

func (h *Handler) validateAutopilotAgent(w http.ResponseWriter, r *http.Request, workspaceID string, input string) (db.Agent, bool) {
	agent, found := h.resolveAgentForAutopilot(r, workspaceID, strings.TrimSpace(input))
	if !found {
		writeError(w, http.StatusBadRequest, "agent not found")
		return db.Agent{}, false
	}
	if ok, msg := h.canAssignAgent(r.Context(), r, uuidToString(agent.ID), workspaceID); !ok {
		writeError(w, http.StatusForbidden, msg)
		return db.Agent{}, false
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "agent has no runtime")
		return db.Agent{}, false
	}
	return agent, true
}

func (h *Handler) resolveAgentForAutopilot(r *http.Request, workspaceID string, input string) (db.Agent, bool) {
	if input == "" {
		return db.Agent{}, false
	}
	if id := parseUUID(input); id.Valid {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          id,
			WorkspaceID: parseUUID(workspaceID),
		})
		return agent, err == nil
	}
	agents, err := h.Queries.ListAllAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		return db.Agent{}, false
	}
	for _, agent := range agents {
		if strings.EqualFold(agent.Name, input) {
			return agent, true
		}
	}
	return db.Agent{}, false
}

func (h *Handler) resolveAutopilotProject(w http.ResponseWriter, r *http.Request, workspaceID, input string, allowClear bool) (pgtype.UUID, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		if allowClear {
			return pgtype.UUID{}, true
		}
		return pgtype.UUID{}, true
	}
	projectID := parseUUID(input)
	if !projectID.Valid {
		writeError(w, http.StatusBadRequest, "invalid project")
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          projectID,
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusBadRequest, "project not found")
		return pgtype.UUID{}, false
	}
	return projectID, true
}

func (h *Handler) recordAutopilotActivity(r *http.Request, workspaceID, issueID pgtype.UUID, actorType string, actorID pgtype.UUID, action string, details map[string]any) {
	encoded, err := json.Marshal(redact.InputMap(details))
	if err != nil {
		encoded = []byte("{}")
	}
	_, _ = h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		ActorType:   strToText(actorType),
		ActorID:     actorID,
		Action:      action,
		Details:     encoded,
	})
}

func firstNonEmptyPtr(primary, fallback *string) string {
	if primary != nil && strings.TrimSpace(*primary) != "" {
		return *primary
	}
	if fallback != nil {
		return *fallback
	}
	return ""
}

func optionalFirstPtr(primary, fallback *string) (string, bool) {
	if primary != nil {
		return *primary, true
	}
	if fallback != nil {
		return *fallback, true
	}
	return "", false
}

func validAutopilotStatus(status string) bool {
	return status == "active" || status == "paused"
}

func validIssuePriority(priority string) bool {
	switch priority {
	case "urgent", "high", "medium", "low", "none":
		return true
	default:
		return false
	}
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func normalizeScheduleForHTTP(w http.ResponseWriter, cronExpr, timezone string, now time.Time) (string, string, time.Time, bool) {
	normalizedCron, normalizedTimezone, nextRunAt, err := service.NormalizeSchedule(cronExpr, timezone, now)
	if err != nil {
		switch err.Error() {
		case "invalid cron", "cron is required":
			writeError(w, http.StatusBadRequest, err.Error())
		case "invalid timezone", "timezone must be an IANA timezone":
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusBadRequest, "invalid schedule")
		}
		return "", "", time.Time{}, false
	}
	return normalizedCron, normalizedTimezone, nextRunAt, true
}
