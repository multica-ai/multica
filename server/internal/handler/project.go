package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type ProjectResponse struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	Name        string           `json:"name"`
	Description *string          `json:"description"`
	Status      string           `json:"status"`
	Icon        *string          `json:"icon"`
	Color       *string          `json:"color"`
	LeadType    *string          `json:"lead_type"`
	LeadID      *string          `json:"lead_id"`
	StartDate   *string          `json:"start_date"`
	TargetDate  *string          `json:"target_date"`
	SortOrder   float64          `json:"sort_order"`
	Progress    *ProjectProgress `json:"progress,omitempty"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
}

type ProjectProgress struct {
	Total     int32   `json:"total"`
	Completed int32   `json:"completed"`
	Percent   float64 `json:"percent"`
}

func dateToPtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}

func projectToResponse(p db.Project) ProjectResponse {
	return ProjectResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		Name:        p.Name,
		Description: textToPtr(p.Description),
		Status:      p.Status,
		Icon:        textToPtr(p.Icon),
		Color:       textToPtr(p.Color),
		LeadType:    textToPtr(p.LeadType),
		LeadID:      uuidToPtr(p.LeadID),
		StartDate:   dateToPtr(p.StartDate),
		TargetDate:  dateToPtr(p.TargetDate),
		SortOrder:   p.SortOrder,
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
}

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

type CreateProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Icon        *string `json:"icon"`
	Color       *string `json:"color"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	StartDate   *string `json:"start_date"`
	TargetDate  *string `json:"target_date"`
}

type UpdateProjectRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Status      *string  `json:"status"`
	Icon        *string  `json:"icon"`
	Color       *string  `json:"color"`
	LeadType    *string  `json:"lead_type"`
	LeadID      *string  `json:"lead_id"`
	StartDate   *string  `json:"start_date"`
	TargetDate  *string  `json:"target_date"`
	SortOrder   *float64 `json:"sort_order"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{
		WorkspaceID: parseUUID(workspaceID),
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	// Batch-load progress for all projects.
	progressRows, _ := h.Queries.ListProjectsProgress(r.Context(), parseUUID(workspaceID))
	progressMap := make(map[string]ProjectProgress, len(progressRows))
	for _, row := range progressRows {
		pid := uuidToString(row.ProjectID)
		pct := float64(0)
		if row.Total > 0 {
			pct = float64(row.Completed) / float64(row.Total) * 100
		}
		progressMap[pid] = ProjectProgress{Total: row.Total, Completed: row.Completed, Percent: pct}
	}

	resp := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		resp[i] = projectToResponse(p)
		if prog, ok := progressMap[resp[i].ID]; ok {
			resp[i].Progress = &prog
		} else {
			resp[i].Progress = &ProjectProgress{}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"projects": resp,
		"total":    len(resp),
	})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	project, err := h.Queries.GetProject(r.Context(), db.GetProjectParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	resp := projectToResponse(project)

	// Attach progress.
	progress, err := h.Queries.GetProjectProgress(r.Context(), db.GetProjectProgressParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err == nil {
		pct := float64(0)
		if progress.Total > 0 {
			pct = float64(progress.Completed) / float64(progress.Total) * 100
		}
		resp.Progress = &ProjectProgress{Total: progress.Total, Completed: progress.Completed, Percent: pct}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	workspaceID := resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	status := req.Status
	if status == "" {
		status = "backlog"
	}

	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil {
		leadID = parseUUID(*req.LeadID)
	}

	var startDate pgtype.Date
	if req.StartDate != nil && *req.StartDate != "" {
		t, err := time.Parse("2006-01-02", *req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
			return
		}
		startDate = pgtype.Date{Time: t, Valid: true}
	}

	var targetDate pgtype.Date
	if req.TargetDate != nil && *req.TargetDate != "" {
		t, err := time.Parse("2006-01-02", *req.TargetDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid target_date format, expected YYYY-MM-DD")
			return
		}
		targetDate = pgtype.Date{Time: t, Valid: true}
	}

	project, err := h.Queries.CreateProject(r.Context(), db.CreateProjectParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Description: ptrToText(req.Description),
		Status:      status,
		Icon:        ptrToText(req.Icon),
		Color:       ptrToText(req.Color),
		LeadType:    leadType,
		LeadID:      leadID,
		StartDate:   startDate,
		TargetDate:  targetDate,
		SortOrder:   0,
	})
	if err != nil {
		slog.Warn("create project failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	resp := projectToResponse(project)
	resp.Progress = &ProjectProgress{}
	slog.Info("project created", append(logger.RequestAttrs(r), "project_id", resp.ID, "name", project.Name, "workspace_id", workspaceID)...)

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventProjectCreated, workspaceID, actorType, actorID, map[string]any{"project": resp})

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	prevProject, err := h.Queries.GetProject(r.Context(), db.GetProjectParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
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

	// Track which fields were explicitly present in JSON.
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateProjectParams{
		ID:          prevProject.ID,
		WorkspaceID: prevProject.WorkspaceID,
		LeadType:    prevProject.LeadType,
		LeadID:      prevProject.LeadID,
		StartDate:   prevProject.StartDate,
		TargetDate:  prevProject.TargetDate,
	}

	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Icon != nil {
		params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
	}
	if req.Color != nil {
		params.Color = pgtype.Text{String: *req.Color, Valid: true}
	}
	if req.SortOrder != nil {
		params.SortOrder = pgtype.Float8{Float64: *req.SortOrder, Valid: true}
	}

	// Nullable fields — only override when explicitly present.
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
	if _, ok := rawFields["start_date"]; ok {
		if req.StartDate != nil && *req.StartDate != "" {
			t, err := time.Parse("2006-01-02", *req.StartDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
				return
			}
			params.StartDate = pgtype.Date{Time: t, Valid: true}
		} else {
			params.StartDate = pgtype.Date{Valid: false}
		}
	}
	if _, ok := rawFields["target_date"]; ok {
		if req.TargetDate != nil && *req.TargetDate != "" {
			t, err := time.Parse("2006-01-02", *req.TargetDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid target_date format, expected YYYY-MM-DD")
				return
			}
			params.TargetDate = pgtype.Date{Time: t, Valid: true}
		} else {
			params.TargetDate = pgtype.Date{Valid: false}
		}
	}

	project, err := h.Queries.UpdateProject(r.Context(), params)
	if err != nil {
		slog.Warn("update project failed", append(logger.RequestAttrs(r), "error", err, "project_id", id, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}

	resp := projectToResponse(project)
	slog.Info("project updated", append(logger.RequestAttrs(r), "project_id", id, "workspace_id", workspaceID)...)

	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventProjectUpdated, workspaceID, actorType, actorID, map[string]any{"project": resp})

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := resolveWorkspaceID(r)

	_, err := h.Queries.GetProject(r.Context(), db.GetProjectParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	err = h.Queries.DeleteProject(r.Context(), db.DeleteProjectParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventProjectDeleted, workspaceID, actorType, actorID, map[string]any{"project_id": id})
	slog.Info("project deleted", append(logger.RequestAttrs(r), "project_id", id, "workspace_id", workspaceID)...)
	w.WriteHeader(http.StatusNoContent)
}
