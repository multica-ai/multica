package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/autopilot"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TestDispatch_LeaseExpired_AllowsNewDispatch is the ALL-211 BLOCKING 1
// regression, exercising the exact stuck scenario reported: a create_issue
// run parked in 'issue_created' whose linked issue sits in a non-terminal
// status (backlog) — nothing in SyncRunFromIssue / SyncRunFromLinkedIssueTask
// ever terminalizes it, so under the unbounded ALL-206 gate every later slot
// was skipped forever. With the lease the expired run is reclaimed (failed +
// lease_expired) and the new slot is admitted.
func TestDispatch_LeaseExpired_AllowsNewDispatch(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	const leaseTimeout = 6 * time.Minute // above autopilot.MinLeaseDuration (5m) so the floor clamp does not apply
	autopilotSvc.SetLeaseTimeout(leaseTimeout)

	agentID := loadFixtureAgentID(t, ctx)

	title := "Autopilot lease expired " + time.Now().UTC().Format("20060102150405.000000000")
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 lease expired", agentID, "create_issue", title)
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	// Step 1: seed the STALE in-flight run (issue_created, created_at beyond
	// the lease) and link it to a real issue parked in a NON-terminal status.
	// This is the exact permanent-stuck precondition from the issue.
	staleRunID := dbid.NewV7()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		WITH bumped AS (
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		SELECT $1, $2, 'backlog', 'none', 'member', $3, (SELECT issue_counter FROM bumped)
		RETURNING id
	`, testWorkspaceID, "stale linked issue (non-terminal)", testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed linked issue: %v", err)
	}
	t.Cleanup(func() {
		// Both issues created by this test live in the shared fixture
		// workspace, whose uq_issue_workspace_number forbids duplicate
		// numbers across the whole package run — clean them up so sibling
		// tests never collide.
		if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
			t.Logf("cleanup seeded issue: %v", err)
		}
	})
	insertStaleInFlightRun(t, ctx, staleRunID, ap.ID, "issue_created", issueID, leaseTimeout)

	// Step 2: the next scheduled slot fires. Its lease-expired predecessor
	// must be terminalized and the slot must be ADMITTED (not skipped).
	plannedAt := time.Now().UTC().Truncate(time.Second).Add(-1 * time.Minute)
	newRun, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt)
	if err != nil {
		t.Fatalf("DispatchAutopilotForPlan after lease expiry: %v", err)
	}
	if newRun == nil {
		t.Fatalf("dispatch returned nil run")
	}
	if newRun.Status != "issue_created" {
		t.Fatalf("new run status = %q, want issue_created", newRun.Status)
	}
	if !newRun.IssueID.Valid {
		t.Fatalf("new run must be linked to a freshly created issue, got issue_id invalid")
	}
	// The dispatched issue also persists in the shared fixture workspace.
	newRunIssueID := util.UUIDToString(newRun.IssueID)
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, newRunIssueID); err != nil {
			t.Logf("cleanup dispatched issue: %v", err)
		}
	})

	// Step 3: the stale run must have been terminalized as failed with
	// reason_code=lease_expired — and NOT merely orphaned.
	var staleStatus, staleReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, reason_code FROM autopilot_run WHERE id = $1`, staleRunID,
	).Scan(&staleStatus, &staleReason); err != nil {
		t.Fatalf("read stale run after dispatch: %v", err)
	}
	if staleStatus != "failed" {
		t.Fatalf("stale run status = %q, want failed", staleStatus)
	}
	if staleReason != "lease_expired" {
		t.Fatalf("stale run reason_code = %q, want lease_expired", staleReason)
	}

	// Step 4: exactly one in-flight run remains (the new one).
	assertInFlightCount(t, ctx, ap.ID, 1)
}

// TestDispatch_Concurrent_OnlyOneSucceeds is the ALL-211 BLOCKING 3
// regression: even when ten slots race the same autopilot at once (scheduler
// tick + manual + webhook replicas), the partial unique index
// uq_autopilot_run_inflight admits exactly one run. Every loser comes back
// skipped with already_active — either from the lease gate or from the 23505
// conflict mapped to a skip.
func TestDispatch_Concurrent_OnlyOneSucceeds(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 concurrent dispatch", agentID, "run_only", "")
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	const n = 10
	base := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)

	var wg sync.WaitGroup
	statuses := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			run, err := autopilotSvc.DispatchAutopilotForPlan(
				ctx, ap, trigger.ID, "schedule", nil, base.Add(time.Duration(i)*30*time.Second),
			)
			errs[i] = err
			if run != nil {
				statuses[i] = run.Status
			}
		}(i)
	}
	wg.Wait()

	var successCount, skipCount int
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d returned unexpected error: %v", i, errs[i])
		}
		switch statuses[i] {
		case "running":
			successCount++
		case "skipped":
			skipCount++
		default:
			t.Fatalf("goroutine %d returned run status %q, want running or skipped", i, statuses[i])
		}
	}
	if successCount != 1 {
		t.Fatalf("exactly one dispatch must succeed, got %d", successCount)
	}
	if skipCount != n-1 {
		t.Fatalf("exactly %d dispatches must be skipped, got %d", n-1, skipCount)
	}

	// The DB is the authority: at most one in-flight run exists.
	assertInFlightCount(t, ctx, ap.ID, 1)

	// And every skipped run carries the already_active reason code, so the
	// admission vocabulary stays type-safe for clients.
	var nonAlreadyActive int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM autopilot_run
		 WHERE autopilot_id = $1 AND status = 'skipped'
		   AND COALESCE(reason_code, '') <> 'already_active'
	`, ap.ID).Scan(&nonAlreadyActive); err != nil {
		t.Fatalf("count skipped runs without already_active: %v", err)
	}
	if nonAlreadyActive != 0 {
		t.Fatalf("%d skipped runs lack reason_code=already_active", nonAlreadyActive)
	}
}

// TestDispatch_ManualTrigger_RecoverFromStaleRun is the escape-hatch
// regression (ALL-211 BLOCKING 1, amplification item B): an owner clicking
// "run now" on an autopilot whose previous run is stuck in 'running' past
// the lease MUST be able to self-recover — the manual dispatch reclaims the
// stale run and starts fresh, instead of returning already_active forever.
func TestDispatch_ManualTrigger_RecoverFromStaleRun(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	const leaseTimeout = 6 * time.Minute // above autopilot.MinLeaseDuration (5m) so the floor clamp does not apply
	autopilotSvc.SetLeaseTimeout(leaseTimeout)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 manual recovery", agentID, "run_only", "")
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	// Seed a stale 'running' run (no task linkage — the crashed/corrupt
	// shape that leaves the slot wedged) older than the lease.
	staleRunID := dbid.NewV7()
	insertStaleInFlightRun(t, ctx, staleRunID, ap.ID, "running", "", leaseTimeout)

	// Manual trigger with a real member actor.
	manual, code, err := autopilotSvc.DispatchAutopilotManual(ctx, ap, trigger.ID, nil, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("DispatchAutopilotManual on expired lease: %v", err)
	}
	if manual == nil {
		t.Fatalf("manual dispatch returned nil run")
	}
	if code != "" {
		t.Fatalf("manual dispatch reason code = %q, want empty (admitted)", code)
	}
	if manual.Status != "running" || !manual.TaskID.Valid {
		t.Fatalf("manual recovery run = status %q, task_id valid %v; want running with task", manual.Status, manual.TaskID.Valid)
	}
	if manual.ID == staleRunID {
		t.Fatalf("manual recovery must create a FRESH run, not reuse the stale one")
	}

	var staleStatus, staleReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, reason_code FROM autopilot_run WHERE id = $1`, staleRunID,
	).Scan(&staleStatus, &staleReason); err != nil {
		t.Fatalf("read stale run after manual recovery: %v", err)
	}
	if staleStatus != "failed" || staleReason != "lease_expired" {
		t.Fatalf("stale run after manual recovery = status %q, reason %q; want failed/lease_expired", staleStatus, staleReason)
	}

	assertInFlightCount(t, ctx, ap.ID, 1)
}

// TestDispatch_InFlight_BlocksWithinLease keeps the ALL-206 behavior locked
// in: while the in-flight run is WITHIN its lease, the next slot (and a
// manual trigger) is still skipped with already_active. This is the bound
// that must hold so long-running legitimate work is never interrupted.
func TestDispatch_InFlight_BlocksWithinLease(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	// Default lease (30m) — the fresh run below is well within it.
	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 within lease", agentID, "run_only", "")
	trigger := createTriggerForMutexTest(t, ctx, queries, ap)

	plannedAt1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	first, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt1)
	if err != nil {
		t.Fatalf("slot 1 DispatchAutopilotForPlan: %v", err)
	}
	if first == nil || first.Status != "running" || !first.TaskID.Valid {
		t.Fatalf("slot 1 run = %+v, want running with task", first)
	}

	plannedAt2 := plannedAt1.Add(30 * time.Second)
	second, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt2)
	if err != nil {
		t.Fatalf("slot 2 DispatchAutopilotForPlan: %v", err)
	}
	if second == nil || second.Status != "skipped" {
		t.Fatalf("slot 2 run status = %v, want skipped (lease not expired)", secondStatus(second))
	}

	manual, code, err := autopilotSvc.DispatchAutopilotManual(ctx, ap, trigger.ID, nil, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("manual dispatch within lease: %v", err)
	}
	if code != dispatch.ReasonAlreadyActive {
		t.Fatalf("manual dispatch within lease reason code = %q, want already_active", code)
	}
	if manual == nil || manual.Status != "skipped" {
		t.Fatalf("manual dispatch within lease = %+v, want skipped run", manual)
	}
}

// TestGetInFlightRun_UsesPartialIndex proves the BLOCKING 2 fix at the
// planner level: the in-flight query predicate now matches the partial
// indexes (uq_autopilot_run_inflight / idx_autopilot_run_status, both
// covering exactly ('issue_created','running')) so EXPLAIN shows an Index
// Scan on one of them. The old pending-widened predicate could not match
// the partial-index WHERE clause and forced the planner back onto the
// non-partial idx_autopilot_run_autopilot (or worse, a Seq Scan).
//
// The table is seeded with a thousand terminal rows for the target
// autopilot so the planner prefers the partial in-flight index (which
// contains none of them) over idx_autopilot_run_autopilot, which would have
// to scan every one of the thousand rows to confirm no in-flight run exists.
// enable_seqscan is turned off so the assertion stays deterministic even on
// a small table where a Seq Scan would otherwise win on cost.
func TestGetInFlightRun_UsesPartialIndex(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 explain index", agentID, "run_only", "")

	if _, err := testPool.Exec(ctx, `
		INSERT INTO autopilot_run (id, autopilot_id, source, status, created_at)
		SELECT gen_random_uuid(), $1, 'schedule', 'completed', now() - (i || ' hours')::interval
		FROM generate_series(1, 1000) AS i
	`, ap.ID); err != nil {
		t.Fatalf("seed explain data: %v", err)
	}
	if _, err := testPool.Exec(ctx, `ANALYZE autopilot_run`); err != nil {
		t.Fatalf("analyze autopilot_run: %v", err)
	}

	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire test connection: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "RESET enable_seqscan")
	}()

	var plan string
	if err := conn.QueryRow(ctx, `
		EXPLAIN (FORMAT JSON)
		SELECT id, autopilot_id, status, created_at
		FROM autopilot_run
		WHERE autopilot_id = $1
		  AND status IN ('issue_created', 'running')
		ORDER BY created_at DESC
		LIMIT 1
	`, ap.ID).Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN in-flight query: %v", err)
	}

	indexName, nodeType, err := extractExplainIndexScan(plan)
	if err != nil {
		t.Fatalf("parse EXPLAIN plan: %v\nplan: %s", err, plan)
	}
	if nodeType == "Seq Scan" {
		t.Fatalf("in-flight query degraded to Seq Scan; the predicate must match a partial index\nplan: %s", plan)
	}
	switch indexName {
	case "uq_autopilot_run_inflight", "idx_autopilot_run_status":
		// Both partial indexes carry the aligned ('issue_created','running')
		// predicate; either proves the query matches the partial-index set.
	default:
		t.Fatalf("in-flight query used index %q; want uq_autopilot_run_inflight or idx_autopilot_run_status\nplan: %s", indexName, plan)
	}
}

// TestStaleRunSweeper_TerminalizesExpiredRuns covers the defensive second
// layer (ALL-211 BLOCKING 1 requirement 2): the sweeper reclaims in-flight
// runs older than the hard timeout even when no new dispatch ever arrives,
// making the failure visible to the failure-rate auto-pause monitor. Runs
// within the hard timeout are left untouched.
func TestStaleRunSweeper_TerminalizesExpiredRuns(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	agentID := loadFixtureAgentID(t, ctx)

	// Two autopilots: one with a run past the hard timeout, one with a
	// fresh run. The sweeper must reclaim only the former.
	staleAp := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 sweeper stale", agentID, "run_only", "")
	freshAp := createLeaseGateAutopilot(t, ctx, queries, "ALL-211 sweeper fresh", agentID, "run_only", "")

	const hardTimeout = time.Hour
	staleRunID := dbid.NewV7()
	freshRunID := dbid.NewV7()
	insertRunAtAge(t, ctx, staleRunID, staleAp.ID, "issue_created", hardTimeout+time.Minute)
	insertRunAtAge(t, ctx, freshRunID, freshAp.ID, "running", time.Minute)

	sweeper := autopilot.NewStaleRunSweeper(testPool, &autopilot.SweeperConfig{
		Interval:    5 * time.Minute,
		HardTimeout: hardTimeout,
		Enabled:     true,
		Logger:      testLogger(t),
	})
	sweeper.SweepOnce(ctx)

	var staleStatus, staleReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, reason_code FROM autopilot_run WHERE id = $1`, staleRunID,
	).Scan(&staleStatus, &staleReason); err != nil {
		t.Fatalf("read swept stale run: %v", err)
	}
	if staleStatus != "failed" || staleReason != "lease_expired" {
		t.Fatalf("swept stale run = status %q, reason %q; want failed/lease_expired", staleStatus, staleReason)
	}

	var freshStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM autopilot_run WHERE id = $1`, freshRunID,
	).Scan(&freshStatus); err != nil {
		t.Fatalf("read fresh run: %v", err)
	}
	if freshStatus != "running" {
		t.Fatalf("fresh run status = %q, want running (untouched)", freshStatus)
	}
}

// TestStaleRunSweeper_RespectsDailyScheduleSlotInterval is the ALL-235
// BLOCKING 2 regression: a daily-schedule autopilot (SlotInterval=24h) has a
// legitimate in-flight run with a linked agent task. The run is OLDER than
// the sweeper's flat hard timeout (2h) but well WITHIN its 24h slot interval
// — exactly the case where the dispatch lease gate would still consider it
// live (lease = max(base 30m, 24h) = 24h). The sweeper must NOT reclaim it:
// with the defect, the flat hard timeout terminalized the run (and, via the
// buggy cancel path, its task) at 2h, contradicting the lease semantics. The
// per-autopilot deadline max(HardTimeout, SlotInterval) keeps both alive.
func TestStaleRunSweeper_RespectsDailyScheduleSlotInterval(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, taskSvc)

	const hardTimeout = 2 * time.Hour
	agentID := loadFixtureAgentID(t, ctx)
	ap := createLeaseGateAutopilot(t, ctx, queries, "ALL-235 sweeper daily", agentID, "run_only", "")
	// Daily cadence: slot interval = 24h, far beyond the flat hard timeout.
	trigger := createTriggerWithCron(t, ctx, queries, ap, "0 0 * * *")

	// Start a legitimate run with a linked task, then backdate it past the
	// hard timeout but well inside the slot interval.
	plannedAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	run, err := autopilotSvc.DispatchAutopilotForPlan(ctx, ap, trigger.ID, "schedule", nil, plannedAt)
	if err != nil {
		t.Fatalf("slot 1 dispatch: %v", err)
	}
	if run == nil || run.Status != "running" || !run.TaskID.Valid {
		t.Fatalf("slot 1 run = %+v, want running with linked task", run)
	}
	backdateRunCreatedAt(t, ctx, run.ID, 3*time.Hour) // > 2h hard timeout, < 24h slot interval

	sweeper := autopilot.NewStaleRunSweeper(testPool, &autopilot.SweeperConfig{
		Interval:     5 * time.Minute,
		HardTimeout:  hardTimeout,
		SlotInterval: autopilotSvc.SlotIntervalForAutopilot,
		Enabled:      true,
		Logger:       testLogger(t),
	})
	sweeper.SweepOnce(ctx)

	var status, reason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, COALESCE(reason_code, '') FROM autopilot_run WHERE id = $1`, run.ID,
	).Scan(&status, &reason); err != nil {
		t.Fatalf("read daily-schedule run after sweep: %v", err)
	}
	if status != "running" {
		t.Fatalf("daily-schedule in-flight run = status %q (reason %q), want running; the sweeper must not reclaim a run within its 24h slot interval", status, reason)
	}

	// The linked task must also still be alive — the run was not treated as
	// stale, so its work was not cancelled.
	var taskStatus string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, run.TaskID,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("read linked task after sweep: %v", err)
	}
	if taskStatus == "cancelled" || taskStatus == "failed" {
		t.Fatalf("daily-schedule linked task = %q after sweep, want still active", taskStatus)
	}

	assertInFlightCount(t, ctx, ap.ID, 1)
}

// --- helpers ---------------------------------------------------------------

// createLeaseGateAutopilot seeds an active autopilot for lease-gate tests.
// create_issue mode requires a title template; run_only passes "".
func createLeaseGateAutopilot(t *testing.T, ctx context.Context, queries *db.Queries, title, agentID, mode, issueTitle string) db.Autopilot {
	t.Helper()
	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:        parseUUID(testWorkspaceID),
		Title:              title,
		Description:        pgtype.Text{String: "ALL-211 lease gate test", Valid: true},
		AssigneeType:       "agent",
		AssigneeID:         parseUUID(agentID),
		Status:             "active",
		ExecutionMode:      mode,
		IssueTitleTemplate: pgtype.Text{String: issueTitle, Valid: issueTitle != ""},
		CreatedByType:      "member",
		CreatedByID:        parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
			t.Logf("cleanup autopilot: %v", err)
		}
	})
	return ap
}

func loadFixtureAgentID(t *testing.T, ctx context.Context) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	return agentID
}

// insertStaleInFlightRun seeds an in-flight autopilot_run whose created_at
// is just past the given lease timeout, optionally linked to an issue. The
// margin is deliberately generous so clock skew between the app process and
// the DB never flips the lease-expired check the wrong way.
func insertStaleInFlightRun(t *testing.T, ctx context.Context, runID, autopilotID pgtype.UUID, status, issueID string, leaseTimeout time.Duration) {
	t.Helper()
	insertRunAtAge(t, ctx, runID, autopilotID, status, leaseTimeout+5*time.Second)
	if issueID != "" {
		if _, err := testPool.Exec(ctx,
			`UPDATE autopilot_run SET issue_id = $1 WHERE id = $2`, issueID, runID); err != nil {
			t.Fatalf("link stale run to issue: %v", err)
		}
	}
}

// insertRunAtAge seeds an autopilot_run with a backdated created_at
// (age = now - age).
func insertRunAtAge(t *testing.T, ctx context.Context, runID, autopilotID pgtype.UUID, status string, age time.Duration) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO autopilot_run (id, autopilot_id, source, status, created_at)
		VALUES ($1, $2, 'schedule', $3, now() - $4::interval)
	`, runID, autopilotID, status, fmt.Sprintf("%d seconds", int(age.Seconds()))); err != nil {
		t.Fatalf("seed stale in-flight run: %v", err)
	}
}

func assertInFlightCount(t *testing.T, ctx context.Context, autopilotID pgtype.UUID, want int) {
	t.Helper()
	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM autopilot_run
		 WHERE autopilot_id = $1 AND status IN ('issue_created', 'running')
	`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count in-flight runs: %v", err)
	}
	if count != want {
		t.Fatalf("in-flight run count = %d, want %d", count, want)
	}
}

func secondStatus(run *db.AutopilotRun) string {
	if run == nil {
		return "<nil>"
	}
	return run.Status
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(testOutputWriter{t}, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

type testOutputWriter struct{ t *testing.T }

func (w testOutputWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

// extractExplainIndexScan walks a FORMAT JSON EXPLAIN plan and returns the
// index name and node type of the first Index Scan on autopilot_run (or the
// Seq Scan node type if the planner degraded). The recursive walk finds the
// node regardless of LIMIT/SORT wrappers.
func extractExplainIndexScan(plan string) (indexName, nodeType string, err error) {
	var results []struct {
		Plan struct {
			NodeType   string `json:"Node Type"`
			IndexName  string `json:"Index Name"`
			Relation   string `json:"Relation Name"`
			Plans      []struct {
				NodeType  string `json:"Node Type"`
				IndexName string `json:"Index Name"`
				Relation  string `json:"Relation Name"`
				Plans     []struct {
					NodeType  string `json:"Node Type"`
					IndexName string `json:"Index Name"`
					Relation  string `json:"Relation Name"`
				} `json:"Plans"`
			} `json:"Plans"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &results); err != nil {
		return "", "", err
	}
	if len(results) == 0 {
		return "", "", fmt.Errorf("empty EXPLAIN result")
	}
	root := results[0].Plan
	if isScanNode(root.NodeType, root.Relation) {
		return root.IndexName, root.NodeType, nil
	}
	for _, l1 := range root.Plans {
		if isScanNode(l1.NodeType, l1.Relation) {
			return l1.IndexName, l1.NodeType, nil
		}
		for _, l2 := range l1.Plans {
			if isScanNode(l2.NodeType, l2.Relation) {
				return l2.IndexName, l2.NodeType, nil
			}
		}
	}
	return "", "", fmt.Errorf("no scan node found under plan root %q", root.NodeType)
}

func isScanNode(nodeType, relation string) bool {
	return (nodeType == "Seq Scan" || nodeType == "Index Scan") && relation == "autopilot_run"
}
