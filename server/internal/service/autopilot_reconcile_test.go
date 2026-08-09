package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
	res, err := svc.ReconcileAutopilotRuns(ctx)
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
	if _, err := svc.ReconcileAutopilotRuns(ctx); err != nil {
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

	res, err := svc.ReconcileAutopilotRunsAtBoot(ctx, pool)
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
	if _, err := svc.ReconcileAutopilotRuns(cancelled); err == nil {
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
	res, err := svc.ReconcileAutopilotRuns(ctx)
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
	res, err := svc.ReconcileAutopilotRunsAtBoot(ctx, pool)
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

	res, err = svc.ReconcileAutopilotRunsAtBoot(ctx, pool)
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

// TestReconcileBackfillsMissingRetryForOrphanedFailedLeaf is the P0-2 "durable
// recovery" counter-example (MUL-4809 §4.1 P0-2): the bulk sweeper committed the fail
// but crashed before HandleFailedTasks, so the dispatched leaf is terminal-failed with
// an infra reason yet has no retry successor. A plain periodic reconcile that only
// SKIPS such a leaf would strand the run forever. The reconcile must instead BACK-FILL
// the owed retry (idempotently), leave the run pending, and converge it once the retry
// settles.
func TestReconcileBackfillsMissingRetryForOrphanedFailedLeaf(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t) // gate ON

	dispatched := insertTask(agentID, 0, "failed", run.ID) // attempt 1/max_attempts 2 by default
	if _, err := pool.Exec(ctx,
		`UPDATE agent_task_queue SET failure_reason = 'runtime_offline', completed_at = now() - interval '10 minutes' WHERE id = $1`,
		dispatched.ID); err != nil {
		t.Fatalf("set orphaned failed state: %v", err)
	}

	countSuccessors := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id = $1`, dispatched.ID).Scan(&n); err != nil {
			t.Fatalf("count successors: %v", err)
		}
		return n
	}

	// Tick 1: back-fill the missing retry; do not finalize while it is pending.
	res, err := svc.ReconcileAutopilotRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.finalized != 0 {
		t.Fatalf("must not finalize while the back-filled retry is pending, finalized=%d", res.finalized)
	}
	if n := countSuccessors(); n != 1 {
		t.Fatalf("reconcile did not back-fill the owed retry: successors=%d", n)
	}
	mid, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if mid.Status != "issue_created" {
		t.Fatalf("run finalized before the back-filled retry ran: %q", mid.Status)
	}

	// Tick 2 (retry still queued) must not create a duplicate — idempotent back-fill.
	if _, err := svc.ReconcileAutopilotRuns(ctx); err != nil {
		t.Fatalf("reconcile (idempotency tick): %v", err)
	}
	if n := countSuccessors(); n != 1 {
		t.Fatalf("reconcile created a duplicate retry: successors=%d", n)
	}

	// The back-filled retry runs and completes; a later tick converges the run.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed' WHERE retry_of_task_id = $1`, dispatched.ID); err != nil {
		t.Fatalf("complete retry: %v", err)
	}
	res, err = svc.ReconcileAutopilotRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile (converge tick): %v", err)
	}
	if res.finalized < 1 {
		t.Fatalf("reconcile did not converge the settled lineage: finalized=%d", res.finalized)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("run did not converge off the back-filled retry: %q", got.Status)
	}
}

// installRetryInsertFault makes every agent_task_queue INSERT that carries a
// retry_of_task_id raise, simulating a transient failure while creating a retry.
func installRetryInsertFault(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
CREATE OR REPLACE FUNCTION mul4809_retry_insert_fault() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.retry_of_task_id IS NOT NULL THEN
		RAISE EXCEPTION 'forced retry-create fault';
	END IF;
	RETURN NEW;
END;
$$;`); err != nil {
		t.Fatalf("install retry-insert fault fn: %v", err)
	}
	if _, err := pool.Exec(ctx, `
CREATE TRIGGER mul4809_retry_insert_fault_trg
BEFORE INSERT ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION mul4809_retry_insert_fault();`); err != nil {
		t.Fatalf("install retry-insert fault trigger: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS mul4809_retry_insert_fault_trg ON agent_task_queue`)
		pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS mul4809_retry_insert_fault()`)
	})
}

// TestHandleFailedTasksRetryErrorDefersRunNotPrematureFail is the P0-2 "transient
// retry-create error" counter-example (MUL-4809 §4.1 P0-2): MaybeRetryFailedTask errors
// on its first attempt. HandleFailedTasks must not silently drop that error and let the
// run fail prematurely — it must keep the issue retry-pending, and the create_issue
// listener must not finalize the run off the non-final attempt. After the fault clears,
// the reconcile back-fills the retry and the run converges off the final leaf.
func TestHandleFailedTasksRetryErrorDefersRunNotPrematureFail(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t) // gate ON

	// A bound dispatched task, terminal-failed with an infra reason, no retry yet.
	dispatched := insertTask(agentID, 0, "failed", run.ID)
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET failure_reason = 'runtime_offline' WHERE id = $1`, dispatched.ID); err != nil {
		t.Fatalf("set failure reason: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE autopilot_run SET task_id = $2 WHERE id = $1`, run.ID, dispatched.ID); err != nil {
		t.Fatalf("bind run to dispatched task: %v", err)
	}
	// Put the issue in_progress so a spurious reset-to-todo would be observable.
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, dispatched.IssueID); err != nil {
		t.Fatalf("set issue in_progress: %v", err)
	}

	// MaybeRetryFailedTask errors (retry INSERT fault). HandleFailedTasks must keep the
	// issue retry-pending rather than resetting it to todo.
	installRetryInsertFault(t, pool)
	failedTask, err := svc.Queries.GetAgentTask(ctx, dispatched.ID)
	if err != nil {
		t.Fatalf("reload failed task: %v", err)
	}
	svc.TaskSvc.HandleFailedTasks(ctx, []db.AgentTaskQueue{failedTask})

	issueAfter, err := svc.Queries.GetIssue(ctx, dispatched.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issueAfter.Status != "in_progress" {
		t.Fatalf("retry-create error must not reset the issue: status=%q", issueAfter.Status)
	}

	// The listener firing on the failed task must NOT finalize the run — a retry is owed.
	svc.SyncRunFromCreateIssueTask(ctx, failedTask)
	mid, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if mid.Status != "issue_created" {
		t.Fatalf("run was prematurely finalized while a retry was owed: %q", mid.Status)
	}

	// Fault clears; the reconcile back-fills the owed retry, which then completes and
	// converges the run off the final leaf.
	if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS mul4809_retry_insert_fault_trg ON agent_task_queue`); err != nil {
		t.Fatalf("clear fault: %v", err)
	}
	if _, err := svc.ReconcileAutopilotRuns(ctx); err != nil {
		t.Fatalf("reconcile (back-fill): %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed' WHERE retry_of_task_id = $1`, dispatched.ID); err != nil {
		t.Fatalf("complete back-filled retry: %v", err)
	}
	if _, err := svc.ReconcileAutopilotRuns(ctx); err != nil {
		t.Fatalf("reconcile (converge): %v", err)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("run did not converge off the final leaf after recovery: %q", got.Status)
	}
}

// TestMaybeRetryFailedTaskIdempotentUnderUniqueConstraint pins the concurrency-safe
// back-fill contract (MUL-4809 §4.1 P0-2): the retry_of_task_id unique index turns a
// second retry-create for the same parent into a no-op, so racing creators (FailTask,
// sweeper, reconcile back-fill) never produce a duplicate attempt.
func TestMaybeRetryFailedTaskIdempotentUnderUniqueConstraint(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)

	parent := insertTask(agentID, 0, "failed", run.ID)
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET failure_reason = 'runtime_offline' WHERE id = $1`, parent.ID); err != nil {
		t.Fatalf("set failure reason: %v", err)
	}
	reloaded, err := svc.Queries.GetAgentTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("reload parent: %v", err)
	}

	first, err := svc.TaskSvc.MaybeRetryFailedTask(ctx, reloaded)
	if err != nil {
		t.Fatalf("first retry: %v", err)
	}
	if first == nil {
		t.Fatal("first retry should create a successor")
	}
	// Second call on the same still-failed parent must not create a duplicate. It reports
	// the SAME successor rather than nil so callers still treat the failure as
	// retry-pending — a nil here would let HandleFailedTasks reset the issue while a
	// deferred retry is armed (MUL-4809 §4.1 P1).
	second, err := svc.TaskSvc.MaybeRetryFailedTask(ctx, reloaded)
	if err != nil {
		t.Fatalf("second retry must be idempotent, got error: %v", err)
	}
	if second == nil {
		t.Fatal("second retry must report the existing successor, not nil")
	}
	if util.UUIDToString(second.ID) != util.UUIDToString(first.ID) {
		t.Fatalf("second retry created a duplicate successor: %s (first %s)",
			util.UUIDToString(second.ID), util.UUIDToString(first.ID))
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id = $1`, parent.ID).Scan(&n); err != nil {
		t.Fatalf("count successors: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one retry successor, got %d", n)
	}
}

// TestHandleFailedTasksKeepsIssuePendingWhenConflictHasDeferredSuccessor is the P1
// counter-example (MUL-4809 §4.1 P1). When two creators race, the loser's
// MaybeRetryFailedTask hits the retry_of_task_id unique constraint. If it reported "no
// child" the caller would treat the failure as terminal — and because a backoff-armed
// DEFERRED successor is invisible to HasActiveTaskForIssue, HandleFailedTasks would reset
// the issue to todo while a retry is still pending. The loser must instead see the
// winner's successor and keep the issue retry-pending.
func TestHandleFailedTasksKeepsIssuePendingWhenConflictHasDeferredSuccessor(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, pool, insertTask := newCreateIssueRunFixture(t)

	// A retry-eligible failed parent whose successor already exists — and is DEFERRED,
	// so HasActiveTaskForIssue does not see it.
	parent := insertTask(agentID, 0, "failed", run.ID)
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET failure_reason = 'runtime_offline' WHERE id = $1`, parent.ID); err != nil {
		t.Fatalf("set failure reason: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, retry_of_task_id, fire_at, created_at)
		VALUES ($1, $2, $3, 'deferred', 0, 2, $4, now() + interval '5 seconds', now())`,
		parent.AgentID, parent.RuntimeID, parent.IssueID, parent.ID); err != nil {
		t.Fatalf("insert deferred successor: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, parent.IssueID); err != nil {
		t.Fatalf("set issue in_progress: %v", err)
	}

	// The losing creator: its INSERT conflicts, so it must surface the winner's successor.
	reloaded, err := svc.Queries.GetAgentTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	child, err := svc.TaskSvc.MaybeRetryFailedTask(ctx, reloaded)
	if err != nil {
		t.Fatalf("losing retry must not error: %v", err)
	}
	if child == nil {
		t.Fatal("losing retry must report the winner's existing successor, not nil")
	}

	// End to end: the losing HandleFailedTasks must leave the issue in_progress.
	svc.TaskSvc.HandleFailedTasks(ctx, []db.AgentTaskQueue{reloaded})
	issueAfter, err := svc.Queries.GetIssue(ctx, parent.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issueAfter.Status != "in_progress" {
		t.Fatalf("issue was reset while a deferred retry was still pending: status=%q", issueAfter.Status)
	}

	// And no duplicate successor was created by the losing path.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id = $1`, parent.ID).Scan(&n); err != nil {
		t.Fatalf("count successors: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one retry successor, got %d", n)
	}
}
