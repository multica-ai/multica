package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ProjectUpdateResponse is the JSON shape returned by the project update API.
type ProjectUpdateResponse struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
	Health      string `json:"health"`
	Body        string `json:"body"`
	AuthorType  string `json:"author_type"`
	AuthorID    string `json:"author_id"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func projectUpdateToResponse(u db.ProjectUpdate) ProjectUpdateResponse {
	return ProjectUpdateResponse{
		ID:          uuidToString(u.ID),
		ProjectID:   uuidToString(u.ProjectID),
		WorkspaceID: uuidToString(u.WorkspaceID),
		Health:      u.Health,
		Body:        u.Body,
		AuthorType:  u.AuthorType,
		AuthorID:    uuidToString(u.AuthorID),
		CreatedAt:   timestampToString(u.CreatedAt),
		UpdatedAt:   timestampToString(u.UpdatedAt),
	}
}

// validProjectHealth reports whether s is one of the accepted health values.
func validProjectHealth(s string) bool {
	switch s {
	case "on_track", "at_risk", "off_track":
		return true
	default:
		return false
	}
}

// CreateProjectUpdateRequest is the body for POST /api/projects/{id}/updates.
type CreateProjectUpdateRequest struct {
	Health     string  `json:"health"`
	Body       string  `json:"body"`
	AuthorType *string `json:"author_type"`
	AuthorID   *string `json:"author_id"`
}

// UpdateProjectUpdateRequest is the body for PUT /api/projects/{id}/updates/{updateId}.
// Every field is optional; omitted fields keep their current value.
type UpdateProjectUpdateRequest struct {
	Health *string `json:"health"`
	Body   *string `json:"body"`
}

// ListProjectUpdates returns the updates posted to a project, newest first.
func (h *Handler) ListProjectUpdates(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	updates, err := h.Queries.ListProjectUpdates(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project updates")
		return
	}
	resp := make([]ProjectUpdateResponse, len(updates))
	for i, u := range updates {
		resp[i] = projectUpdateToResponse(u)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updates": resp, "total": len(resp)})
}

// CreateProjectUpdate posts a new health update to a project.
func (h *Handler) CreateProjectUpdate(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateProjectUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Health = strings.TrimSpace(req.Health)
	if !validProjectHealth(req.Health) {
		writeError(w, http.StatusBadRequest, "health must be one of on_track, at_risk, off_track")
		return
	}

	// Author defaults to the authenticated member. A caller may override the
	// author (e.g. attributing an update to an agent) by sending author_type
	// and optionally author_id.
	authorType := "member"
	authorIDStr := userID
	if req.AuthorType != nil {
		at := strings.TrimSpace(*req.AuthorType)
		if at != "member" && at != "agent" {
			writeError(w, http.StatusBadRequest, "author_type must be member or agent")
			return
		}
		authorType = at
	}
	if req.AuthorID != nil && strings.TrimSpace(*req.AuthorID) != "" {
		authorIDStr = strings.TrimSpace(*req.AuthorID)
	}
	authorUUID, ok := parseUUIDOrBadRequest(w, authorIDStr, "author_id")
	if !ok {
		return
	}

	update, err := h.Queries.CreateProjectUpdate(r.Context(), db.CreateProjectUpdateParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		Health:      req.Health,
		Body:        req.Body,
		AuthorType:  authorType,
		AuthorID:    authorUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project update")
		return
	}

	resp := projectUpdateToResponse(update)
	h.publish(
		protocol.EventProjectUpdateCreated,
		uuidToString(project.WorkspaceID),
		authorType,
		authorIDStr,
		map[string]any{"update": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateProjectUpdate edits an existing project update's health and/or body.
func (h *Handler) UpdateProjectUpdate(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	updateUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "updateId"), "update id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	existing, err := h.Queries.GetProjectUpdateInWorkspace(r.Context(), db.GetProjectUpdateInWorkspaceParams{
		ID: updateUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}
	if uuidToString(existing.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}

	var req UpdateProjectUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var health pgtype.Text
	if req.Health != nil {
		hv := strings.TrimSpace(*req.Health)
		if !validProjectHealth(hv) {
			writeError(w, http.StatusBadRequest, "health must be one of on_track, at_risk, off_track")
			return
		}
		health = pgtype.Text{String: hv, Valid: true}
	}
	var body pgtype.Text
	if req.Body != nil {
		body = pgtype.Text{String: *req.Body, Valid: true}
	}

	updated, err := h.Queries.UpdateProjectUpdate(r.Context(), db.UpdateProjectUpdateParams{
		ID:     existing.ID,
		Health: health,
		Body:   body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project update")
		return
	}

	resp := projectUpdateToResponse(updated)
	h.publish(
		protocol.EventProjectUpdateUpdated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"update": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusOK, resp)
}

// DeleteProjectUpdate removes a project update.
func (h *Handler) DeleteProjectUpdate(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	updateUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "updateId"), "update id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	existing, err := h.Queries.GetProjectUpdateInWorkspace(r.Context(), db.GetProjectUpdateInWorkspaceParams{
		ID: updateUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}
	if uuidToString(existing.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}

	if err := h.Queries.DeleteProjectUpdate(r.Context(), existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project update")
		return
	}
	h.publish(
		protocol.EventProjectUpdateDeleted,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{
			"project_id": uuidToString(project.ID),
			"update_id":  uuidToString(existing.ID),
		},
	)
	w.WriteHeader(http.StatusNoContent)
}
