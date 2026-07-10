package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type autopilotTemplateRequest struct {
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	ExecutionMode      string  `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
	TriggerKind        string  `json:"trigger_kind"`
	CronExpression     *string `json:"cron_expression"`
	Timezone           *string `json:"timezone"`
}

type autopilotTemplateResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspace_id"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	ExecutionMode      string  `json:"execution_mode"`
	IssueTitleTemplate *string `json:"issue_title_template"`
	TriggerKind        string  `json:"trigger_kind"`
	CronExpression     *string `json:"cron_expression"`
	Timezone           *string `json:"timezone"`
	CreatedBy          string  `json:"created_by"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func autopilotTemplateToResponse(template db.AutopilotTemplate) autopilotTemplateResponse {
	return autopilotTemplateResponse{
		ID: uuidToString(template.ID), WorkspaceID: uuidToString(template.WorkspaceID),
		Name: template.Name, Description: template.Description,
		ExecutionMode:      template.ExecutionMode,
		IssueTitleTemplate: textToPtr(template.IssueTitleTemplate),
		TriggerKind:        template.TriggerKind,
		CronExpression:     textToPtr(template.CronExpression), Timezone: textToPtr(template.Timezone),
		CreatedBy: uuidToString(template.CreatedBy),
		CreatedAt: timestampToString(template.CreatedAt), UpdatedAt: timestampToString(template.UpdatedAt),
	}
}

func validateAutopilotTemplateRequest(req autopilotTemplateRequest) (autopilotTemplateRequest, string) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, "name is required"
	}
	if req.ExecutionMode == "" {
		req.ExecutionMode = "create_issue"
	}
	if req.ExecutionMode != "create_issue" && req.ExecutionMode != "run_only" {
		return req, "execution_mode must be create_issue or run_only"
	}
	if req.IssueTitleTemplate != nil {
		if err := service.ValidateIssueTitleTemplate(*req.IssueTitleTemplate); err != nil {
			return req, err.Error()
		}
	}
	if req.TriggerKind == "" {
		req.TriggerKind = "schedule"
	}
	if req.TriggerKind != "schedule" && req.TriggerKind != "webhook" {
		return req, "trigger_kind must be schedule or webhook"
	}
	if req.TriggerKind == "schedule" {
		if req.CronExpression == nil || strings.TrimSpace(*req.CronExpression) == "" {
			return req, "cron_expression is required for schedule templates"
		}
		if req.Timezone == nil || strings.TrimSpace(*req.Timezone) == "" {
			return req, "timezone is required for schedule templates"
		}
		if _, err := service.ComputeNextRun(*req.CronExpression, *req.Timezone); err != nil {
			return req, "invalid schedule: " + err.Error()
		}
	} else {
		req.CronExpression = nil
		req.Timezone = nil
	}
	return req, ""
}

func (h *Handler) ListAutopilotTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	rows, err := h.Queries.ListAutopilotTemplates(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Autopilot templates")
		return
	}
	resp := make([]autopilotTemplateResponse, len(rows))
	for i, row := range rows {
		resp[i] = autopilotTemplateToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": resp, "total": len(resp)})
}

func (h *Handler) CreateAutopilotTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	var req autopilotTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, message := validateAutopilotTemplateRequest(req)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	template, err := h.Queries.CreateAutopilotTemplate(r.Context(), db.CreateAutopilotTemplateParams{
		WorkspaceID: parseUUID(workspaceID), Name: req.Name, Description: req.Description,
		ExecutionMode: req.ExecutionMode, IssueTitleTemplate: ptrToText(req.IssueTitleTemplate),
		TriggerKind: req.TriggerKind, CronExpression: ptrToText(req.CronExpression),
		Timezone: ptrToText(req.Timezone), CreatedBy: member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "an Autopilot template with this name already exists")
		return
	}
	writeJSON(w, http.StatusCreated, autopilotTemplateToResponse(template))
}

func (h *Handler) loadManageableAutopilotTemplate(w http.ResponseWriter, r *http.Request) (db.AutopilotTemplate, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return db.AutopilotTemplate{}, pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return db.AutopilotTemplate{}, pgtype.UUID{}, false
	}
	template, err := h.Queries.GetAutopilotTemplateInWorkspace(r.Context(), db.GetAutopilotTemplateInWorkspaceParams{
		ID: id, WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "Autopilot template not found")
		return db.AutopilotTemplate{}, pgtype.UUID{}, false
	}
	if !roleAllowed(member.Role, "owner", "admin") && template.CreatedBy != member.UserID {
		writeError(w, http.StatusForbidden, "only the template creator or a workspace admin can manage it")
		return db.AutopilotTemplate{}, pgtype.UUID{}, false
	}
	return template, parseUUID(workspaceID), true
}

func (h *Handler) UpdateAutopilotTemplate(w http.ResponseWriter, r *http.Request) {
	template, workspaceID, ok := h.loadManageableAutopilotTemplate(w, r)
	if !ok {
		return
	}
	var req autopilotTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// PUT is a full replacement; seed omitted optional values from the current row.
	if req.Name == "" {
		req.Name = template.Name
	}
	if req.ExecutionMode == "" {
		req.ExecutionMode = template.ExecutionMode
	}
	if req.TriggerKind == "" {
		req.TriggerKind = template.TriggerKind
	}
	if req.CronExpression == nil {
		req.CronExpression = textToPtr(template.CronExpression)
	}
	if req.Timezone == nil {
		req.Timezone = textToPtr(template.Timezone)
	}
	req, message := validateAutopilotTemplateRequest(req)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	updated, err := h.Queries.UpdateAutopilotTemplate(r.Context(), db.UpdateAutopilotTemplateParams{
		ID: template.ID, WorkspaceID: workspaceID,
		Name:               pgtype.Text{String: req.Name, Valid: true},
		Description:        pgtype.Text{String: req.Description, Valid: true},
		ExecutionMode:      pgtype.Text{String: req.ExecutionMode, Valid: true},
		IssueTitleTemplate: ptrToText(req.IssueTitleTemplate),
		TriggerKind:        pgtype.Text{String: req.TriggerKind, Valid: true},
		CronExpression:     ptrToText(req.CronExpression), Timezone: ptrToText(req.Timezone),
	})
	if err != nil {
		writeError(w, http.StatusConflict, "failed to update Autopilot template")
		return
	}
	writeJSON(w, http.StatusOK, autopilotTemplateToResponse(updated))
}

func (h *Handler) DeleteAutopilotTemplate(w http.ResponseWriter, r *http.Request) {
	template, workspaceID, ok := h.loadManageableAutopilotTemplate(w, r)
	if !ok {
		return
	}
	count, err := h.Queries.DeleteAutopilotTemplate(r.Context(), db.DeleteAutopilotTemplateParams{
		ID: template.ID, WorkspaceID: workspaceID,
	})
	if err != nil || count == 0 {
		writeError(w, http.StatusInternalServerError, "failed to delete Autopilot template")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
