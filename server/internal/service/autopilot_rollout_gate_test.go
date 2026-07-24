package service

import (
	"context"
	"testing"
)

// These tests cover the two-phase rollout gate (MUL-4809 §4.1 P0-3): exactly one
// finalization path is live per process, and a run dispatched while the gate was
// off is still finalized correctly after the gate flips on.

// TestLegacyModeTaskOutcomeDoesNotFinalizeRun: with the gate OFF, a terminal
// create_issue task must NOT finalize the run — issue status owns finalization, so
// task outcome must be inert (otherwise a gate-off new pod and an old pod would run
// two termination semantics at once).
func TestLegacyModeTaskOutcomeDoesNotFinalizeRun(t *testing.T) {
	ctx := context.Background()
	svc, agentID, run, _, insertTask := newCreateIssueRunFixture(t)
	svc.FeatureFlags = autopilotTaskDrivenFlags(false) // legacy

	dispatched := insertTask(agentID, 0, "completed", run.ID)
	svc.SyncRunFromCreateIssueTask(ctx, dispatched)

	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "issue_created" {
		t.Fatalf("legacy mode: task outcome finalized the run (status=%q); issue status should own finalization", got.Status)
	}
}

// TestLegacyModeIssueStatusFinalizesRun: with the gate OFF, SyncRunFromIssue
// finalizes the run from the linked issue's terminal status — the behavior a
// gate-off pod must keep so it matches the old pods it runs alongside.
func TestLegacyModeIssueStatusFinalizesRun(t *testing.T) {
	ctx := context.Background()
	svc, _, run, pool, _ := newCreateIssueRunFixture(t)
	svc.FeatureFlags = autopilotTaskDrivenFlags(false) // legacy

	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, run.IssueID); err != nil {
		t.Fatalf("set issue done: %v", err)
	}
	issue, err := svc.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	svc.SyncRunFromIssue(ctx, issue)

	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("legacy mode: issue status did not finalize the run: status=%q", got.Status)
	}
}

// TestTaskDrivenModeIssueStatusIsNoOp: with the gate ON, issue status must NOT
// touch the run — the run is finalized by task outcome, so an agent moving the
// issue to done/in_review/etc. is a pure issue-workflow action.
func TestTaskDrivenModeIssueStatusIsNoOp(t *testing.T) {
	ctx := context.Background()
	svc, _, run, pool, _ := newCreateIssueRunFixture(t) // gate ON by default

	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, run.IssueID); err != nil {
		t.Fatalf("set issue done: %v", err)
	}
	issue, err := svc.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	svc.SyncRunFromIssue(ctx, issue)

	got, err := svc.Queries.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "issue_created" {
		t.Fatalf("task-driven mode: issue status finalized the run (status=%q); it must be inert", got.Status)
	}
}

// The real gate-flip enablement boundary — an OFF-mode terminal task event that is
// NOT replayed after the flip, converged only by the boot reconcile — is covered by
// TestReconcileFinalizesRunWhoseTaskTerminatedWhileGateOff in autopilot_reconcile_test.go.
// (The earlier TestGateFlipFinalizesLegacyDispatchedRun manually re-invoked the sync
// after the flip, which only proved the finalizer handles an old row, not that a real
// flip triggers convergence; it is replaced by the reconcile coverage.)
