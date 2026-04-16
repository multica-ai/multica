package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- Response types ----

type WorkflowResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Steps          any     `json:"steps"`
	Mode           string  `json:"mode"`
	PlannerAgentID *string `json:"planner_agent_id"`
	CreatedBy      string  `json:"created_by"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func workflowToResponse(w db.Workflow) WorkflowResponse {
	var steps any
	json.Unmarshal(w.Steps, &steps)
	return WorkflowResponse{
		ID:             uuidToString(w.ID),
		WorkspaceID:    uuidToString(w.WorkspaceID),
		Name:           w.Name,
		Description:    w.Description,
		Steps:          steps,
		Mode:           w.Mode,
		PlannerAgentID: uuidToPtr(w.PlannerAgentID),
		CreatedBy:      uuidToString(w.CreatedBy),
		CreatedAt:      timestampToString(w.CreatedAt),
		UpdatedAt:      timestampToString(w.UpdatedAt),
	}
}

type WorkflowRunResponse struct {
	ID               string  `json:"id"`
	WorkflowID       string  `json:"workflow_id"`
	WorkspaceID      string  `json:"workspace_id"`
	TriggerType      string  `json:"trigger_type"`
	TriggerID        *string `json:"trigger_id"`
	Status           string  `json:"status"`
	CurrentStepIndex int32   `json:"current_step_index"`
	IssueID          *string `json:"issue_id"`
	DynamicSteps     any     `json:"dynamic_steps"`
	PlannerTaskID    *string `json:"planner_task_id"`
	StartedAt        *string `json:"started_at"`
	CompletedAt      *string `json:"completed_at"`
	CreatedAt        string  `json:"created_at"`
}

func workflowRunToResponse(r db.WorkflowRun) WorkflowRunResponse {
	var dynamicSteps any
	if len(r.DynamicSteps) > 0 {
		json.Unmarshal(r.DynamicSteps, &dynamicSteps)
	}
	return WorkflowRunResponse{
		ID:               uuidToString(r.ID),
		WorkflowID:       uuidToString(r.WorkflowID),
		WorkspaceID:      uuidToString(r.WorkspaceID),
		TriggerType:      r.TriggerType,
		TriggerID:        uuidToPtr(r.TriggerID),
		Status:           r.Status,
		CurrentStepIndex: r.CurrentStepIndex,
		IssueID:          uuidToPtr(r.IssueID),
		DynamicSteps:     dynamicSteps,
		PlannerTaskID:    uuidToPtr(r.PlannerTaskID),
		StartedAt:        timestampToPtr(r.StartedAt),
		CompletedAt:      timestampToPtr(r.CompletedAt),
		CreatedAt:        timestampToString(r.CreatedAt),
	}
}

type WorkflowStepRunResponse struct {
	ID              string  `json:"id"`
	WorkflowRunID   string  `json:"workflow_run_id"`
	StepIndex       int32   `json:"step_index"`
	StepType        string  `json:"step_type"`
	Status          string  `json:"status"`
	AgentTaskID     *string `json:"agent_task_id"`
	ReviewerID      *string `json:"reviewer_id"`
	Decision        *string `json:"decision"`
	DecisionComment *string `json:"decision_comment"`
	RedirectToStep  *int32  `json:"redirect_to_step"`
	StartedAt       *string `json:"started_at"`
	CompletedAt     *string `json:"completed_at"`
	CreatedAt       string  `json:"created_at"`
}

func stepRunToResponse(sr db.WorkflowStepRun) WorkflowStepRunResponse {
	var redirectTo *int32
	if sr.RedirectToStep.Valid {
		redirectTo = &sr.RedirectToStep.Int32
	}
	return WorkflowStepRunResponse{
		ID:              uuidToString(sr.ID),
		WorkflowRunID:   uuidToString(sr.WorkflowRunID),
		StepIndex:       sr.StepIndex,
		StepType:        sr.StepType,
		Status:          sr.Status,
		AgentTaskID:     uuidToPtr(sr.AgentTaskID),
		ReviewerID:      uuidToPtr(sr.ReviewerID),
		Decision:        textToPtr(sr.Decision),
		DecisionComment: textToPtr(sr.DecisionComment),
		RedirectToStep:  redirectTo,
		StartedAt:       timestampToPtr(sr.StartedAt),
		CompletedAt:     timestampToPtr(sr.CompletedAt),
		CreatedAt:       timestampToString(sr.CreatedAt),
	}
}

type ScheduleResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	WorkflowID   string  `json:"workflow_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	ScheduleType string  `json:"schedule_type"`
	CronExpr     *string `json:"cron_expr"`
	OnceAt       *string `json:"once_at"`
	Timezone     string  `json:"timezone"`
	Weekdays     []int32 `json:"weekdays"`
	TimeOfDay    string  `json:"time_of_day"`
	NextRunAt    string  `json:"next_run_at"`
	LastRunAt    *string `json:"last_run_at"`
	Enabled      bool    `json:"enabled"`
	CreatedBy    string  `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func scheduleToResponse(s db.Schedule) ScheduleResponse {
	var timeStr string
	if s.TimeOfDay.Valid {
		usec := s.TimeOfDay.Microseconds
		hours := usec / 3_600_000_000
		minutes := (usec % 3_600_000_000) / 60_000_000
		timeStr = strconv.FormatInt(hours, 10) + ":" + strconv.FormatInt(minutes, 10)
		if len(timeStr) == 3 {
			timeStr = "0" + timeStr
		}
		// Pad to HH:MM
		parts := []byte(timeStr)
		if len(parts) < 5 {
			timeStr = "0" + timeStr
		}
	}
	return ScheduleResponse{
		ID:           uuidToString(s.ID),
		WorkspaceID:  uuidToString(s.WorkspaceID),
		WorkflowID:   uuidToString(s.WorkflowID),
		Name:         s.Name,
		Description:  s.Description,
		ScheduleType: s.ScheduleType,
		CronExpr:     textToPtr(s.CronExpr),
		OnceAt:       timestampToPtr(s.OnceAt),
		Timezone:     s.Timezone,
		Weekdays:     s.Weekdays,
		TimeOfDay:    timeStr,
		NextRunAt:    timestampToString(s.NextRunAt),
		LastRunAt:    timestampToPtr(s.LastRunAt),
		Enabled:      s.Enabled,
		CreatedBy:    uuidToString(s.CreatedBy),
		CreatedAt:    timestampToString(s.CreatedAt),
		UpdatedAt:    timestampToString(s.UpdatedAt),
	}
}

// ---- Workflow CRUD ----

type CreateWorkflowRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Steps          any    `json:"steps"`
	Mode           string `json:"mode"`
	PlannerAgentID string `json:"planner_agent_id"`
}

func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	workflows, err := h.Queries.ListWorkflows(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	resp := make([]WorkflowResponse, len(workflows))
	for i, wf := range workflows {
		resp[i] = workflowToResponse(wf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": resp, "total": len(resp)})
}

func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	wf, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, workflowToResponse(wf))
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	stepsJSON, err := json.Marshal(req.Steps)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid steps")
		return
	}

	// Validate steps structure
	var steps []service.WorkflowStep
	if err := json.Unmarshal(stepsJSON, &steps); err != nil {
		writeError(w, http.StatusBadRequest, "invalid steps format")
		return
	}
	for i, s := range steps {
		switch s.Type {
		case "agent":
			if s.AgentID == "" {
				writeError(w, http.StatusBadRequest, "step "+strconv.Itoa(i)+" (agent) requires agent_id")
				return
			}
		case "approval":
			if len(s.ReviewerIDs) == 0 {
				writeError(w, http.StatusBadRequest, "step "+strconv.Itoa(i)+" (approval) requires reviewer_ids")
				return
			}
		case "review":
			if s.ReviewerType == "agent" && s.ReviewAgentID == "" {
				writeError(w, http.StatusBadRequest, "step "+strconv.Itoa(i)+" (review) with agent reviewer requires review_agent_id")
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "step "+strconv.Itoa(i)+" has invalid type: "+s.Type)
			return
		}
	}

	mode := req.Mode
	if mode == "" {
		mode = "sequential"
	}
	if mode != "sequential" && mode != "orchestrated" {
		writeError(w, http.StatusBadRequest, "mode must be sequential or orchestrated")
		return
	}

	var plannerAgentID pgtype.UUID
	if req.PlannerAgentID != "" {
		plannerAgentID = parseUUID(req.PlannerAgentID)
	}
	if mode == "orchestrated" && !plannerAgentID.Valid {
		writeError(w, http.StatusBadRequest, "orchestrated mode requires planner_agent_id")
		return
	}

	wf, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID:    parseUUID(workspaceID),
		Name:           req.Name,
		Description:    req.Description,
		Steps:          stepsJSON,
		CreatedBy:      parseUUID(userID),
		Mode:           mode,
		PlannerAgentID: plannerAgentID,
		IsCeoCommand:   false,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}

	resp := workflowToResponse(wf)
	h.publish(protocol.EventWorkflowCreated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateWorkflowRequest struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	Steps          any     `json:"steps"`
	Mode           *string `json:"mode"`
	PlannerAgentID *string `json:"planner_agent_id"`
}

func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	if _, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	var req UpdateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	params := db.UpdateWorkflowParams{ID: parseUUID(id)}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Steps != nil {
		stepsJSON, err := json.Marshal(req.Steps)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid steps")
			return
		}
		params.Steps = stepsJSON
	}
	if req.Mode != nil {
		params.Mode = pgtype.Text{String: *req.Mode, Valid: true}
	}
	if req.PlannerAgentID != nil {
		if *req.PlannerAgentID == "" {
			params.PlannerAgentID = pgtype.UUID{Valid: false}
		} else {
			params.PlannerAgentID = parseUUID(*req.PlannerAgentID)
		}
	}

	wf, err := h.Queries.UpdateWorkflow(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
		return
	}

	resp := workflowToResponse(wf)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	wf, err := h.Queries.ArchiveWorkflow(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	h.publish(protocol.EventWorkflowDeleted, workspaceID, "member", userID, map[string]any{"id": uuidToString(wf.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"id": uuidToString(wf.ID)})
}

// ---- Workflow Runs ----

func (h *Handler) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	runs, err := h.Queries.ListWorkflowRuns(r.Context(), db.ListWorkflowRunsParams{
		WorkflowID: parseUUID(workflowID),
		Limit:      int32(limit),
		Offset:     int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow runs")
		return
	}

	resp := make([]WorkflowRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = workflowRunToResponse(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": resp, "total": len(resp)})
}

func (h *Handler) GetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.Queries.GetWorkflowRun(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow run not found")
		return
	}

	stepRuns, err := h.Queries.ListWorkflowStepRuns(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list step runs")
		return
	}

	stepResp := make([]WorkflowStepRunResponse, len(stepRuns))
	for i, sr := range stepRuns {
		stepResp[i] = stepRunToResponse(sr)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run":       workflowRunToResponse(run),
		"step_runs": stepResp,
	})
}

func (h *Handler) TriggerWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify workflow belongs to workspace
	if _, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	run, err := h.WorkflowService.StartWorkflow(r.Context(), parseUUID(id), "manual", parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to trigger workflow: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, workflowRunToResponse(*run))
}

func (h *Handler) CancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := h.Queries.CancelWorkflowRun(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow run not found or not cancellable")
		return
	}
	writeJSON(w, http.StatusOK, workflowRunToResponse(run))
}

// ---- Approval ----

type ApprovalDecisionRequest struct {
	Decision string `json:"decision"` // "approved", "rejected", "stopped"
	Comment  string `json:"comment"`
}

func (h *Handler) ApproveStepRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req ApprovalDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Decision != "approved" && req.Decision != "rejected" && req.Decision != "stopped" {
		writeError(w, http.StatusBadRequest, "decision must be approved, rejected, or stopped")
		return
	}

	if err := h.WorkflowService.HandleApprovalDecision(r.Context(), parseUUID(id), req.Decision, req.Comment); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to process approval: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) ListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	steps, err := h.Queries.GetPendingApprovalStepsByReviewer(r.Context(), db.GetPendingApprovalStepsByReviewerParams{
		ReviewerID:  parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending approvals")
		return
	}

	resp := make([]map[string]any, len(steps))
	for i, s := range steps {
		resp[i] = map[string]any{
			"id":              uuidToString(s.ID),
			"workflow_run_id": uuidToString(s.WorkflowRunID),
			"workflow_id":     uuidToString(s.WorkflowID),
			"step_index":      s.StepIndex,
			"status":          s.Status,
			"created_at":      timestampToString(s.CreatedAt),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"approvals": resp, "total": len(resp)})
}

// ---- Plan submission ----

type SubmitPlanRequest struct {
	Steps []service.WorkflowStep `json:"steps"`
}

func (h *Handler) SubmitWorkflowPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req SubmitPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Steps) == 0 {
		writeError(w, http.StatusBadRequest, "steps are required")
		return
	}

	if err := h.WorkflowService.SubmitPlan(r.Context(), parseUUID(id), req.Steps); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ---- Review decision ----

type ReviewDecisionRequest struct {
	Decision     string `json:"decision"`       // "approved", "rejected", "stopped", "redirect"
	Comment      string `json:"comment"`
	RedirectToStep *int `json:"redirect_to_step"`
}

func (h *Handler) SubmitReview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req ReviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	validDecisions := map[string]bool{"approved": true, "rejected": true, "stopped": true, "redirect": true}
	if !validDecisions[req.Decision] {
		writeError(w, http.StatusBadRequest, "decision must be approved, rejected, stopped, or redirect")
		return
	}
	if req.Decision == "redirect" && req.RedirectToStep == nil {
		writeError(w, http.StatusBadRequest, "redirect decision requires redirect_to_step")
		return
	}

	if err := h.WorkflowService.HandleReviewDecision(r.Context(), parseUUID(id), req.Decision, req.Comment, req.RedirectToStep); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to process review: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ---- Schedule CRUD ----

type CreateScheduleRequest struct {
	WorkflowID   string  `json:"workflow_id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	ScheduleType string  `json:"schedule_type"` // "once" or "recurring"
	CronExpr     *string `json:"cron_expr"`
	OnceAt       *string `json:"once_at"` // ISO 8601
	Timezone     string  `json:"timezone"`
	Weekdays     []int32 `json:"weekdays"`
	TimeOfDay    string  `json:"time_of_day"` // "HH:MM"
	Enabled      *bool   `json:"enabled"`
}

func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	schedules, err := h.Queries.ListSchedules(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}

	resp := make([]ScheduleResponse, len(schedules))
	for i, s := range schedules {
		resp[i] = scheduleToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": resp, "total": len(resp)})
}

func (h *Handler) ListSchedulesForWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	workflowID := chi.URLParam(r, "id")
	schedules, err := h.Queries.ListSchedulesForWorkflow(r.Context(), db.ListSchedulesForWorkflowParams{
		WorkspaceID: parseUUID(workspaceID),
		WorkflowID:  parseUUID(workflowID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}

	resp := make([]ScheduleResponse, len(schedules))
	for i, s := range schedules {
		resp[i] = scheduleToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": resp, "total": len(resp)})
}

func (h *Handler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	s, err := h.Queries.GetScheduleInWorkspace(r.Context(), db.GetScheduleInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	writeJSON(w, http.StatusOK, scheduleToResponse(s))
}

func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req CreateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.WorkflowID == "" {
		writeError(w, http.StatusBadRequest, "workflow_id is required")
		return
	}
	if req.ScheduleType != "once" && req.ScheduleType != "recurring" {
		writeError(w, http.StatusBadRequest, "schedule_type must be once or recurring")
		return
	}
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Shanghai"
	}
	if req.Weekdays == nil {
		req.Weekdays = []int32{}
	}

	// Load user timezone for time calculations
	loc, tzErr := time.LoadLocation(req.Timezone)
	if tzErr != nil {
		loc = time.FixedZone("CST", 8*3600) // fallback to UTC+8
	}

	// Calculate next_run_at
	var nextRunAt time.Time
	var cronExpr pgtype.Text
	var onceAt pgtype.Timestamptz

	switch req.ScheduleType {
	case "once":
		if req.OnceAt == nil || *req.OnceAt == "" {
			writeError(w, http.StatusBadRequest, "once_at is required for one-time schedules")
			return
		}
		var t time.Time
		var parseErr error
		// Try RFC3339 first (already has timezone)
		t, parseErr = time.Parse(time.RFC3339, *req.OnceAt)
		if parseErr != nil {
			// No timezone — parse as local time in user's timezone
			for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
				t, parseErr = time.ParseInLocation(layout, *req.OnceAt, loc)
				if parseErr == nil {
					break
				}
			}
		}
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid once_at format")
			return
		}
		nextRunAt = t
		onceAt = pgtype.Timestamptz{Time: t, Valid: true}

	case "recurring":
		// If cron_expr not provided, generate from weekdays + time_of_day
		if req.CronExpr == nil || *req.CronExpr == "" {
			hour, minute := 9, 0 // default 09:00
			if req.TimeOfDay != "" {
				if t, err := time.Parse("15:04", req.TimeOfDay); err == nil {
					hour, minute = t.Hour(), t.Minute()
				}
			}
			dowPart := "*"
			if len(req.Weekdays) > 0 {
				days := ""
				for i, d := range req.Weekdays {
					if i > 0 {
						days += ","
					}
					days += strconv.Itoa(int(d))
				}
				dowPart = days
			}
			generated := strconv.Itoa(minute) + " " + strconv.Itoa(hour) + " * * " + dowPart
			req.CronExpr = &generated
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		sched, err := parser.Parse(*req.CronExpr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cron expression: "+err.Error())
			return
		}
		// Calculate next_run_at in user's timezone so cron hours match local time
		nextRunAt = sched.Next(time.Now().In(loc)).UTC()
		cronExpr = pgtype.Text{String: *req.CronExpr, Valid: true}
	}

	// Parse time_of_day
	timeOfDay := pgtype.Time{Microseconds: 9 * 3600 * 1_000_000, Valid: true} // default 09:00
	if req.TimeOfDay != "" {
		t, err := time.Parse("15:04", req.TimeOfDay)
		if err == nil {
			usec := int64(t.Hour())*3_600_000_000 + int64(t.Minute())*60_000_000
			timeOfDay = pgtype.Time{Microseconds: usec, Valid: true}
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	s, err := h.Queries.CreateSchedule(r.Context(), db.CreateScheduleParams{
		WorkspaceID:  parseUUID(workspaceID),
		WorkflowID:   parseUUID(req.WorkflowID),
		Name:         req.Name,
		Description:  req.Description,
		ScheduleType: req.ScheduleType,
		CronExpr:     cronExpr,
		OnceAt:       onceAt,
		Timezone:     req.Timezone,
		Weekdays:     req.Weekdays,
		TimeOfDay:    timeOfDay,
		NextRunAt:    pgtype.Timestamptz{Time: nextRunAt, Valid: true},
		Enabled:      enabled,
		CreatedBy:    parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create schedule: "+err.Error())
		return
	}

	resp := scheduleToResponse(s)
	h.publish(protocol.EventScheduleCreated, workspaceID, "member", userID, map[string]any{"schedule": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateScheduleRequest struct {
	Name         *string  `json:"name"`
	Description  *string  `json:"description"`
	WorkflowID   *string  `json:"workflow_id"`
	ScheduleType *string  `json:"schedule_type"`
	CronExpr     *string  `json:"cron_expr"`
	OnceAt       *string  `json:"once_at"`
	Timezone     *string  `json:"timezone"`
	Weekdays     *[]int32 `json:"weekdays"`
	TimeOfDay    *string  `json:"time_of_day"`
	NextRunAt    *string  `json:"next_run_at"`
	Enabled      *bool    `json:"enabled"`
}

func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	if _, err := h.Queries.GetScheduleInWorkspace(r.Context(), db.GetScheduleInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	var req UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	params := db.UpdateScheduleParams{ID: parseUUID(id)}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.WorkflowID != nil {
		params.WorkflowID = parseUUID(*req.WorkflowID)
	}
	if req.ScheduleType != nil {
		params.ScheduleType = pgtype.Text{String: *req.ScheduleType, Valid: true}
	}
	if req.CronExpr != nil {
		params.CronExpr = pgtype.Text{String: *req.CronExpr, Valid: true}
	}
	if req.Timezone != nil {
		params.Timezone = pgtype.Text{String: *req.Timezone, Valid: true}
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	s, err := h.Queries.UpdateSchedule(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update schedule")
		return
	}

	resp := scheduleToResponse(s)
	h.publish(protocol.EventScheduleUpdated, workspaceID, "member", userID, map[string]any{"schedule": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteSchedule(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	h.publish(protocol.EventScheduleDeleted, workspaceID, "member", userID, map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *Handler) ToggleSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	s, err := h.Queries.ToggleSchedule(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	resp := scheduleToResponse(s)
	h.publish(protocol.EventScheduleUpdated, workspaceID, "member", userID, map[string]any{"schedule": resp})
	writeJSON(w, http.StatusOK, resp)
}
