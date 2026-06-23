package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestDispatchAutopilotForPlanIsIdempotent locks in the
// occurrence-level idempotency contract (MUL-3551):
//
//   - A second DispatchAutopilotForPlan with the same (trigger_id,
//     planned_at) MUST return the SAME run row that the first call
//     created. No second autopilot_run, no second issue / task, no
//     second failure recorded.
//
// This is the dispatch-layer half of the two-defence design. The
// primary defence lives in sys_cron_executions
// (uq_sys_cron_execution). This one catches the stale-steal case
// where a runner crashes between "create run" and "write SUCCESS in
// sys_cron_executions": the next runner re-enters the dispatch and
// must reuse the in-flight run instead of duplicating it.
func TestDispatchAutopilotForPlanIsIdempotent(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              "Dispatch for plan idempotency",
		Description:        pgtype.Text{String: "Dispatch for plan test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
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
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})

	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: "*/5 * * * *", Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}

	// Use a fixed planned_at so the partial unique index has something
	// concrete to enforce against. Truncate to seconds — the column is
	// TIMESTAMPTZ and pgx round-trips sub-microsecond, but we want the
	// comparison to be byte-stable across the two calls.
	plannedAt := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Second)

	first, err := autopilotSvc.DispatchAutopilotForPlan(
		ctx, ap, trigger.ID, "schedule", nil, plannedAt,
	)
	if err != nil {
		t.Fatalf("first DispatchAutopilotForPlan: %v", err)
	}
	if first == nil {
		t.Fatalf("first call returned nil run")
	}
	if !first.PlannedAt.Valid {
		t.Fatalf("first run should have planned_at set")
	}
	if !first.PlannedAt.Time.Equal(plannedAt) {
		t.Fatalf("first run planned_at mismatch: got %s, want %s",
			first.PlannedAt.Time.Format(time.RFC3339Nano),
			plannedAt.Format(time.RFC3339Nano))
	}

	// Second call with the SAME (trigger, planned_at) must reuse the
	// first run, not create a new one.
	second, err := autopilotSvc.DispatchAutopilotForPlan(
		ctx, ap, trigger.ID, "schedule", nil, plannedAt,
	)
	if err != nil {
		t.Fatalf("second DispatchAutopilotForPlan: %v", err)
	}
	if second == nil {
		t.Fatalf("second call returned nil run")
	}
	if second.ID != first.ID {
		t.Fatalf("second call must reuse first run: first.ID=%s second.ID=%s",
			util.UUIDToString(first.ID), util.UUIDToString(second.ID))
	}

	// Belt-and-suspenders: the partial unique index plus the lookup
	// in DispatchAutopilotForPlan together guarantee exactly one row.
	var rowCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly 1 autopilot_run for the (trigger, planned_at) pair, got %d", rowCount)
	}

	// A different planned_at for the same trigger MUST be allowed —
	// it represents the next scheduled occurrence, not a duplicate.
	plannedAt2 := plannedAt.Add(5 * time.Minute)
	third, err := autopilotSvc.DispatchAutopilotForPlan(
		ctx, ap, trigger.ID, "schedule", nil, plannedAt2,
	)
	if err != nil {
		t.Fatalf("third DispatchAutopilotForPlan with new planned_at: %v", err)
	}
	if third.ID == first.ID {
		t.Fatalf("different planned_at must produce a different run, got reuse")
	}

	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM autopilot_run WHERE autopilot_id = $1`, ap.ID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count rows after 3rd call: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("expected 2 autopilot_run rows after distinct planned_ats, got %d", rowCount)
	}
}

// TestDispatchAutopilotForPlanRejectsZeroArgs locks in the
// fail-loud contract: a caller that forgets to set trigger_id or
// planned_at would silently disable the idempotency guard, and the
// only safe answer is an error.
func TestDispatchAutopilotForPlanRejectsZeroArgs(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	ap := db.Autopilot{
		ID:            parseUUID(testWorkspaceID), // placeholder; will not be loaded since validation fails first
		WorkspaceID:   parseUUID(testWorkspaceID),
		ExecutionMode: "run_only",
		AssigneeType:  "agent",
		AssigneeID:    parseUUID(testWorkspaceID), // arbitrary; we never get past the input guard
		Status:        "active",
	}

	t.Run("invalid trigger_id", func(t *testing.T) {
		_, err := autopilotSvc.DispatchAutopilotForPlan(
			ctx, ap, pgtype.UUID{}, "schedule", nil, time.Now().UTC(),
		)
		if err == nil {
			t.Fatalf("expected error for invalid trigger_id")
		}
	})

	t.Run("zero planned_at", func(t *testing.T) {
		_, err := autopilotSvc.DispatchAutopilotForPlan(
			ctx, ap, parseUUID(testWorkspaceID), "schedule", nil, time.Time{},
		)
		if err == nil {
			t.Fatalf("expected error for zero planned_at")
		}
	})
}
