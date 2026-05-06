package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// schedulerTestFixture wires the bare minimum for the scheduler tests:
// queries, a service stack capable of dispatching run_only autopilots,
// and a fixture agent. It uses the package-level testPool / testWorkspaceID
// / testUserID set up in TestMain (see integration_test.go).
type schedulerTestFixture struct {
	queries *db.Queries
	svc     *service.AutopilotService
	agentID string
}

func newSchedulerTestFixture(t *testing.T) *schedulerTestFixture {
	t.Helper()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	svc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	return &schedulerTestFixture{queries: queries, svc: svc, agentID: agentID}
}

// createScheduledAutopilot inserts an active run_only autopilot plus a
// schedule trigger with the supplied next_run_at. Cleanup of the autopilot
// row cascades to the trigger; the autopilot_run rows do NOT cascade
// (issue_id/task_id are SET NULL), so we sweep them in a t.Cleanup.
func (f *schedulerTestFixture) createScheduledAutopilot(
	t *testing.T,
	title string,
	cron string,
	nextRunAt pgtype.Timestamptz,
) (db.Autopilot, db.AutopilotTrigger) {
	t.Helper()
	ctx := context.Background()

	ap, err := f.queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              title,
		Description:        pgtype.Text{String: "scheduler regression fixture", Valid: true},
		AssigneeID:         parseUUID(f.agentID),
		Status:             "active",
		ExecutionMode:      "run_only",
		IssueTitleTemplate: pgtype.Text{},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		// Autopilot run rows reference the autopilot via FK with ON DELETE
		// CASCADE; the agent_task_queue rows reference autopilot_run via
		// ON DELETE SET NULL. Drop runs and tasks explicitly so the test DB
		// stays tidy (and so other tests counting agent_task_queue don't
		// see leftovers).
		bg := context.Background()
		_, _ = testPool.Exec(bg, `DELETE FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)`, ap.ID)
		_, _ = testPool.Exec(bg, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, ap.ID)
		_, _ = testPool.Exec(bg, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	trig, err := f.queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: cron, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
		NextRunAt:      nextRunAt,
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}
	return ap, trig
}

func (f *schedulerTestFixture) countRuns(t *testing.T, autopilotID pgtype.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`,
		autopilotID,
	).Scan(&n); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	return n
}

func (f *schedulerTestFixture) getTrigger(t *testing.T, id pgtype.UUID) db.AutopilotTrigger {
	t.Helper()
	got, err := f.queries.GetAutopilotTrigger(context.Background(), id)
	if err != nil {
		t.Fatalf("GetAutopilotTrigger: %v", err)
	}
	return got
}

// TestAutopilotSchedulerNoRunsWithoutTick asserts the trivial-but-load-bearing
// invariant: while the scheduler is not ticking (server down), no dispatch
// happens regardless of how overdue the trigger is. This documents the
// "scheduler is the only thing that fires schedule triggers" contract.
func TestAutopilotSchedulerNoRunsWithoutTick(t *testing.T) {
	if testPool == nil {
		t.Skip("integration DB not available")
	}
	f := newSchedulerTestFixture(t)

	overdue := pgtype.Timestamptz{Time: time.Now().Add(-6 * time.Hour), Valid: true}
	ap, _ := f.createScheduledAutopilot(t, "autopilot_catchup_no_tick", "*/5 * * * *", overdue)

	// Deliberately do NOT call tickScheduledAutopilots. Wait briefly to give
	// any rogue background dispatch a chance to fire (there shouldn't be one).
	time.Sleep(50 * time.Millisecond)

	if got := f.countRuns(t, ap.ID); got != 0 {
		t.Fatalf("expected 0 runs while scheduler is not ticking, got %d", got)
	}
}

// TestAutopilotSchedulerOverdueFiresOnce asserts the catch-up semantics:
// even if a 5-minute trigger is 6 hours overdue, a single tick produces
// exactly ONE dispatch (not 72 catch-up runs), and next_run_at advances
// to a future time.
func TestAutopilotSchedulerOverdueFiresOnce(t *testing.T) {
	if testPool == nil {
		t.Skip("integration DB not available")
	}
	f := newSchedulerTestFixture(t)
	ctx := context.Background()

	overdue := pgtype.Timestamptz{Time: time.Now().Add(-6 * time.Hour), Valid: true}
	ap, trig := f.createScheduledAutopilot(t, "autopilot_catchup_overdue", "*/5 * * * *", overdue)

	tickStart := time.Now()
	tickScheduledAutopilots(ctx, f.queries, f.svc)

	if got := f.countRuns(t, ap.ID); got != 1 {
		t.Fatalf("expected exactly 1 run after a single tick on a 6h-overdue trigger, got %d", got)
	}

	updated := f.getTrigger(t, trig.ID)
	if !updated.NextRunAt.Valid {
		t.Fatalf("expected next_run_at to be re-armed (non-NULL) after dispatch, got NULL")
	}
	if !updated.NextRunAt.Time.After(tickStart) {
		t.Fatalf("expected next_run_at to be in the future, got %v (tick started %v)",
			updated.NextRunAt.Time, tickStart)
	}

	// Second tick immediately afterward must NOT produce another run: the
	// trigger's next_run_at is now in the future, so the claim predicate
	// won't match.
	tickScheduledAutopilots(ctx, f.queries, f.svc)
	if got := f.countRuns(t, ap.ID); got != 1 {
		t.Fatalf("expected 1 run after a second immediate tick (next_run_at is in the future), got %d", got)
	}
}

// TestAutopilotSchedulerRecoverLostTriggers asserts that recoverLostTriggers
// re-arms triggers whose next_run_at is NULL (claimed but not advanced --
// typically a crash mid-tick) without firing any runs.
func TestAutopilotSchedulerRecoverLostTriggers(t *testing.T) {
	if testPool == nil {
		t.Skip("integration DB not available")
	}
	f := newSchedulerTestFixture(t)
	ctx := context.Background()

	// next_run_at = NULL simulates a crash mid-tick (ClaimDueScheduleTriggers
	// already nulled it but the scheduler died before AdvanceTriggerNextRun).
	ap, trig := f.createScheduledAutopilot(t, "autopilot_catchup_lost", "*/5 * * * *",
		pgtype.Timestamptz{})

	recoveryStart := time.Now()
	recoverLostTriggers(ctx, f.queries)

	updated := f.getTrigger(t, trig.ID)
	if !updated.NextRunAt.Valid {
		t.Fatalf("expected recoverLostTriggers to set next_run_at non-NULL, still NULL")
	}
	if !updated.NextRunAt.Time.After(recoveryStart) {
		t.Fatalf("expected recovered next_run_at to be in the future, got %v (recovery started %v)",
			updated.NextRunAt.Time, recoveryStart)
	}

	// Recovery is a re-arm only — it must NOT dispatch any runs.
	if got := f.countRuns(t, ap.ID); got != 0 {
		t.Fatalf("expected 0 runs after recovery (recovery does not fire), got %d", got)
	}
}
