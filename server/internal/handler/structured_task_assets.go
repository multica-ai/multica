package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
)

type StructuredTaskTemplateParameter struct {
	Key          string `json:"key"`
	Description  string `json:"description"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"default_value"`
}

type StructuredTaskTemplateResponse struct {
	ID           string                            `json:"id"`
	WorkspaceID  string                            `json:"workspace_id"`
	TemplateName string                            `json:"template_name"`
	Description  string                            `json:"description"`
	Goal         string                            `json:"goal"`
	Audience     []string                          `json:"audience"`
	Output       string                            `json:"output"`
	Constraints  []string                          `json:"constraints"`
	Style        []string                          `json:"style"`
	Parameters   []StructuredTaskTemplateParameter `json:"parameters"`
	Scope        string                            `json:"scope"`
	CreatedBy    *string                           `json:"created_by"`
	CreatedAt    string                            `json:"created_at"`
	UpdatedAt    string                            `json:"updated_at"`
}

type CreateStructuredTaskTemplateRequest struct {
	TemplateName string                            `json:"template_name"`
	Description  string                            `json:"description"`
	Goal         string                            `json:"goal"`
	Audience     []string                          `json:"audience"`
	Output       string                            `json:"output"`
	Constraints  []string                          `json:"constraints"`
	Style        []string                          `json:"style"`
	Parameters   []StructuredTaskTemplateParameter `json:"parameters"`
	Scope        string                            `json:"scope"`
}

type StructuredTaskHistoryResponse struct {
	ID             string                     `json:"id"`
	WorkspaceID    string                     `json:"workspace_id"`
	IssueID        *string                    `json:"issue_id"`
	Goal           string                     `json:"goal"`
	UsedTemplateID *string                    `json:"used_template_id"`
	ClarityStatus  string                     `json:"clarity_status"`
	Spec           StructuredTaskSpecResponse `json:"spec"`
	CreatedBy      *string                    `json:"created_by"`
	ExecutedAt     string                     `json:"executed_at"`
}

type CreateStructuredTaskHistoryRequest struct {
	IssueID        *string                    `json:"issue_id"`
	Goal           string                     `json:"goal"`
	UsedTemplateID *string                    `json:"used_template_id"`
	ClarityStatus  string                     `json:"clarity_status"`
	Spec           StructuredTaskSpecResponse `json:"spec"`
}

func decodeStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return []string{}
	}
	return items
}

func decodeTemplateParameters(raw []byte) []StructuredTaskTemplateParameter {
	if len(raw) == 0 {
		return []StructuredTaskTemplateParameter{}
	}
	var items []StructuredTaskTemplateParameter
	if err := json.Unmarshal(raw, &items); err != nil {
		return []StructuredTaskTemplateParameter{}
	}
	return items
}

func (h *Handler) ListStructuredTaskTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	rows, err := h.DB.Query(
		r.Context(),
		`SELECT id, workspace_id, template_name, description, goal, audience, output, constraints, style, parameters, scope, created_by, created_at, updated_at
		 FROM structured_task_template
		 WHERE workspace_id = $1
		 ORDER BY created_at DESC`,
		parseUUID(workspaceID),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list structured task templates")
		return
	}
	defer rows.Close()

	templates := make([]StructuredTaskTemplateResponse, 0)
	for rows.Next() {
		var id, wsID, templateName, description, goal, output, scope string
		var audienceRaw, constraintsRaw, styleRaw, parametersRaw []byte
		var createdBy pgtype.UUID
		var createdAt, updatedAt pgtype.Timestamptz
		if err := rows.Scan(
			&id,
			&wsID,
			&templateName,
			&description,
			&goal,
			&audienceRaw,
			&output,
			&constraintsRaw,
			&styleRaw,
			&parametersRaw,
			&scope,
			&createdBy,
			&createdAt,
			&updatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read structured task templates")
			return
		}

		templates = append(templates, StructuredTaskTemplateResponse{
			ID:           id,
			WorkspaceID:  wsID,
			TemplateName: templateName,
			Description:  description,
			Goal:         goal,
			Audience:     decodeStringArray(audienceRaw),
			Output:       output,
			Constraints:  decodeStringArray(constraintsRaw),
			Style:        decodeStringArray(styleRaw),
			Parameters:   decodeTemplateParameters(parametersRaw),
			Scope:        scope,
			CreatedBy:    uuidToPtr(createdBy),
			CreatedAt:    timestampToString(createdAt),
			UpdatedAt:    timestampToString(updatedAt),
		})
	}

	writeJSON(w, http.StatusOK, templates)
}

func (h *Handler) CreateStructuredTaskTemplate(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateStructuredTaskTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TemplateName == "" || req.Goal == "" || req.Output == "" {
		writeError(w, http.StatusBadRequest, "template_name, goal, and output are required")
		return
	}
	if req.Scope == "" {
		req.Scope = "personal"
	}

	audienceJSON, _ := json.Marshal(req.Audience)
	constraintsJSON, _ := json.Marshal(req.Constraints)
	styleJSON, _ := json.Marshal(req.Style)
	parametersJSON, _ := json.Marshal(req.Parameters)

	var resp StructuredTaskTemplateResponse
	var audienceRaw, constraintsRaw, styleRaw, parametersRaw []byte
	var createdBy pgtype.UUID
	var createdAt, updatedAt pgtype.Timestamptz
	err := h.DB.QueryRow(
		r.Context(),
		`INSERT INTO structured_task_template (
			workspace_id, template_name, description, goal, audience, output, constraints, style, parameters, scope, created_by
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7::jsonb,$8::jsonb,$9::jsonb,$10,$11)
		RETURNING id, workspace_id, template_name, description, goal, audience, output, constraints, style, parameters, scope, created_by, created_at, updated_at`,
		parseUUID(workspaceID),
		req.TemplateName,
		req.Description,
		req.Goal,
		string(audienceJSON),
		req.Output,
		string(constraintsJSON),
		string(styleJSON),
		string(parametersJSON),
		req.Scope,
		parseUUID(userID),
	).Scan(
		&resp.ID,
		&resp.WorkspaceID,
		&resp.TemplateName,
		&resp.Description,
		&resp.Goal,
		&audienceRaw,
		&resp.Output,
		&constraintsRaw,
		&styleRaw,
		&parametersRaw,
		&resp.Scope,
		&createdBy,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create structured task template")
		return
	}

	resp.Audience = decodeStringArray(audienceRaw)
	resp.Constraints = decodeStringArray(constraintsRaw)
	resp.Style = decodeStringArray(styleRaw)
	resp.Parameters = decodeTemplateParameters(parametersRaw)
	resp.CreatedBy = uuidToPtr(createdBy)
	resp.CreatedAt = timestampToString(createdAt)
	resp.UpdatedAt = timestampToString(updatedAt)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListStructuredTaskHistory(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	rows, err := h.DB.Query(
		r.Context(),
		`SELECT id, workspace_id, issue_id, goal, used_template_id, clarity_status, spec, created_by, executed_at
		 FROM structured_task_history
		 WHERE workspace_id = $1
		 ORDER BY executed_at DESC
		 LIMIT 100`,
		parseUUID(workspaceID),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list structured task history")
		return
	}
	defer rows.Close()

	history := make([]StructuredTaskHistoryResponse, 0)
	for rows.Next() {
		var item StructuredTaskHistoryResponse
		var issueID, usedTemplateID, createdBy pgtype.UUID
		var specRaw []byte
		var executedAt pgtype.Timestamptz
		if err := rows.Scan(
			&item.ID,
			&item.WorkspaceID,
			&issueID,
			&item.Goal,
			&usedTemplateID,
			&item.ClarityStatus,
			&specRaw,
			&createdBy,
			&executedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read structured task history")
			return
		}
		_ = json.Unmarshal(specRaw, &item.Spec)
		item.IssueID = uuidToPtr(issueID)
		item.UsedTemplateID = uuidToPtr(usedTemplateID)
		item.CreatedBy = uuidToPtr(createdBy)
		item.ExecutedAt = timestampToString(executedAt)
		history = append(history, item)
	}

	writeJSON(w, http.StatusOK, history)
}

func (h *Handler) CreateStructuredTaskHistory(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateStructuredTaskHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Goal == "" {
		writeError(w, http.StatusBadRequest, "goal is required")
		return
	}
	if req.ClarityStatus == "" {
		req.ClarityStatus = "clear"
	}

	specJSON, _ := json.Marshal(req.Spec)
	var item StructuredTaskHistoryResponse
	var issueID, usedTemplateID, createdBy pgtype.UUID
	var specRaw []byte
	var executedAt pgtype.Timestamptz
	err := h.DB.QueryRow(
		r.Context(),
		`INSERT INTO structured_task_history (
			workspace_id, issue_id, goal, used_template_id, clarity_status, spec, created_by
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
		RETURNING id, workspace_id, issue_id, goal, used_template_id, clarity_status, spec, created_by, executed_at`,
		parseUUID(workspaceID),
		ptrToUUID(req.IssueID),
		req.Goal,
		ptrToUUID(req.UsedTemplateID),
		req.ClarityStatus,
		string(specJSON),
		parseUUID(userID),
	).Scan(
		&item.ID,
		&item.WorkspaceID,
		&issueID,
		&item.Goal,
		&usedTemplateID,
		&item.ClarityStatus,
		&specRaw,
		&createdBy,
		&executedAt,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create structured task history")
		return
	}
	_ = json.Unmarshal(specRaw, &item.Spec)
	item.IssueID = uuidToPtr(issueID)
	item.UsedTemplateID = uuidToPtr(usedTemplateID)
	item.CreatedBy = uuidToPtr(createdBy)
	item.ExecutedAt = timestampToString(executedAt)
	writeJSON(w, http.StatusCreated, item)
}

func ptrToUUID(value *string) pgtype.UUID {
	if value == nil || *value == "" {
		return pgtype.UUID{Valid: false}
	}
	return parseUUID(*value)
}
