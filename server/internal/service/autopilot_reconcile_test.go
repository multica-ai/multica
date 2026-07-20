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
	n, err := svc.ReconcileTaskDrivenRuns(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n < 1 {
		t.Fatalf("reconcile finalized %d runs, want >= 1 (this run)", n)
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

	n, err := svc.ReconcileTaskDrivenRunsAtBoot(ctx, pool)
	if err != nil {
		t.Fatalf("boot reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("boot reconcile finalized %d runs while gate off, want 0", n)
	}
	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "issue_created" {
		t.Fatalf("gate-off boot reconcile changed the run: status=%q", got.Status)
	}
}
