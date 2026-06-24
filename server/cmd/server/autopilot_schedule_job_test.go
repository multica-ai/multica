package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/scheduler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setupAutopilotScheduleJob creates the test fixture for the
// autopilot_schedule_dispatch JobSpec: an active autopilot, a
// schedule trigger with the given cron, and a *scheduler.Manager with
// the JobSpec registered. Cleanup is registered on t.
//
// Returns the trigger and the manager so the test can call mgr.runOnce
// directly (no goroutine — we want deterministic ticks).
func setupAutopilotScheduleJob(t *testing.T, cron string) (db.AutopilotTrigger, *scheduler.Manager, *service.AutopilotService) {
	t.Helper()
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
		Title:              "Schedule dispatch fixture",
		Description:        pgtype.Text{String: "schedule dispatch test", Valid: true},
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

	trigger, err := queries.CreateAutopilotTrigger(ctx, db.CreateAutopilotTriggerParams{
		AutopilotID:    ap.ID,
		Kind:           "schedule",
		Enabled:        true,
		CronExpression: pgtype.Text{String: cron, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}

	// Anchor trigger.created_at to one minute ago so even an
	// every-minute cron is guaranteed to have at least one occurrence
	// in (created_at, dbNow] on the first tick. Without this the test
	// occasionally races the cron evaluator's minute boundary and the
	// first tick produces zero plans.
	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET created_at = now() - INTERVAL '2 minute' WHERE id = $1`,
		trigger.ID,
	); err != nil {
		t.Fatalf("backdate trigger.created_at: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = testPool.Exec(bg,
			`DELETE FROM sys_cron_executions WHERE scope_kind = $1 AND scope_id = $2`,
			scheduler.ScopeKindAutopilotTrigger, util.UUIDToString(trigger.ID),
		)
		_, _ = testPool.Exec(bg, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	mgr := scheduler.NewManager(testPool, scheduler.Options{
		RunnerID: "autopilot-job-test",
	})
	if err := mgr.Register(scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)); err != nil {
		t.Fatalf("register autopilot_schedule_dispatch job: %v", err)
	}

	return trigger, mgr, autopilotSvc
}

// TestAutopilotScheduleJobDispatchesOnce verifies the end-to-end
// happy path: one tick of the JobSpec produces exactly one
// sys_cron_executions row (SUCCESS) and exactly one autopilot_run
// row tagged with the canonical UTC planned_at. This is the
// occurrence-level idempotency contract from MUL-3551 §1.
func TestAutopilotScheduleJobDispatchesOnce(t *testing.T) {
	ctx := context.Background()

	// `*/1 * * * *` fires every minute. Trigger.created_at is
	// backdated 2 minutes in the fixture so there is always at least
	// one due occurrence.
	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	// Exactly one sys_cron_executions row for this scope, status SUCCESS.
	var execRows int
	var status string
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MAX(status), '')
		  FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows, &status); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows != 1 || status != "SUCCESS" {
		t.Fatalf("expected 1 SUCCESS exec row, got %d rows with status %q", execRows, status)
	}

	// Exactly one autopilot_run with planned_at set.
	var runRows int
	var plannedAtValid bool
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*), bool_or(planned_at IS NOT NULL)
		  FROM autopilot_run
		 WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows, &plannedAtValid); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 1 || !plannedAtValid {
		t.Fatalf("expected 1 autopilot_run with planned_at set, got %d rows planned_at_valid=%v", runRows, plannedAtValid)
	}

	// A second tick must NOT create another row at the same plan_time
	// (idempotency via uq_sys_cron_execution). Because the trigger
	// fires every minute, the second tick may produce a NEW row at
	// the next minute boundary if a minute passed between the two
	// runOnce calls — that is fine, the test just asserts that the
	// existing row is not duplicated and the COUNT doesn't decrease.
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	var execRowsAfter int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRowsAfter); err != nil {
		t.Fatalf("count exec rows after 2nd tick: %v", err)
	}
	if execRowsAfter < execRows {
		t.Fatalf("second tick should never delete rows; before=%d after=%d", execRows, execRowsAfter)
	}
}

// TestAutopilotScheduleJobMissedSchedulesCollapse covers MUL-3551 §4:
// when many occurrences are due (e.g. server was offline for a long
// stretch), the CatchUpLatestOnly hook should fire ONCE per tick — not
// replay every missed occurrence.
func TestAutopilotScheduleJobMissedSchedulesCollapse(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/5 * * * *")

	// Force a large historical window: the trigger thinks it was
	// registered an hour ago, so without collapse the hook would
	// emit 12 occurrences.
	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET created_at = now() - INTERVAL '1 hour' WHERE id = $1`,
		trigger.ID,
	); err != nil {
		t.Fatalf("backdate trigger.created_at: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var rows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&rows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("CatchUpLatestOnly must collapse missed fires to 1 row per tick, got %d", rows)
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 1 {
		t.Fatalf("missed schedules must collapse to a single autopilot_run, got %d", runRows)
	}
}

// TestAutopilotScheduleJobCrashRecovery covers MUL-3551 §5: a runner
// that crashes after claiming a plan_time and creating its
// downstream issue/task — but before writing terminal SUCCESS — must
// be recovered on the next tick. The stale lease is swept to FAILED
// (error_code='stale_timeout'), the planner returns the SAME
// plan_time (because latest.RetryEligible(now) is true), tryClaim's
// FAILED-with-retry branch fires (incrementing attempt), the
// dispatch path is re-entered, DispatchAutopilotForPlan sees the
// already-complete run from the first attempt and reuses it, and
// the row finally transitions to SUCCESS — all without duplicating
// the autopilot_run.
func TestAutopilotScheduleJobCrashRecovery(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	// Tick 1: dispatch creates run + task; sys_cron_executions row
	// reaches SUCCESS.
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	var execID, leaseToken string
	var planTime time.Time
	var attempt int
	if err := testPool.QueryRow(ctx, `
		SELECT id, lease_token, plan_time, attempt
		  FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execID, &leaseToken, &planTime, &attempt); err != nil {
		t.Fatalf("read first exec row: %v", err)
	}
	if attempt != 1 {
		t.Fatalf("first attempt must be attempt=1, got %d", attempt)
	}

	// The first-tick run carries planned_at + a real task linkage —
	// that's the "complete" snapshot the retry must reuse, not
	// duplicate.
	var firstRunID pgtype.UUID
	var firstRunTaskValid bool
	if err := testPool.QueryRow(ctx, `
		SELECT id, task_id IS NOT NULL FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&firstRunID, &firstRunTaskValid); err != nil {
		t.Fatalf("read first run: %v", err)
	}
	if !firstRunTaskValid {
		t.Fatalf("first attempt must have created a real downstream task; task_id is NULL")
	}

	// Simulate a crash AFTER the first attempt's terminal write
	// would have been ignored: rewrite the exec row to RUNNING with
	// an expired stale_after AND a different (ghost) lease_token, so
	// it looks exactly like the post-crash state where the runner
	// died before its terminal UPDATE landed. The autopilot_run row
	// from tick 1 stays as the "complete" snapshot — that is what
	// DispatchAutopilotForPlan must reuse on the retry.
	if _, err := testPool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status      = 'RUNNING',
		       runner_id   = 'ghost-runner',
		       lease_token = gen_random_uuid(),
		       stale_after = now() - INTERVAL '10 minutes',
		       finished_at = NULL,
		       duration_ms = NULL,
		       updated_at  = now()
		 WHERE id = $1
	`, execID); err != nil {
		t.Fatalf("simulate crash mid-dispatch: %v", err)
	}

	// Tick 2: stale sweep + retry + dispatch + final SUCCESS.
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick 2 (recovery): %v", err)
	}

	// The exec row must now be SUCCESS at attempt=2, still at the
	// SAME plan_time (proving the retry path fired and the planner
	// did not silently advance past the FAILED bucket — the canonical
	// bug from the #4444 review).
	var recoveredStatus string
	var recoveredAttempt int
	var recoveredPlan time.Time
	if err := testPool.QueryRow(ctx, `
		SELECT status, attempt, plan_time
		  FROM sys_cron_executions
		 WHERE id = $1
	`, execID).Scan(&recoveredStatus, &recoveredAttempt, &recoveredPlan); err != nil {
		t.Fatalf("read recovered exec row: %v", err)
	}
	if !recoveredPlan.Equal(planTime) {
		t.Fatalf("retry must stay on the same plan_time: tick1=%s tick2=%s",
			planTime.Format(time.RFC3339), recoveredPlan.Format(time.RFC3339))
	}
	if recoveredAttempt != 2 {
		t.Fatalf("retry must increment attempt (the FAILED-retry branch fired); got attempt=%d", recoveredAttempt)
	}
	if recoveredStatus != "SUCCESS" {
		t.Fatalf("retry must reach terminal SUCCESS on the same row; got status=%q", recoveredStatus)
	}

	// Exactly one autopilot_run, and it is the SAME one tick 1
	// created — DispatchAutopilotForPlan's idempotency + the
	// complete-run reuse path in isAutopilotRunComplete prevent a
	// duplicate.
	rows, err := testPool.Query(ctx, `SELECT id FROM autopilot_run WHERE trigger_id = $1`, trigger.ID)
	if err != nil {
		t.Fatalf("list run rows: %v", err)
	}
	defer rows.Close()
	var seenIDs []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seenIDs = append(seenIDs, id)
	}
	if len(seenIDs) != 1 {
		t.Fatalf("crash recovery must keep autopilot_run at exactly one row, got %d", len(seenIDs))
	}
	if seenIDs[0] != firstRunID {
		t.Fatalf("retry must reuse tick-1's run row; got new id %s",
			util.UUIDToString(seenIDs[0]))
	}
}

// TestAutopilotScheduleJobTwoRunnersSingleWinner covers the
// multi-replica claim race from MUL-3551 §1. Two scheduler.Manager
// instances tick concurrently against the same trigger; exactly one
// should win the claim, the other no-ops via the sys_cron_executions
// uniqueness key.
func TestAutopilotScheduleJobTwoRunnersSingleWinner(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	trigger, _, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	mgrA := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "runner-A"})
	mgrB := scheduler.NewManager(testPool, scheduler.Options{RunnerID: "runner-B"})
	if err := mgrA.Register(scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if err := mgrB.Register(scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)); err != nil {
		t.Fatalf("register B: %v", err)
	}

	type result struct {
		err error
	}
	results := make(chan result, 2)
	go func() { results <- result{err: mgrA.RunOnce(ctx)} }()
	go func() { results <- result{err: mgrB.RunOnce(ctx)} }()

	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("runOnce: %v", r.err)
		}
	}

	// At most one sys_cron_executions row for this plan_time, and
	// exactly one autopilot_run.
	var execRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows < 1 || execRows > 2 {
		// At most one per plan_time, but two ticks racing across a
		// minute boundary could produce up to 2 distinct plan_times.
		// Anything outside [1,2] means the uniqueness guarantee broke.
		t.Fatalf("expected 1 or 2 exec rows (one per plan_time), got %d", execRows)
	}
	// Per plan_time: exactly one runner_id should ever be recorded.
	rowsByPlan, err := testPool.Query(ctx, `
		SELECT plan_time, runner_id FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
		 ORDER BY plan_time
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID))
	if err != nil {
		t.Fatalf("query per-plan rows: %v", err)
	}
	defer rowsByPlan.Close()
	seen := map[string]string{}
	for rowsByPlan.Next() {
		var plan time.Time
		var runner string
		if err := rowsByPlan.Scan(&plan, &runner); err != nil {
			t.Fatalf("scan: %v", err)
		}
		key := plan.Format(time.RFC3339Nano)
		if prev, ok := seen[key]; ok && prev != runner {
			t.Fatalf("plan_time %s claimed by both %s and %s — uniqueness broke", key, prev, runner)
		}
		seen[key] = runner
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != execRows {
		t.Fatalf("expected 1 autopilot_run per exec row, got exec=%d run=%d", execRows, runRows)
	}
}

// TestAutopilotScheduleJobDisabledTriggerSkips locks in that a
// trigger toggled off between scope-list and handler run is treated
// as a SUCCESS no-op — no autopilot_run created. This protects
// against the race the legacy goroutine could not prove safe (it
// reloaded the autopilot, but never re-checked the trigger's
// enabled flag in-handler).
func TestAutopilotScheduleJobDisabledTriggerSkips(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	// Disable the trigger AFTER the manager is wired but before tick.
	// scope provider's SQL will not include it, so no plan_time is
	// produced. (The handler-side belt-and-suspenders guard is
	// covered by the unit-level autopilot_inactive case below.)
	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET enabled = FALSE WHERE id = $1`, trigger.ID,
	); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var execRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows != 0 {
		t.Fatalf("disabled trigger must not produce sys_cron_executions rows, got %d", execRows)
	}
}

// TestAutopilotScheduleJobPausedAutopilotSkipsAtHandler covers the
// in-handler race window: scope-list says "active" at tick start,
// but the autopilot is paused before the per-scope handler runs
// (e.g. by an HTTP PUT that landed between scope-list and dispatch).
// The handler MUST re-read autopilot.status and treat the run as a
// SUCCESS no-op (skipped_reason: "autopilot_inactive") instead of
// firing a real dispatch.
//
// Driving this via `mgr.RunOnce` would not exercise the guard —
// scope-list's SQL filter already excludes paused autopilots so the
// handler is never reached. Instead we invoke `job.Handler` directly
// with a stub HandlerInput, which is exactly how the manager calls
// it after a successful claim. JobSpec.Handler is a public field on
// the JobSpec, so no test-only export is needed.
func TestAutopilotScheduleJobPausedAutopilotSkipsAtHandler(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	trigger, _, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	// Pause the parent autopilot AFTER setup — simulates the
	// "paused after scope-list" race the in-handler guard exists for.
	if _, err := queries.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
		ID:     trigger.AutopilotID,
		Status: pgtype.Text{String: "paused", Valid: true},
	}); err != nil {
		t.Fatalf("pause autopilot: %v", err)
	}

	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)

	// Drive the handler directly. plan_time is arbitrary — what we
	// are asserting is that the handler re-reads autopilot.status
	// and returns a SUCCESS no-op WITHOUT calling DispatchAutopilotForPlan.
	planTime := time.Now().UTC().Truncate(time.Minute)
	result, err := job.Handler(ctx, scheduler.HandlerInput{
		Job:      &job,
		Scope:    scheduler.Scope{Kind: scheduler.ScopeKindAutopilotTrigger, ID: util.UUIDToString(trigger.ID)},
		PlanTime: planTime,
		Attempt:  1,
		RunnerID: "handler-guard-test",
		Heartbeat: func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if result.RowsAffected != 0 {
		t.Fatalf("paused autopilot must produce a no-op (rows_affected=0), got %d", result.RowsAffected)
	}
	if got, _ := result.Result["skipped_reason"].(string); got != "autopilot_inactive" {
		t.Fatalf("expected skipped_reason=autopilot_inactive, got %q", got)
	}

	// And — critically — no autopilot_run should have been created
	// by this handler invocation. (Dispatch would have produced one.)
	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 0 {
		t.Fatalf("paused-autopilot handler guard must not create a run, got %d", runRows)
	}
}

// TestAutopilotScheduleJobBadCronStaysSilent locks in the failure
// surface for malformed cron expressions: the trigger create/update
// handlers reject bad cron at HTTP time (so this is a defence in
// depth), but if a corrupt expression ever lands in
// autopilot_trigger.cron_expression, the planner hook must fail
// without dispatching anything.
//
// The hook returns its parse error to manager.runJob, which logs a
// warning and SKIPS this scope for the tick. No row is inserted
// into sys_cron_executions because nothing was ever claimed —
// `tryClaim` only writes when a plan_time is offered. This is the
// right shape: a parse error is a permanent configuration problem,
// not a transient lease failure, so the retry-with-FAILED-row
// machinery does not apply. The audit signal is the manager
// warning log instead.
//
// The contract this test pins is therefore: NO dispatch, NO exec
// row, and NO autopilot_run created. The bad cron MUST NOT silently
// look like a SUCCESS.
func TestAutopilotScheduleJobBadCronStaysSilent(t *testing.T) {
	ctx := context.Background()

	trigger, mgr, _ := setupAutopilotScheduleJob(t, "*/1 * * * *")

	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot_trigger SET cron_expression = $2 WHERE id = $1`,
		trigger.ID, "garbage not a cron",
	); err != nil {
		t.Fatalf("set bad cron: %v", err)
	}

	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	var execRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions
		 WHERE job_name = $1 AND scope_kind = $2 AND scope_id = $3
	`, scheduler.JobNameAutopilotScheduleDispatch, scheduler.ScopeKindAutopilotTrigger,
		util.UUIDToString(trigger.ID)).Scan(&execRows); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if execRows != 0 {
		t.Fatalf("bad cron must not produce sys_cron_executions rows (no claim happens), got %d", execRows)
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_run WHERE trigger_id = $1
	`, trigger.ID).Scan(&runRows); err != nil {
		t.Fatalf("count run rows: %v", err)
	}
	if runRows != 0 {
		t.Fatalf("bad cron must not fire dispatch, got %d run rows", runRows)
	}
}

// seedColdStartTrigger creates an autopilot + a schedule trigger
// (kind='schedule', timezone=UTC, given cron) and returns the
// trigger plus the queries/service handles. Cleanup of the autopilot
// (which cascades to the trigger) is registered on t. The trigger's
// timestamps are NOT touched here — callers must set them
// explicitly via SQL so test wall-clock has no influence on the
// outcome.
func seedColdStartTrigger(t *testing.T, cron string) (db.AutopilotTrigger, *db.Queries, *service.AutopilotService) {
	t.Helper()
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
		Title:              "Cold-start regression",
		Description:        pgtype.Text{String: "deterministic cold-start", Valid: true},
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
		CronExpression: pgtype.Text{String: cron, Valid: true},
		Timezone:       pgtype.Text{String: "UTC", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAutopilotTrigger: %v", err)
	}
	return trigger, queries, autopilotSvc
}

// TestAutopilotScheduleJobColdStartHonorsLastFiredAt is the regression
// for the post-deploy spurious-fire reported on MUL-3551:
//
//	Trigger fired by the legacy goroutine at Mon 17:10 Beijing
//	(last_fired_at written then). Deploy switches to the new
//	scheduler at Tue ~12:30 Beijing. First tick re-fired Monday's
//	already-handled 17:10 occurrence because the cold-start anchor
//	was (created_at, capped to now-24h) — well before Mon 17:10.
//
// Fix: the cold-start anchor must be `last_fired_at` when it is set,
// so an occurrence that the legacy code (or a previous incarnation
// of this scheduler) already processed is NOT replayed.
//
// The test is wall-clock-independent: it sets created_at and
// last_fired_at to fixed UTC timestamps, then invokes the
// JobSpec.PlansForScope hook directly with a pinned `now`. The cron
// (`0 17 * * 1-5` UTC) has a deterministic next fire at Tue 17:00
// UTC, which is in the future relative to the pinned now=Tue 12:00
// UTC; with the fix the half-open `(last_fired_at=Mon 17:00 UTC,
// now=Tue 12:00 UTC]` interval is empty, so the hook returns no
// plans. Without the fix the anchor would degrade to
// `max(created_at, now-24h)` which still includes Mon 17:00 UTC and
// the hook would replay it — the exact bug we are pinning.
func TestAutopilotScheduleJobColdStartHonorsLastFiredAt(t *testing.T) {
	ctx := context.Background()
	trigger, queries, autopilotSvc := seedColdStartTrigger(t, "0 17 * * 1-5")

	// Fully pinned UTC timestamps. Both real days, but the test does
	// NOT compare against time.Now() — the pinned `now` below is
	// what feeds the cron evaluator.
	createdAt := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)    // Sat, several days before
	lastFiredAt := time.Date(2026, 6, 22, 17, 0, 0, 0, time.UTC) // Mon 17:00 UTC — last legacy fire
	pinnedNow := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)   // Tue 12:00 UTC — 5h before next fire

	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_trigger
		   SET created_at    = $2,
		       last_fired_at = $3
		 WHERE id = $1
	`, trigger.ID, createdAt, lastFiredAt); err != nil {
		t.Fatalf("seed deterministic timestamps: %v", err)
	}

	// Build a fresh JobSpec; its scope cache lives inside the
	// closure returned here. Populate the cache by running the scope
	// provider once — the provider does not consult `now`, so the
	// value passed here is ignored.
	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)
	if _, err := job.Scopes(ctx, pinnedNow); err != nil {
		t.Fatalf("populate scope cache: %v", err)
	}

	scope := scheduler.Scope{
		Kind: scheduler.ScopeKindAutopilotTrigger,
		ID:   util.UUIDToString(trigger.ID),
	}

	// Cold-start invariant: no sys_cron_executions row exists yet
	// for this scope, so LatestPlanInfo.Found is false. The fix
	// must NOT enumerate Mon 17:00 UTC.
	plans, err := job.PlansForScope(ctx, scope, pinnedNow, scheduler.LatestPlanInfo{Found: false})
	if err != nil {
		t.Fatalf("planner hook: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("cold-start cron=%q with last_fired_at=%s and now=%s must yield no plans (next fire is Tue 17:00 UTC, in the future); got %v",
			"0 17 * * 1-5",
			lastFiredAt.Format(time.RFC3339),
			pinnedNow.Format(time.RFC3339),
			plans,
		)
	}
}

// TestAutopilotScheduleJobColdStartBrandNewTriggerStillFires is the
// counterpart to TestAutopilotScheduleJobColdStartHonorsLastFiredAt:
// a brand-new trigger (last_fired_at NULL) MUST still fire its
// first due occurrence on cold start. The fix for the
// last_fired_at-honors-cold-start path must not regress the
// "never-fired-before" path.
//
// Also wall-clock-independent: created_at and `now` are both
// pinned, and the cron (`0 12 * * *` daily noon UTC) has a single
// fire (Tue 12:00 UTC) inside the deterministic `(created_at=Tue
// 11:50 UTC, now=Tue 12:05 UTC]` window.
func TestAutopilotScheduleJobColdStartBrandNewTriggerStillFires(t *testing.T) {
	ctx := context.Background()
	trigger, queries, autopilotSvc := seedColdStartTrigger(t, "0 12 * * *")

	createdAt := time.Date(2026, 6, 23, 11, 50, 0, 0, time.UTC)  // 10 min before the fire
	pinnedNow := time.Date(2026, 6, 23, 12, 5, 0, 0, time.UTC)   // 5 min after the fire
	expectedFire := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC) // the daily noon UTC fire

	if _, err := testPool.Exec(ctx, `
		UPDATE autopilot_trigger
		   SET created_at    = $2,
		       last_fired_at = NULL
		 WHERE id = $1
	`, trigger.ID, createdAt); err != nil {
		t.Fatalf("seed deterministic timestamps: %v", err)
	}

	job := scheduler.AutopilotScheduleDispatchJob(testPool, queries, autopilotSvc)
	if _, err := job.Scopes(ctx, pinnedNow); err != nil {
		t.Fatalf("populate scope cache: %v", err)
	}

	scope := scheduler.Scope{
		Kind: scheduler.ScopeKindAutopilotTrigger,
		ID:   util.UUIDToString(trigger.ID),
	}

	plans, err := job.PlansForScope(ctx, scope, pinnedNow, scheduler.LatestPlanInfo{Found: false})
	if err != nil {
		t.Fatalf("planner hook: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("brand-new trigger should fire its first due occurrence; got %d plans: %v", len(plans), plans)
	}
	if !plans[0].Equal(expectedFire) {
		t.Fatalf("plan_time mismatch: got %s want %s",
			plans[0].Format(time.RFC3339), expectedFire.Format(time.RFC3339))
	}
}
