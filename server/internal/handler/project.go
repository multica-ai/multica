package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type CreateProjectRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
}

type UpdateProjectRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      *string `json:"status"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
}

var validProjectStatuses = map[string]struct{}{
	"planned":     {},
	"in_progress": {},
	"paused":      {},
	"completed":   {},
	"cancelled":   {},
}

func projectToResponse(project db.Project) ProjectResponse {
	return ProjectResponse{
		ID:          uuidToString(project.ID),
		WorkspaceID: uuidToString(project.WorkspaceID),
		Title:       project.Title,
		Description: textToPtr(project.Description),
		Icon:        textToPtr(project.Icon),
		Status:      project.Status,
		LeadType:    textToPtr(project.LeadType),
		LeadID:      uuidToPtr(project.LeadID),
		CreatedAt:   timestampToString(project.CreatedAt),
		UpdatedAt:   timestampToString(project.UpdatedAt),
	}
}

func parseProjectStatus(value string) (pgtype.Text, error) {
	if value == "" {
		return pgtype.Text{}, nil
	}

	if _, ok := validProjectStatuses[value]; !ok {
		return pgtype.Text{}, fmt.Errorf("invalid project status")
	}

	return pgtype.Text{String: value, Valid: true}, nil
}

func validateProjectStatus(value string) error {
	if _, ok := validProjectStatuses[value]; !ok {
		return fmt.Errorf("invalid project status")
	}

	return nil
}

func (h *Handler) loadProjectForUser(w http.ResponseWriter, r *http.Request, id string) (db.Project, bool) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Project{}, false
	}

	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}

	return project, true
}

func (h *Handler) validateProjectLead(ctx context.Context, workspaceID string, leadType pgtype.Text, leadID pgtype.UUID) error {
	if leadType.Valid != leadID.Valid {
		return fmt.Errorf("lead_type and lead_id must be set together")
	}

	if !leadType.Valid {
		return nil
	}

	switch leadType.String {
	case "member":
		_, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      leadID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			return fmt.Errorf("lead member not found")
		}
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          leadID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			return fmt.Errorf("lead agent not found")
		}
		if agent.ArchivedAt.Valid {
			return fmt.Errorf("lead agent is archived")
		}
	default:
		return fmt.Errorf("invalid lead_type")
	}

	return nil
}

func (h *Handler) validateIssueProject(ctx context.Context, workspaceID string, projectID *string) (pgtype.UUID, error) {
	if projectID == nil || strings.TrimSpace(*projectID) == "" {
		return pgtype.UUID{}, nil
	}

	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID:          parseUUID(*projectID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("project not found")
	}

	return project.ID, nil
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	statusFilter, err := parseProjectStatus(r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{
		WorkspaceID: parseUUID(workspaceID),
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	resp := make([]ProjectResponse, len(projects))
	for index, project := range projects {
		resp[index] = projectToResponse(project)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"projects": resp,
		"total":    len(resp),
	})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, ok := h.loadProjectForUser(w, r, id)
	if !ok {
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

	title := strings.TrimSpace(req.Title)
	if title == "" {
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
	if err := validateProjectStatus(status); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil {
		leadID = parseUUID(*req.LeadID)
	}
	if err := h.validateProjectLead(r.Context(), workspaceID, leadType, leadID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	project, err := h.Queries.CreateProject(r.Context(), db.CreateProjectParams{
		WorkspaceID: parseUUID(workspaceID),
		Title:       title,
		Description: ptrToText(req.Description),
		Icon:        ptrToText(req.Icon),
		Status:      status,
		LeadType:    leadType,
		LeadID:      leadID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	resp := projectToResponse(project)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventProjectCreated, workspaceID, actorType, actorID, map[string]any{"project": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevProject, ok := h.loadProjectForUser(w, r, id)
	if !ok {
		return
	}

	workspaceID := uuidToString(prevProject.WorkspaceID)
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
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}
		params.Title = pgtype.Text{String: title, Valid: true}
	}
	if req.Status != nil {
		if err := validateProjectStatus(*req.Status); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
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

	if err := h.validateProjectLead(r.Context(), workspaceID, params.LeadType, params.LeadID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	project, err := h.Queries.UpdateProject(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}

	resp := projectToResponse(project)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventProjectUpdated, workspaceID, actorType, actorID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	project, ok := h.loadProjectForUser(w, r, id)
	if !ok {
		return
	}

	workspaceID := uuidToString(project.WorkspaceID)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.Queries.DeleteProject(r.Context(), project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventProjectDeleted, workspaceID, actorType, actorID, map[string]any{"project_id": id})
	w.WriteHeader(http.StatusNoContent)
}
