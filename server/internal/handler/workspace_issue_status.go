package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueStatusResponse is the JSON shape returned for a single workspace issue status.
type IssueStatusResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	Color       string `json:"color"`
	Category    string `json:"category"`
	Position    int32  `json:"position"`
	IsDefault   bool   `json:"is_default"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func issueStatusToResponse(s db.WorkspaceIssueStatus) IssueStatusResponse {
	return IssueStatusResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		Name:        s.Name,
		Label:       s.Label,
		Color:       s.Color,
		Category:    s.Category,
		Position:    s.Position,
		IsDefault:   s.IsDefault,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

// ListWorkspaceIssueStatuses returns all issue statuses for the workspace.
// GET /api/workspaces/{workspaceId}/issue-statuses
func (h *Handler) ListWorkspaceIssueStatuses(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspaceId")
	if !ok {
		return
	}

	statuses, err := h.Queries.ListWorkspaceIssueStatuses(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue statuses")
		return
	}

	resp := make([]IssueStatusResponse, len(statuses))
	for i, s := range statuses {
		resp[i] = issueStatusToResponse(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateIssueStatusRequest is the JSON body for creating a custom issue status.
type CreateIssueStatusRequest struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Color    string `json:"color"`
	Category string `json:"category"`
	Position int32  `json:"position"`
}

// CreateWorkspaceIssueStatus creates a custom issue status for the workspace.
// POST /api/workspaces/{workspaceId}/issue-statuses
func (h *Handler) CreateWorkspaceIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspaceId")
	if !ok {
		return
	}

	// Require admin or owner role.
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req CreateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}
	if req.Color == "" {
		writeError(w, http.StatusBadRequest, "color is required")
		return
	}
	validCategories := map[string]bool{
		"not_started": true,
		"started":     true,
		"completed":   true,
		"cancelled":   true,
	}
	if !validCategories[req.Category] {
		writeError(w, http.StatusBadRequest, "category must be one of: not_started, started, completed, cancelled")
		return
	}

	status, err := h.Queries.CreateWorkspaceIssueStatus(r.Context(), db.CreateWorkspaceIssueStatusParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Label:       req.Label,
		Color:       req.Color,
		Category:    req.Category,
		Position:    req.Position,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a status with this name already exists in this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create issue status")
		return
	}

	writeJSON(w, http.StatusCreated, issueStatusToResponse(status))
}

// UpdateIssueStatusRequest is the JSON body for updating an issue status.
type UpdateIssueStatusRequest struct {
	Label    *string `json:"label"`
	Color    *string `json:"color"`
	Category *string `json:"category"`
	Position *int32  `json:"position"`
}

// UpdateWorkspaceIssueStatus updates an issue status.
// For built-in statuses, only label, color, and position can be changed.
// For custom statuses, category can also be changed.
// PUT /api/workspaces/{workspaceId}/issue-statuses/{statusId}
func (h *Handler) UpdateWorkspaceIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspaceId")
	if !ok {
		return
	}
	statusID := chi.URLParam(r, "statusId")
	statusUUID, ok := parseUUIDOrBadRequest(w, statusID, "statusId")
	if !ok {
		return
	}

	// Require admin or owner role.
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	var req UpdateIssueStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Category != nil {
		validCategories := map[string]bool{
			"not_started": true,
			"started":     true,
			"completed":   true,
			"cancelled":   true,
		}
		if !validCategories[*req.Category] {
			writeError(w, http.StatusBadRequest, "category must be one of: not_started, started, completed, cancelled")
			return
		}
	}

	// Check if status exists and whether it's built-in.
	existing, err := h.Queries.GetWorkspaceIssueStatus(r.Context(), db.GetWorkspaceIssueStatusParams{
		WorkspaceID: wsUUID,
		ID:          statusUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue status")
		return
	}

	var updated db.WorkspaceIssueStatus

	if existing.IsDefault {
		// Built-in status: use UpdateBuiltinStatusDisplay (label, color, position only).
		params := db.UpdateBuiltinStatusDisplayParams{
			WorkspaceID: wsUUID,
			ID:          statusUUID,
		}
		if req.Label != nil {
			params.Label = pgtype.Text{String: *req.Label, Valid: true}
		}
		if req.Color != nil {
			params.Color = pgtype.Text{String: *req.Color, Valid: true}
		}
		if req.Position != nil {
			params.Position = pgtype.Int4{Int32: *req.Position, Valid: true}
		}
		updated, err = h.Queries.UpdateBuiltinStatusDisplay(r.Context(), params)
	} else {
		// Custom status: use UpdateWorkspaceIssueStatus (label, color, category, position).
		params := db.UpdateWorkspaceIssueStatusParams{
			WorkspaceID: wsUUID,
			ID:          statusUUID,
		}
		if req.Label != nil {
			params.Label = pgtype.Text{String: *req.Label, Valid: true}
		}
		if req.Color != nil {
			params.Color = pgtype.Text{String: *req.Color, Valid: true}
		}
		if req.Category != nil {
			params.Category = pgtype.Text{String: *req.Category, Valid: true}
		}
		if req.Position != nil {
			params.Position = pgtype.Int4{Int32: *req.Position, Valid: true}
		}
		updated, err = h.Queries.UpdateWorkspaceIssueStatus(r.Context(), params)
	}

	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update issue status")
		return
	}

	writeJSON(w, http.StatusOK, issueStatusToResponse(updated))
}

// DeleteWorkspaceIssueStatus deletes a custom issue status.
// Built-in statuses cannot be deleted.
// DELETE /api/workspaces/{workspaceId}/issue-statuses/{statusId}
func (h *Handler) DeleteWorkspaceIssueStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspaceId")
	if !ok {
		return
	}
	statusID := chi.URLParam(r, "statusId")
	statusUUID, ok := parseUUIDOrBadRequest(w, statusID, "statusId")
	if !ok {
		return
	}

	// Require admin or owner role.
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	// Check if the status exists and whether it's built-in.
	existing, err := h.Queries.GetWorkspaceIssueStatus(r.Context(), db.GetWorkspaceIssueStatusParams{
		WorkspaceID: wsUUID,
		ID:          statusUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "status not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get issue status")
		return
	}

	if existing.IsDefault {
		writeError(w, http.StatusBadRequest, "cannot delete built-in statuses")
		return
	}

	if err := h.Queries.DeleteWorkspaceIssueStatus(r.Context(), db.DeleteWorkspaceIssueStatusParams{
		WorkspaceID: wsUUID,
		ID:          statusUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue status")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
