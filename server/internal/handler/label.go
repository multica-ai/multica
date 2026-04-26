package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// --- Constants -------------------------------------------------------------

// AllowedLabelColors must match the CHECK constraint in migration 101.
var AllowedLabelColors = map[string]struct{}{
	"slate": {}, "gray": {}, "red": {}, "orange": {}, "amber": {},
	"green": {}, "teal": {}, "blue": {}, "indigo": {}, "purple": {}, "pink": {},
}

const (
	maxLabelNameLen      = 32
	maxLabelsPerWorkspace = 100
	maxLabelsPerIssue     = 8
)

// Label names are trimmed, and must not be empty or whitespace-only. We also
// reject newlines/tabs which would break chip rendering.
var labelNameRejectRe = regexp.MustCompile(`[\r\n\t]`)

// --- Response types --------------------------------------------------------

type IssueLabelResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	CreatorType string  `json:"creator_type"`
	CreatorID   *string `json:"creator_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func issueLabelToResponse(l db.IssueLabel) IssueLabelResponse {
	return IssueLabelResponse{
		ID:          uuidToString(l.ID),
		WorkspaceID: uuidToString(l.WorkspaceID),
		Name:        l.Name,
		Color:       l.Color,
		CreatorType: l.CreatorType,
		CreatorID:   uuidToPtr(l.CreatorID),
		CreatedAt:   timestampToString(l.CreatedAt),
		UpdatedAt:   timestampToString(l.UpdatedAt),
	}
}

// --- Request types ---------------------------------------------------------

type CreateLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type UpdateLabelRequest struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

type AttachLabelRequest struct {
	LabelID string `json:"label_id"`
}

// --- Helpers ---------------------------------------------------------------

// normalizeLabelName trims and validates. Returns ("", errorMsg) on failure.
func normalizeLabelName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", "name is required"
	}
	if labelNameRejectRe.MatchString(name) {
		return "", "name may not contain newlines or tabs"
	}
	if len([]rune(name)) > maxLabelNameLen {
		return "", "name exceeds 32 characters"
	}
	return name, ""
}

func validateLabelColor(color string) bool {
	_, ok := AllowedLabelColors[color]
	return ok
}

// --- Handlers: workspace-scoped label CRUD ---------------------------------

// ListWorkspaceLabels GET /api/workspaces/{id}/labels
func (h *Handler) ListWorkspaceLabels(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	labels, err := h.Queries.ListIssueLabels(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list labels failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}

	resp := make([]IssueLabelResponse, 0, len(labels))
	for _, l := range labels {
		resp = append(resp, issueLabelToResponse(l))
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateWorkspaceLabel POST /api/workspaces/{id}/labels
func (h *Handler) CreateWorkspaceLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve actor. Agents are NOT allowed to create labels.
	userID := uuidToString(member.UserID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot create labels; ask a member to create it first")
		return
	}

	name, errMsg := normalizeLabelName(req.Name)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if !validateLabelColor(req.Color) {
		writeError(w, http.StatusBadRequest, "invalid color; must be one of the preset palette")
		return
	}

	// Cap per-workspace label count.
	count, err := h.Queries.CountLabelsInWorkspace(r.Context(), parseUUID(workspaceID))
	if err == nil && count >= int64(maxLabelsPerWorkspace) {
		writeError(w, http.StatusConflict, "workspace has reached the 100 label limit")
		return
	}

	label, err := h.Queries.CreateIssueLabel(r.Context(), db.CreateIssueLabelParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        name,
		Color:       req.Color,
		CreatorType: actorType,
		CreatorID:   parseUUIDNullable(actorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a label with this name already exists")
			return
		}
		slog.Warn("create label failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create label")
		return
	}

	resp := issueLabelToResponse(label)
	h.publish(protocol.EventLabelCreated, workspaceID, actorType, actorID, map[string]any{"label": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateWorkspaceLabel PATCH /api/workspaces/{id}/labels/{labelId}
func (h *Handler) UpdateWorkspaceLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	labelID := chi.URLParam(r, "labelId")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := uuidToString(member.UserID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify labels")
		return
	}

	var req UpdateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateIssueLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
	}
	if req.Name != nil {
		name, errMsg := normalizeLabelName(*req.Name)
		if errMsg != "" {
			writeError(w, http.StatusBadRequest, errMsg)
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Color != nil {
		if !validateLabelColor(*req.Color) {
			writeError(w, http.StatusBadRequest, "invalid color")
			return
		}
		params.Color = pgtype.Text{String: *req.Color, Valid: true}
	}

	label, err := h.Queries.UpdateIssueLabel(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a label with this name already exists")
			return
		}
		slog.Warn("update label failed", append(logger.RequestAttrs(r), "error", err, "label_id", labelID)...)
		writeError(w, http.StatusInternalServerError, "failed to update label")
		return
	}

	resp := issueLabelToResponse(label)
	h.publish(protocol.EventLabelUpdated, workspaceID, actorType, actorID, map[string]any{"label": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteWorkspaceLabel DELETE /api/workspaces/{id}/labels/{labelId}
func (h *Handler) DeleteWorkspaceLabel(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	labelID := chi.URLParam(r, "labelId")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	userID := uuidToString(member.UserID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot delete labels")
		return
	}

	// Confirm label exists in this workspace (prevents cross-workspace delete).
	if _, err := h.Queries.GetIssueLabel(r.Context(), db.GetIssueLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "label not found")
		return
	}

	if err := h.Queries.DeleteIssueLabel(r.Context(), db.DeleteIssueLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		slog.Warn("delete label failed", append(logger.RequestAttrs(r), "error", err, "label_id", labelID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete label")
		return
	}

	h.publish(protocol.EventLabelDeleted, workspaceID, actorType, actorID, map[string]any{"label_id": labelID})
	w.WriteHeader(http.StatusNoContent)
}

// --- Handlers: issue-label associations ------------------------------------

// AttachLabelToIssue POST /api/issues/{id}/labels
func (h *Handler) AttachLabelToIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	var req AttachLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LabelID == "" {
		writeError(w, http.StatusBadRequest, "label_id is required")
		return
	}

	// Verify the label exists in this workspace.
	label, err := h.Queries.GetIssueLabel(r.Context(), db.GetIssueLabelParams{
		ID:          parseUUID(req.LabelID),
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "label not found in this workspace")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	// Note: agents CAN attach existing labels. They just cannot create new ones.

	// Cap per-issue label count.
	count, err := h.Queries.CountLabelsForIssue(r.Context(), issue.ID)
	if err == nil && count >= int64(maxLabelsPerIssue) {
		writeError(w, http.StatusConflict, "issue has reached the 8 label limit")
		return
	}

	if err := h.Queries.AttachLabelToIssue(r.Context(), db.AttachLabelToIssueParams{
		IssueID:   issue.ID,
		LabelID:   parseUUID(req.LabelID),
		ActorType: actorType,
		ActorID:   parseUUIDNullable(actorID),
	}); err != nil {
		slog.Warn("attach label failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "label_id", req.LabelID)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}

	// Log to activity_log.
	h.insertLabelActivity(r, issue.WorkspaceID, issue.ID, "label_attached", actorType, actorID, map[string]any{
		"label_id":   uuidToString(label.ID),
		"label_name": label.Name,
	})

	// Refresh issue labels for the WS payload.
	labels := h.fetchLabelsForIssue(r, issue.ID)
	h.publish(protocol.EventIssueLabelsChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   labels,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   labels,
	})
}

// DetachLabelFromIssue DELETE /api/issues/{id}/labels/{labelId}
func (h *Handler) DetachLabelFromIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	labelID := chi.URLParam(r, "labelId")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	// Guard: label must belong to this workspace.
	label, err := h.Queries.GetIssueLabel(r.Context(), db.GetIssueLabelParams{
		ID:          parseUUID(labelID),
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "label not found in this workspace")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	if err := h.Queries.DetachLabelFromIssue(r.Context(), db.DetachLabelFromIssueParams{
		IssueID: issue.ID,
		LabelID: parseUUID(labelID),
	}); err != nil {
		slog.Warn("detach label failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "label_id", labelID)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}

	h.insertLabelActivity(r, issue.WorkspaceID, issue.ID, "label_detached", actorType, actorID, map[string]any{
		"label_id":   uuidToString(label.ID),
		"label_name": label.Name,
	})

	labels := h.fetchLabelsForIssue(r, issue.ID)
	h.publish(protocol.EventIssueLabelsChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   labels,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id": uuidToString(issue.ID),
		"labels":   labels,
	})
}

// --- Private helpers -------------------------------------------------------

func (h *Handler) fetchLabelsForIssue(r *http.Request, issueID pgtype.UUID) []IssueLabelResponse {
	rows, err := h.Queries.ListLabelsForIssue(r.Context(), issueID)
	if err != nil {
		slog.Debug("fetch labels for issue failed", "error", err, "issue_id", uuidToString(issueID))
		return []IssueLabelResponse{}
	}
	out := make([]IssueLabelResponse, 0, len(rows))
	for _, l := range rows {
		out = append(out, issueLabelToResponse(l))
	}
	return out
}

func (h *Handler) insertLabelActivity(r *http.Request, workspaceID, issueID pgtype.UUID, action, actorType, actorID string, details map[string]any) {
	payload, _ := json.Marshal(details)
	_, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		ActorType:   pgtype.Text{String: actorType, Valid: true},
		ActorID:     parseUUIDNullable(actorID),
		Action:      action,
		Details:     payload,
	})
	if err != nil {
		slog.Debug("activity log insert failed", "error", err, "action", action)
	}
}

// parseUUIDNullable returns Valid=false if the string is empty, otherwise the parsed UUID.
func parseUUIDNullable(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	return parseUUID(s)
}

// --- Enrich helpers for embedding labels on issues -------------------------

// enrichIssuesWithLabels bulk-fetches labels for every issue in resp and
// assigns them to resp[i].Labels. Issues with no labels keep the empty slice
// that the converter initialised. Safe to call with an empty slice.
func (h *Handler) enrichIssuesWithLabels(ctx context.Context, resp []IssueResponse) {
	if len(resp) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(resp))
	idxByID := make(map[string]int, len(resp))
	for i, r := range resp {
		ids = append(ids, parseUUID(r.ID))
		idxByID[r.ID] = i
	}

	rows, err := h.Queries.ListLabelsForIssues(ctx, ids)
	if err != nil {
		slog.Debug("bulk fetch labels failed", "error", err, "count", len(ids))
		return
	}

	for _, row := range rows {
		idx, ok := idxByID[uuidToString(row.IssueID)]
		if !ok {
			continue
		}
		resp[idx].Labels = append(resp[idx].Labels, IssueLabelResponse{
			ID:          uuidToString(row.ID),
			WorkspaceID: uuidToString(row.WorkspaceID),
			Name:        row.Name,
			Color:       row.Color,
			CreatorType: row.CreatorType,
			CreatorID:   uuidToPtr(row.CreatorID),
			CreatedAt:   timestampToString(row.CreatedAt),
			UpdatedAt:   timestampToString(row.UpdatedAt),
		})
	}
}

// enrichIssueWithLabels is the single-issue variant, for GetIssue / CreateIssue /
// UpdateIssue response paths.
func (h *Handler) enrichIssueWithLabels(ctx context.Context, resp *IssueResponse) {
	if resp == nil || resp.ID == "" {
		return
	}
	rows, err := h.Queries.ListLabelsForIssue(ctx, parseUUID(resp.ID))
	if err != nil {
		slog.Debug("fetch labels for single issue failed", "error", err, "issue_id", resp.ID)
		return
	}
	out := make([]IssueLabelResponse, 0, len(rows))
	for _, l := range rows {
		out = append(out, issueLabelToResponse(l))
	}
	resp.Labels = out
}
