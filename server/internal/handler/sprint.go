package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ─── Response types ──────────────────────────────────────────────────────────

type SprintResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ProjectID   string  `json:"project_id"`
	Name        string  `json:"name"`
	Goal        *string `json:"goal"`
	StartDate   *string `json:"start_date"`
	EndDate     *string `json:"end_date"`
	State       string  `json:"state"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func sprintToResponse(s db.Sprint) SprintResponse {
	return SprintResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		ProjectID:   uuidToString(s.ProjectID),
		Name:        s.Name,
		Goal:        textToPtr(s.Goal),
		StartDate:   timestampToPtr(s.StartDate),
		EndDate:     timestampToPtr(s.EndDate),
		State:       s.State,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

type SprintIssueResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ProjectID   *string `json:"project_id"`
	Number      int32   `json:"number"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	AssigneeID  *string `json:"assignee_id"`
	SprintID    *string `json:"sprint_id"`
	Estimate    *int32  `json:"estimate"`
	CreatedAt   string  `json:"created_at"`
}

func int4ToPtr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func sprintIssueToResponse(i db.ListSprintIssuesRow) SprintIssueResponse {
	return SprintIssueResponse{
		ID:          uuidToString(i.ID),
		WorkspaceID: uuidToString(i.WorkspaceID),
		ProjectID:   uuidToPtr(i.ProjectID),
		Number:      i.Number,
		Title:       i.Title,
		Status:      i.Status,
		Priority:    i.Priority,
		AssigneeID:  uuidToPtr(i.AssigneeID),
		SprintID:    uuidToPtr(i.SprintID),
		Estimate:    int4ToPtr(i.Estimate),
		CreatedAt:   timestampToString(i.CreatedAt),
	}
}

func backlogIssueToResponse(i db.ListBacklogIssuesRow) SprintIssueResponse {
	return SprintIssueResponse{
		ID:          uuidToString(i.ID),
		WorkspaceID: uuidToString(i.WorkspaceID),
		ProjectID:   uuidToPtr(i.ProjectID),
		Number:      i.Number,
		Title:       i.Title,
		Status:      i.Status,
		Priority:    i.Priority,
		AssigneeID:  uuidToPtr(i.AssigneeID),
		SprintID:    uuidToPtr(i.SprintID),
		Estimate:    int4ToPtr(i.Estimate),
		CreatedAt:   timestampToString(i.CreatedAt),
	}
}

type VelocityPointResponse struct {
	SprintID        string  `json:"sprint_id"`
	SprintName      string  `json:"sprint_name"`
	StartDate       *string `json:"start_date"`
	EndDate         *string `json:"end_date"`
	CompletedPoints int64   `json:"completed_points"`
	TotalPoints     int64   `json:"total_points"`
}

type BurndownIssueResponse struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Estimate  *int32  `json:"estimate"`
	UpdatedAt string  `json:"updated_at"`
}

// ─── Request types ────────────────────────────────────────────────────────────

type CreateSprintRequest struct {
	Name      string  `json:"name"`
	Goal      *string `json:"goal"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

type UpdateSprintRequest struct {
	Name      *string `json:"name"`
	Goal      *string `json:"goal"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

type CompleteSprintRequest struct {
	// CarryTo: "backlog" or a sprint UUID
	CarryTo string `json:"carry_to"`
}

// ─── Date helper ─────────────────────────────────────────────────────────────

func sprintParseTimestamp(s *string) (pgtype.Timestamptz, error) {
	if s == nil || *s == "" {
		return pgtype.Timestamptz{}, nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, nil
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// POST /api/projects/{id}/sprints
func (h *Handler) CreateSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	projectIDStr := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDStr, "project_id")
	if !ok {
		return
	}

	// Verify project belongs to workspace
	proj, err := h.Queries.GetProject(r.Context(), projectUUID)
	if err == nil && uuidToString(proj.WorkspaceID) != workspaceID {
		err = pgx.ErrNoRows
	}
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		slog.Error("CreateSprint: GetProject", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	var req CreateSprintRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	startDate, err := sprintParseTimestamp(req.StartDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start_date format, expected RFC3339")
		return
	}
	endDate, err := sprintParseTimestamp(req.EndDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end_date format, expected RFC3339")
		return
	}

	sprint, err := h.Queries.CreateSprint(r.Context(), db.CreateSprintParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectUUID,
		Name:        req.Name,
		Goal:        ptrToText(req.Goal),
		StartDate:   startDate,
		EndDate:     endDate,
	})
	if err != nil {
		slog.Error("CreateSprint: insert", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, sprintToResponse(sprint))
}

// GET /api/projects/{id}/sprints
func (h *Handler) ListSprints(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	projectIDStr := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDStr, "project_id")
	if !ok {
		return
	}

	sprints, err := h.Queries.ListSprints(r.Context(), db.ListSprintsParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectUUID,
	})
	if err != nil {
		slog.Error("ListSprints", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := make([]SprintResponse, len(sprints))
	for i, s := range sprints {
		resp[i] = sprintToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sprints": resp})
}

// GET /api/sprints/{sprint_id}
func (h *Handler) GetSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	sprint, err := h.Queries.GetSprintByWorkspace(r.Context(), db.GetSprintByWorkspaceParams{
		ID: sprintUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		slog.Error("GetSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, sprintToResponse(sprint))
}

// PATCH /api/sprints/{sprint_id}
func (h *Handler) UpdateSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	var req UpdateSprintRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	startDate, err := sprintParseTimestamp(req.StartDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start_date format, expected RFC3339")
		return
	}
	endDate, err := sprintParseTimestamp(req.EndDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end_date format, expected RFC3339")
		return
	}

	sprint, err := h.Queries.UpdateSprint(r.Context(), db.UpdateSprintParams{
		ID:          sprintUUID,
		WorkspaceID: wsUUID,
		Name:        ptrToText(req.Name),
		Goal:        ptrToText(req.Goal),
		StartDate:   startDate,
		EndDate:     endDate,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		slog.Error("UpdateSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, sprintToResponse(sprint))
}

// POST /api/sprints/{sprint_id}/start
func (h *Handler) StartSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	// Get the sprint to find its project
	sprint, err := h.Queries.GetSprintByWorkspace(r.Context(), db.GetSprintByWorkspaceParams{
		ID: sprintUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		slog.Error("StartSprint: GetSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if sprint.State != "planning" {
		writeError(w, http.StatusConflict, "only planning sprints can be started")
		return
	}

	// Check no active sprint exists for this project (unique index enforced at DB too)
	_, err = h.Queries.GetActiveSprintForProject(r.Context(), sprint.ProjectID)
	if err == nil {
		writeError(w, http.StatusConflict, "another sprint is already active for this project")
		return
	}

	updated, err := h.Queries.StartSprint(r.Context(), db.StartSprintParams{
		ID: sprintUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "another sprint is already active for this project")
			return
		}
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "sprint not found or not in planning state")
			return
		}
		slog.Error("StartSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, sprintToResponse(updated))
}

// POST /api/sprints/{sprint_id}/complete
func (h *Handler) CompleteSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	body, _ := io.ReadAll(r.Body)
	var req CompleteSprintRequest
	_ = json.Unmarshal(body, &req)

	sprint, err := h.Queries.CompleteSprint(r.Context(), db.CompleteSprintParams{
		ID: sprintUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "sprint not found or not active")
			return
		}
		slog.Error("CompleteSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Handle carry-over of incomplete issues
	if req.CarryTo == "" || req.CarryTo == "backlog" {
		if err := h.Queries.CarryIncompleteToBacklog(r.Context(), db.CarryIncompleteToBacklogParams{
			SprintID:    sprintUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			slog.Error("CompleteSprint: carry to backlog", "error", err)
		}
	} else {
		// carry_to is a sprint UUID
		targetUUID, err := util.ParseUUID(req.CarryTo)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid carry_to sprint id")
			return
		}
		if err := h.Queries.CarryIncompleteToSprint(r.Context(), db.CarryIncompleteToSprintParams{
			SprintID:    sprintUUID,
			SprintID_2:  targetUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			slog.Error("CompleteSprint: carry to sprint", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, sprintToResponse(sprint))
}

// POST /api/sprints/{sprint_id}/tickets/{ticket_id}
func (h *Handler) AddTicketToSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	ticketIDStr := chi.URLParam(r, "ticket_id")
	ticketUUID, ok := parseUUIDOrBadRequest(w, ticketIDStr, "ticket_id")
	if !ok {
		return
	}

	if err := h.Queries.AddTicketToSprint(r.Context(), db.AddTicketToSprintParams{
		SprintID:    sprintUUID,
		ID:          ticketUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("AddTicketToSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/sprints/{sprint_id}/tickets/{ticket_id}
func (h *Handler) RemoveTicketFromSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	ticketIDStr := chi.URLParam(r, "ticket_id")
	ticketUUID, ok := parseUUIDOrBadRequest(w, ticketIDStr, "ticket_id")
	if !ok {
		return
	}

	if err := h.Queries.RemoveTicketFromSprint(r.Context(), db.RemoveTicketFromSprintParams{
		ID:          ticketUUID,
		SprintID:    sprintUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("RemoveTicketFromSprint", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /api/projects/{id}/backlog
func (h *Handler) ListBacklog(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	projectIDStr := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDStr, "project_id")
	if !ok {
		return
	}

	issues, err := h.Queries.ListBacklogIssues(r.Context(), db.ListBacklogIssuesParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectUUID,
	})
	if err != nil {
		slog.Error("ListBacklog", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := make([]SprintIssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = backlogIssueToResponse(issue)
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": resp})
}

// GET /api/sprints/{sprint_id}/issues
func (h *Handler) ListSprintIssues(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	issues, err := h.Queries.ListSprintIssues(r.Context(), db.ListSprintIssuesParams{
		WorkspaceID: wsUUID,
		SprintID:    sprintUUID,
	})
	if err != nil {
		slog.Error("ListSprintIssues", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := make([]SprintIssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = sprintIssueToResponse(issue)
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": resp})
}

// GET /api/sprints/{sprint_id}/velocity
func (h *Handler) GetSprintVelocity(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	velocity, err := h.Queries.GetSprintVelocity(r.Context(), db.GetSprintVelocityParams{
		SprintID:    sprintUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("GetSprintVelocity", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"velocity": velocity})
}

// GET /api/projects/{id}/velocity
func (h *Handler) GetProjectVelocity(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	projectIDStr := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectIDStr, "project_id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListCompletedSprintVelocities(r.Context(), db.ListCompletedSprintVelocitiesParams{
		ProjectID:   projectUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("GetProjectVelocity", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := make([]VelocityPointResponse, len(rows))
	for i, row := range rows {
		resp[i] = VelocityPointResponse{
			SprintID:        uuidToString(row.ID),
			SprintName:      row.Name,
			StartDate:       timestampToPtr(row.StartDate),
			EndDate:         timestampToPtr(row.EndDate),
			CompletedPoints: row.CompletedPoints,
			TotalPoints:     row.TotalPoints,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"velocity": resp})
}

// GET /api/sprints/{sprint_id}/burndown
func (h *Handler) GetSprintBurndown(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	sprintIDStr := chi.URLParam(r, "sprint_id")
	sprintUUID, ok := parseUUIDOrBadRequest(w, sprintIDStr, "sprint_id")
	if !ok {
		return
	}

	issues, err := h.Queries.GetSprintBurndown(r.Context(), db.GetSprintBurndownParams{
		SprintID:    sprintUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("GetSprintBurndown", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := make([]BurndownIssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = BurndownIssueResponse{
			ID:        uuidToString(issue.ID),
			Status:    issue.Status,
			Estimate:  int4ToPtr(issue.Estimate),
			UpdatedAt: timestampToString(issue.UpdatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": resp})
}
