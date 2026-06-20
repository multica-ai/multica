package recurringissue

// Integration tests for schedule-based recurrence (FIR-1639). They verify the
// behaviour completion-based rules cannot give:
//
//   1. A schedule rule fires when its clock (next_run_at) is due, regardless of
//      the source issue's status, stamps the {{date}} placeholder, leaves the
//      template source issue in place, and advances the clock.
//   2. stale_handling=auto_close moves the previous occurrence to stale_status
//      when the next one is created; leave_open does not.
//
// Tests skip cleanly when no test DB is reachable, mirroring the completion
// sweeper tests in sweeper_db_test.go (which owns TestMain + the fixture).

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedTitledIssue creates an upstream issue with an explicit title (so a
// {{date}} placeholder can be exercised) and returns it.
func seedTitledIssue(t *testing.T, ctx context.Context, title, status string, due time.Time) db.Issue {
	t.Helper()
	q := db.New(recTestPool)
	number, err := q.IncrementIssueCounter(ctx, recTestWorkspaceID)
	if err != nil {
		t.Fatalf("issue counter: %v", err)
	}
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: recTestWorkspaceID,
		Title:       title,
		Description: pgtype.Text{String: "Check the campaign", Valid: true},
		Status:      status,
		Priority:    "high",
		CreatorType: "member",
		CreatorID:   recTestUserID,
		Position:    0,
		DueDate:     pgtype.Date{Time: due, Valid: true},
		Number:      number,
		ProjectID:   recTestProjectID,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return issue
}

func seedScheduleRecurrence(t *testing.T, ctx context.Context, sourceIssue pgtype.UUID, nextRun time.Time, staleHandling, staleStatus, dateFormat string) cerebrodb.CerebroIssueRecurrence {
	t.Helper()
	q := cerebrodb.New(recTestPool)
	rec, err := q.CreateCerebroIssueRecurrence(ctx, cerebrodb.CreateCerebroIssueRecurrenceParams{
		WorkspaceID:    recTestWorkspaceID,
		ProjectID:      recTestProjectID,
		SourceIssueID:  sourceIssue,
		Frequency:      FreqWeekly,
		IntervalCount:  1,
		Weekdays:       []int16{},
		DaysAfter:      0,
		TriggerStatus:  "done",
		Anchor:         AnchorCompletion,
		CreateNewIssue: true,
		NewStatus:      "todo",
		RecurForever:   true,
		Armed:          true,
		Enabled:        true,
		TriggerType:    TriggerSchedule,
		StaleHandling:  staleHandling,
		StaleStatus:    staleStatus,
		DateFormat:     dateFormat,
		NextRunAt:      pgtype.Timestamptz{Time: nextRun, Valid: true},
		CreatedByType:  pgtype.Text{String: "member", Valid: true},
		CreatedByID:    recTestUserID,
	})
	if err != nil {
		t.Fatalf("create schedule recurrence: %v", err)
	}
	return rec
}

func issueStatusByID(t *testing.T, ctx context.Context, id pgtype.UUID) string {
	t.Helper()
	var status string
	if err := recTestPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("status by id: %v", err)
	}
	return status
}

func spawnedIssueByOccurrence(t *testing.T, ctx context.Context, recurrenceID pgtype.UUID, occurrence int32) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := recTestPool.QueryRow(ctx,
		`SELECT spawned_issue_id FROM cerebro_issue_recurrence_log WHERE recurrence_id = $1 AND occurrence_number = $2`,
		recurrenceID, occurrence).Scan(&id); err != nil {
		t.Fatalf("spawned by occurrence %d: %v", occurrence, err)
	}
	return id
}

// TestSweeper_Schedule_FiresRegardlessOfStatus: a schedule rule whose clock is
// due spawns a dated occurrence even though the source issue is still open, and
// it leaves the template source in place while advancing the clock by one week.
func TestSweeper_Schedule_FiresRegardlessOfStatus(t *testing.T) {
	if recTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetRecState(t, ctx)
	enableRecFlag(t, ctx)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	nextRun := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	// Source issue is deliberately still in_progress — completion would never
	// fire here.
	issue := seedTitledIssue(t, ctx, "Campaign {{date}}", "in_progress", nextRun)
	rec := seedScheduleRecurrence(t, ctx, issue.ID, nextRun, StaleLeaveOpen, "cancelled", DateFormatISO)

	sw := newRecSweeper(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if got := countIssues(t, ctx); got != 2 {
		t.Fatalf("schedule must spawn regardless of status; issue count = %d, want 2", got)
	}

	// Spawned occurrence: dated title, due = the occurrence date.
	spawnedID := spawnedIssueByOccurrence(t, ctx, rec.ID, 1)
	var title string
	var dueDate pgtype.Date
	if err := recTestPool.QueryRow(ctx, `SELECT title, due_date FROM issue WHERE id = $1`, spawnedID).Scan(&title, &dueDate); err != nil {
		t.Fatalf("load spawned: %v", err)
	}
	if title != "Campaign 2026-06-17" {
		t.Errorf("spawned title = %q, want {{date}} substituted to 2026-06-17", title)
	}
	if got := dueDate.Time.Format("2006-01-02"); got != "2026-06-17" {
		t.Errorf("spawned due_date = %s, want 2026-06-17", got)
	}

	// Source issue must stay the template (untouched, not repointed) and its
	// status must be unchanged.
	if s := issueStatusByID(t, ctx, issue.ID); s != "in_progress" {
		t.Errorf("source issue status = %q, want unchanged in_progress", s)
	}
	q := cerebrodb.New(recTestPool)
	updated, err := q.GetCerebroIssueRecurrence(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get recurrence: %v", err)
	}
	if !uuidEqual(updated.SourceIssueID, issue.ID) {
		t.Errorf("schedule rule must NOT repoint source off the template")
	}
	if updated.OccurrenceCount != 1 {
		t.Errorf("occurrence_count = %d, want 1", updated.OccurrenceCount)
	}
	if !updated.NextRunAt.Valid || updated.NextRunAt.Time.Format("2006-01-02") != "2026-06-24" {
		t.Errorf("next_run_at = %v, want advanced to 2026-06-24", updated.NextRunAt.Time)
	}

	// Same instant again: clock is now in the future → no double-spawn.
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := countIssues(t, ctx); got != 2 {
		t.Fatalf("clock not due → must not double-spawn; issue count = %d, want 2", got)
	}
}

// TestSweeper_Schedule_AutoClosesPrevious: with stale_handling=auto_close, the
// second fire moves the first occurrence to stale_status.
func TestSweeper_Schedule_AutoClosesPrevious(t *testing.T) {
	if recTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetRecState(t, ctx)
	enableRecFlag(t, ctx)

	nextRun := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	issue := seedTitledIssue(t, ctx, "Campaign {{date}}", "in_progress", nextRun)
	rec := seedScheduleRecurrence(t, ctx, issue.ID, nextRun, StaleAutoClose, "cancelled", DateFormatISO)

	// First fire creates occurrence #1.
	sw1 := newRecSweeper(t, time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC))
	if err := sw1.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	first := spawnedIssueByOccurrence(t, ctx, rec.ID, 1)
	if s := issueStatusByID(t, ctx, first); s != "todo" {
		t.Fatalf("occurrence #1 status = %q, want todo before next fire", s)
	}

	// Second fire (clock advanced to 2026-06-24) creates occurrence #2 and must
	// auto-close occurrence #1.
	sw2 := newRecSweeper(t, time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC))
	if err := sw2.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := countIssues(t, ctx); got != 3 {
		t.Fatalf("expected template + 2 occurrences = 3 issues, got %d", got)
	}
	if s := issueStatusByID(t, ctx, first); s != "cancelled" {
		t.Errorf("occurrence #1 status = %q, want auto-closed to cancelled", s)
	}
	second := spawnedIssueByOccurrence(t, ctx, rec.ID, 2)
	if s := issueStatusByID(t, ctx, second); s != "todo" {
		t.Errorf("occurrence #2 status = %q, want todo", s)
	}
}

// TestSweeper_Schedule_NotDue: a schedule rule whose clock is in the future
// does not fire.
func TestSweeper_Schedule_NotDue(t *testing.T) {
	if recTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetRecState(t, ctx)
	enableRecFlag(t, ctx)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	future := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	issue := seedTitledIssue(t, ctx, "Campaign {{date}}", "in_progress", future)
	seedScheduleRecurrence(t, ctx, issue.ID, future, StaleLeaveOpen, "cancelled", DateFormatISO)

	sw := newRecSweeper(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := countIssues(t, ctx); got != 1 {
		t.Fatalf("clock in future → must not spawn; issue count = %d, want 1", got)
	}
}
