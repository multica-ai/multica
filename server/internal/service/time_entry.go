package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TimeEntryService orchestrates the timer lifecycle (start, stop, CRUD).
type TimeEntryService struct {
	Queries   *db.Queries
	TxStarter txStarter
}

// txStarter is the minimum interface needed to open a transaction.
type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// NewTimeEntryService creates a new TimeEntryService.
func NewTimeEntryService(q *db.Queries, tx txStarter) *TimeEntryService {
	return &TimeEntryService{Queries: q, TxStarter: tx}
}

// StartTimer stops any existing running timer for the user, then starts a new
// one. Returns the newly created time entry. Uses a transaction so auto-stop
// and create are atomic.
func (s *TimeEntryService) StartTimer(
	ctx context.Context,
	workspaceID, userID string,
	description *string,
	issueID *string,
	startTime time.Time,
) (db.TimeEntry, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.TimeEntry{}, fmt.Errorf("start timer: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Auto-stop any existing running timer for this user.
	existing, err := qtx.GetRunningTimerByUser(ctx, db.GetRunningTimerByUserParams{
		UserID:      util.ParseUUID(userID),
		WorkspaceID: util.ParseUUID(workspaceID),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.TimeEntry{}, fmt.Errorf("start timer: check running: %w", err)
	}
	if err == nil {
		// Auto-stop the existing entry.
		stopTime := time.Now()
		elapsed := int64(stopTime.Sub(existing.StartTime.Time).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		_, err = qtx.StopTimeEntry(ctx, db.StopTimeEntryParams{
			ID:              existing.ID,
			WorkspaceID:     existing.WorkspaceID,
			StopTime:        pgtype.Timestamptz{Time: stopTime, Valid: true},
			DurationSeconds: elapsed,
		})
		if err != nil {
			return db.TimeEntry{}, fmt.Errorf("start timer: auto-stop existing: %w", err)
		}
		if err := qtx.ClearRunningTimerByUser(ctx, util.ParseUUID(userID)); err != nil {
			return db.TimeEntry{}, fmt.Errorf("start timer: clear running timer: %w", err)
		}
	}

	// duration_seconds = -start_time.Unix() while running (Toggl convention).
	durationSeconds := -startTime.Unix()

	entry, err := qtx.CreateTimeEntry(ctx, db.CreateTimeEntryParams{
		WorkspaceID:     util.ParseUUID(workspaceID),
		UserID:          util.ParseUUID(userID),
		IssueID:         optionalUUID(issueID),
		Description:     util.PtrToText(description),
		StartTime:       pgtype.Timestamptz{Time: startTime, Valid: true},
		StopTime:        pgtype.Timestamptz{}, // NULL: timer is running
		DurationSeconds: durationSeconds,
	})
	if err != nil {
		return db.TimeEntry{}, fmt.Errorf("start timer: create entry: %w", err)
	}

	// Record the running timer for O(1) lookups.
	if err := qtx.SetRunningTimer(ctx, db.SetRunningTimerParams{
		UserID:      util.ParseUUID(userID),
		TimeEntryID: entry.ID,
	}); err != nil {
		return db.TimeEntry{}, fmt.Errorf("start timer: set running timer: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.TimeEntry{}, fmt.Errorf("start timer: commit: %w", err)
	}

	slog.Debug("timer started", "workspace_id", workspaceID, "user_id", userID, "entry_id", util.UUIDToString(entry.ID))
	return entry, nil
}

// StopTimer stops the given running time entry, computing the final duration.
func (s *TimeEntryService) StopTimer(
	ctx context.Context,
	workspaceID, userID, timeEntryID string,
) (db.TimeEntry, error) {
	// Verify the entry belongs to this user.
	entry, err := s.Queries.GetTimeEntryByID(ctx, db.GetTimeEntryByIDParams{
		ID:          util.ParseUUID(timeEntryID),
		WorkspaceID: util.ParseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.TimeEntry{}, ErrTimeEntryNotFound
		}
		return db.TimeEntry{}, fmt.Errorf("stop timer: get entry: %w", err)
	}
	if util.UUIDToString(entry.UserID) != userID {
		return db.TimeEntry{}, ErrTimeEntryNotFound
	}
	if entry.StopTime.Valid {
		return db.TimeEntry{}, ErrTimerNotRunning
	}

	stopTime := time.Now()
	elapsed := int64(stopTime.Sub(entry.StartTime.Time).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}

	stopped, err := s.Queries.StopTimeEntry(ctx, db.StopTimeEntryParams{
		ID:              entry.ID,
		WorkspaceID:     entry.WorkspaceID,
		StopTime:        pgtype.Timestamptz{Time: stopTime, Valid: true},
		DurationSeconds: elapsed,
	})
	if err != nil {
		return db.TimeEntry{}, fmt.Errorf("stop timer: update entry: %w", err)
	}

	if err := s.Queries.ClearRunningTimerByUser(ctx, util.ParseUUID(userID)); err != nil {
		// Non-fatal: running_timer is a cache; log and continue.
		slog.Warn("stop timer: clear running timer failed", "user_id", userID, "error", err)
	}

	slog.Debug("timer stopped", "workspace_id", workspaceID, "user_id", userID, "entry_id", timeEntryID, "elapsed_s", elapsed)
	return stopped, nil
}

// GetCurrentTimer returns the running timer for the user, or nil if none.
func (s *TimeEntryService) GetCurrentTimer(ctx context.Context, workspaceID, userID string) (*db.TimeEntry, error) {
	entry, err := s.Queries.GetRunningTimerByUser(ctx, db.GetRunningTimerByUserParams{
		UserID:      util.ParseUUID(userID),
		WorkspaceID: util.ParseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get current timer: %w", err)
	}
	return &entry, nil
}

// ListTimeEntries returns paginated time entries for the given user, newest first.
func (s *TimeEntryService) ListTimeEntries(
	ctx context.Context,
	workspaceID, userID string,
	limit, offset int32,
) ([]db.TimeEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	entries, err := s.Queries.ListTimeEntriesByUser(ctx, db.ListTimeEntriesByUserParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		UserID:      util.ParseUUID(userID),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list time entries: %w", err)
	}
	return entries, nil
}

// ListIssueTimeEntries returns all time entries linked to the given issue.
func (s *TimeEntryService) ListIssueTimeEntries(ctx context.Context, workspaceID, issueID string) ([]db.TimeEntry, error) {
	entries, err := s.Queries.ListTimeEntriesByIssue(ctx, db.ListTimeEntriesByIssueParams{
		IssueID:     util.ParseUUID(issueID),
		WorkspaceID: util.ParseUUID(workspaceID),
	})
	if err != nil {
		return nil, fmt.Errorf("list issue time entries: %w", err)
	}
	return entries, nil
}

// ListTimeEntriesByRange returns all time entries for the user whose start_time
// falls within [since, until). Intended for calendar/day-grouped views that know
// the visible date window and should not rely on a fixed record limit.
func (s *TimeEntryService) ListTimeEntriesByRange(
	ctx context.Context,
	workspaceID, userID string,
	since, until time.Time,
) ([]db.TimeEntry, error) {
	entries, err := s.Queries.ListTimeEntriesByUserRange(ctx, db.ListTimeEntriesByUserRangeParams{
		WorkspaceID: util.ParseUUID(workspaceID),
		UserID:      util.ParseUUID(userID),
		StartTime:   pgtype.Timestamptz{Time: since, Valid: true},
		StartTime_2: pgtype.Timestamptz{Time: until, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list time entries by range: %w", err)
	}
	return entries, nil
}


// If startTime or stopTime are provided and the entry is stopped, duration_seconds is
// recalculated automatically.
//
// issueID is a double pointer to distinguish three states:
//   - nil outer pointer: field not provided — keep existing issue link
//   - non-nil outer, nil inner: explicit null — clear the issue link
//   - non-nil outer and inner: set to this UUID
func (s *TimeEntryService) UpdateTimeEntry(
	ctx context.Context,
	workspaceID, userID, timeEntryID string,
	description *string,
	issueID **string,
	startTime *time.Time,
	stopTime *time.Time,
) (db.TimeEntry, error) {
	// Verify ownership.
	entry, err := s.Queries.GetTimeEntryByID(ctx, db.GetTimeEntryByIDParams{
		ID:          util.ParseUUID(timeEntryID),
		WorkspaceID: util.ParseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.TimeEntry{}, ErrTimeEntryNotFound
		}
		return db.TimeEntry{}, fmt.Errorf("update time entry: get entry: %w", err)
	}
	if util.UUIDToString(entry.UserID) != userID {
		return db.TimeEntry{}, ErrTimeEntryNotFound
	}

	// Build optional timestamps for the query.
	var pgStart, pgStop pgtype.Timestamptz
	if startTime != nil {
		pgStart = pgtype.Timestamptz{Time: *startTime, Valid: true}
	}
	if stopTime != nil {
		pgStop = pgtype.Timestamptz{Time: *stopTime, Valid: true}
	}

	// Resolve the final issue_id to pass to SQL.
	//   issueID == nil            → field not provided → preserve the existing value
	//   *issueID == nil           → "issue_id": null → clear the link
	//   *issueID != nil           → "issue_id": "uuid" → set to this UUID
	var resolvedIssueID *string
	if issueID == nil {
		// Not provided — preserve existing.
		if entry.IssueID.Valid {
			uuidStr := util.UUIDToString(entry.IssueID)
			resolvedIssueID = &uuidStr
		}
		// else: already null, keep nil
	} else {
		// Explicit value: either clear (nil) or a new UUID.
		resolvedIssueID = *issueID
	}

	// Recalculate duration if start or stop is being changed on a completed entry.
	// For running entries (stop_time IS NULL) we leave duration_seconds as-is.
	var newDuration int64
	effectiveStart := entry.StartTime.Time
	effectiveStop := entry.StopTime.Time
	entryIsStopped := entry.StopTime.Valid

	if startTime != nil {
		effectiveStart = *startTime
	}
	if stopTime != nil {
		effectiveStop = *stopTime
	}

	if entryIsStopped && (startTime != nil || stopTime != nil) {
		secs := int64(effectiveStop.Sub(effectiveStart).Seconds())
		if secs < 0 {
			secs = 0
		}
		newDuration = secs
	} else {
		// Keep the existing value (COALESCE will ignore a zero pgtype value).
		newDuration = entry.DurationSeconds
	}

	updated, err := s.Queries.UpdateTimeEntry(ctx, db.UpdateTimeEntryParams{
		ID:              util.ParseUUID(timeEntryID),
		WorkspaceID:     util.ParseUUID(workspaceID),
		Description:     util.PtrToText(description),
		IssueID:         optionalUUID(resolvedIssueID),
		StartTime:       pgStart,
		StopTime:        pgStop,
		DurationSeconds: newDuration,
	})
	if err != nil {
		return db.TimeEntry{}, fmt.Errorf("update time entry: %w", err)
	}
	return updated, nil
}

// DeleteTimeEntry deletes the entry and clears the running_timer row if the
// entry was running.
func (s *TimeEntryService) DeleteTimeEntry(ctx context.Context, workspaceID, userID, timeEntryID string) error {
	entry, err := s.Queries.GetTimeEntryByID(ctx, db.GetTimeEntryByIDParams{
		ID:          util.ParseUUID(timeEntryID),
		WorkspaceID: util.ParseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTimeEntryNotFound
		}
		return fmt.Errorf("delete time entry: get entry: %w", err)
	}
	if util.UUIDToString(entry.UserID) != userID {
		return ErrTimeEntryNotFound
	}

	if err := s.Queries.DeleteTimeEntry(ctx, db.DeleteTimeEntryParams{
		ID:          util.ParseUUID(timeEntryID),
		WorkspaceID: util.ParseUUID(workspaceID),
	}); err != nil {
		return fmt.Errorf("delete time entry: %w", err)
	}

	// Clear the running_timer cache if this was the running entry.
	if !entry.StopTime.Valid {
		if err := s.Queries.ClearRunningTimerByUser(ctx, util.ParseUUID(userID)); err != nil {
			slog.Warn("delete time entry: clear running timer failed", "user_id", userID, "error", err)
		}
	}

	return nil
}

// optionalUUID converts a nullable *string into a pgtype.UUID.
// Passing nil or "" produces an invalid (NULL) UUID.
func optionalUUID(s *string) pgtype.UUID {
	if s == nil || *s == "" {
		return pgtype.UUID{}
	}
	return util.ParseUUID(*s)
}

// Sentinel errors.
var (
	ErrTimeEntryNotFound = errors.New("time entry not found")
	ErrTimerNotRunning   = errors.New("timer is not running")
)
