package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TimeEntryResponse is the JSON shape returned to clients.
type TimeEntryResponse struct {
	ID              string                   `json:"id"`
	WorkspaceID     string                   `json:"workspace_id"`
	UserID          string                   `json:"user_id"`
	IssueID         *string                  `json:"issue_id"`
	Description     *string                  `json:"description"`
	StartTime       string                   `json:"start_time"`
	StopTime        *string                  `json:"stop_time"`
	DurationSeconds int64                    `json:"duration_seconds"`
	Type            string                   `json:"type"`
	Labels          []TimeEntryLabelResponse `json:"labels,omitempty"`
	CreatedAt       string                   `json:"created_at"`
	UpdatedAt       string                   `json:"updated_at"`
}

// TimeEntryLabelResponse is the JSON shape of a workspace-scoped time entry label.
type TimeEntryLabelResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
}

// CreateTimeEntryRequest is used for both "start live timer" and "create manual entry".
type CreateTimeEntryRequest struct {
	Description *string  `json:"description"`
	IssueID     *string  `json:"issue_id"`
	LabelIDs    []string `json:"label_ids"`
	// StartTime is required. ISO 8601 / RFC 3339.
	StartTime string `json:"start_time"`
	// StopTime is optional. Omit to start a live timer; include for manual entries.
	StopTime       *string `json:"stop_time"`
	ConfirmOverlap bool    `json:"confirm_overlap"`
}

// SwitchTimeEntryRequest is used to explicitly switch from one timer to another.
type SwitchTimeEntryRequest struct {
	Description *string  `json:"description"`
	IssueID     *string  `json:"issue_id"`
	LabelIDs    []string `json:"label_ids"`
	StartTime   string   `json:"start_time"`
}

// UpdateTimeEntryRequest allows patching description, issue link, and start/stop times.
// All fields are optional. Duration is recalculated automatically when start or stop changes.
//
// issue_id semantics (using **string to distinguish JSON absent vs null):
//   - field absent in JSON body  → outer pointer is nil → keep existing issue link unchanged
//   - "issue_id": null in body   → outer pointer non-nil, inner nil → clear the issue link
//   - "issue_id": "uuid" in body → both pointers non-nil → link to this issue
type UpdateTimeEntryRequest struct {
	Description *string   `json:"description"`
	IssueID     **string  `json:"issue_id"`
	LabelIDs    *[]string `json:"label_ids"`
	// StartTime and StopTime are ISO 8601 / RFC 3339. Only valid for stopped entries.
	StartTime      *string `json:"start_time"`
	StopTime       *string `json:"stop_time"`
	ConfirmOverlap bool    `json:"confirm_overlap"`
}

// TimeEntryOverlapResponse is returned when creating/updating overlapping entries.
type TimeEntryOverlapResponse struct {
	Error     string              `json:"error"`
	Code      string              `json:"code"`
	Conflicts []TimeEntryResponse `json:"conflicts"`
}

// timeEntryToResponse converts a db.TimeEntry row into the public response shape.
func timeEntryToResponse(e db.TimeEntry) TimeEntryResponse {
	return TimeEntryResponse{
		ID:              uuidToString(e.ID),
		WorkspaceID:     uuidToString(e.WorkspaceID),
		UserID:          uuidToString(e.UserID),
		IssueID:         uuidToPtr(e.IssueID),
		Description:     textToPtr(e.Description),
		StartTime:       timestampToString(e.StartTime),
		StopTime:        timestampToPtr(e.StopTime),
		DurationSeconds: e.DurationSeconds,
		Type:            e.Type,
		CreatedAt:       timestampToString(e.CreatedAt),
		UpdatedAt:       timestampToString(e.UpdatedAt),
	}
}

// timeEntryLabelToResponse converts a db.TimeEntryLabel into the public response shape.
func timeEntryLabelToResponse(label db.TimeEntryLabel) TimeEntryLabelResponse {
	return TimeEntryLabelResponse{
		ID:          uuidToString(label.ID),
		WorkspaceID: uuidToString(label.WorkspaceID),
		Name:        label.Name,
		Color:       label.Color,
	}
}

// buildTimeEntryResponseWithQueries builds a time entry response and attaches its labels.
func (h *Handler) buildTimeEntryResponseWithQueries(ctx context.Context, queries *db.Queries, entry db.TimeEntry) (TimeEntryResponse, error) {
	resp := timeEntryToResponse(entry)

	labels, err := queries.ListLabelsForTimeEntry(ctx, entry.ID)
	if err != nil {
		return TimeEntryResponse{}, err
	}
	if len(labels) > 0 {
		resp.Labels = make([]TimeEntryLabelResponse, len(labels))
		for index, label := range labels {
			resp.Labels[index] = timeEntryLabelToResponse(label)
		}
	}

	return resp, nil
}

// buildTimeEntryResponse builds a time entry response with the default query handle.
func (h *Handler) buildTimeEntryResponse(ctx context.Context, entry db.TimeEntry) (TimeEntryResponse, error) {
	return h.buildTimeEntryResponseWithQueries(ctx, h.Queries, entry)
}

// buildTimeEntryListResponse converts multiple rows and attaches labels for each row.
func (h *Handler) buildTimeEntryListResponse(ctx context.Context, entries []db.TimeEntry) ([]TimeEntryResponse, error) {
	return h.buildTimeEntryListResponseWithQueries(ctx, h.Queries, entries)
}

// buildTimeEntryListResponseWithQueries converts multiple rows and attaches labels using the provided query handle.
func (h *Handler) buildTimeEntryListResponseWithQueries(ctx context.Context, queries *db.Queries, entries []db.TimeEntry) ([]TimeEntryResponse, error) {
	resp := make([]TimeEntryResponse, len(entries))
	for index, entry := range entries {
		entryResp, err := h.buildTimeEntryResponseWithQueries(ctx, queries, entry)
		if err != nil {
			return nil, err
		}
		resp[index] = entryResp
	}
	return resp, nil
}

// timeEntryService returns (creating lazily if not yet cached) the service instance.
// The handler holds it directly as a field so we don't create multiple instances.
func (h *Handler) timeEntrySvc() *service.TimeEntryService {
	if h.TimeEntryService == nil {
		h.TimeEntryService = service.NewTimeEntryService(h.Queries, h.TxStarter)
	}
	return h.TimeEntryService
}

// CreateTimeEntry handles POST /api/issues/:id/time-entries (linked to issue)
// and POST /api/time-entries (standalone).
//
// If stop_time is omitted a live timer is started; any existing running timer is
// auto-stopped first. If stop_time is provided a manual/historical entry is created.
func (h *Handler) CreateTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req CreateTimeEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.StartTime == "" {
		writeError(w, http.StatusBadRequest, "start_time is required")
		return
	}
	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start_time must be RFC 3339")
		return
	}

	// If the route contains an issue path param, pre-fill issue_id.
	if issueIDParam := chi.URLParam(r, "id"); issueIDParam != "" && req.IssueID == nil {
		req.IssueID = &issueIDParam
	}

	svc := h.timeEntrySvc()

	if req.StopTime != nil {
		stopTime, err := time.Parse(time.RFC3339, *req.StopTime)
		if err != nil {
			writeError(w, http.StatusBadRequest, "stop_time must be RFC 3339")
			return
		}
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			slog.Warn("begin manual time entry transaction failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create time entry")
			return
		}
		defer tx.Rollback(r.Context())

		qtx := h.Queries.WithTx(tx)
		if !req.ConfirmOverlap {
			if err := service.LockUserTimeEntryMutations(r.Context(), tx, workspaceID, userID); err != nil {
				slog.Warn("lock manual time entry overlap scope failed", append(logger.RequestAttrs(r), "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to check overlaps")
				return
			}
			overlaps, err := qtx.ListOverlappingStoppedTimeEntries(r.Context(), db.ListOverlappingStoppedTimeEntriesParams{
				WorkspaceID: parseUUID(workspaceID),
				UserID:      parseUUID(userID),
				RangeStart:  pgTimestamp(startTime),
				RangeStop:   pgTimestamp(stopTime),
			})
			if err != nil {
				slog.Warn("check overlaps failed", append(logger.RequestAttrs(r), "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to check overlaps")
				return
			}
			if len(overlaps) > 0 {
				conflicts, buildErr := h.buildTimeEntryListResponseWithQueries(r.Context(), qtx, overlaps)
				if buildErr != nil {
					slog.Warn("build overlap response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
					writeError(w, http.StatusInternalServerError, "failed to check overlaps")
					return
				}
				writeJSON(w, http.StatusConflict, TimeEntryOverlapResponse{
					Error:     "time entry overlaps an existing entry",
					Code:      "time_entry_overlap",
					Conflicts: conflicts,
				})
				return
			}
		}

		elapsed := int64(stopTime.Sub(startTime).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		entry, err := qtx.CreateTimeEntry(r.Context(), db.CreateTimeEntryParams{
			WorkspaceID:     parseUUID(workspaceID),
			UserID:          parseUUID(userID),
			IssueID:         parseOptionalUUID(req.IssueID),
			Description:     ptrToText(req.Description),
			StartTime:       pgTimestamp(startTime),
			StopTime:        pgTimestamp(stopTime),
			DurationSeconds: elapsed,
			Type:            "manual",
		})
		if err != nil {
			slog.Warn("create manual time entry failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create time entry")
			return
		}
		if len(req.LabelIDs) > 0 {
			if err := h.replaceTimeEntryLabelsWithQueries(r.Context(), qtx, entry, req.LabelIDs); err != nil {
				statusCode, message := timeEntryLabelMutationErrorResponse(err)
				if statusCode >= http.StatusInternalServerError {
					slog.Warn("apply manual time entry labels failed", append(logger.RequestAttrs(r), "error", err)...)
				}
				writeError(w, statusCode, message)
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			slog.Warn("commit manual time entry transaction failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create time entry")
			return
		}
		resp, buildErr := h.buildTimeEntryResponse(r.Context(), entry)
		if buildErr != nil {
			slog.Warn("build time entry response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
			writeError(w, http.StatusInternalServerError, "failed to create time entry")
			return
		}
		h.publish(protocol.EventTimeEntryStarted, workspaceID, "member", userID, map[string]any{"time_entry": resp})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Live timer start.
	entry, err := svc.StartTimer(r.Context(), workspaceID, userID, req.Description, req.IssueID, startTime, func(ctx context.Context, queries *db.Queries, entry db.TimeEntry) error {
		if len(req.LabelIDs) == 0 {
			return nil
		}
		return h.replaceTimeEntryLabelsWithQueries(ctx, queries, entry, req.LabelIDs)
	})
	if err != nil {
		statusCode, message := timeEntryLabelMutationErrorResponse(err)
		if len(req.LabelIDs) > 0 && statusCode == http.StatusBadRequest {
			writeError(w, statusCode, message)
			return
		}
		slog.Warn("start timer failed", append(logger.RequestAttrs(r), "error", err)...)
		if len(req.LabelIDs) > 0 {
			writeError(w, statusCode, message)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to start timer")
		return
	}
	resp, buildErr := h.buildTimeEntryResponse(r.Context(), entry)
	if buildErr != nil {
		slog.Warn("build started time entry response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
		writeError(w, http.StatusInternalServerError, "failed to start timer")
		return
	}
	h.publish(protocol.EventTimeEntryStarted, workspaceID, "member", userID, map[string]any{"time_entry": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// SwitchTimeEntry handles POST /api/time-entries/switch.
// Explicitly stops the current timer and starts a new one.
func (h *Handler) SwitchTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	var req SwitchTimeEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start_time must be RFC 3339")
		return
	}
	_, started, err := h.timeEntrySvc().SwitchTimer(r.Context(), workspaceID, userID, req.Description, req.IssueID, startTime, func(ctx context.Context, queries *db.Queries, entry db.TimeEntry) error {
		if len(req.LabelIDs) == 0 {
			return nil
		}
		return h.replaceTimeEntryLabelsWithQueries(ctx, queries, entry, req.LabelIDs)
	})
	if err != nil {
		if errors.Is(err, service.ErrTimerNotRunning) {
			writeError(w, http.StatusBadRequest, "no timer is running")
			return
		}
		statusCode, message := timeEntryLabelMutationErrorResponse(err)
		if len(req.LabelIDs) > 0 && statusCode == http.StatusBadRequest {
			writeError(w, statusCode, message)
			return
		}
		slog.Warn("switch timer failed", append(logger.RequestAttrs(r), "error", err)...)
		if len(req.LabelIDs) > 0 {
			writeError(w, statusCode, message)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to switch timer")
		return
	}
	startedResp, buildErr := h.buildTimeEntryResponse(r.Context(), started)
	if buildErr != nil {
		slog.Warn("build started time entry response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
		writeError(w, http.StatusInternalServerError, "failed to switch timer")
		return
	}
	h.publish(protocol.EventTimeEntryStarted, workspaceID, "member", userID, map[string]any{"time_entry": startedResp})
	writeJSON(w, http.StatusCreated, startedResp)
}

// StopTimeEntry handles PATCH /api/time-entries/:entry_id/stop.
func (h *Handler) StopTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "entry_id")

	entry, err := h.timeEntrySvc().StopTimer(r.Context(), workspaceID, userID, entryID)
	if err != nil {
		if errors.Is(err, service.ErrTimeEntryNotFound) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		if errors.Is(err, service.ErrTimerNotRunning) {
			writeError(w, http.StatusBadRequest, "timer is not running")
			return
		}
		slog.Warn("stop timer failed", append(logger.RequestAttrs(r), "error", err, "entry_id", entryID)...)
		writeError(w, http.StatusInternalServerError, "failed to stop timer")
		return
	}
	resp, buildErr := h.buildTimeEntryResponse(r.Context(), entry)
	if buildErr != nil {
		slog.Warn("build stopped time entry response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
		writeError(w, http.StatusInternalServerError, "failed to stop timer")
		return
	}
	h.publish(protocol.EventTimeEntryStopped, workspaceID, "member", userID, map[string]any{"time_entry": resp})
	writeJSON(w, http.StatusOK, resp)
}

// GetCurrentTimeEntry handles GET /api/time-entries/current.
func (h *Handler) GetCurrentTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	entry, err := h.timeEntrySvc().GetCurrentTimer(r.Context(), workspaceID, userID)
	if err != nil {
		slog.Warn("get current timer failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get current timer")
		return
	}
	if entry == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	resp, buildErr := h.buildTimeEntryResponse(r.Context(), *entry)
	if buildErr != nil {
		slog.Warn("build current time entry response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
		writeError(w, http.StatusInternalServerError, "failed to get current timer")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListTimeEntries handles GET /api/time-entries — list time entries for the current user.
//
// Supports two modes:
//   - Date-range mode (preferred): ?since=RFC3339&until=RFC3339 — returns all entries whose
//     start_time falls within [since, until). Ideal for calendar and day-grouped views.
//   - Pagination mode (fallback): ?limit=N&offset=N — returns at most N entries.
func (h *Handler) ListTimeEntries(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	sinceStr := r.URL.Query().Get("since")
	untilStr := r.URL.Query().Get("until")

	var entries []db.TimeEntry
	var err error

	if sinceStr != "" && untilStr != "" {
		// Date-range mode.
		since, parseErr := time.Parse(time.RFC3339, sinceStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid since format (use RFC 3339)")
			return
		}
		until, parseErr := time.Parse(time.RFC3339, untilStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid until format (use RFC 3339)")
			return
		}
		entries, err = h.timeEntrySvc().ListTimeEntriesByRange(r.Context(), workspaceID, userID, since, until)
	} else {
		// Pagination fallback.
		limit := parseInt32Query(r, "limit", 50)
		offset := parseInt32Query(r, "offset", 0)
		entries, err = h.timeEntrySvc().ListTimeEntries(r.Context(), workspaceID, userID, limit, offset)
	}

	if err != nil {
		slog.Warn("list time entries failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list time entries")
		return
	}

	resp, buildErr := h.buildTimeEntryListResponse(r.Context(), entries)
	if buildErr != nil {
		slog.Warn("build time entry list response failed", append(logger.RequestAttrs(r), "error", buildErr)...)
		writeError(w, http.StatusInternalServerError, "failed to list time entries")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListIssueTimeEntries handles GET /api/issues/:id/time-entries.
func (h *Handler) ListIssueTimeEntries(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	entries, err := h.timeEntrySvc().ListIssueTimeEntries(
		r.Context(),
		uuidToString(issue.WorkspaceID),
		uuidToString(issue.ID),
	)
	if err != nil {
		slog.Warn("list issue time entries failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to list time entries")
		return
	}

	resp, buildErr := h.buildTimeEntryListResponse(r.Context(), entries)
	if buildErr != nil {
		slog.Warn("build issue time entry list response failed", append(logger.RequestAttrs(r), "error", buildErr, "issue_id", issueID)...)
		writeError(w, http.StatusInternalServerError, "failed to list time entries")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateTimeEntry handles PATCH /api/time-entries/:entry_id.
func (h *Handler) UpdateTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "entry_id")

	var req UpdateTimeEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Parse optional timestamps.
	var startTime, stopTime *time.Time
	if req.StartTime != nil && *req.StartTime != "" {
		t, err := time.Parse(time.RFC3339, *req.StartTime)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_time format (use RFC 3339)")
			return
		}
		startTime = &t
	}
	if req.StopTime != nil && *req.StopTime != "" {
		t, err := time.Parse(time.RFC3339, *req.StopTime)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid stop_time format (use RFC 3339)")
			return
		}
		stopTime = &t
	}

	entry, err := h.timeEntrySvc().UpdateTimeEntry(r.Context(), workspaceID, userID, entryID, req.Description, req.IssueID, startTime, stopTime, req.ConfirmOverlap, func(ctx context.Context, queries *db.Queries, entry db.TimeEntry) error {
		if req.LabelIDs == nil {
			return nil
		}
		return h.replaceTimeEntryLabelsWithQueries(ctx, queries, entry, *req.LabelIDs)
	})
	if err != nil {
		if errors.Is(err, service.ErrTimeEntryNotFound) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		var overlapErr *service.TimeEntryOverlapError
		if errors.As(err, &overlapErr) {
			conflicts, buildErr := h.buildTimeEntryListResponse(r.Context(), overlapErr.Conflicts)
			if buildErr != nil {
				slog.Warn("build overlap response failed", append(logger.RequestAttrs(r), "error", buildErr, "entry_id", entryID)...)
				writeError(w, http.StatusInternalServerError, "failed to check overlaps")
				return
			}
			writeJSON(w, http.StatusConflict, TimeEntryOverlapResponse{
				Error:     overlapErr.Error(),
				Code:      "time_entry_overlap",
				Conflicts: conflicts,
			})
			return
		}
		if req.LabelIDs != nil {
			statusCode, message := timeEntryLabelMutationErrorResponse(err)
			if statusCode >= http.StatusInternalServerError {
				slog.Warn("update time entry failed", append(logger.RequestAttrs(r), "error", err, "entry_id", entryID)...)
			}
			writeError(w, statusCode, message)
			return
		}
		slog.Warn("update time entry failed", append(logger.RequestAttrs(r), "error", err, "entry_id", entryID)...)
		writeError(w, http.StatusInternalServerError, "failed to update time entry")
		return
	}
	resp, buildErr := h.buildTimeEntryResponse(r.Context(), entry)
	if buildErr != nil {
		slog.Warn("build updated time entry response failed", append(logger.RequestAttrs(r), "error", buildErr, "entry_id", entryID)...)
		writeError(w, http.StatusInternalServerError, "failed to update time entry")
		return
	}
	h.publish(protocol.EventTimeEntryUpdated, workspaceID, "member", userID, map[string]any{"time_entry": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteTimeEntry handles DELETE /api/time-entries/:entry_id.
func (h *Handler) DeleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "entry_id")

	if err := h.timeEntrySvc().DeleteTimeEntry(r.Context(), workspaceID, userID, entryID); err != nil {
		if errors.Is(err, service.ErrTimeEntryNotFound) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		slog.Warn("delete time entry failed", append(logger.RequestAttrs(r), "error", err, "entry_id", entryID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete time entry")
		return
	}
	h.publish(protocol.EventTimeEntryDeleted, workspaceID, "member", userID, map[string]any{"time_entry_id": entryID})
	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// TeamTimeUserStatResponse is one row in the by_user slice of GetTeamTimeStats.
type TeamTimeUserStatResponse struct {
	UserID       string `json:"user_id"`
	TotalSeconds int64  `json:"total_seconds"`
}

// TeamTimeProjectStatResponse is one row in the by_project slice of GetTeamTimeStats.
type TeamTimeProjectStatResponse struct {
	// ProjectID is nil for time entries not linked to any project.
	ProjectID    *string `json:"project_id"`
	TotalSeconds int64   `json:"total_seconds"`
}

// TeamTimeStatsResponse is returned by GET /api/time-entries/team-stats.
type TeamTimeStatsResponse struct {
	Since     string                        `json:"since"`
	Until     string                        `json:"until"`
	ByUser    []TeamTimeUserStatResponse    `json:"by_user"`
	ByProject []TeamTimeProjectStatResponse `json:"by_project"`
}

// GetTeamTimeStats handles GET /api/time-entries/team-stats?since=RFC3339&until=RFC3339.
// Returns aggregated time data for all members in the workspace grouped by user and by project.
// Only stopped entries are counted. Requires workspace membership.
func (h *Handler) GetTeamTimeStats(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	sinceStr := r.URL.Query().Get("since")
	untilStr := r.URL.Query().Get("until")
	if sinceStr == "" || untilStr == "" {
		writeError(w, http.StatusBadRequest, "since and until are required (RFC 3339)")
		return
	}

	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid since format (use RFC 3339)")
		return
	}
	until, err := time.Parse(time.RFC3339, untilStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid until format (use RFC 3339)")
		return
	}

	wsUUID := parseUUID(workspaceID)
	pgSince := pgTimestamp(since)
	pgUntil := pgTimestamp(until)

	// Aggregate by user.
	userRows, err := h.Queries.SumTimeEntriesByUserInWorkspace(r.Context(), db.SumTimeEntriesByUserInWorkspaceParams{
		WorkspaceID: wsUUID,
		StartTime:   pgSince,
		StartTime_2: pgUntil,
	})
	if err != nil {
		slog.Warn("team time stats by user failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to fetch team time stats")
		return
	}

	// Aggregate by project.
	projectRows, err := h.Queries.SumTimeEntriesByProjectInWorkspace(r.Context(), db.SumTimeEntriesByProjectInWorkspaceParams{
		WorkspaceID: wsUUID,
		StartTime:   pgSince,
		StartTime_2: pgUntil,
	})
	if err != nil {
		slog.Warn("team time stats by project failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to fetch team time stats")
		return
	}

	// Build response — convert pgtype UUIDs to strings.
	byUser := make([]TeamTimeUserStatResponse, len(userRows))
	for i, row := range userRows {
		byUser[i] = TeamTimeUserStatResponse{
			UserID:       uuidToString(row.UserID),
			TotalSeconds: row.TotalSeconds,
		}
	}

	byProject := make([]TeamTimeProjectStatResponse, len(projectRows))
	for i, row := range projectRows {
		byProject[i] = TeamTimeProjectStatResponse{
			ProjectID:    uuidToPtr(row.ProjectID),
			TotalSeconds: row.TotalSeconds,
		}
	}

	writeJSON(w, http.StatusOK, TeamTimeStatsResponse{
		Since:     sinceStr,
		Until:     untilStr,
		ByUser:    byUser,
		ByProject: byProject,
	})
}

// pgTimestamp converts a time.Time to pgtype.Timestamptz.
func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// parseOptionalUUID converts a *string to pgtype.UUID (NULL if nil or empty).
func parseOptionalUUID(s *string) pgtype.UUID {
	if s == nil || *s == "" {
		return pgtype.UUID{}
	}
	return parseUUID(*s)
}

// parseInt32Query reads an int32 query param with a default fallback.
func parseInt32Query(r *http.Request, key string, def int32) int32 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < 0 {
		return def
	}
	return int32(n)
}
