package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type LabelResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	ResourceType string `json:"resource_type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Color        string `json:"color"`
	UsageCount   int64  `json:"usage_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func labelToResponse(l db.IssueLabel) LabelResponse {
	return LabelResponse{
		ID:           uuidToString(l.ID),
		WorkspaceID:  uuidToString(l.WorkspaceID),
		ResourceType: l.ResourceType,
		Name:         l.Name,
		Description:  l.Description,
		Color:        l.Color,
		CreatedAt:    timestampToString(l.CreatedAt),
		UpdatedAt:    timestampToString(l.UpdatedAt),
	}
}

func labelListRowToResponse(l db.ListLabelsRow) LabelResponse {
	return LabelResponse{
		ID:           uuidToString(l.ID),
		WorkspaceID:  uuidToString(l.WorkspaceID),
		ResourceType: l.ResourceType,
		Name:         l.Name,
		Description:  l.Description,
		Color:        l.Color,
		UsageCount:   l.UsageCount,
		CreatedAt:    timestampToString(l.CreatedAt),
		UpdatedAt:    timestampToString(l.UpdatedAt),
	}
}

func labelsToResponse(list []db.IssueLabel) []LabelResponse {
	out := make([]LabelResponse, len(list))
	for i, l := range list {
		out[i] = labelToResponse(l)
	}
	return out
}

type CreateLabelRequest struct {
	ResourceType string `json:"resource_type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Color        string `json:"color"`
}

type UpdateLabelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

const defaultLabelResourceType = "issue"

func parseLabelResourceType(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultLabelResourceType, nil
	}
	switch value {
	case "issue", "agent", "skill":
		return value, nil
	default:
		return "", errors.New("resource_type must be issue, agent, or skill")
	}
}

// 6-digit hex, with or without leading '#'.
var hexColorRE = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

// normalizeColor returns a canonical "#rrggbb" form or an error if invalid.
//
// LOAD-BEARING INVARIANT: LabelChip renders `style={{ backgroundColor: color }}`
// directly in the frontend. If this regex is ever relaxed to accept arbitrary
// CSS (named colors, `url(...)`, etc.), that inline style becomes an injection
// surface. Keep the regex strict.
func normalizeColor(c string) (string, error) {
	c = strings.TrimSpace(c)
	if !hexColorRE.MatchString(c) {
		return "", errors.New("color must be a 6-digit hex value like #3b82f6")
	}
	if !strings.HasPrefix(c, "#") {
		c = "#" + c
	}
	return strings.ToLower(c), nil
}

const maxLabelNameLen = 32

// validateLabelName trims and validates a label name. Returns the trimmed
// name or an error suitable for a 400 response.
func validateLabelName(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain tabs, newlines, or control characters")
		}
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxLabelNameLen {
		return "", errors.New("name must be 32 characters or fewer")
	}
	return name, nil
}

// ---------------------------------------------------------------------------
// Handlers — label CRUD
// ---------------------------------------------------------------------------

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	resourceType, err := parseLabelResourceType(r.URL.Query().Get("resource_type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if resourceType != defaultLabelResourceType && !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	labels, err := h.Queries.ListLabels(r.Context(), db.ListLabelsParams{
		WorkspaceID: parseUUID(workspaceID), ResourceType: resourceType,
	})
	if err != nil {
		slog.Warn("ListLabels failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	resp := make([]LabelResponse, len(labels))
	for i, label := range labels {
		resp[i] = labelListRowToResponse(label)
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp, "total": len(resp)})
}

func (h *Handler) GetLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "label id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get label")
		return
	}
	writeJSON(w, http.StatusOK, labelToResponse(label))
}

func (h *Handler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	var req CreateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, err := validateLabelName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	color, err := normalizeColor(req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceType, err := parseLabelResourceType(req.ResourceType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if resourceType != defaultLabelResourceType && !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	label, err := h.Queries.CreateLabel(r.Context(), db.CreateLabelParams{
		WorkspaceID:  parseUUID(workspaceID),
		ResourceType: resourceType,
		Name:         name,
		Description:  sanitizeNullBytes(strings.TrimSpace(req.Description)),
		Color:        color,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a label with that name already exists")
			return
		}
		slog.Warn("CreateLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create label")
		return
	}
	resp := labelToResponse(label)
	h.publish(protocol.EventLabelCreated, workspaceID, "member", userID, map[string]any{"label": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	var req UpdateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	idUUID, ok := parseUUIDOrBadRequest(w, id, "label id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	params := db.UpdateLabelParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}
	if req.Name != nil {
		name, err := validateLabelName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(*req.Description)), Valid: true}
	}
	if req.Color != nil {
		color, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Color = pgtype.Text{String: color, Valid: true}
	}

	// Branch on pgx.ErrNoRows directly from the UPDATE — the WHERE clause
	// already enforces (id, workspace_id), so a missing row means either the
	// label doesn't exist or it's not in this workspace. Dropping the prior
	// GetLabel precheck removes a TOCTOU window and saves a round-trip.
	label, err := h.Queries.UpdateLabel(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a label with that name already exists")
			return
		}
		slog.Warn("UpdateLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update label")
		return
	}
	resp := labelToResponse(label)
	h.publish(protocol.EventLabelUpdated, workspaceID, "member", userID, map[string]any{"label": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, id, "label id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Keep every relationship cleanup and the catalog deletion atomic. The
	// resource-label junctions intentionally use application-level cleanup
	// rather than database cascades.
	for _, cleanup := range []func() error{
		func() error { return qtx.DeleteIssueLabelAssignmentsByLabel(r.Context(), idUUID) },
		func() error { return qtx.DeleteAgentLabelAssignmentsByLabel(r.Context(), idUUID) },
		func() error { return qtx.DeleteSkillLabelAssignmentsByLabel(r.Context(), idUUID) },
	} {
		if err := cleanup(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove label assignments")
			return
		}
	}

	// DeleteLabel is :one RETURNING id — ErrNoRows means the label wasn't in
	// this workspace (404). Any other error is a real 500.
	if _, err := qtx.DeleteLabel(r.Context(), db.DeleteLabelParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("DeleteLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete label")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit label deletion")
		return
	}
	h.publish(protocol.EventLabelDeleted, workspaceID, "member", userID, map[string]any{"label_id": uuidToString(idUUID)})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Handlers — issue↔label attach/detach
// ---------------------------------------------------------------------------

type AttachLabelRequest struct {
	LabelID string `json:"label_id"`
}

// listLabelsForIssueSafe reads the attached-label list and handles the error
// by logging + returning nil. Callers use this after a successful attach/detach
// mutation: if the read fails, the mutation is already committed, so returning
// nil → clients refetch via query invalidation, and we skip broadcasting an
// empty list that would incorrectly overwrite every subscriber's optimistic
// state.
func (h *Handler) listLabelsForIssueSafe(r *http.Request, issueID, workspaceID pgtype.UUID) ([]db.IssueLabel, bool) {
	labels, err := h.Queries.ListLabelsByIssue(r.Context(), db.ListLabelsByIssueParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("ListLabelsByIssue failed after mutation", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issueID))...)
		return nil, false
	}
	return labels, true
}

// ListLabelsForIssue returns the labels currently attached to an issue.
func (h *Handler) ListLabelsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	// Authorize via the issue — if it's not in this workspace, the caller
	// shouldn't see its labels.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	labels, err := h.Queries.ListLabelsByIssue(r.Context(), db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("ListLabelsForIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labelsToResponse(labels)})
}

// AttachLabel attaches a label to an issue.
func (h *Handler) AttachLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req AttachLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LabelID == "" {
		writeError(w, http.StatusBadRequest, "label_id is required")
		return
	}

	// Both the issue and label must belong to this workspace.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	labelID, ok := parseUUIDOrBadRequest(w, req.LabelID, "label_id")
	if !ok {
		return
	}
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: labelID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel in AttachLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}
	if label.ResourceType != "issue" {
		writeError(w, http.StatusNotFound, "issue label not found")
		return
	}

	if err := h.Queries.AttachLabelToIssue(r.Context(), db.AttachLabelToIssueParams{
		IssueID:     issue.ID,
		LabelID:     labelID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("AttachLabelToIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}

	// Read the updated label list; on read failure, the attach is already
	// committed — return success without a labels body (clients refetch via
	// query invalidation) and skip the broadcast so we don't overwrite every
	// subscriber's optimistic state with an incorrect empty list.
	labels, ok2 := h.listLabelsForIssueSafe(r, issue.ID, issue.WorkspaceID)
	if !ok2 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	resp := labelsToResponse(labels)
	h.publish(protocol.EventIssueLabelsChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   resp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp})
}

// DetachLabel removes a label from an issue.
func (h *Handler) DetachLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	labelID := chi.URLParam(r, "labelId")
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify both issue and label belong to this workspace before detaching
	// (mirror of AttachLabel). Without this, a crafted request with a foreign
	// labelID would no-op and return 200 — "silent success" is worse than an
	// explicit 404.
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	labelUUID, ok := parseUUIDOrBadRequest(w, labelID, "label id")
	if !ok {
		return
	}
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: labelUUID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel in DetachLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}
	if label.ResourceType != "issue" {
		writeError(w, http.StatusNotFound, "issue label not found")
		return
	}

	if err := h.Queries.DetachLabelFromIssue(r.Context(), db.DetachLabelFromIssueParams{
		IssueID:     issue.ID,
		LabelID:     labelUUID,
		WorkspaceID: issue.WorkspaceID,
	}); err != nil {
		slog.Warn("DetachLabelFromIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}

	labels, ok2 := h.listLabelsForIssueSafe(r, issue.ID, issue.WorkspaceID)
	if !ok2 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	resp := labelsToResponse(labels)
	h.publish(protocol.EventIssueLabelsChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   resp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp})
}

// ---------------------------------------------------------------------------
// Handlers — agent/skill↔label attach/detach
// ---------------------------------------------------------------------------

func (h *Handler) ListLabelsForAgent(w http.ResponseWriter, r *http.Request) {
	if !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	labels, err := h.Queries.ListLabelsByAgent(r.Context(), db.ListLabelsByAgentParams{
		AgentID: agent.ID, WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent labels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labelsToResponse(labels)})
}

func (h *Handler) AttachLabelToAgent(w http.ResponseWriter, r *http.Request) {
	if !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	var req AttachLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LabelID == "" {
		writeError(w, http.StatusBadRequest, "label_id is required")
		return
	}
	labelID, ok := parseUUIDOrBadRequest(w, req.LabelID, "label_id")
	if !ok {
		return
	}
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{ID: labelID, WorkspaceID: agent.WorkspaceID})
	if err != nil || label.ResourceType != "agent" {
		writeError(w, http.StatusNotFound, "agent label not found")
		return
	}
	if err := h.Queries.AttachLabelToAgent(r.Context(), db.AttachLabelToAgentParams{
		AgentID: agent.ID, LabelID: labelID, WorkspaceID: agent.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to attach agent label")
		return
	}
	h.publish(protocol.EventLabelUpdated, uuidToString(agent.WorkspaceID), "member", requestUserID(r), map[string]any{"label": labelToResponse(label)})
	h.ListLabelsForAgent(w, r)
}

func (h *Handler) DetachLabelFromAgent(w http.ResponseWriter, r *http.Request) {
	if !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageAgent(w, r, agent) {
		return
	}
	labelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "labelId"), "label id")
	if !ok {
		return
	}
	if err := h.Queries.DetachLabelFromAgent(r.Context(), db.DetachLabelFromAgentParams{
		AgentID: agent.ID, LabelID: labelID, WorkspaceID: agent.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach agent label")
		return
	}
	h.publish(protocol.EventLabelUpdated, uuidToString(agent.WorkspaceID), "member", requestUserID(r), map[string]any{"label_id": uuidToString(labelID), "resource_type": "agent"})
	h.ListLabelsForAgent(w, r)
}

func (h *Handler) ListLabelsForSkill(w http.ResponseWriter, r *http.Request) {
	if !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	labels, err := h.Queries.ListLabelsBySkill(r.Context(), db.ListLabelsBySkillParams{
		SkillID: skill.ID, WorkspaceID: skill.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill labels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labelsToResponse(labels)})
}

func (h *Handler) AttachLabelToSkill(w http.ResponseWriter, r *http.Request) {
	if !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageSkill(w, r, skill) {
		return
	}
	var req AttachLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LabelID == "" {
		writeError(w, http.StatusBadRequest, "label_id is required")
		return
	}
	labelID, ok := parseUUIDOrBadRequest(w, req.LabelID, "label_id")
	if !ok {
		return
	}
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{ID: labelID, WorkspaceID: skill.WorkspaceID})
	if err != nil || label.ResourceType != "skill" {
		writeError(w, http.StatusNotFound, "skill label not found")
		return
	}
	if err := h.Queries.AttachLabelToSkill(r.Context(), db.AttachLabelToSkillParams{
		SkillID: skill.ID, LabelID: labelID, WorkspaceID: skill.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to attach skill label")
		return
	}
	h.publish(protocol.EventLabelUpdated, uuidToString(skill.WorkspaceID), "member", requestUserID(r), map[string]any{"label": labelToResponse(label)})
	h.ListLabelsForSkill(w, r)
}

func (h *Handler) DetachLabelFromSkill(w http.ResponseWriter, r *http.Request) {
	if !featureflags.ResourceLabelsEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "resource labels are not enabled")
		return
	}
	skill, ok := h.loadSkillForUser(w, r, chi.URLParam(r, "id"))
	if !ok || !h.canManageSkill(w, r, skill) {
		return
	}
	labelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "labelId"), "label id")
	if !ok {
		return
	}
	if err := h.Queries.DetachLabelFromSkill(r.Context(), db.DetachLabelFromSkillParams{
		SkillID: skill.ID, LabelID: labelID, WorkspaceID: skill.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to detach skill label")
		return
	}
	h.publish(protocol.EventLabelUpdated, uuidToString(skill.WorkspaceID), "member", requestUserID(r), map[string]any{"label_id": uuidToString(labelID), "resource_type": "skill"})
	h.ListLabelsForSkill(w, r)
}
