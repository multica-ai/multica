package service

import (
	"context"
	"testing"
)

// TestReconcileFinalizesRunWhoseTaskTerminatedWhileGateOff is the "no event replay"
// counter-example Elon asked for (MUL-4809 §4.1 P0-3). A create_issue task is stamped
// and completes while the gate is OFF, so its terminal event is a no-op and the run
// stays issue_created (issue status stays in_progress, so legacy never finalizes it).
// The gate then flips ON WITHOUT any new event being published — only the boot
// reconcile runs — and the run must converge to completed off the already-persisted
// task result.
func TestReconcileFinalizesRunWhoseTaskTerminatedWhileGateOff(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t) // gate ON by default

	// Phase 1 — OFF: the dispatched task completes; its terminal event is consumed but
	// legacy mode leaves the run alone (only issue status finalizes runs when OFF).
	svc.FeatureFlags = autopilotTaskDrivenFlags(false)
	dispatched := insertTask(agentID, 0, "completed", run.ID)
	svc.SyncRunFromCreateIssueTask(ctx, dispatched) // OFF → no-op
	off, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if off.Status != "issue_created" {
		t.Fatalf("precondition: an OFF-mode task event must not finalize the run, got %q", off.Status)
	}

	// Phase 2 — flip ON, publish NO new event, run only the boot reconcile.
	svc.FeatureFlags = autopilotTaskDrivenFlags(true)
	res, err := svc.ReconcileTaskDrivenRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.finalized < 1 {
		t.Fatalf("reconcile finalized %d runs, want >= 1 (this run)", res.finalized)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("reconcile did not finalize the stranded run: status=%q", got.Status)
	}
	if !got.TaskID.Valid || got.TaskID.Bytes != dispatched.ID.Bytes {
		t.Fatalf("reconcile did not bind the run to its dispatched task")
	}
}

// TestReconcileFinalizesOffRetryLeaf verifies the reconcile walks the retry lineage
// to the FINAL attempt: the dispatched attempt failed while the gate was OFF but its
// system retry completed. The reconcile must converge the run to completed (off the
// retry leaf), not failed (off the dispatched root).
func TestReconcileFinalizesOffRetryLeaf(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)

	svc.FeatureFlags = autopilotTaskDrivenFlags(false)
	dispatched := insertTask(agentID, 0, "failed", run.ID) // stamped root, failed
	// Its system retry (inherits lineage via retry_of_task_id, not the stamp) completed.
	var retryID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, retry_of_task_id, created_at)
		 VALUES ($1, $2, $3, 'completed', 0, $4, now()) RETURNING id`,
		dispatched.AgentID, dispatched.RuntimeID, dispatched.IssueID, dispatched.ID).Scan(&retryID); err != nil {
		t.Fatalf("insert retry: %v", err)
	}

	svc.FeatureFlags = autopilotTaskDrivenFlags(true)
	if _, err := svc.ReconcileTaskDrivenRuns(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("reconcile finalized off the wrong lineage attempt: status=%q, want completed (retry leaf)", got.Status)
	}
}

// TestReconcileAtBootNoopWhenGateOff verifies the boot reconcile is inert while the
// gate is off — it must not finalize anything (that would defeat legacy mode).
func TestReconcileAtBootNoopWhenGateOff(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)
	svc.FeatureFlags = autopilotTaskDrivenFlags(false)

	insertTask(agentID, 0, "completed", run.ID)

	res, err := svc.ReconcileTaskDrivenRunsAtBoot(ctx, pool)
	if err != nil {
		t.Fatalf("boot reconcile: %v", err)
	}
	if res.finalized != 0 {
		t.Fatalf("boot reconcile finalized %d runs while gate off, want 0", res.finalized)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "issue_created" {
		t.Fatalf("gate-off boot reconcile changed the run: status=%q", got.Status)
	}
}

// TestReconcileSkipsRetryEligibleFailedLeafThenConverges is the P0-2 concurrency-
// boundary counter-example (MUL-4809 §4.1 P0-2). The dispatched task is terminal-
// FAILED with an infrastructure-shaped reason (runtime_offline, attempt 1/2) but its
// system retry has not been created yet — the sweeper marks the task failed, then
// creates the retry in a separate step. Finalizing the run now would fail it before
// the retry runs, and a later successful retry cannot un-fail a terminal run. The
// reconcile must SKIP the run while the leaf is still retry-eligible, then converge it
// on a later tick once the (now-completed) retry successor settles the lineage.
func TestReconcileSkipsRetryEligibleFailedLeafThenConverges(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t) // gate ON

	// Stamped dispatched root: terminal-failed, still within its retry budget.
	dispatched := insertTask(agentID, 0, "failed", run.ID) // attempt=1, max_attempts=2 by default
	if _, err := pool.Exec(ctx,
		`UPDATE agent_task_queue SET failure_reason = 'runtime_offline' WHERE id = $1`,
		dispatched.ID); err != nil {
		t.Fatalf("set failure_reason: %v", err)
	}

	// Tick 1: the retry successor does not exist yet, so the leaf is a retry-eligible
	// failed root. The reconcile must NOT finalize the run.
	res, err := svc.ReconcileTaskDrivenRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile (pre-retry): %v", err)
	}
	if res.finalized != 0 {
		t.Fatalf("reconcile finalized a run whose failed leaf is still retry-eligible: finalized=%d", res.finalized)
	}
	mid, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if mid.Status != "issue_created" {
		t.Fatalf("reconcile prematurely finalized a retry-eligible run: status=%q", mid.Status)
	}

	// The sweeper now creates the retry, which succeeds. The lineage has settled.
	var retryID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, retry_of_task_id, created_at)
		 VALUES ($1, $2, $3, 'completed', 0, $4, now()) RETURNING id`,
		dispatched.AgentID, dispatched.RuntimeID, dispatched.IssueID, dispatched.ID).Scan(&retryID); err != nil {
		t.Fatalf("insert retry: %v", err)
	}

	// Tick 2: the leaf is now the completed retry. The reconcile converges the run to
	// completed (off the retry leaf), not failed (off the earlier attempt).
	res, err = svc.ReconcileTaskDrivenRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile (post-retry): %v", err)
	}
	if res.finalized < 1 {
		t.Fatalf("reconcile did not converge the settled lineage: finalized=%d", res.finalized)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("settled lineage did not converge to completed: status=%q", got.Status)
	}
}

// TestReconcileConvergesOnLaterTickAfterTransientError is the P0-3 "first round query
// fails, next round succeeds" counter-example (MUL-4809 §4.1 P0-3). A one-shot boot
// scan would permanently strand a run whose reconcile query hit a transient DB error.
// Here the first pass fails on a cancelled context (the page-load query errors); the
// run must be left untouched, and a later pass with a live context must converge it —
// no event is ever replayed.
func TestReconcileConvergesOnLaterTickAfterTransientError(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t) // gate ON

	// Stranded run: a completed stamped task exists, but no event was published, so
	// the run is stuck at issue_created — exactly what the reconcile must converge.
	dispatched := insertTask(agentID, 0, "completed", run.ID)

	// Tick 1: a cancelled context makes the page-load query fail. The pass aborts with
	// an error and must not touch the run.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.ReconcileTaskDrivenRuns(cancelled); err == nil {
		t.Fatal("expected the first reconcile pass to error on the cancelled context")
	}
	mid, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if mid.Status != "issue_created" {
		t.Fatalf("a failed reconcile pass must leave the run untouched, got %q", mid.Status)
	}

	// Tick 2: a live context converges the run off the already-persisted task result.
	res, err := svc.ReconcileTaskDrivenRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile (recovery): %v", err)
	}
	if res.finalized < 1 {
		t.Fatalf("recovery tick did not finalize the stranded run: finalized=%d", res.finalized)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("run not converged after recovery tick: status=%q", got.Status)
	}
	if !got.TaskID.Valid || got.TaskID.Bytes != dispatched.ID.Bytes {
		t.Fatal("recovery tick did not bind the run to its dispatched task")
	}
}

// TestReconcileAtBootLockLoserSkipsThenTakesOver is the P0-3 "lock contention winner
// fails, loser / a subsequent round takes over" counter-example (MUL-4809 §4.1 P0-3).
// While another replica holds the reconcile advisory lock (its own tick in flight or
// errored), this replica must skip without stranding the run; once the lock frees, a
// later tick on this replica takes over and converges it.
func TestReconcileAtBootLockLoserSkipsThenTakesOver(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t) // gate ON

	insertTask(agentID, 0, "completed", run.ID) // stranded, reconcilable run

	// Simulate another replica ("winner") holding the reconcile advisory lock on its
	// own session.
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock holder: %v", err)
	}
	var locked bool
	if err := holder.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", autopilotReconcileAdvisoryLockKey).Scan(&locked); err != nil || !locked {
		holder.Release()
		t.Fatalf("lock holder failed to take advisory lock: locked=%v err=%v", locked, err)
	}

	// This replica loses the lock → skips this tick, leaving the run untouched.
	res, err := svc.ReconcileTaskDrivenRunsAtBoot(ctx, pool)
	if err != nil {
		holder.Release()
		t.Fatalf("boot reconcile (lock loser): %v", err)
	}
	if res.finalized != 0 {
		holder.Release()
		t.Fatalf("lock loser finalized %d runs, want 0", res.finalized)
	}
	mid, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		holder.Release()
		t.Fatalf("get run: %v", err)
	}
	if mid.Status != "issue_created" {
		holder.Release()
		t.Fatalf("lock loser changed the run: status=%q", mid.Status)
	}

	// The winner's tick ends (here: without converging) and releases the lock. A later
	// tick on this replica takes over.
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock($1)", autopilotReconcileAdvisoryLockKey); err != nil {
		holder.Release()
		t.Fatalf("release advisory lock: %v", err)
	}
	holder.Release()

	res, err = svc.ReconcileTaskDrivenRunsAtBoot(ctx, pool)
	if err != nil {
		t.Fatalf("boot reconcile (takeover): %v", err)
	}
	if res.finalized < 1 {
		t.Fatalf("takeover tick did not finalize the stranded run: finalized=%d", res.finalized)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("run not converged after lock takeover: status=%q", got.Status)
	}
}
