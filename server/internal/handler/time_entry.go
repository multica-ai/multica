package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- Response types ----

type TimeEntryResponse struct {
	ID                  string  `json:"id"`
	IssueID             string  `json:"issue_id"`
	UserID              string  `json:"user_id"`
	DurationMinutes     int32   `json:"duration_minutes"`
	ActivityName        *string `json:"activity_name"`
	RedmineActivityID   *int32  `json:"redmine_activity_id"`
	Comment             string  `json:"comment"`
	SpentOn             string  `json:"spent_on"`
	ExternalTimeEntryID *string `json:"external_time_entry_id"`
	SyncStatus          string  `json:"sync_status"`
	TimerStartedAt      *string `json:"timer_started_at"`
	TimerStoppedAt      *string `json:"timer_stopped_at"`
	CreatedAt           string  `json:"created_at"`
}

func timeEntryToResponse(t db.TimeEntry) TimeEntryResponse {
	var activityName *string
	if t.ActivityName.Valid {
		activityName = &t.ActivityName.String
	}
	var activityID *int32
	if t.RedmineActivityID.Valid {
		activityID = &t.RedmineActivityID.Int32
	}
	spentOn := ""
	if t.SpentOn.Valid {
		spentOn = t.SpentOn.Time.Format("2006-01-02")
	}
	return TimeEntryResponse{
		ID:                  uuidToString(t.ID),
		IssueID:             uuidToString(t.IssueID),
		UserID:              uuidToString(t.UserID),
		DurationMinutes:     t.DurationMinutes,
		ActivityName:        activityName,
		RedmineActivityID:   activityID,
		Comment:             t.Comment,
		SpentOn:             spentOn,
		ExternalTimeEntryID: textToPtr(t.ExternalTimeEntryID),
		SyncStatus:          t.SyncStatus,
		TimerStartedAt:      timestampToPtr(t.TimerStartedAt),
		TimerStoppedAt:      timestampToPtr(t.TimerStoppedAt),
		CreatedAt:           timestampToString(t.CreatedAt),
	}
}

// ---- Handlers ----

// CreateTimeEntry creates a time entry for an issue and optionally syncs it to Redmine.
func (h *Handler) CreateTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	issueID := chi.URLParam(r, "id")

	var req struct {
		DurationMinutes   int32   `json:"duration_minutes"`
		RedmineActivityID *int32  `json:"redmine_activity_id"`
		ActivityName      *string `json:"activity_name"`
		Comment           *string `json:"comment"`
		SpentOn           *string `json:"spent_on"`
		TimerStartedAt    *string `json:"timer_started_at"`
		TimerStoppedAt    *string `json:"timer_stopped_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DurationMinutes <= 0 {
		writeError(w, http.StatusBadRequest, "duration_minutes must be positive")
		return
	}

	// Parse spent_on (default today)
	var spentOn pgtype.Date
	if req.SpentOn != nil && *req.SpentOn != "" {
		t, err := time.Parse("2006-01-02", *req.SpentOn)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid spent_on date format, use YYYY-MM-DD")
			return
		}
		spentOn = pgtype.Date{Time: t, Valid: true}
	} else {
		spentOn = pgtype.Date{Time: time.Now(), Valid: true}
	}

	// Parse optional timer timestamps
	var timerStartedAt, timerStoppedAt pgtype.Timestamptz
	if req.TimerStartedAt != nil && *req.TimerStartedAt != "" {
		t, err := time.Parse(time.RFC3339, *req.TimerStartedAt)
		if err == nil {
			timerStartedAt = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	if req.TimerStoppedAt != nil && *req.TimerStoppedAt != "" {
		t, err := time.Parse(time.RFC3339, *req.TimerStoppedAt)
		if err == nil {
			timerStoppedAt = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	comment := ""
	if req.Comment != nil {
		comment = *req.Comment
	}

	var activityName pgtype.Text
	if req.ActivityName != nil {
		activityName = strToText(*req.ActivityName)
	}
	var activityID pgtype.Int4
	if req.RedmineActivityID != nil {
		activityID = pgtype.Int4{Int32: *req.RedmineActivityID, Valid: true}
	}

	// Create in DB with initial sync_status
	entry, err := h.Queries.CreateTimeEntry(r.Context(), db.CreateTimeEntryParams{
		WorkspaceID:       parseUUID(workspaceID),
		IssueID:           parseUUID(issueID),
		UserID:            parseUUID(userID),
		DurationMinutes:   req.DurationMinutes,
		ActivityName:      activityName,
		RedmineActivityID: activityID,
		Comment:           comment,
		SpentOn:           spentOn,
		SyncStatus:        "pending",
		TimerStartedAt:    timerStartedAt,
		TimerStoppedAt:    timerStoppedAt,
	})
	if err != nil {
		slog.Error("failed to create time entry", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create time entry")
		return
	}

	// Try to sync to Redmine
	h.syncTimeEntryToRedmine(r, workspaceID, userID, issueID, entry)

	// Re-read to get updated sync_status
	entry, _ = h.Queries.GetTimeEntry(r.Context(), db.GetTimeEntryParams{
		ID:          entry.ID,
		WorkspaceID: entry.WorkspaceID,
	})

	resp := timeEntryToResponse(entry)

	// Publish event
	h.publish(protocol.EventTimeEntryCreated, workspaceID, "member", userID, map[string]any{
		"issue_id":   issueID,
		"time_entry": resp,
	})

	writeJSON(w, http.StatusCreated, resp)
}

// syncTimeEntryToRedmine attempts to sync a time entry to Redmine.
// Updates the sync_status in DB accordingly. Best-effort — failures are logged but don't
// block the response.
func (h *Handler) syncTimeEntryToRedmine(r *http.Request, workspaceID, userID, issueID string, entry db.TimeEntry) {
	ctx := r.Context()

	// Check if user has Redmine credentials
	integration, err := h.Queries.GetWorkspaceIntegration(ctx, db.GetWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Provider:    "redmine",
	})
	if err != nil {
		// No Redmine integration configured — not_linked
		h.Queries.UpdateTimeEntrySyncStatus(ctx, db.UpdateTimeEntrySyncStatusParams{
			ID:                  entry.ID,
			WorkspaceID:         entry.WorkspaceID,
			ExternalTimeEntryID: pgtype.Text{},
			SyncStatus:          "not_linked",
		})
		return
	}

	cred, err := h.Queries.GetUserIntegrationCredential(ctx, db.GetUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    "redmine",
	})
	if err != nil {
		h.Queries.UpdateTimeEntrySyncStatus(ctx, db.UpdateTimeEntrySyncStatusParams{
			ID:                  entry.ID,
			WorkspaceID:         entry.WorkspaceID,
			ExternalTimeEntryID: pgtype.Text{},
			SyncStatus:          "not_linked",
		})
		return
	}

	// Check if the issue is linked to Redmine
	link, err := h.Queries.GetIssueIntegrationLink(ctx, db.GetIssueIntegrationLinkParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
		Provider:    "redmine",
	})
	if err != nil {
		h.Queries.UpdateTimeEntrySyncStatus(ctx, db.UpdateTimeEntrySyncStatusParams{
			ID:                  entry.ID,
			WorkspaceID:         entry.WorkspaceID,
			ExternalTimeEntryID: pgtype.Text{},
			SyncStatus:          "not_linked",
		})
		return
	}

	// Parse the external issue ID
	redmineIssueID, err := strconv.Atoi(link.ExternalIssueID)
	if err != nil {
		slog.Error("invalid external issue ID for Redmine sync", "external_issue_id", link.ExternalIssueID, "error", err)
		h.Queries.UpdateTimeEntrySyncStatus(ctx, db.UpdateTimeEntrySyncStatusParams{
			ID:                  entry.ID,
			WorkspaceID:         entry.WorkspaceID,
			ExternalTimeEntryID: pgtype.Text{},
			SyncStatus:          "failed",
		})
		return
	}

	// Build Redmine time entry
	hours := float64(entry.DurationMinutes) / 60.0
	spentOn := ""
	if entry.SpentOn.Valid {
		spentOn = entry.SpentOn.Time.Format("2006-01-02")
	}

	var redmineActivityID int
	if entry.RedmineActivityID.Valid {
		redmineActivityID = int(entry.RedmineActivityID.Int32)
	}

	redmineEntry, err := h.RedmineClient.CreateTimeEntry(integration.InstanceUrl, cred.ApiKey, service.CreateRedmineTimeEntryReq{
		IssueID:    redmineIssueID,
		Hours:      hours,
		ActivityID: redmineActivityID,
		Comments:   entry.Comment,
		SpentOn:    spentOn,
	})
	if err != nil {
		slog.Error("failed to sync time entry to Redmine", "entry_id", uuidToString(entry.ID), "error", err)
		h.Queries.UpdateTimeEntrySyncStatus(ctx, db.UpdateTimeEntrySyncStatusParams{
			ID:                  entry.ID,
			WorkspaceID:         entry.WorkspaceID,
			ExternalTimeEntryID: pgtype.Text{},
			SyncStatus:          "failed",
		})
		return
	}

	// Success — mark as synced
	externalID := fmt.Sprintf("%d", redmineEntry.ID)
	h.Queries.UpdateTimeEntrySyncStatus(ctx, db.UpdateTimeEntrySyncStatusParams{
		ID:                  entry.ID,
		WorkspaceID:         entry.WorkspaceID,
		ExternalTimeEntryID: pgtype.Text{String: externalID, Valid: true},
		SyncStatus:          "synced",
	})
}

// ListTimeEntries returns all time entries for an issue.
func (h *Handler) ListTimeEntries(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	issueID := chi.URLParam(r, "id")

	entries, err := h.Queries.ListTimeEntriesByIssue(r.Context(), db.ListTimeEntriesByIssueParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list time entries")
		return
	}

	total, err := h.Queries.GetTotalTimeByIssue(r.Context(), db.GetTotalTimeByIssueParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		total = 0
	}

	// Aggregate total minutes across all Multica issues linked to the same Redmine task.
	var redmineTaskTotalMinutes *int32
	redmineLink, linkErr := h.Queries.GetIssueIntegrationLink(r.Context(), db.GetIssueIntegrationLinkParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
		Provider:    "redmine",
	})
	if linkErr == nil {
		taskTotal, qtErr := h.Queries.GetTotalTimeByRedmineExternalIssue(r.Context(), db.GetTotalTimeByRedmineExternalIssueParams{
			WorkspaceID:     parseUUID(workspaceID),
			ExternalIssueID: redmineLink.ExternalIssueID,
		})
		if qtErr == nil {
			redmineTaskTotalMinutes = &taskTotal
		}
	}

	resp := make([]TimeEntryResponse, len(entries))
	for i, e := range entries {
		resp[i] = timeEntryToResponse(e)
	}
	body := map[string]any{
		"time_entries":  resp,
		"total_minutes": total,
	}
	if redmineTaskTotalMinutes != nil {
		body["redmine_task_total_minutes"] = *redmineTaskTotalMinutes
	}
	writeJSON(w, http.StatusOK, body)
}

// DeleteTimeEntry deletes a time entry (own entries only) and removes it from Redmine if synced.
func (h *Handler) DeleteTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "id")

	// Load entry and verify ownership
	entry, err := h.Queries.GetTimeEntry(r.Context(), db.GetTimeEntryParams{
		ID:          parseUUID(entryID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "time entry not found")
		return
	}
	if uuidToString(entry.UserID) != userID {
		writeError(w, http.StatusForbidden, "can only delete your own time entries")
		return
	}

	issueID := uuidToString(entry.IssueID)

	// If synced to Redmine, try to delete from there too
	if entry.SyncStatus == "synced" && entry.ExternalTimeEntryID.Valid {
		h.tryDeleteRedmineTimeEntry(r, workspaceID, userID, entry)
	}

	// Delete from DB
	if err := h.Queries.DeleteTimeEntry(r.Context(), db.DeleteTimeEntryParams{
		ID:          parseUUID(entryID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete time entry")
		return
	}

	// Publish event
	h.publish(protocol.EventTimeEntryDeleted, workspaceID, "member", userID, map[string]any{
		"issue_id":      issueID,
		"time_entry_id": entryID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// tryDeleteRedmineTimeEntry attempts to delete a time entry from Redmine. Best-effort.
func (h *Handler) tryDeleteRedmineTimeEntry(r *http.Request, workspaceID, userID string, entry db.TimeEntry) {
	integration, err := h.Queries.GetWorkspaceIntegration(r.Context(), db.GetWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Provider:    "redmine",
	})
	if err != nil {
		return
	}
	cred, err := h.Queries.GetUserIntegrationCredential(r.Context(), db.GetUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    "redmine",
	})
	if err != nil {
		return
	}

	externalID, err := strconv.Atoi(entry.ExternalTimeEntryID.String)
	if err != nil {
		return
	}

	if err := h.RedmineClient.DeleteTimeEntry(integration.InstanceUrl, cred.ApiKey, externalID); err != nil {
		slog.Error("failed to delete time entry from Redmine", "external_id", externalID, "error", err)
	}
}

// ListRedmineActivities proxies the Redmine time entry activities enumeration.
func (h *Handler) ListRedmineActivities(w http.ResponseWriter, r *http.Request) {
	instanceURL, apiKey, ok := h.redmineContext(w, r)
	if !ok {
		return
	}

	activities, err := h.RedmineClient.ListTimeEntryActivities(instanceURL, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch Redmine activities: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": activities})
}

// UpdateTimeEntry updates a time entry (own entries only) and re-syncs to Redmine if synced.
func (h *Handler) UpdateTimeEntry(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	entryID := chi.URLParam(r, "id")

	entry, err := h.Queries.GetTimeEntry(r.Context(), db.GetTimeEntryParams{
		ID:          parseUUID(entryID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "time entry not found")
		return
	}
	if uuidToString(entry.UserID) != userID {
		writeError(w, http.StatusForbidden, "can only edit your own time entries")
		return
	}

	var req struct {
		DurationMinutes   *int32  `json:"duration_minutes"`
		RedmineActivityID *int32  `json:"redmine_activity_id"`
		ActivityName      *string `json:"activity_name"`
		Comment           *string `json:"comment"`
		SpentOn           *string `json:"spent_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply defaults from existing entry
	dur := entry.DurationMinutes
	if req.DurationMinutes != nil {
		if *req.DurationMinutes <= 0 {
			writeError(w, http.StatusBadRequest, "duration_minutes must be positive")
			return
		}
		dur = *req.DurationMinutes
	}

	activityName := entry.ActivityName
	if req.ActivityName != nil {
		activityName = strToText(*req.ActivityName)
	}
	activityID := entry.RedmineActivityID
	if req.RedmineActivityID != nil {
		activityID = pgtype.Int4{Int32: *req.RedmineActivityID, Valid: true}
	}
	comment := entry.Comment
	if req.Comment != nil {
		comment = *req.Comment
	}
	spentOn := entry.SpentOn
	if req.SpentOn != nil && *req.SpentOn != "" {
		t, err := time.Parse("2006-01-02", *req.SpentOn)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid spent_on date format")
			return
		}
		spentOn = pgtype.Date{Time: t, Valid: true}
	}

	updated, err := h.Queries.UpdateTimeEntry(r.Context(), db.UpdateTimeEntryParams{
		ID:                parseUUID(entryID),
		WorkspaceID:       parseUUID(workspaceID),
		DurationMinutes:   dur,
		ActivityName:      activityName,
		RedmineActivityID: activityID,
		Comment:           comment,
		SpentOn:           spentOn,
	})
	if err != nil {
		slog.Error("failed to update time entry", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update time entry")
		return
	}

	issueID := uuidToString(updated.IssueID)

	// If synced to Redmine, update there too.
	// If previously not_linked, re-attempt sync in case the issue has been linked to Redmine since creation.
	if updated.SyncStatus == "synced" && updated.ExternalTimeEntryID.Valid {
		h.tryUpdateRedmineTimeEntry(r, workspaceID, userID, updated)
	} else if updated.SyncStatus == "not_linked" {
		h.syncTimeEntryToRedmine(r, workspaceID, userID, issueID, updated)
		updated, _ = h.Queries.GetTimeEntry(r.Context(), db.GetTimeEntryParams{
			ID:          updated.ID,
			WorkspaceID: updated.WorkspaceID,
		})
	}

	resp := timeEntryToResponse(updated)

	h.publish(protocol.EventTimeEntryCreated, workspaceID, "member", userID, map[string]any{
		"issue_id":   issueID,
		"time_entry": resp,
	})

	writeJSON(w, http.StatusOK, resp)
}

// tryUpdateRedmineTimeEntry pushes updated fields to Redmine. Best-effort.
func (h *Handler) tryUpdateRedmineTimeEntry(r *http.Request, workspaceID, userID string, entry db.TimeEntry) {
	integration, err := h.Queries.GetWorkspaceIntegration(r.Context(), db.GetWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Provider:    "redmine",
	})
	if err != nil {
		return
	}
	cred, err := h.Queries.GetUserIntegrationCredential(r.Context(), db.GetUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    "redmine",
	})
	if err != nil {
		return
	}

	externalID, err := strconv.Atoi(entry.ExternalTimeEntryID.String)
	if err != nil {
		return
	}

	hours := float64(entry.DurationMinutes) / 60.0
	spentOn := ""
	if entry.SpentOn.Valid {
		spentOn = entry.SpentOn.Time.Format("2006-01-02")
	}
	var activityID int
	if entry.RedmineActivityID.Valid {
		activityID = int(entry.RedmineActivityID.Int32)
	}

	if err := h.RedmineClient.UpdateTimeEntry(integration.InstanceUrl, cred.ApiKey, externalID, service.CreateRedmineTimeEntryReq{
		Hours:      hours,
		ActivityID: activityID,
		Comments:   entry.Comment,
		SpentOn:    spentOn,
	}); err != nil {
		slog.Error("failed to update time entry in Redmine", "external_id", externalID, "error", err)
	}
}

// BulkRetrySyncFailed retries syncing all failed time entries for the current user.
func (h *Handler) BulkRetrySyncFailed(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	entries, err := h.Queries.ListFailedTimeEntries(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failed entries")
		return
	}

	retried := 0
	succeeded := 0
	for _, entry := range entries {
		if uuidToString(entry.UserID) != userID {
			continue
		}
		retried++
		issueID := uuidToString(entry.IssueID)

		// Reset to pending, then attempt sync
		h.Queries.UpdateTimeEntrySyncStatus(r.Context(), db.UpdateTimeEntrySyncStatusParams{
			ID:                  entry.ID,
			WorkspaceID:         entry.WorkspaceID,
			ExternalTimeEntryID: pgtype.Text{},
			SyncStatus:          "pending",
		})

		h.syncTimeEntryToRedmine(r, workspaceID, userID, issueID, entry)

		// Re-read to check if it succeeded
		updated, err := h.Queries.GetTimeEntry(r.Context(), db.GetTimeEntryParams{
			ID:          entry.ID,
			WorkspaceID: entry.WorkspaceID,
		})
		if err == nil && updated.SyncStatus == "synced" {
			succeeded++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"retried":   retried,
		"succeeded": succeeded,
		"failed":    retried - succeeded,
	})
}

// TimeTrackingDashboard returns aggregated time tracking data for the current user.
func (h *Handler) TimeTrackingDashboard(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	// Parse date range from query params
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var startDate, endDate pgtype.Date
	if startStr != "" {
		t, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start date")
			return
		}
		startDate = pgtype.Date{Time: t, Valid: true}
	} else {
		// Default: start of current week (Monday)
		now := time.Now()
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		startDate = pgtype.Date{Time: monday, Valid: true}
	}
	if endStr != "" {
		t, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid end date")
			return
		}
		endDate = pgtype.Date{Time: t, Valid: true}
	} else {
		endDate = pgtype.Date{Time: time.Now(), Valid: true}
	}

	// Daily breakdown
	daily, err := h.Queries.GetDailyTimeByUser(r.Context(), db.GetDailyTimeByUserParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		SpentOn:     startDate,
		SpentOn_2:   endDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get daily time")
		return
	}

	// By activity
	byActivity, err := h.Queries.GetTimeByActivity(r.Context(), db.GetTimeByActivityParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		SpentOn:     startDate,
		SpentOn_2:   endDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get time by activity")
		return
	}

	// By issue
	byIssue, err := h.Queries.GetTimeByIssue(r.Context(), db.GetTimeByIssueParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		SpentOn:     startDate,
		SpentOn_2:   endDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get time by issue")
		return
	}

	// Entries with issue info
	entries, err := h.Queries.ListTimeEntriesByUserDateRange(r.Context(), db.ListTimeEntriesByUserDateRangeParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		SpentOn:     startDate,
		SpentOn_2:   endDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list entries")
		return
	}

	// Format responses
	dailyResp := make([]map[string]any, len(daily))
	for i, d := range daily {
		day := ""
		if d.SpentOn.Valid {
			day = d.SpentOn.Time.Format("2006-01-02")
		}
		dailyResp[i] = map[string]any{"date": day, "total_minutes": d.TotalMinutes}
	}

	activityResp := make([]map[string]any, len(byActivity))
	for i, a := range byActivity {
		activityResp[i] = map[string]any{"activity": a.Activity, "total_minutes": a.TotalMinutes}
	}

	issueResp := make([]map[string]any, len(byIssue))
	for i, iss := range byIssue {
		issueResp[i] = map[string]any{
			"issue_id":      uuidToString(iss.IssueID),
			"issue_number":  iss.IssueNumber,
			"issue_title":   iss.IssueTitle,
			"total_minutes": iss.TotalMinutes,
		}
	}

	entryResp := make([]map[string]any, len(entries))
	for i, e := range entries {
		spentOn := ""
		if e.SpentOn.Valid {
			spentOn = e.SpentOn.Time.Format("2006-01-02")
		}
		entryResp[i] = map[string]any{
			"id":               uuidToString(e.ID),
			"issue_id":         uuidToString(e.IssueID),
			"issue_number":     e.IssueNumber,
			"issue_title":      e.IssueTitle,
			"duration_minutes": e.DurationMinutes,
			"activity_name":    textToPtr(e.ActivityName),
			"comment":          e.Comment,
			"spent_on":         spentOn,
			"sync_status":      e.SyncStatus,
			"created_at":       timestampToString(e.CreatedAt),
		}
	}

	// Total for range
	var totalMinutes int32
	for _, d := range daily {
		totalMinutes += d.TotalMinutes
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"start_date":    startDate.Time.Format("2006-01-02"),
		"end_date":      endDate.Time.Format("2006-01-02"),
		"total_minutes": totalMinutes,
		"daily":         dailyResp,
		"by_activity":   activityResp,
		"by_issue":      issueResp,
		"entries":       entryResp,
	})
}
