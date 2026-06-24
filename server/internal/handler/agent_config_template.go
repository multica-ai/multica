package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- Response shapes ---

type AgentConfigTemplateResponse struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Scope       string          `json:"scope"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Config      json.RawMessage `json:"config"`
	IsDefault   bool            `json:"is_default"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func configTemplateToResponse(t db.AgentConfigTemplate) AgentConfigTemplateResponse {
	cfg := json.RawMessage(t.Config)
	if cfg == nil {
		cfg = json.RawMessage("{}")
	}
	resp := AgentConfigTemplateResponse{
		ID:          uuidToString(t.ID),
		WorkspaceID: uuidToString(t.WorkspaceID),
		Scope:       t.Scope,
		Name:        t.Name,
		Description: t.Description,
		Config:      cfg,
		IsDefault:   t.IsDefault,
		CreatedAt:   t.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   t.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if t.CreatedBy.Valid {
		resp.CreatedBy = uuidToString(t.CreatedBy)
	}
	return resp
}

// --- List ---

func (h *Handler) ListAgentConfigTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != "system" && scope != "personal" {
		writeError(w, http.StatusBadRequest, "scope must be 'system' or 'personal'")
		return
	}

	var templates []db.AgentConfigTemplate
	var err error

	// Personal templates: only return the current user's own templates.
	if scope == "personal" {
		userID := requestUserID(r)
		if userID == "" {
			writeJSON(w, http.StatusOK, []AgentConfigTemplateResponse{})
			return
		}
		member, merr := h.getWorkspaceMember(r.Context(), userID, workspaceID)
		if merr != nil {
			writeJSON(w, http.StatusOK, []AgentConfigTemplateResponse{})
			return
		}
		templates, err = h.Queries.ListAgentConfigTemplatesByCreator(r.Context(), db.ListAgentConfigTemplatesByCreatorParams{
			WorkspaceID: wsUUID,
			CreatedBy:   member.ID,
		})
	} else {
		// System templates (or all): use the general query.
		var scopeText pgtype.Text
		if scope != "" {
			scopeText = pgtype.Text{String: scope, Valid: true}
		}
		templates, err = h.Queries.ListAgentConfigTemplates(r.Context(), db.ListAgentConfigTemplatesParams{
			WorkspaceID: wsUUID,
			Scope:       scopeText,
		})
	}

	if err != nil {
		slog.Error("list agent config templates failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list templates")
		return
	}

	resp := make([]AgentConfigTemplateResponse, 0, len(templates))
	for _, t := range templates {
		resp = append(resp, configTemplateToResponse(t))
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Get ---

func (h *Handler) GetAgentConfigTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	templateID := chi.URLParam(r, "templateId")

	tplUUID, ok := parseUUIDOrBadRequest(w, templateID, "template_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	tpl, err := h.Queries.GetAgentConfigTemplateInWorkspace(r.Context(), db.GetAgentConfigTemplateInWorkspaceParams{
		ID:          tplUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get template")
		return
	}

	writeJSON(w, http.StatusOK, configTemplateToResponse(tpl))
}

// --- Create ---

type CreateAgentConfigTemplateRequest struct {
	Scope       string          `json:"scope"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Config      json.RawMessage `json:"config"`
	IsDefault   bool            `json:"is_default"`
}

func (h *Handler) CreateAgentConfigTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID := requestUserID(r)

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var req CreateAgentConfigTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Scope != "system" && req.Scope != "personal" {
		writeError(w, http.StatusBadRequest, "scope must be 'system' or 'personal'")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// System templates require owner/admin role
	if req.Scope == "system" {
		member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
		if !ok {
			return
		}
		if member.Role != "owner" && member.Role != "admin" {
			writeError(w, http.StatusForbidden, "only owner/admin can create system templates")
			return
		}
	}

	// Resolve creator (member ID) for personal templates
	var createdBy pgtype.UUID
	if req.Scope == "personal" && userID != "" {
		if member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err == nil {
			createdBy = member.ID
		}
	}

	cfg := req.Config
	if cfg == nil {
		cfg = json.RawMessage("{}")
	}

	tpl, err := h.Queries.CreateAgentConfigTemplate(r.Context(), db.CreateAgentConfigTemplateParams{
		WorkspaceID: wsUUID,
		Scope:       req.Scope,
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		Config:      cfg,
		IsDefault:   req.IsDefault,
		CreatedBy:   createdBy,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a default template of this scope already exists")
			return
		}
		slog.Error("create agent config template failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}

	writeJSON(w, http.StatusCreated, configTemplateToResponse(tpl))
}

// --- Update ---

type UpdateAgentConfigTemplateRequest struct {
	Name        *string          `json:"name,omitempty"`
	Description *string          `json:"description,omitempty"`
	Config      *json.RawMessage `json:"config,omitempty"`
	IsDefault   *bool            `json:"is_default,omitempty"`
}

func (h *Handler) UpdateAgentConfigTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	templateID := chi.URLParam(r, "templateId")

	tplUUID, ok := parseUUIDOrBadRequest(w, templateID, "template_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Verify template exists and user has permission
	tpl, err := h.Queries.GetAgentConfigTemplateInWorkspace(r.Context(), db.GetAgentConfigTemplateInWorkspaceParams{
		ID:          tplUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get template")
		return
	}

	// Permission check
	userID := requestUserID(r)
	if tpl.Scope == "system" {
		member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
		if !ok {
			return
		}
		if member.Role != "owner" && member.Role != "admin" {
			writeError(w, http.StatusForbidden, "only owner/admin can edit system templates")
			return
		}
	} else {
		// Personal: only creator can edit
		if tpl.CreatedBy.Valid && userID != "" {
			member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
			if err != nil || member.ID != tpl.CreatedBy {
				writeError(w, http.StatusForbidden, "only the creator can edit this template")
				return
			}
		}
	}

	var req UpdateAgentConfigTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var nameText, descText pgtype.Text
	var configBytes []byte
	var defaultBool pgtype.Bool

	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		nameText = pgtype.Text{String: n, Valid: true}
	}
	if req.Description != nil {
		descText = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Config != nil {
		configBytes = []byte(*req.Config)
	}
	if req.IsDefault != nil {
		defaultBool = pgtype.Bool{Bool: *req.IsDefault, Valid: true}
	}

	// Each template carries its own instructions history. When a template's
	// instructions change, record a version keyed by that template — the
	// template-keyed replacement for the legacy scope/member history.
	previousInstructions, _ := instructionsFromConfigJSON(tpl.Config)
	nextInstructions, hasInstructions := instructionsFromConfigJSON(configBytes)

	// Resolve the acting member once (outside the tx): needed for the history
	// actor_id, and for personal scope the history member_id is the template
	// owner (== actor, since only the creator can edit a personal template).
	var actorMemberID pgtype.UUID
	if userID := requestUserID(r); userID != "" {
		if member, merr := h.getWorkspaceMember(r.Context(), userID, workspaceID); merr == nil {
			actorMemberID = member.ID
		}
	}

	q := h.Queries
	commit := func() error { return nil }
	if h.TxStarter != nil {
		realTx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update template")
			return
		}
		defer realTx.Rollback(r.Context())
		q = h.Queries.WithTx(realTx)
		commit = func() error { return realTx.Commit(r.Context()) }
	}

	// Setting a new default must vacate the existing default slot first —
	// the partial unique index allows at most one default per (workspace,
	// scope) [system] or (workspace, created_by) [personal]. Without this
	// swap the UpdateAgentConfigTemplate below would hit a 23505 conflict.
	// Personal defaults are scoped per creator, so only clear within the
	// same created_by; system templates pass a NULL created_by to clear
	// across the whole workspace+scope.
	if req.IsDefault != nil && *req.IsDefault && !tpl.IsDefault {
		var createdByFilter pgtype.UUID
		if tpl.Scope == "personal" {
			createdByFilter = tpl.CreatedBy
		}
		if err := q.ClearOtherDefaultTemplates(r.Context(), db.ClearOtherDefaultTemplatesParams{
			WorkspaceID: wsUUID,
			Scope:       tpl.Scope,
			ID:          tplUUID,
			CreatedBy:   createdByFilter,
		}); err != nil {
			slog.Error("clear other default templates failed", "template_id", templateID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update template")
			return
		}
	}

	updated, err := q.UpdateAgentConfigTemplate(r.Context(), db.UpdateAgentConfigTemplateParams{
		ID:          tplUUID,
		Name:        nameText,
		Description: descText,
		Config:      configBytes,
		IsDefault:   defaultBool,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a default template of this scope already exists")
			return
		}
		slog.Error("update agent config template failed", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update template")
		return
	}

	// Record an instructions version when the template's instructions changed.
	// Every template (default or not) carries its own history.
	if hasInstructions && nextInstructions != previousInstructions {
		memberID := historyMemberID(updated.Scope, updated.CreatedBy)
		if _, err := q.InsertInstructionsHistory(r.Context(), db.InsertInstructionsHistoryParams{
			WorkspaceID: wsUUID,
			Scope:       updated.Scope,
			MemberID:    memberID,
			TemplateID: updated.ID,
			Content:    nextInstructions,
			ActorID:    actorMemberID,
		}); err != nil {
			slog.Error("insert instructions history for template failed", "template_id", templateID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update template")
			return
		}
	}

	if err := commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update template")
		return
	}

	writeJSON(w, http.StatusOK, configTemplateToResponse(updated))
}

// --- Delete ---

func (h *Handler) DeleteAgentConfigTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	templateID := chi.URLParam(r, "templateId")

	tplUUID, ok := parseUUIDOrBadRequest(w, templateID, "template_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Verify template exists and user has permission
	tpl, err := h.Queries.GetAgentConfigTemplateInWorkspace(r.Context(), db.GetAgentConfigTemplateInWorkspaceParams{
		ID:          tplUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get template")
		return
	}

	// Permission check
	userID := requestUserID(r)
	if tpl.Scope == "system" {
		member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
		if !ok {
			return
		}
		if member.Role != "owner" && member.Role != "admin" {
			writeError(w, http.StatusForbidden, "only owner/admin can delete system templates")
			return
		}
	} else {
		if tpl.CreatedBy.Valid && userID != "" {
			member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
			if err != nil || member.ID != tpl.CreatedBy {
				writeError(w, http.StatusForbidden, "only the creator can delete this template")
				return
			}
		}
	}

	// Check for references before deleting
	refCount, err := h.Queries.CountAgentTemplateReferences(r.Context(), tplUUID)
	if err != nil {
		slog.Error("count template references failed", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check template references")
		return
	}
	if refCount > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("template is referenced by %d agent(s); unbind them first", refCount))
		return
	}

	if err := h.Queries.DeleteAgentConfigTemplate(r.Context(), tplUUID); err != nil {
		slog.Error("delete agent config template failed", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete template")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Agent template binding ---

type AgentTemplateBindingResponse struct {
	SystemTemplateID    string `json:"system_template_id,omitempty"`
	PersonalTemplateID  string `json:"personal_template_id,omitempty"`
	SkipSystemTemplate  bool   `json:"skip_system_template"`
	SkipPersonalTemplate bool  `json:"skip_personal_template"`
}

func (h *Handler) GetAgentTemplateBinding(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent_id")
	if !ok {
		return
	}

	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}

	resp := AgentTemplateBindingResponse{
		SkipSystemTemplate:   agent.SkipSystemTemplate,
		SkipPersonalTemplate: agent.SkipPersonalTemplate,
	}
	if agent.SystemTemplateID.Valid {
		resp.SystemTemplateID = uuidToString(agent.SystemTemplateID)
	}
	if agent.PersonalTemplateID.Valid {
		resp.PersonalTemplateID = uuidToString(agent.PersonalTemplateID)
	}
	writeJSON(w, http.StatusOK, resp)
}

type UpdateAgentTemplateBindingRequest struct {
	SystemTemplateID    *string `json:"system_template_id"`
	PersonalTemplateID  *string `json:"personal_template_id"`
	SkipSystemTemplate  *bool   `json:"skip_system_template"`
	SkipPersonalTemplate *bool  `json:"skip_personal_template"`
}

func (h *Handler) UpdateAgentTemplateBinding(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")

	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent_id")
	if !ok {
		return
	}

	// Verify agent exists
	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get agent")
		return
	}

	// Verify user has access to agent's workspace
	wsID := uuidToString(agent.WorkspaceID)
	if !h.requireDaemonWorkspaceAccess(w, r, wsID) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	var req UpdateAgentTemplateBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate template IDs if provided
	if req.SystemTemplateID != nil && *req.SystemTemplateID != "" {
		tplUUID, ok := parseUUIDOrBadRequest(w, *req.SystemTemplateID, "system_template_id")
		if !ok {
			return
		}
		_, err := h.Queries.GetAgentConfigTemplateInWorkspace(r.Context(), db.GetAgentConfigTemplateInWorkspaceParams{
			ID:          tplUUID,
			WorkspaceID: agent.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid system_template_id")
			return
		}
	}

	if req.PersonalTemplateID != nil && *req.PersonalTemplateID != "" {
		tplUUID, ok := parseUUIDOrBadRequest(w, *req.PersonalTemplateID, "personal_template_id")
		if !ok {
			return
		}
		_, err := h.Queries.GetAgentConfigTemplateInWorkspace(r.Context(), db.GetAgentConfigTemplateInWorkspaceParams{
			ID:          tplUUID,
			WorkspaceID: agent.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid personal_template_id")
			return
		}
	}

	// Apply skip flag changes
	if req.SkipSystemTemplate != nil {
		if _, err := h.Queries.UpdateAgent(r.Context(), db.UpdateAgentParams{
			ID:                 agentUUID,
			SkipSystemTemplate: pgtype.Bool{Bool: *req.SkipSystemTemplate, Valid: true},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update skip_system_template")
			return
		}
		// If skipping, also clear any specific template
		if *req.SkipSystemTemplate {
			h.Queries.ClearAgentSystemTemplate(r.Context(), agentUUID)
		}
	}

	if req.SkipPersonalTemplate != nil {
		if _, err := h.Queries.UpdateAgent(r.Context(), db.UpdateAgentParams{
			ID:                   agentUUID,
			SkipPersonalTemplate: pgtype.Bool{Bool: *req.SkipPersonalTemplate, Valid: true},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update skip_personal_template")
			return
		}
		// If skipping, also clear any specific template
		if *req.SkipPersonalTemplate {
			h.Queries.ClearAgentPersonalTemplate(r.Context(), agentUUID)
		}
	}

	// Apply template ID changes
	if req.SystemTemplateID != nil {
		if *req.SystemTemplateID == "" {
			if _, err := h.Queries.ClearAgentSystemTemplate(r.Context(), agentUUID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to clear system template")
				return
			}
		} else {
			tplUUID := parseUUID(*req.SystemTemplateID)
			if _, err := h.Queries.UpdateAgentTemplateBinding(r.Context(), db.UpdateAgentTemplateBindingParams{
				ID:                 agentUUID,
				SystemTemplateID:   pgtype.UUID{Bytes: tplUUID.Bytes, Valid: true},
				PersonalTemplateID: pgtype.UUID{}, // no change
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update system template binding")
				return
			}
			// Setting a specific template clears skip flag
			h.Queries.UpdateAgent(r.Context(), db.UpdateAgentParams{
				ID:                 agentUUID,
				SkipSystemTemplate: pgtype.Bool{Bool: false, Valid: true},
			})
		}
	}

	if req.PersonalTemplateID != nil {
		if *req.PersonalTemplateID == "" {
			if _, err := h.Queries.ClearAgentPersonalTemplate(r.Context(), agentUUID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to clear personal template")
				return
			}
		} else {
			tplUUID := parseUUID(*req.PersonalTemplateID)
			if _, err := h.Queries.UpdateAgentTemplateBinding(r.Context(), db.UpdateAgentTemplateBindingParams{
				ID:                 agentUUID,
				SystemTemplateID:   pgtype.UUID{}, // no change
				PersonalTemplateID: pgtype.UUID{Bytes: tplUUID.Bytes, Valid: true},
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update personal template binding")
				return
			}
			// Setting a specific template clears skip flag
			h.Queries.UpdateAgent(r.Context(), db.UpdateAgentParams{
				ID:                   agentUUID,
				SkipPersonalTemplate: pgtype.Bool{Bool: false, Valid: true},
			})
		}
	}

	// Return updated binding
	agent, _ = h.Queries.GetAgent(r.Context(), agentUUID)
	resp := AgentTemplateBindingResponse{
		SkipSystemTemplate:   agent.SkipSystemTemplate,
		SkipPersonalTemplate: agent.SkipPersonalTemplate,
	}
	if agent.SystemTemplateID.Valid {
		resp.SystemTemplateID = uuidToString(agent.SystemTemplateID)
	}
	if agent.PersonalTemplateID.Valid {
		resp.PersonalTemplateID = uuidToString(agent.PersonalTemplateID)
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- resolveAgentConfig resolves the effective agent configuration by
// merging system template → personal template → agent own settings.
// This is the template-aware replacement for the old inline merge in
// ClaimTaskByRuntime.
func (h *Handler) resolveAgentConfig(ctx context.Context, agent db.Agent) (MergedAgentConfig, []byte) {
	// 1. Resolve system layer (skip if skip_system_template is true)
	var systemLayer AgentConfigLayer
	if !agent.SkipSystemTemplate {
		if agent.SystemTemplateID.Valid {
			if tpl, err := h.Queries.GetAgentConfigTemplate(ctx, agent.SystemTemplateID); err == nil {
				systemLayer = parseAgentConfigLayer(tpl.Config, "")
			}
		} else {
			// Fall back to workspace default system template
			if tpl, err := h.Queries.GetDefaultSystemTemplate(ctx, agent.WorkspaceID); err == nil {
				systemLayer = parseAgentConfigLayer(tpl.Config, "")
			} else {
				// Legacy fallback: workspace.settings.agent_defaults
				if ws, err := h.Queries.GetWorkspace(ctx, agent.WorkspaceID); err == nil {
					systemLayer = parseAgentConfigLayer(ws.Settings, "agent_defaults")
				}
			}
		}
	}

	// 2. Resolve personal layer (skip if skip_personal_template is true)
	var personalLayer AgentConfigLayer
	if !agent.SkipPersonalTemplate {
		if agent.PersonalTemplateID.Valid {
			if tpl, err := h.Queries.GetAgentConfigTemplate(ctx, agent.PersonalTemplateID); err == nil {
				personalLayer = parseAgentConfigLayer(tpl.Config, "")
			}
		} else if agent.OwnerID.Valid {
			// Fall back to user's default personal template
			if member, merr := h.getWorkspaceMember(ctx, uuidToString(agent.OwnerID), uuidToString(agent.WorkspaceID)); merr == nil {
				if tpl, err := h.Queries.GetDefaultPersonalTemplate(ctx, db.GetDefaultPersonalTemplateParams{
					WorkspaceID: agent.WorkspaceID,
					CreatedBy:   member.ID,
				}); err == nil {
					personalLayer = parseAgentConfigLayer(tpl.Config, "")
				} else {
					// Legacy fallback: member_agent_config
					if cfg, err := h.Queries.GetMemberAgentConfigByOwner(ctx, db.GetMemberAgentConfigByOwnerParams{
						UserID:      agent.OwnerID,
						WorkspaceID: agent.WorkspaceID,
					}); err == nil {
						personalLayer = parseAgentConfigLayer(cfg.Config, "")
					}
				}
			}
		}
	}

	// 3. Agent's own config
	var agentEnv map[string]string
	if agent.CustomEnv != nil {
		json.Unmarshal(agent.CustomEnv, &agentEnv)
	}
	var agentArgs []string
	if agent.CustomArgs != nil {
		json.Unmarshal(agent.CustomArgs, &agentArgs)
	}
	agentLayer := AgentConfigLayer{
		Instructions:  agent.Instructions,
		CustomEnv:     agentEnv,
		CustomArgs:    agentArgs,
		Model:         agent.Model.String,
		ThinkingLevel: agent.ThinkingLevel.String,
		ServiceTier:   agent.ServiceTier.String,
	}

	// 4. Merge: system → personal → agent
	merged := MergeAgentConfigs(systemLayer, personalLayer, agentLayer)

	// Return merged config and mcp_config
	var mcpConfig []byte
	if agent.McpConfig != nil {
		mcpConfig = agent.McpConfig
	}
	return merged, mcpConfig
}

// ListAllAgentDefaultTemplates returns every member's default personal
// template for the workspace — the template-model replacement for the legacy
// ListAllAgentDefaults roster (which read member_agent_config). Env values are
// masked to "***" (keys are safe to share). Member-visible: any workspace
// member can browse others' default configs for reference / duplication.
func (h *Handler) ListAllAgentDefaultTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	if userID := requestUserID(r); userID != "" {
		if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
			writeError(w, http.StatusForbidden, "workspace membership required")
			return
		}
	}

	rows, err := h.Queries.ListAllDefaultPersonalTemplates(r.Context(), wsUUID)
	if err != nil {
		slog.Error("list default personal templates failed", "workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list default configs")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var cfg agentDefaultsConfig
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			cfg = agentDefaultsConfig{}
		}
		// Mask custom_env values — only expose keys (mirrors ListAllAgentDefaults).
		maskedEnv := make(map[string]string, len(cfg.CustomEnv))
		for k := range cfg.CustomEnv {
			maskedEnv[k] = "***"
		}
		maskedCfg := agentDefaultsConfig{
			Instructions: cfg.Instructions,
			CustomEnv:    maskedEnv,
			CustomArgs:   cfg.CustomArgs,
			Skills:       cfg.Skills,
		}
		items = append(items, map[string]any{
			"id":              uuidToString(row.ID),
			"config":          maskedCfg,
			"user_id":         uuidToString(row.UserID),
			"user_name":       row.UserName,
			"user_avatar_url": row.UserAvatarUrl.String,
			"created_at":      timestampToString(row.CreatedAt),
			"updated_at":      timestampToString(row.UpdatedAt),
		})
	}

	writeJSON(w, http.StatusOK, items)
}

// DuplicateAgentConfigTemplate copies another member's default personal
// template into a new personal template owned by the current user. Env values
// are secrets and are NOT copied — only the keys are (as empty placeholders the
// user fills in), mirroring the legacy DuplicateAgentDefaults contract.
func (h *Handler) DuplicateAgentConfigTemplate(w http.ResponseWriter, r *http.Request) {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	templateID := chi.URLParam(r, "templateId")
	srcUUID, ok := parseUUIDOrBadRequest(w, templateID, "template_id")
	if !ok {
		return
	}

	src, err := h.Queries.GetAgentConfigTemplateInWorkspace(r.Context(), db.GetAgentConfigTemplateInWorkspaceParams{
		ID:          srcUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load source template")
		return
	}

	var srcCfg agentDefaultsConfig
	if err := json.Unmarshal(src.Config, &srcCfg); err != nil {
		srcCfg = agentDefaultsConfig{}
	}
	// Copy env keys only — values are the owner's secrets.
	envCopy := make(map[string]string, len(srcCfg.CustomEnv))
	for k := range srcCfg.CustomEnv {
		envCopy[k] = ""
	}
	copyCfg := agentDefaultsConfig{
		Instructions: srcCfg.Instructions,
		CustomEnv:    envCopy,
		CustomArgs:   srcCfg.CustomArgs,
		Skills:       srcCfg.Skills,
	}
	copyJSON, err := json.Marshal(copyCfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to copy template config")
		return
	}

	dup, err := h.Queries.CreateAgentConfigTemplate(r.Context(), db.CreateAgentConfigTemplateParams{
		WorkspaceID: wsUUID,
		Scope:       "personal",
		Name:        src.Name + " (copy)",
		Description: src.Description,
		Config:      copyJSON,
		IsDefault:   false,
		CreatedBy:   member.ID,
	})
	if err != nil {
		slog.Error("duplicate agent config template failed", "source_template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to duplicate template")
		return
	}

	writeJSON(w, http.StatusCreated, configTemplateToResponse(dup))
}
