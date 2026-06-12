package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type QuickActionResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func quickActionToResponse(a db.QuickAction) QuickActionResponse {
	return QuickActionResponse{
		ID:          uuidToString(a.ID),
		WorkspaceID: uuidToString(a.WorkspaceID),
		Label:       a.Label,
		Body:        a.Body,
		CreatedAt:   timestampToString(a.CreatedAt),
		UpdatedAt:   timestampToString(a.UpdatedAt),
	}
}

func quickActionsToResponse(list []db.QuickAction) []QuickActionResponse {
	out := make([]QuickActionResponse, len(list))
	for i, a := range list {
		out[i] = quickActionToResponse(a)
	}
	return out
}

type CreateQuickActionRequest struct {
	Label string `json:"label"`
	Body  string `json:"body"`
}

type UpdateQuickActionRequest struct {
	Label *string `json:"label"`
	Body  *string `json:"body"`
}

const (
	maxQuickActionLabelLen = 80
	maxQuickActionBodyLen  = 10000
)

func validateQuickActionLabel(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("label is required")
	}
	if len(s) > maxQuickActionLabelLen {
		return "", errors.New("label must be 80 characters or fewer")
	}
	return s, nil
}

func validateQuickActionBody(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("body is required")
	}
	if len(s) > maxQuickActionBodyLen {
		return "", errors.New("body must be 10000 characters or fewer")
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Handlers — quick action CRUD (workspace-scoped). Membership is enforced by
// the RequireWorkspaceMember middleware on the route group; every query is
// additionally guarded on workspace_id so a forged id can't reach another
// workspace's rows.
// ---------------------------------------------------------------------------

func (h *Handler) ListQuickActions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actions, err := h.Queries.ListQuickActions(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("ListQuickActions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list quick actions")
		return
	}
	resp := quickActionsToResponse(actions)
	writeJSON(w, http.StatusOK, map[string]any{"quick_actions": resp, "total": len(resp)})
}

func (h *Handler) CreateQuickAction(w http.ResponseWriter, r *http.Request) {
	var req CreateQuickActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	label, err := validateQuickActionLabel(req.Label)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := validateQuickActionBody(req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	action, err := h.Queries.CreateQuickAction(r.Context(), db.CreateQuickActionParams{
		WorkspaceID: parseUUID(workspaceID),
		Label:       label,
		Body:        body,
	})
	if err != nil {
		slog.Warn("CreateQuickAction failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create quick action")
		return
	}
	writeJSON(w, http.StatusCreated, quickActionToResponse(action))
}

func (h *Handler) UpdateQuickAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	var req UpdateQuickActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, id, "quick action id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	params := db.UpdateQuickActionParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}
	if req.Label != nil {
		label, err := validateQuickActionLabel(*req.Label)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Label = pgtype.Text{String: label, Valid: true}
	}
	if req.Body != nil {
		body, err := validateQuickActionBody(*req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Body = pgtype.Text{String: body, Valid: true}
	}

	// Branch on pgx.ErrNoRows directly from the UPDATE — the WHERE clause
	// already enforces (id, workspace_id), so a missing row means the action
	// doesn't exist or isn't in this workspace. No precheck, no TOCTOU window.
	action, err := h.Queries.UpdateQuickAction(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "quick action not found")
			return
		}
		slog.Warn("UpdateQuickAction failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update quick action")
		return
	}
	writeJSON(w, http.StatusOK, quickActionToResponse(action))
}

func (h *Handler) DeleteQuickAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "quick action id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	// DeleteQuickAction is :one RETURNING id — ErrNoRows means the row wasn't
	// in this workspace (404). Any other error is a real 500.
	if _, err := h.Queries.DeleteQuickAction(r.Context(), db.DeleteQuickActionParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "quick action not found")
			return
		}
		slog.Warn("DeleteQuickAction failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete quick action")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
