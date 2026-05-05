package handler

import (
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
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	UserID          string  `json:"user_id"`
	IssueID         *string `json:"issue_id"`
	Description     *string `json:"description"`
	StartTime       string  `json:"start_time"`
	StopTime        *string `json:"stop_time"`
	DurationSeconds int64   `json:"duration_seconds"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// CreateTimeEntryRequest is used for both "start live timer" and "create manual entry".
type CreateTimeEntryRequest struct {
	Description *string `json:"description"`
	IssueID     *string `json:"issue_id"`
	// StartTime is required. ISO 8601 / RFC 3339.
	StartTime string `json:"start_time"`
	// StopTime is optional. Omit to start a live timer; include for manual entries.
	StopTime *string `json:"stop_time"`
}

// UpdateTimeEntryRequest allows patching description and/or issue link.
type UpdateTimeEntryRequest struct {
	Description *string `json:"description"`
	IssueID     *string `json:"issue_id"`
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
		CreatedAt:       timestampToString(e.CreatedAt),
		UpdatedAt:       timestampToString(e.UpdatedAt),
	}
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
		// Manual/historical entry — just create directly, no auto-stop.
		stopTime, err := time.Parse(time.RFC3339, *req.StopTime)
		if err != nil {
			writeError(w, http.StatusBadRequest, "stop_time must be RFC 3339")
			return
		}
		elapsed := int64(stopTime.Sub(startTime).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		entry, err := h.Queries.CreateTimeEntry(r.Context(), db.CreateTimeEntryParams{
			WorkspaceID:     parseUUID(workspaceID),
			UserID:          parseUUID(userID),
			IssueID:         parseOptionalUUID(req.IssueID),
			Description:     ptrToText(req.Description),
			StartTime:       pgTimestamp(startTime),
			StopTime:        pgTimestamp(stopTime),
			DurationSeconds: elapsed,
		})
		if err != nil {
			slog.Warn("create manual time entry failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create time entry")
			return
		}
		resp := timeEntryToResponse(entry)
		h.publish(protocol.EventTimeEntryStarted, workspaceID, "member", userID, map[string]any{"time_entry": resp})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Live timer start.
	entry, err := svc.StartTimer(r.Context(), workspaceID, userID, req.Description, req.IssueID, startTime)
	if err != nil {
		slog.Warn("start timer failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start timer")
		return
	}
	resp := timeEntryToResponse(entry)
	h.publish(protocol.EventTimeEntryStarted, workspaceID, "member", userID, map[string]any{"time_entry": resp})
	writeJSON(w, http.StatusCreated, resp)
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
	resp := timeEntryToResponse(entry)
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
	writeJSON(w, http.StatusOK, timeEntryToResponse(*entry))
}

// ListTimeEntries handles GET /api/time-entries — paginated list for the current user.
func (h *Handler) ListTimeEntries(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := resolveWorkspaceID(r)

	limit := parseInt32Query(r, "limit", 50)
	offset := parseInt32Query(r, "offset", 0)

	entries, err := h.timeEntrySvc().ListTimeEntries(r.Context(), workspaceID, userID, limit, offset)
	if err != nil {
		slog.Warn("list time entries failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list time entries")
		return
	}

	resp := make([]TimeEntryResponse, len(entries))
	for i, e := range entries {
		resp[i] = timeEntryToResponse(e)
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

	resp := make([]TimeEntryResponse, len(entries))
	for i, e := range entries {
		resp[i] = timeEntryToResponse(e)
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

	entry, err := h.timeEntrySvc().UpdateTimeEntry(r.Context(), workspaceID, userID, entryID, req.Description, req.IssueID)
	if err != nil {
		if errors.Is(err, service.ErrTimeEntryNotFound) {
			writeError(w, http.StatusNotFound, "time entry not found")
			return
		}
		slog.Warn("update time entry failed", append(logger.RequestAttrs(r), "error", err, "entry_id", entryID)...)
		writeError(w, http.StatusInternalServerError, "failed to update time entry")
		return
	}
	resp := timeEntryToResponse(entry)
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
