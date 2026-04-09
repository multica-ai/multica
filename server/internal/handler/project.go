package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ProjectResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func projectToResponse(p db.Project) ProjectResponse {
	return ProjectResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		Title:       p.Title,
		Description: textToPtr(p.Description),
		Icon:        textToPtr(p.Icon),
		Status:      p.Status,
		Priority:    p.Priority,
		LeadType:    textToPtr(p.LeadType),
		LeadID:      uuidToPtr(p.LeadID),
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
}

type CreateProjectRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
}

type UpdateProjectRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      *string `json:"status"`
	Priority    *string `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{
		WorkspaceID: parseUUID(workspaceID),
		Status:      statusFilter,
		Priority:    priorityFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	resp := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		resp[i] = projectToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": resp, "total": len(resp)})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, projectToResponse(project))
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	status := req.Status
	if status == "" {
		status = "planned"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil {
		leadID = parseUUID(*req.LeadID)
	}
	project, err := h.Queries.CreateProject(r.Context(), db.CreateProjectParams{
		WorkspaceID: parseUUID(workspaceID),
		Title:       req.Title,
		Description: ptrToText(req.Description),
		Icon:        ptrToText(req.Icon),
		Status:      status,
		LeadType:    leadType,
		LeadID:      leadID,
		Priority:    priority,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}
	resp := projectToResponse(project)
	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"project": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	prevProject, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateProjectRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateProjectParams{
		ID:          prevProject.ID,
		Description: prevProject.Description,
		Icon:        prevProject.Icon,
		LeadType:    prevProject.LeadType,
		LeadID:      prevProject.LeadID,
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if _, ok := rawFields["description"]; ok {
		if req.Description != nil {
			params.Description = pgtype.Text{String: *req.Description, Valid: true}
		} else {
			params.Description = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["icon"]; ok {
		if req.Icon != nil {
			params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
		} else {
			params.Icon = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["lead_type"]; ok {
		if req.LeadType != nil {
			params.LeadType = pgtype.Text{String: *req.LeadType, Valid: true}
		} else {
			params.LeadType = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["lead_id"]; ok {
		if req.LeadID != nil {
			params.LeadID = parseUUID(*req.LeadID)
		} else {
			params.LeadID = pgtype.UUID{Valid: false}
		}
	}
	project, err := h.Queries.UpdateProject(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}
	resp := projectToResponse(project)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: parseUUID(id), WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteProject(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}
	h.publish(protocol.EventProjectDeleted, workspaceID, "member", userID, map[string]any{"project_id": id})
	w.WriteHeader(http.StatusNoContent)
}
