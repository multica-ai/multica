package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueScheduleFixture seeds the shared (workspace, agent, issue) trio via
// seedAttributionFixture and returns an IssueScheduleService wired against
// the same pool, plus the ids tests need.
func issueScheduleFixture(t *testing.T) (svc *IssueScheduleService, pool *pgxpool.Pool, workspaceID, userID, agentID, issueID string) {
	t.Helper()
	pool = newResolveOriginatorPool(t)
	workspaceID, userID, agentID, issueID = seedAttributionFixture(t, pool)
	taskSvc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	svc = &IssueScheduleService{Queries: db.New(pool), TxStarter: pool, TaskService: taskSvc}
	return svc, pool, workspaceID, userID, agentID, issueID
}

// createPendingSchedule inserts a pending issue_scheduled_trigger row through
// CreateSchedule (exercising the same write path production traffic uses)
// and registers cleanup.
func createPendingSchedule(t *testing.T, svc *IssueScheduleService, pool *pgxpool.Pool, issue db.Issue, createdByUserID string) db.IssueScheduledTrigger {
	t.Helper()
	trigger, err := svc.CreateSchedule(context.Background(), issue, time.Now().Add(time.Hour), util.MustParseUUID(createdByUserID))
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM issue_scheduled_trigger WHERE id = $1`, trigger.ID)
	})
	return trigger
}

func queuedTaskCount(t *testing.T, pool *pgxpool.Pool, issueID, agentID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`,
		issueID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return count
}

func scheduleStatus(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM issue_scheduled_trigger WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read schedule status: %v", err)
	}
	return status
}

// The success path: a pending schedule on an agent-assigned issue enqueues a
// run and transitions to 'fired'.
func TestDispatchIssueScheduleFiresForAgentAssignedIssue(t *testing.T) {
	svc, pool, _, userID, agentID, issueID := issueScheduleFixture(t)
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	trigger := createPendingSchedule(t, svc, pool, issue, userID)

	if err := svc.DispatchIssueSchedule(context.Background(), trigger); err != nil {
		t.Fatalf("DispatchIssueSchedule: %v", err)
	}

	if got := scheduleStatus(t, pool, trigger.ID); got != "fired" {
		t.Fatalf("schedule status = %q, want fired", got)
	}
	if got := queuedTaskCount(t, pool, issueID, agentID); got != 1 {
		t.Fatalf("queued task count = %d, want 1", got)
	}
}

// The miss path: the issue's assignee was cleared after the schedule was
// created (the reload-fresh behavior DispatchIssueSchedule's doc comment
// promises). Firing must mark the trigger 'missed' and notify its creator —
// not enqueue against a stale assignee, not error out silently.
func TestDispatchIssueScheduleMissedWhenAssigneeRemoved(t *testing.T) {
	svc, pool, workspaceID, userID, agentID, issueID := issueScheduleFixture(t)
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	trigger := createPendingSchedule(t, svc, pool, issue, userID)

	if _, err := pool.Exec(context.Background(),
		`UPDATE issue SET assignee_type = NULL, assignee_id = NULL WHERE id = $1`, issueID); err != nil {
		t.Fatalf("clear assignee: %v", err)
	}

	if err := svc.DispatchIssueSchedule(context.Background(), trigger); err != nil {
		t.Fatalf("DispatchIssueSchedule: %v", err)
	}

	if got := scheduleStatus(t, pool, trigger.ID); got != "missed" {
		t.Fatalf("schedule status = %q, want missed", got)
	}
	if got := queuedTaskCount(t, pool, issueID, agentID); got != 0 {
		t.Fatalf("queued task count = %d, want 0 (no assignee to dispatch to)", got)
	}

	var inboxCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_item
		WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
		  AND type = $3 AND severity = 'action_required' AND issue_id = $4
	`, workspaceID, userID, issueScheduleMissedInboxType, issueID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox items: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("expected exactly 1 action-required inbox item for the creator, got %d", inboxCount)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND issue_id = $2`, workspaceID, issueID)
	})
}

// A trigger that is no longer 'pending' (already fired by a previous
// attempt, or cancelled) must be a safe no-op — this is what makes the
// scheduler's one crash-recovery retry (MaxAttempts=2 in
// jobs_issue_schedule.go) idempotent instead of a double-fire.
func TestDispatchIssueScheduleNoopWhenAlreadyResolved(t *testing.T) {
	svc, pool, _, userID, agentID, issueID := issueScheduleFixture(t)
	issue, err := svc.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	trigger := createPendingSchedule(t, svc, pool, issue, userID)

	if err := svc.DispatchIssueSchedule(context.Background(), trigger); err != nil {
		t.Fatalf("first DispatchIssueSchedule: %v", err)
	}
	if got := queuedTaskCount(t, pool, issueID, agentID); got != 1 {
		t.Fatalf("queued task count after first dispatch = %d, want 1", got)
	}

	// Re-invoke with the SAME (now-stale) trigger value a caller might still
	// be holding after a retry — DispatchIssueSchedule must re-check status
	// itself rather than trust the caller's copy.
	staleTrigger := trigger
	staleTrigger.Status = "pending"
	if err := svc.DispatchIssueSchedule(context.Background(), staleTrigger); err != nil {
		t.Fatalf("second DispatchIssueSchedule: %v", err)
	}

	if got := queuedTaskCount(t, pool, issueID, agentID); got != 1 {
		t.Fatalf("queued task count after retried dispatch = %d, want still 1 (no double-fire)", got)
	}
}
