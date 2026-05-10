package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AutomationHandler exposes HTTP endpoints for automation templates and rules.
type AutomationHandler struct {
	queries    *db.Queries
	standupSvc *service.StandupService
}

// NewAutomationHandler creates an AutomationHandler with the given dependencies.
func NewAutomationHandler(queries *db.Queries, standupSvc *service.StandupService) *AutomationHandler {
	return &AutomationHandler{queries: queries, standupSvc: standupSvc}
}

// TemplateResponse is the JSON shape for a template returned to clients.
// It enriches the static template definition with workspace-specific enablement state.
type TemplateResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TriggerType string `json:"trigger_type"`
	Schedule    string `json:"schedule,omitempty"`
	Icon        string `json:"icon"`
	Enabled     bool   `json:"enabled"`
}

// ListTemplates returns all built-in templates, annotated with the workspace's
// current enablement state for each.
//
// GET /api/automation/templates
func (h *AutomationHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	rules, err := h.queries.ListAutomationRules(r.Context(), parseUUID(workspaceID))
	if err != nil {
		// Non-fatal: proceed with all templates as disabled.
		rules = nil
	}

	// Build a quick lookup from template_id → enabled state.
	enabledMap := make(map[string]bool, len(rules))
	for _, rule := range rules {
		enabledMap[rule.TemplateID] = rule.Enabled
	}

	resp := make([]TemplateResponse, 0, len(automation.BuiltinTemplates))
	for _, t := range automation.BuiltinTemplates {
		resp = append(resp, TemplateResponse{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			TriggerType: t.TriggerType,
			Schedule:    t.Schedule,
			Icon:        t.Icon,
			Enabled:     enabledMap[t.ID],
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// EnableRule enables (or re-enables) a template for the current workspace.
// Body: { "template_id": "nightly_review" }
//
// POST /api/automation/rules
func (h *AutomationHandler) EnableRule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	userID := r.Header.Get("X-User-ID")

	var req struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TemplateID == "" {
		writeError(w, http.StatusBadRequest, "template_id is required")
		return
	}

	// Validate that the template_id references a known built-in template.
	if automation.FindTemplate(req.TemplateID) == nil {
		writeError(w, http.StatusBadRequest, "unknown template_id: "+req.TemplateID)
		return
	}

	rule, err := h.queries.UpsertAutomationRule(r.Context(), db.UpsertAutomationRuleParams{
		WorkspaceID: parseUUID(workspaceID),
		TemplateID:  req.TemplateID,
		Enabled:     true,
		CreatedBy:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enable automation rule")
		return
	}

	writeJSON(w, http.StatusOK, automationRuleToResponse(rule))
}

// DisableRule disables (deletes) a template rule for the current workspace.
//
// DELETE /api/automation/rules/:template_id
func (h *AutomationHandler) DisableRule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	templateID := chi.URLParam(r, "templateId")

	if automation.FindTemplate(templateID) == nil {
		writeError(w, http.StatusBadRequest, "unknown template_id: "+templateID)
		return
	}

	err := h.queries.DeleteAutomationRule(r.Context(), db.DeleteAutomationRuleParams{
		WorkspaceID: parseUUID(workspaceID),
		TemplateID:  templateID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable automation rule")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RunRule manually triggers a manual template for the current workspace.
// Only templates with trigger_type "manual" are allowed here.
//
// POST /api/automation/rules/:template_id/run
func (h *AutomationHandler) RunRule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	templateID := chi.URLParam(r, "templateId")

	tpl := automation.FindTemplate(templateID)
	if tpl == nil {
		writeError(w, http.StatusBadRequest, "unknown template_id: "+templateID)
		return
	}
	if tpl.TriggerType != "manual" {
		writeError(w, http.StatusBadRequest, "template is not manually triggerable")
		return
	}

	switch templateID {
	case "standup_summary":
		result, err := h.standupSvc.GenerateSummary(r.Context(), parseUUID(workspaceID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate standup summary: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"template_id":  templateID,
			"content":      result.Content,
			"date":         result.Date.Format("2006-01-02"),
			"member_count": result.MemberCount,
		})
	default:
		writeError(w, http.StatusBadRequest, "no runner registered for template: "+templateID)
	}
}

// automationRuleToResponse converts a DB automation rule to a JSON response.
func automationRuleToResponse(r db.AutomationRule) map[string]any {
	return map[string]any{
		"id":           util.UUIDToString(r.ID),
		"workspace_id": util.UUIDToString(r.WorkspaceID),
		"template_id":  r.TemplateID,
		"enabled":      r.Enabled,
		"created_by":   uuidToStringMaybe(r.CreatedBy),
		"created_at":   util.TimestampToString(r.CreatedAt),
		"updated_at":   util.TimestampToString(r.UpdatedAt),
	}
}

// uuidToStringMaybe converts an optional pgtype.UUID to a string pointer.
func uuidToStringMaybe(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}
