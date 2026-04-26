package handler

import (
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

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type LabelResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Name         string `json:"name"`
	Color        string `json:"color"`
	Instructions string `json:"instructions"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func labelToResponse(l db.IssueLabel) LabelResponse {
	return LabelResponse{
		ID:           uuidToString(l.ID),
		WorkspaceID:  uuidToString(l.WorkspaceID),
		Name:         l.Name,
		Color:        l.Color,
		Instructions: l.Instructions,
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
	Name         string `json:"name"`
	Color        string `json:"color"`
	Instructions string `json:"instructions"`
}

type UpdateLabelRequest struct {
	Name         *string `json:"name"`
	Color        *string `json:"color"`
	Instructions *string `json:"instructions"`
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

const (
	maxLabelNameLen         = 32
	maxLabelInstructionsLen = 2000
)

// validateLabelName trims and validates a label name. Returns the trimmed
// name or an error suitable for a 400 response.
//
// Control characters (newlines, tabs, anything below U+0020 except space)
// are rejected — labels with active instructions land verbatim in the
// agent's system prompt as `[name] instructions`, so a name containing
// `\n\nIgnore prior instructions` would break out of the [name] bracket
// and inject an arbitrary directive. The 32-char cap limits damage but
// doesn't eliminate it; rejecting control chars closes the vector at the
// source. Emoji and multi-byte characters are still allowed.
func validateLabelName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > maxLabelNameLen {
		return "", errors.New("name must be 32 characters or fewer")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("name must not contain newlines, tabs, or control characters")
		}
	}
	return name, nil
}

// validateLabelInstructions checks the instructions text for the upper
// length bound. Empty is allowed (organizational-only labels). The length
// cap protects against unbounded inputs being shipped over the wire on
// every task claim and inflating the agent's prompt.
func validateLabelInstructions(raw string) (string, error) {
	if len(raw) > maxLabelInstructionsLen {
		return "", errors.New("instructions must be 2000 characters or fewer")
	}
	return raw, nil
}

// ---------------------------------------------------------------------------
// Handlers — label CRUD
// ---------------------------------------------------------------------------

func (h *Handler) ListLabels(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	labels, err := h.Queries.ListLabels(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("ListLabels failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	resp := labelsToResponse(labels)
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp, "total": len(resp)})
}

func (h *Handler) GetLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID),
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
	instructions, err := validateLabelInstructions(req.Instructions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	label, err := h.Queries.CreateLabel(r.Context(), db.CreateLabelParams{
		WorkspaceID:  parseUUID(workspaceID),
		Name:         name,
		Color:        color,
		Instructions: instructions,
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

	params := db.UpdateLabelParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}
	if req.Name != nil {
		name, err := validateLabelName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Color != nil {
		color, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Color = pgtype.Text{String: color, Valid: true}
	}
	if req.Instructions != nil {
		instructions, err := validateLabelInstructions(*req.Instructions)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Instructions = pgtype.Text{String: instructions, Valid: true}
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
	// DeleteLabel is :one RETURNING id — ErrNoRows means the label wasn't in
	// this workspace (404). Any other error is a real 500.
	if _, err := h.Queries.DeleteLabel(r.Context(), db.DeleteLabelParams{
		ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("DeleteLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete label")
		return
	}
	h.publish(protocol.EventLabelDeleted, workspaceID, "member", userID, map[string]any{"label_id": id})
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
func (h *Handler) listLabelsForIssueSafe(r *http.Request, issueID, workspaceID string) ([]db.IssueLabel, bool) {
	labels, err := h.Queries.ListLabelsByIssue(r.Context(), db.ListLabelsByIssueParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Warn("ListLabelsByIssue failed after mutation", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		return nil, false
	}
	return labels, true
}

// ListLabelsForIssue returns the labels currently attached to an issue.
func (h *Handler) ListLabelsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	// Authorize via the issue — if it's not in this workspace, the caller
	// shouldn't see its labels.
	issue, err := h.Queries.GetIssue(r.Context(), parseUUID(issueID))
	if err != nil || uuidToString(issue.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	labels, err := h.Queries.ListLabelsByIssue(r.Context(), db.ListLabelsByIssueParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: parseUUID(workspaceID),
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
	workspaceID := h.resolveWorkspaceID(r)
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
	issue, err := h.Queries.GetIssue(r.Context(), parseUUID(issueID))
	if err != nil || uuidToString(issue.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: parseUUID(req.LabelID), WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel in AttachLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}

	if err := h.Queries.AttachLabelToIssue(r.Context(), db.AttachLabelToIssueParams{
		IssueID:     parseUUID(issueID),
		LabelID:     parseUUID(req.LabelID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		slog.Warn("AttachLabelToIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to attach label")
		return
	}

	// Read the updated label list; on read failure, the attach is already
	// committed — return success without a labels body (clients refetch via
	// query invalidation) and skip the broadcast so we don't overwrite every
	// subscriber's optimistic state with an incorrect empty list.
	labels, ok2 := h.listLabelsForIssueSafe(r, issueID, workspaceID)
	if !ok2 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	resp := labelsToResponse(labels)
	h.publish(protocol.EventIssueLabelsChanged, workspaceID, "member", userID, map[string]any{
		"issue_id": issueID,
		"labels":   resp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp})
}

// DetachLabel removes a label from an issue.
func (h *Handler) DetachLabel(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	labelID := chi.URLParam(r, "labelId")
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify both issue and label belong to this workspace before detaching
	// (mirror of AttachLabel). Without this, a crafted request with a foreign
	// labelID would no-op and return 200 — "silent success" is worse than an
	// explicit 404.
	issue, err := h.Queries.GetIssue(r.Context(), parseUUID(issueID))
	if err != nil || uuidToString(issue.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if _, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
		ID: parseUUID(labelID), WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "label not found")
			return
		}
		slog.Warn("GetLabel in DetachLabel failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}

	if err := h.Queries.DetachLabelFromIssue(r.Context(), db.DetachLabelFromIssueParams{
		IssueID:     parseUUID(issueID),
		LabelID:     parseUUID(labelID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		slog.Warn("DetachLabelFromIssue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to detach label")
		return
	}

	labels, ok2 := h.listLabelsForIssueSafe(r, issueID, workspaceID)
	if !ok2 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	resp := labelsToResponse(labels)
	h.publish(protocol.EventIssueLabelsChanged, workspaceID, "member", userID, map[string]any{
		"issue_id": issueID,
		"labels":   resp,
	})
	writeJSON(w, http.StatusOK, map[string]any{"labels": resp})
}
