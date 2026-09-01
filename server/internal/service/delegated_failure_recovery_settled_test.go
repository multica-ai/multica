package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedRecoverySignal drives one delegated failure to the point where the
// durable recovery comment and its coordinator task exist, and returns both.
func (f *delegatedFailureFixture) seedRecoverySignal(t *testing.T, svc *TaskService) (recoveryTaskID, recoveryCommentID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	failedID := f.insertWorkerTask(t, "failed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET failure_reason = 'agent_error.process_failure', error = 'worker exited', completed_at = now()
		WHERE id = $1`, failedID); err != nil {
		t.Fatalf("stamp failed task: %v", err)
	}
	failed, err := svc.Queries.GetAgentTask(ctx, failedID)
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	if handled, err := svc.recoverDelegatedTaskFailure(ctx, failed); err != nil || !handled {
		t.Fatalf("initial recovery = handled %v err %v", handled, err)
	}
	if err := f.pool.QueryRow(ctx, `
		SELECT task.id, recovery.id
		FROM agent_task_queue task
		JOIN comment recovery ON recovery.id = task.trigger_comment_id
		WHERE task.trigger_evidence_kind = 'delegated_failure'
		  AND task.trigger_evidence_ref_id = $1`, failedID).Scan(&recoveryTaskID, &recoveryCommentID); err != nil {
		t.Fatalf("load recovery task/comment: %v", err)
	}
	return recoveryTaskID, recoveryCommentID
}

func (f *delegatedFailureFixture) settled(t *testing.T, commentID pgtype.UUID) bool {
	t.Helper()
	var marked bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT recovery_settled_at IS NOT NULL FROM comment WHERE id = $1`, commentID).Scan(&marked); err != nil {
		t.Fatalf("read recovery_settled_at: %v", err)
	}
	return marked
}

// markDelivered simulates the dispatch receipt a daemon writes before it runs
// the coordinator task, and puts the task in the status the terminal callbacks
// expect.
func (f *delegatedFailureFixture) markDelivered(t *testing.T, taskID, commentID pgtype.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET status = 'running', started_at = now(), dispatched_at = now(),
		    delivered_comment_ids = ARRAY[$2]::uuid[]
		WHERE id = $1`, taskID, commentID); err != nil {
		t.Fatalf("mark recovery delivered: %v", err)
	}
}

// A coordinator task that received the recovery comment and then reached a
// terminal status has consumed the obligation. The marker is what lets the
// outbox scan skip that comment through the partial index instead of
// re-proving it settled through four joins on every tick.
func TestTerminalCoordinatorTaskSettlesDeliveredRecovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		finalize func(t *testing.T, svc *TaskService, taskID pgtype.UUID)
	}{
		{"complete", func(t *testing.T, svc *TaskService, taskID pgtype.UUID) {
			if _, err := svc.CompleteTask(context.Background(), taskID, []byte(`{"ok":true}`), "", "", "", false, "", ""); err != nil {
				t.Fatalf("CompleteTask: %v", err)
			}
		}},
		{"fail", func(t *testing.T, svc *TaskService, taskID pgtype.UUID) {
			if _, err := svc.FailTask(context.Background(), taskID, "coordinator crashed", "", "", "", "agent_error.process_failure", false, "", ""); err != nil {
				t.Fatalf("FailTask: %v", err)
			}
		}},
		{"cancel", func(t *testing.T, svc *TaskService, taskID pgtype.UUID) {
			if _, err := svc.CancelTask(context.Background(), taskID); err != nil {
				t.Fatalf("CancelTask: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, svc := seedDelegatedFailureFixture(t)
			ctx := context.Background()
			recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)
			f.markDelivered(t, recoveryTaskID, recoveryCommentID)

			if f.settled(t, recoveryCommentID) {
				t.Fatal("recovery settled before the coordinator task reached a terminal status")
			}
			tc.finalize(t, svc, recoveryTaskID)

			if !f.settled(t, recoveryCommentID) {
				t.Fatalf("delivered recovery not settled after %s", tc.name)
			}
			pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(ctx, 100)
			if err != nil {
				t.Fatalf("ListPendingDelegatedFailureRecoveries: %v", err)
			}
			for _, c := range pending {
				if c.ID == recoveryCommentID {
					t.Fatalf("settled recovery %s still scanned as pending", util.UUIDToString(recoveryCommentID))
				}
			}
		})
	}
}

// The marker must never run ahead of delivery. A task that only planned the
// comment and was cancelled automatically leaves the obligation open, and the
// sweeper still has to replay it — this is the regression that would silently
// drop crash recovery.
func TestPlannedButUndeliveredRecoveryStaysPending(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)

	if _, err := svc.CancelTask(ctx, recoveryTaskID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if f.settled(t, recoveryCommentID) {
		t.Fatal("automatic cancellation settled a recovery it never delivered")
	}

	result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
	if err != nil {
		t.Fatalf("RecoverPendingDelegatedFailures: %v", err)
	}
	if result.Replayed != 1 {
		t.Fatalf("sweep after undelivered cancellation = %+v, want one replay", result)
	}
}

// The user's explicit cancellation is a terminal acknowledgement: the receipt
// is appended and the comment retires in the same transaction.
func TestUserCancelledRecoveryIsSettled(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)

	if _, err := svc.CancelTaskByUser(ctx, recoveryTaskID); err != nil {
		t.Fatalf("CancelTaskByUser: %v", err)
	}
	if !f.settled(t, recoveryCommentID) {
		t.Fatal("user cancellation did not settle the recovery signal")
	}
	if result, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil || result != (DelegatedFailureRecoverySweepResult{}) {
		t.Fatalf("sweep after user cancel = %+v, %v; want zero result, nil", result, err)
	}
}

// Exhaustion writes its receipt onto an attempt row that may still be running,
// so the task-scoped settle cannot see it. It has to retire the comment
// directly or the outbox keeps re-checking a permanently stopped obligation.
func TestExhaustedRecoveryIsSettled(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	_, recoveryCommentID := f.seedRecoverySignal(t, svc)

	for attempt := 1; attempt <= delegatedFailureRecoveryMaxTaskAttempts; attempt++ {
		var currentTaskID pgtype.UUID
		if err := f.pool.QueryRow(ctx, `
			SELECT id FROM agent_task_queue
			WHERE trigger_comment_id = $1 AND status = 'queued'
			ORDER BY created_at DESC, id DESC
			LIMIT 1`, recoveryCommentID).Scan(&currentTaskID); err != nil {
			t.Fatalf("load recovery attempt %d: %v", attempt, err)
		}
		if _, err := f.pool.Exec(ctx, `
			UPDATE agent_task_queue
			SET status = 'failed', completed_at = now(), failure_reason = 'queued_expired',
			    delivered_comment_ids = '{}'
			WHERE id = $1`, currentTaskID); err != nil {
			t.Fatalf("fail recovery attempt %d: %v", attempt, err)
		}
		if _, err := svc.RecoverPendingDelegatedFailures(ctx, 100); err != nil {
			t.Fatalf("recovery sweep after attempt %d: %v", attempt, err)
		}
	}

	if !f.settled(t, recoveryCommentID) {
		t.Fatal("exhausted recovery was not settled")
	}
	pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(ctx, 100)
	if err != nil {
		t.Fatalf("ListPendingDelegatedFailureRecoveries: %v", err)
	}
	for _, c := range pending {
		if c.ID == recoveryCommentID {
			t.Fatal("exhausted recovery still scanned as pending")
		}
	}
}

// A manual rerun clears the pending slot with its own bulk cancel. That
// statement terminates a coordinator task that may already hold a recovery
// receipt, so it has to settle like every other terminal path — otherwise the
// row stays in the unsettled index forever and the index regrows the unbounded
// history this change set removes.
func TestManualRerunSettlesDeliveredRecovery(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)

	// The rerun path only clears tasks that have not begun executing, so leave
	// the coordinator task queued and give it the dispatch receipt.
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue SET delivered_comment_ids = ARRAY[$2]::uuid[] WHERE id = $1`,
		recoveryTaskID, recoveryCommentID); err != nil {
		t.Fatalf("mark recovery delivered: %v", err)
	}

	issueID, err := util.ParseUUID(f.issueID)
	if err != nil {
		t.Fatalf("parse issue id: %v", err)
	}
	actorID, err := util.ParseUUID(f.userID)
	if err != nil {
		t.Fatalf("parse actor id: %v", err)
	}
	if _, err := svc.RerunIssue(ctx, issueID, pgtype.UUID{}, pgtype.UUID{}, actorID, func(db.Agent) bool { return true }); err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}

	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, recoveryTaskID).Scan(&status); err != nil {
		t.Fatalf("read coordinator task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("rerun left the pending coordinator task in %q; the test no longer covers the cancel path", status)
	}
	if !f.settled(t, recoveryCommentID) {
		t.Fatal("manual rerun cancelled a task holding a recovery receipt without settling it")
	}
}

// Archiving an agent cancels its tasks with a bulk statement. The cancel and
// the settlement have to commit together: CaptureCancelledTasks runs after the
// commit, so a settlement there could neither be rolled back nor repaired.
func TestArchivedAgentCancelSettlesDeliveredRecovery(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)
	f.markDelivered(t, recoveryTaskID, recoveryCommentID)

	agentID, err := util.ParseUUID(f.coordinator)
	if err != nil {
		t.Fatalf("parse agent id: %v", err)
	}
	cancelled, err := svc.CancelTasksForArchivedAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("CancelTasksForArchivedAgent: %v", err)
	}
	if len(cancelled) == 0 {
		t.Fatal("archive cancelled nothing; the test no longer covers the cancel path")
	}
	if !f.settled(t, recoveryCommentID) {
		t.Fatal("archive cancellation did not settle the delivered recovery receipt")
	}
}

// The settlement marker is monotonic — nothing can undo it — so the terminal
// requirement has to live in the SQL rather than in caller discipline. A
// dispatched task's receipt is still replaceable by a reclaiming daemon, so
// settling one would freeze that window into a permanently lost recovery.
func TestNonTerminalTaskWithAReceiptIsNotSettled(t *testing.T) {
	for _, status := range []string{"dispatched", "running"} {
		t.Run(status, func(t *testing.T) {
			f, svc := seedDelegatedFailureFixture(t)
			ctx := context.Background()
			recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)
			if _, err := f.pool.Exec(ctx, `
				UPDATE agent_task_queue
				SET status = $2, dispatched_at = now(), delivered_comment_ids = ARRAY[$3]::uuid[]
				WHERE id = $1`, recoveryTaskID, status, recoveryCommentID); err != nil {
				t.Fatalf("stage %s task: %v", status, err)
			}

			task, err := svc.Queries.GetAgentTask(ctx, recoveryTaskID)
			if err != nil {
				t.Fatalf("load task: %v", err)
			}
			if err := SettleDeliveredDelegatedFailureRecoveries(ctx, svc.Queries, task); err != nil {
				t.Fatalf("SettleDeliveredDelegatedFailureRecoveries: %v", err)
			}
			if f.settled(t, recoveryCommentID) {
				t.Fatalf("a %s task's receipt was settled; a reclaim would now lose the recovery", status)
			}
		})
	}
}

// The atomicity that replaced the old best-effort call: if settlement cannot
// commit, the terminal write must not commit either. Otherwise the stranded
// receipt is unreachable — the outbox scan excludes a comment whose covering
// task is terminal and holds it, so it would never replay and never settle.
func TestBulkCancelRollsBackWhenSettlementFails(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	recoveryTaskID, recoveryCommentID := f.seedRecoverySignal(t, svc)
	f.markDelivered(t, recoveryTaskID, recoveryCommentID)

	// A trigger that fails only on the settlement UPDATE, so the cancel inside
	// the same transaction has to roll back with it.
	if _, err := f.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION reject_recovery_settlement() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'settlement rejected'; END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create settlement trigger function: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		CREATE TRIGGER reject_recovery_settlement
		BEFORE UPDATE OF recovery_settled_at ON comment
		FOR EACH ROW EXECUTE FUNCTION reject_recovery_settlement()`); err != nil {
		t.Fatalf("install settlement trigger: %v", err)
	}
	// Drop the function as well as the trigger: the test database is shared, so
	// a leftover reject_recovery_settlement() would outlive this test and stay
	// attachable to comment by any later run.
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = f.pool.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS reject_recovery_settlement ON comment`)
		_, _ = f.pool.Exec(cleanupCtx, `DROP FUNCTION IF EXISTS reject_recovery_settlement()`)
	})

	agentID, err := util.ParseUUID(f.coordinator)
	if err != nil {
		t.Fatalf("parse agent id: %v", err)
	}
	if _, err := svc.CancelTasksForArchivedAgent(ctx, agentID); err == nil {
		t.Fatal("bulk cancel reported success while its settlement failed")
	}

	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, recoveryTaskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status == "cancelled" {
		t.Fatal("the cancel committed without its settlement, stranding the receipt permanently")
	}
	if f.settled(t, recoveryCommentID) {
		t.Fatal("settlement committed despite the failure")
	}
}
