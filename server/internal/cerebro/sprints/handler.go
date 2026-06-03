package sprints

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler hosts the REST surface for the cerebro sprint feature. Wired
// into the router under /api/cerebro/sprints by the
// cerebro-sprints-routes CEREBRO-PATCH in server/cmd/server/router.go.
//
// Every endpoint is workspace-scoped via the X-Workspace-ID header (read
// through middleware.WorkspaceIDFromContext) and authenticated via the
// X-User-ID header (requireUserID).
//
// Pool and Upstream are only used by SweepProject, which builds a Sweeper
// on demand to run a one-off project sweep. The daily background Sweeper
// stays owned by main.go.
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Pool     *pgxpool.Pool
	Upstream *db.Queries
}

func NewHandler(cerebro *cerebrodb.Queries, pool *pgxpool.Pool, upstream *db.Queries) *Handler {
	return &Handler{Cerebro: cerebro, Pool: pool, Upstream: upstream}
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

type settingsRequest struct {
	Enabled               *bool   `json:"enabled,omitempty"`
	DurationUnit          string  `json:"duration_unit,omitempty"`
	DurationCount         *int32  `json:"duration_count,omitempty"`
	StartWeekday          *int    `json:"start_weekday,omitempty"`
	NameTemplate          *string `json:"name_template,omitempty"`
	AutoCreateEnabled     *bool   `json:"auto_create_enabled,omitempty"`
	AutoCreateLeadDays    *int32  `json:"auto_create_lead_days,omitempty"`
	MoveIncompleteEnabled *bool   `json:"move_incomplete_enabled,omitempty"`
	Timezone              *string `json:"timezone,omitempty"`
}

// GetSettings returns the sprint settings for a project. 404 when none
// exist — the client treats that as "sprints are not enabled on this
// project yet" and shows the empty state.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	settings, err := h.Cerebro.GetCerebroSprintSettings(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no sprint settings for project")
			return
		}
		writeError(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, settings.WorkspaceID) {
		return
	}
	writeJSON(w, http.StatusOK, settingsToResponse(settings))
}

// UpsertSettings creates or updates the per-project settings row. The
// project's workspace is read from the workspace context — clients cannot
// override it.
func (h *Handler) UpsertSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}

	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	existing, err := h.Cerebro.GetCerebroSprintSettings(r.Context(), projectID)
	hasExisting := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	if hasExisting && !uuidEqual(existing.WorkspaceID, wsUUID) {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return
	}

	merged := mergeSettings(req, existing, hasExisting)
	merged.WorkspaceID = wsUUID
	merged.ProjectID = projectID

	if err := ValidateUnit(merged.DurationUnit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if merged.DurationCount <= 0 {
		writeError(w, http.StatusBadRequest, "duration_count must be > 0")
		return
	}
	if merged.AutoCreateLeadDays < 0 {
		writeError(w, http.StatusBadRequest, "auto_create_lead_days must be >= 0")
		return
	}
	if merged.StartWeekday < 1 || merged.StartWeekday > 7 {
		writeError(w, http.StatusBadRequest, "start_weekday must be between 1 and 7")
		return
	}

	out, err := h.Cerebro.UpsertCerebroSprintSettings(r.Context(), cerebrodb.UpsertCerebroSprintSettingsParams{
		ProjectID:             projectID,
		WorkspaceID:           wsUUID,
		Enabled:               merged.Enabled,
		DurationUnit:          merged.DurationUnit,
		DurationCount:         merged.DurationCount,
		StartWeekday:          int16(merged.StartWeekday),
		NameTemplate:          merged.NameTemplate,
		AutoCreateEnabled:     merged.AutoCreateEnabled,
		AutoCreateLeadDays:    merged.AutoCreateLeadDays,
		MoveIncompleteEnabled: merged.MoveIncompleteEnabled,
		Timezone:              merged.Timezone,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save settings failed")
		return
	}
	writeJSON(w, http.StatusOK, settingsToResponse(out))
}

// DeleteSettings removes the settings row. Effectively turns the project
// back into a non-sprint container.
func (h *Handler) DeleteSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroSprintSettings(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}
	if err := h.Cerebro.DeleteCerebroSprintSettings(r.Context(), projectID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete settings failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Sprints
// ---------------------------------------------------------------------------

type createSprintRequest struct {
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Status    string `json:"status,omitempty"`
	Goal      string `json:"goal,omitempty"`
}

type updateSprintRequest struct {
	Name      *string `json:"name,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
	Status    *string `json:"status,omitempty"`
	Goal      *string `json:"goal,omitempty"`
}

// ListSprints returns the sprints of a project newest-first.
func (h *Handler) ListSprints(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroSprintsByProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list sprints failed")
		return
	}
	out := make([]SprintResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, sprintToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sprints": out})
}

// CreateSprint creates a manual sprint (used when the admin wants to start
// the sequence before the sweeper has had a chance).
func (h *Handler) CreateSprint(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}

	var req createSprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	start, err := parseDate(req.StartDate)
	if err != nil || !start.Valid {
		writeError(w, http.StatusBadRequest, "invalid start_date (YYYY-MM-DD)")
		return
	}
	end, err := parseDate(req.EndDate)
	if err != nil || !end.Valid {
		writeError(w, http.StatusBadRequest, "invalid end_date (YYYY-MM-DD)")
		return
	}
	if end.Time.Before(start.Time) {
		writeError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = StatusPlanned
	}
	if !validSprintStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	seq, err := nextSequenceFor(r.Context(), h.Cerebro, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "compute sequence failed")
		return
	}
	out, err := h.Cerebro.CreateCerebroSprint(r.Context(), cerebrodb.CreateCerebroSprintParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectID,
		Name:        req.Name,
		SequenceNo:  seq,
		Status:      status,
		StartDate:   start,
		EndDate:     end,
		Goal:        pgTextFromString(req.Goal),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create sprint failed")
		return
	}
	writeJSON(w, http.StatusCreated, sprintToResponse(out))
}

// GetSprint returns one sprint.
func (h *Handler) GetSprint(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	sprintID, ok := parseUUIDParam(w, r, "sprintID")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroSprint(r.Context(), sprintID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load sprint failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, row.WorkspaceID) {
		return
	}
	writeJSON(w, http.StatusOK, sprintToResponse(row))
}

// UpdateSprint updates name, dates, status, goal. The sequence number is
// immutable from the API — it's set on creation only.
func (h *Handler) UpdateSprint(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	sprintID, ok := parseUUIDParam(w, r, "sprintID")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroSprint(r.Context(), sprintID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load sprint failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}

	var req updateSprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	name := existing.Name
	if req.Name != nil {
		if *req.Name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		name = *req.Name
	}

	start := existing.StartDate
	if req.StartDate != nil {
		d, err := parseDate(*req.StartDate)
		if err != nil || !d.Valid {
			writeError(w, http.StatusBadRequest, "invalid start_date")
			return
		}
		start = d
	}
	end := existing.EndDate
	if req.EndDate != nil {
		d, err := parseDate(*req.EndDate)
		if err != nil || !d.Valid {
			writeError(w, http.StatusBadRequest, "invalid end_date")
			return
		}
		end = d
	}
	if end.Time.Before(start.Time) {
		writeError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return
	}

	status := existing.Status
	if req.Status != nil {
		if !validSprintStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		status = *req.Status
	}
	goal := existing.Goal
	if req.Goal != nil {
		goal = pgTextFromString(*req.Goal)
	}

	out, err := h.Cerebro.UpdateCerebroSprint(r.Context(), cerebrodb.UpdateCerebroSprintParams{
		ID:        sprintID,
		Name:      name,
		Status:    status,
		StartDate: start,
		EndDate:   end,
		Goal:      goal,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update sprint failed")
		return
	}
	writeJSON(w, http.StatusOK, sprintToResponse(out))
}

// DeleteSprint removes a sprint. The sprint_issue rows cascade.
func (h *Handler) DeleteSprint(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	sprintID, ok := parseUUIDParam(w, r, "sprintID")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroSprint(r.Context(), sprintID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "load sprint failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}
	if err := h.Cerebro.DeleteCerebroSprint(r.Context(), sprintID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete sprint failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListSprintIssues returns the issue IDs assigned to a sprint.
func (h *Handler) ListSprintIssues(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	sprintID, ok := parseUUIDParam(w, r, "sprintID")
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroSprintIssuesBySprint(r.Context(), sprintID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list sprint issues failed")
		return
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]string{
			"issue_id":  util.UUIDToString(row.IssueID),
			"sprint_id": util.UUIDToString(row.SprintID),
			"added_at":  tsString(row.AddedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out})
}

// ---------------------------------------------------------------------------
// Issue ↔ sprint assignment
// ---------------------------------------------------------------------------

type assignIssueRequest struct {
	SprintID string `json:"sprint_id"`
}

// AssignIssue puts an issue in a sprint. Send sprint_id="" (or DELETE on
// the same path) to remove the issue from its current sprint.
func (h *Handler) AssignIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, ok := parseUUIDParam(w, r, "issueID")
	if !ok {
		return
	}

	var req assignIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.SprintID) == "" {
		if err := h.Cerebro.RemoveIssueFromCerebroSprint(r.Context(), issueID); err != nil {
			writeError(w, http.StatusInternalServerError, "remove from sprint failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sprintUUID, err := util.ParseUUID(req.SprintID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid sprint_id")
		return
	}
	sprint, err := h.Cerebro.GetCerebroSprint(r.Context(), sprintUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sprint not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load sprint failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, sprint.WorkspaceID) {
		return
	}
	if err := h.Cerebro.AssignIssueToCerebroSprint(r.Context(), cerebrodb.AssignIssueToCerebroSprintParams{
		IssueID:  issueID,
		SprintID: sprintUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "assign failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetIssueAssignment returns the sprint an issue is in, or 404 when none.
func (h *Handler) GetIssueAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, ok := parseUUIDParam(w, r, "issueID")
	if !ok {
		return
	}
	row, err := h.Cerebro.GetCerebroSprintForIssue(r.Context(), issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue not in any sprint")
			return
		}
		writeError(w, http.StatusInternalServerError, "load assignment failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"issue_id":  util.UUIDToString(row.IssueID),
		"sprint_id": util.UUIDToString(row.SprintID),
		"added_at":  tsString(row.AddedAt),
	})
}

// ---------------------------------------------------------------------------
// Recurring tasks
// ---------------------------------------------------------------------------

type recurringTaskRequest struct {
	CadenceUnit  string  `json:"cadence_unit"`
	CadenceCount int32   `json:"cadence_count"`
	Title        string  `json:"title"`
	Description  string  `json:"description,omitempty"`
	Priority     string  `json:"priority,omitempty"`
	AssigneeType string  `json:"assignee_type,omitempty"`
	AssigneeID   string  `json:"assignee_id,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

// ListRecurringTasks returns the recurring-task templates for a project.
func (h *Handler) ListRecurringTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListCerebroSprintRecurringTasksByProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list recurring tasks failed")
		return
	}
	out := make([]RecurringTaskResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, recurringTaskToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"recurring_tasks": out})
}

// CreateRecurringTask adds a template.
func (h *Handler) CreateRecurringTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	wsUUID, ok := workspaceFromContext(w, r)
	if !ok {
		return
	}

	var req recurringTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := ValidateUnit(req.CadenceUnit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.CadenceCount <= 0 {
		writeError(w, http.StatusBadRequest, "cadence_count must be > 0")
		return
	}
	assigneeType, assigneeID, err := decodeAssignee(req.AssigneeType, req.AssigneeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	out, err := h.Cerebro.CreateCerebroSprintRecurringTask(r.Context(), cerebrodb.CreateCerebroSprintRecurringTaskParams{
		WorkspaceID:  wsUUID,
		ProjectID:    projectID,
		CadenceUnit:  req.CadenceUnit,
		CadenceCount: req.CadenceCount,
		Title:        req.Title,
		Description:  pgTextFromString(req.Description),
		Priority:     pgTextFromString(req.Priority),
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		Enabled:      enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create recurring task failed")
		return
	}
	writeJSON(w, http.StatusCreated, recurringTaskToResponse(out))
}

// UpdateRecurringTask replaces the fields of a template.
func (h *Handler) UpdateRecurringTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroSprintRecurringTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recurring task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load recurring task failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}

	var req recurringTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := ValidateUnit(req.CadenceUnit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.CadenceCount <= 0 {
		writeError(w, http.StatusBadRequest, "cadence_count must be > 0")
		return
	}
	assigneeType, assigneeID, err := decodeAssignee(req.AssigneeType, req.AssigneeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	out, err := h.Cerebro.UpdateCerebroSprintRecurringTask(r.Context(), cerebrodb.UpdateCerebroSprintRecurringTaskParams{
		ID:           id,
		CadenceUnit:  req.CadenceUnit,
		CadenceCount: req.CadenceCount,
		Title:        req.Title,
		Description:  pgTextFromString(req.Description),
		Priority:     pgTextFromString(req.Priority),
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		Enabled:      enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update recurring task failed")
		return
	}
	writeJSON(w, http.StatusOK, recurringTaskToResponse(out))
}

// DeleteRecurringTask removes a template.
func (h *Handler) DeleteRecurringTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	existing, err := h.Cerebro.GetCerebroSprintRecurringTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "load recurring task failed")
		return
	}
	if !ensureWorkspaceMatch(w, r, existing.WorkspaceID) {
		return
	}
	if err := h.Cerebro.DeleteCerebroSprintRecurringTask(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete recurring task failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mergedSettings is the in-memory projection of cerebro_sprint_settings
// used to merge a partial update on top of an existing row.
type mergedSettings struct {
	ProjectID             pgtype.UUID
	WorkspaceID           pgtype.UUID
	Enabled               bool
	DurationUnit          string
	DurationCount         int32
	StartWeekday          int
	NameTemplate          string
	AutoCreateEnabled     bool
	AutoCreateLeadDays    int32
	MoveIncompleteEnabled bool
	Timezone              string
}

func mergeSettings(req settingsRequest, existing cerebrodb.CerebroSprintSetting, hasExisting bool) mergedSettings {
	merged := defaultsForUpsert()
	if hasExisting {
		merged = mergedSettings{
			ProjectID:             existing.ProjectID,
			WorkspaceID:           existing.WorkspaceID,
			Enabled:               existing.Enabled,
			DurationUnit:          existing.DurationUnit,
			DurationCount:         existing.DurationCount,
			StartWeekday:          int(existing.StartWeekday),
			NameTemplate:          existing.NameTemplate,
			AutoCreateEnabled:     existing.AutoCreateEnabled,
			AutoCreateLeadDays:    existing.AutoCreateLeadDays,
			MoveIncompleteEnabled: existing.MoveIncompleteEnabled,
			Timezone:              existing.Timezone,
		}
	}
	if req.Enabled != nil {
		merged.Enabled = *req.Enabled
	}
	if req.DurationUnit != "" {
		merged.DurationUnit = req.DurationUnit
	}
	if req.DurationCount != nil {
		merged.DurationCount = *req.DurationCount
	}
	if req.StartWeekday != nil {
		merged.StartWeekday = *req.StartWeekday
	}
	if req.NameTemplate != nil {
		merged.NameTemplate = *req.NameTemplate
	}
	if req.AutoCreateEnabled != nil {
		merged.AutoCreateEnabled = *req.AutoCreateEnabled
	}
	if req.AutoCreateLeadDays != nil {
		merged.AutoCreateLeadDays = *req.AutoCreateLeadDays
	}
	if req.MoveIncompleteEnabled != nil {
		merged.MoveIncompleteEnabled = *req.MoveIncompleteEnabled
	}
	if req.Timezone != nil {
		merged.Timezone = *req.Timezone
	}
	return merged
}

func defaultsForUpsert() mergedSettings {
	return mergedSettings{
		Enabled:               true,
		DurationUnit:          UnitWeek,
		DurationCount:         2,
		StartWeekday:          1,
		NameTemplate:          "Sprint {n}",
		AutoCreateEnabled:     true,
		AutoCreateLeadDays:    2,
		MoveIncompleteEnabled: true,
		Timezone:              "UTC",
	}
}

func validSprintStatus(s string) bool {
	switch s {
	case StatusPlanned, StatusActive, StatusDone, StatusCancelled:
		return true
	default:
		return false
	}
}

func decodeAssignee(typ, id string) (pgtype.Text, pgtype.UUID, error) {
	typ = strings.TrimSpace(typ)
	id = strings.TrimSpace(id)
	if typ == "" && id == "" {
		return pgtype.Text{}, pgtype.UUID{}, nil
	}
	if typ == "" || id == "" {
		return pgtype.Text{}, pgtype.UUID{}, errors.New("assignee_type and assignee_id must be provided together")
	}
	if typ != "member" && typ != "agent" {
		return pgtype.Text{}, pgtype.UUID{}, errors.New("assignee_type must be member or agent")
	}
	uid, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.Text{}, pgtype.UUID{}, errors.New("invalid assignee_id")
	}
	return pgtype.Text{String: typ, Valid: true}, uid, nil
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing "+name)
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return pgtype.UUID{}, false
	}
	return uid, true
}

func workspaceFromContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := middleware.WorkspaceIDFromContext(r.Context())
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing workspace")
		return pgtype.UUID{}, false
	}
	uid, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace")
		return pgtype.UUID{}, false
	}
	return uid, true
}

func ensureWorkspaceMatch(w http.ResponseWriter, r *http.Request, target pgtype.UUID) bool {
	current, ok := workspaceFromContext(w, r)
	if !ok {
		return false
	}
	if !uuidEqual(current, target) {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return false
	}
	return true
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func nextSequenceFor(ctx context.Context, q *cerebrodb.Queries, projectID pgtype.UUID) (int32, error) {
	latest, err := q.GetLatestCerebroSprintByProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 1, nil
		}
		return 0, err
	}
	return latest.SequenceNo + 1, nil
}

func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Silence unused-imports when the build only links a subset of the
// handlers. time is referenced through tsString; left here as a guard.
var _ = time.RFC3339

// ---------------------------------------------------------------------------
// Manual sweep trigger
// ---------------------------------------------------------------------------

// sweepResponse is the JSON shape returned by SweepProject. Counters are
// always present (zero when nothing happened) so the QA + ops clients can
// assert on exact numbers.
type sweepResponse struct {
	FlagEnabled           bool   `json:"flag_enabled"`
	SprintsMarkedDone     int    `json:"sprints_marked_done"`
	PlannedActivated      string `json:"planned_activated,omitempty"`
	NextSprintCreated     string `json:"next_sprint_created,omitempty"`
	IncompleteIssuesMoved int    `json:"incomplete_issues_moved"`
	RecurringTasksCloned  int    `json:"recurring_tasks_cloned"`
}

// SweepProject runs the sprint sweeper for one project synchronously and
// returns a summary of what changed. Used by QA + ops to verify behaviour
// without waiting for the daily background tick.
//
// Workspace owner/admin only. The endpoint mirrors the daily sweeper:
// honors the workspace cerebro_sprints flag, runs advance-expired /
// activate-planned / maybe-create-next with the project's settings.
func (h *Handler) SweepProject(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can trigger a sprint sweep")
		return
	}
	projectID, ok := parseUUIDParam(w, r, "projectID")
	if !ok {
		return
	}
	// Settings always carry workspace_id. Load them up front so we can
	// reject a cross-workspace projectID before constructing the Sweeper.
	settings, err := h.Cerebro.GetCerebroSprintSettings(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no sprint settings for project")
			return
		}
		writeError(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	if !uuidEqual(settings.WorkspaceID, member.WorkspaceID) {
		writeError(w, http.StatusForbidden, "workspace mismatch")
		return
	}

	sw := NewSweeper(h.Pool, h.Cerebro, h.Upstream)
	res, err := sw.SweepProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sweep failed: "+err.Error())
		return
	}

	resp := sweepResponse{
		FlagEnabled:           res.FlagEnabled,
		SprintsMarkedDone:     res.SprintsMarkedDone,
		IncompleteIssuesMoved: res.IncompleteIssuesMoved,
		RecurringTasksCloned:  res.RecurringTasksCloned,
	}
	if res.PlannedActivated.Valid {
		resp.PlannedActivated = util.UUIDToString(res.PlannedActivated)
	}
	if res.NextSprintCreated.Valid {
		resp.NextSprintCreated = util.UUIDToString(res.NextSprintCreated)
	}
	writeJSON(w, http.StatusOK, resp)
}
