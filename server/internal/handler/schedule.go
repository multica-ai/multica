package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ScheduleResponse is the JSON shape returned by the schedule endpoints.
// It's a flat projection of db.ScheduledTask with timestamps rendered as
// RFC3339 strings (matching the convention used by every other handler).
type ScheduleResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	CreatedBy      string  `json:"created_by"`
	Name           string  `json:"name"`
	TitleTemplate  string  `json:"title_template"`
	Description    string  `json:"description"`
	AssigneeType   string  `json:"assignee_type"`
	AssigneeID     string  `json:"assignee_id"`
	Priority       string  `json:"priority"`
	CronExpression string  `json:"cron_expression"`
	Timezone       string  `json:"timezone"`
	Enabled        bool    `json:"enabled"`
	NextRunAt      string  `json:"next_run_at"`
	LastRunAt      *string `json:"last_run_at"`
	LastRunIssueID *string `json:"last_run_issue_id"`
	LastRunError   *string `json:"last_run_error"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func scheduleToResponse(s db.ScheduledTask) ScheduleResponse {
	return ScheduleResponse{
		ID:             uuidToString(s.ID),
		WorkspaceID:    uuidToString(s.WorkspaceID),
		CreatedBy:      uuidToString(s.CreatedBy),
		Name:           s.Name,
		TitleTemplate:  s.TitleTemplate,
		Description:    s.Description,
		AssigneeType:   s.AssigneeType,
		AssigneeID:     uuidToString(s.AssigneeID),
		Priority:       s.Priority,
		CronExpression: s.CronExpression,
		Timezone:       s.Timezone,
		Enabled:        s.Enabled,
		NextRunAt:      timestampToString(s.NextRunAt),
		LastRunAt:      timestampToPtr(s.LastRunAt),
		LastRunIssueID: uuidToPtr(s.LastRunIssueID),
		LastRunError:   textToPtr(s.LastRunError),
		CreatedAt:      timestampToString(s.CreatedAt),
		UpdatedAt:      timestampToString(s.UpdatedAt),
	}
}

// validateAssignee checks that the assignee referenced by a schedule actually
// exists in the workspace. Agents must be live (not archived); members must
// be current workspace members.
func (h *Handler) validateAssignee(ctx context.Context, workspaceID pgtype.UUID, assigneeType, assigneeID string) error {
	aid := parseUUID(assigneeID)
	switch assigneeType {
	case "agent":
		agent, err := h.Queries.GetAgent(ctx, aid)
		if err != nil {
			return fmt.Errorf("agent not found")
		}
		if agent.ArchivedAt.Valid {
			return fmt.Errorf("agent is archived")
		}
		if !bytesEqual(agent.WorkspaceID.Bytes[:], workspaceID.Bytes[:]) {
			return fmt.Errorf("agent does not belong to this workspace")
		}
	case "member":
		member, err := h.Queries.GetMember(ctx, aid)
		if err != nil {
			return fmt.Errorf("member not found")
		}
		if !bytesEqual(member.WorkspaceID.Bytes[:], workspaceID.Bytes[:]) {
			return fmt.Errorf("member does not belong to this workspace")
		}
	default:
		return fmt.Errorf("assignee_type must be 'agent' or 'member'")
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	return bytes.Equal(a, b)
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

type CreateScheduleRequest struct {
	Name           string `json:"name"`
	TitleTemplate  string `json:"title_template"`
	Description    string `json:"description"`
	AssigneeType   string `json:"assignee_type"`
	AssigneeID     string `json:"assignee_id"`
	Priority       string `json:"priority"`
	CronExpression string `json:"cron_expression"`
	Timezone       string `json:"timezone"`
	Enabled        *bool  `json:"enabled"`
}

func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.TitleTemplate == "" {
		writeError(w, http.StatusBadRequest, "title_template is required")
		return
	}
	if req.AssigneeType == "" || req.AssigneeID == "" {
		writeError(w, http.StatusBadRequest, "assignee_type and assignee_id are required")
		return
	}
	if req.CronExpression == "" {
		writeError(w, http.StatusBadRequest, "cron_expression is required")
		return
	}
	if _, err := service.ParseCron(req.CronExpression); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := service.LoadTimezone(req.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	wsID := parseUUID(workspaceID)
	if err := h.validateAssignee(r.Context(), wsID, req.AssigneeType, req.AssigneeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	timezone := req.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	next, err := service.NextFireTime(req.CronExpression, timezone, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	row, err := h.Queries.CreateScheduledTask(r.Context(), db.CreateScheduledTaskParams{
		WorkspaceID:    wsID,
		CreatedBy:      member.ID,
		Name:           req.Name,
		TitleTemplate:  req.TitleTemplate,
		Description:    req.Description,
		AssigneeType:   req.AssigneeType,
		AssigneeID:     parseUUID(req.AssigneeID),
		Priority:       priority,
		CronExpression: req.CronExpression,
		Timezone:       timezone,
		Enabled:        enabled,
		NextRunAt:      pgtype.Timestamptz{Time: next, Valid: true},
	})
	if err != nil {
		slog.Warn("create schedule failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create schedule: "+err.Error())
		return
	}
	slog.Info("schedule created", append(logger.RequestAttrs(r), "schedule_id", uuidToString(row.ID), "name", row.Name, "workspace_id", workspaceID)...)
	writeJSON(w, http.StatusCreated, scheduleToResponse(row))
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	rows, err := h.Queries.ListScheduledTasks(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}
	resp := make([]ScheduleResponse, len(rows))
	for i, s := range rows {
		resp[i] = scheduleToResponse(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func (h *Handler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	id := chi.URLParam(r, "scheduleId")
	row, err := h.Queries.GetScheduledTaskForWorkspace(r.Context(), db.GetScheduledTaskForWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	writeJSON(w, http.StatusOK, scheduleToResponse(row))
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

type UpdateScheduleRequest struct {
	Name           *string `json:"name"`
	TitleTemplate  *string `json:"title_template"`
	Description    *string `json:"description"`
	AssigneeType   *string `json:"assignee_type"`
	AssigneeID     *string `json:"assignee_id"`
	Priority       *string `json:"priority"`
	CronExpression *string `json:"cron_expression"`
	Timezone       *string `json:"timezone"`
	Enabled        *bool   `json:"enabled"`
}

func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	id := chi.URLParam(r, "scheduleId")

	existing, err := h.Queries.GetScheduledTaskForWorkspace(r.Context(), db.GetScheduledTaskForWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	var req UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateScheduledTaskParams{ID: existing.ID}

	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.TitleTemplate != nil {
		params.TitleTemplate = pgtype.Text{String: *req.TitleTemplate, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if req.AssigneeType != nil || req.AssigneeID != nil {
		if req.AssigneeType == nil || req.AssigneeID == nil {
			writeError(w, http.StatusBadRequest, "assignee_type and assignee_id must be updated together")
			return
		}
		if err := h.validateAssignee(r.Context(), existing.WorkspaceID, *req.AssigneeType, *req.AssigneeID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.AssigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		params.AssigneeID = parseUUID(*req.AssigneeID)
	}

	// Cron / tz / enabled transitions all affect next_run_at, so they're
	// folded together here. Rule:
	//   - Changing cron or tz: recompute next_run_at from now.
	//   - Enabling a previously-disabled schedule: recompute from now too.
	//   - Disabling: leave enabled=false; next_run_at is ignored while
	//     disabled, and will be recomputed when re-enabled.
	recomputeNext := false
	cronExpr := existing.CronExpression
	timezone := existing.Timezone
	if req.CronExpression != nil {
		if _, err := service.ParseCron(*req.CronExpression); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cronExpr = *req.CronExpression
		params.CronExpression = pgtype.Text{String: cronExpr, Valid: true}
		recomputeNext = true
	}
	if req.Timezone != nil {
		if _, err := service.LoadTimezone(*req.Timezone); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		timezone = *req.Timezone
		params.Timezone = pgtype.Text{String: timezone, Valid: true}
		recomputeNext = true
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
		// Recompute when transitioning from disabled → enabled so the
		// schedule fires at the next future slot, not a stale past slot.
		if *req.Enabled && !existing.Enabled {
			recomputeNext = true
		}
	}

	if recomputeNext {
		next, err := service.NextFireTime(cronExpr, timezone, time.Now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.NextRunAt = pgtype.Timestamptz{Time: next, Valid: true}
	}

	updated, err := h.Queries.UpdateScheduledTask(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update schedule: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scheduleToResponse(updated))
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	id := chi.URLParam(r, "scheduleId")

	// Scope check via workspace lookup so we can't delete other workspaces'
	// schedules even with a valid id guess.
	if _, err := h.Queries.GetScheduledTaskForWorkspace(r.Context(), db.GetScheduledTaskForWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if err := h.Queries.ArchiveScheduledTask(r.Context(), parseUUID(id)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Run now
// ---------------------------------------------------------------------------

// RunScheduleNow fires a single scheduled task immediately, regardless of its
// cron slot. Useful for testing from the UI: users can click "Run now" and
// verify the issue lands in their workspace without waiting for a cron tick.
// The schedule's next_run_at is NOT advanced — a manual fire is independent
// of the schedule's regular cadence.
func (h *Handler) RunScheduleNow(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	id := chi.URLParam(r, "scheduleId")

	row, err := h.Queries.GetScheduledTaskForWorkspace(r.Context(), db.GetScheduledTaskForWorkspaceParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	// Fire via a transactional path identical to the scheduler loop's
	// fireSchedule, so the behavior is the same.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin tx")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), row.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
		return
	}
	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID:   row.WorkspaceID,
		Title:         row.TitleTemplate,
		Description:   pgtype.Text{String: row.Description, Valid: row.Description != ""},
		Status:        "todo",
		Priority:      row.Priority,
		AssigneeType:  pgtype.Text{String: row.AssigneeType, Valid: true},
		AssigneeID:    row.AssigneeID,
		CreatorType:   "member",
		CreatorID:     row.CreatedBy,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit tx")
		return
	}

	// Enqueue task-queue entry for agent assignees.
	if row.AssigneeType == "agent" {
		if _, err := h.TaskService.EnqueueTaskForIssue(r.Context(), issue); err != nil {
			slog.Warn("run-now enqueue failed", append(logger.RequestAttrs(r), "error", err, "schedule_id", id, "issue_id", uuidToString(issue.ID))...)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":   "fired",
		"issue_id": uuidToString(issue.ID),
	})
}
