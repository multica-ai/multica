package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// insertZeroRouteComment seeds an agent-authored closing comment on the worker
// issue with no mention and no other routing signal (#9720).
func (f *delegatedFailureFixture) insertZeroRouteComment(t *testing.T, workerTask pgtype.UUID, body string) pgtype.UUID {
	t.Helper()
	var commentID pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, $4, $5)
		RETURNING id`, f.workspaceID, f.workerIssue, f.worker, body, workerTask).Scan(&commentID); err != nil {
		t.Fatalf("seed zero-route comment: %v", err)
	}
	return commentID
}

// TestWakeDelegatorForCompletedWorker pins the core completion-side wake
// (#9720): when a completed delegated worker task's last agent-authored
// comment resolved to zero triggers, the direct delegator is woken exactly
// once, with the comment as the trigger and the worker task as evidence.
func TestWakeDelegatorForCompletedWorker(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	workerTaskID := f.insertWorkerTask(t, "completed", "comment", 1, 2)

	commentID := f.insertZeroRouteComment(t, workerTaskID, "closing report, no mention")
	comment, err := svc.Queries.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("load zero-route comment: %v", err)
	}
	worker, err := svc.Queries.GetAgentTask(ctx, workerTaskID)
	if err != nil {
		t.Fatalf("load worker task: %v", err)
	}

	woke, err := svc.WakeDelegatorForCompletedWorker(ctx, worker, comment)
	if err != nil {
		t.Fatalf("wake: %v", err)
	}
	if !woke {
		t.Fatal("expected the delegator to be woken")
	}
	var wakeCount int
	var wakeAgent, wakeIssue, triggerComment, evidenceRef, delegatedFrom string
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(agent_id::text), ''), COALESCE(max(issue_id::text), ''),
		       COALESCE(max(trigger_comment_id::text), ''), COALESCE(max(trigger_evidence_ref_id::text), ''),
		       COALESCE(max(delegated_from_task_id::text), '')
		FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_completion' AND trigger_evidence_ref_id = $1`, workerTaskID).
		Scan(&wakeCount, &wakeAgent, &wakeIssue, &triggerComment, &evidenceRef, &delegatedFrom); err != nil {
		t.Fatalf("read wake task: %v", err)
	}
	if wakeCount != 1 {
		t.Fatalf("wake task count = %d, want 1", wakeCount)
	}
	if wakeAgent != f.coordinator || wakeIssue != f.issueID {
		t.Fatalf("wake target = agent %s issue %s, want %s/%s", wakeAgent, wakeIssue, f.coordinator, f.issueID)
	}
	if triggerComment != util.UUIDToString(commentID) || evidenceRef != util.UUIDToString(workerTaskID) ||
		delegatedFrom != util.UUIDToString(workerTaskID) {
		t.Fatalf("wake lineage = trigger %s evidence %s delegated_from %s, want comment %s worker task %s",
			triggerComment, evidenceRef, delegatedFrom, util.UUIDToString(commentID), util.UUIDToString(workerTaskID))
	}
}

// TestWakeDelegatorForCompletedWorkerIsOneShot pins the one-wake-per-worker
// budget: a second wake request for the same worker task must not create a
// second wake task.
func TestWakeDelegatorForCompletedWorkerIsOneShot(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	workerTaskID := f.insertWorkerTask(t, "completed", "comment", 1, 2)

	commentID := f.insertZeroRouteComment(t, workerTaskID, "closing report, no mention")
	comment, err := svc.Queries.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("load zero-route comment: %v", err)
	}
	worker, err := svc.Queries.GetAgentTask(ctx, workerTaskID)
	if err != nil {
		t.Fatalf("load worker task: %v", err)
	}

	if woke, err := svc.WakeDelegatorForCompletedWorker(ctx, worker, comment); err != nil || !woke {
		t.Fatalf("first wake: woke=%v err=%v", woke, err)
	}

	// A second wake request (any comment, same worker task) is already spent.
	comment2ID := f.insertZeroRouteComment(t, workerTaskID, "one more note")
	comment2, err := svc.Queries.GetComment(ctx, comment2ID)
	if err != nil {
		t.Fatalf("load second comment: %v", err)
	}
	woke2, err := svc.WakeDelegatorForCompletedWorker(ctx, worker, comment2)
	if err != nil || woke2 {
		t.Fatalf("second wake must not fire: woke=%v err=%v", woke2, err)
	}
	var wakeCount int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_completion' AND trigger_evidence_ref_id = $1`, workerTaskID).
		Scan(&wakeCount); err != nil {
		t.Fatalf("recount wake tasks: %v", err)
	}
	if wakeCount != 1 {
		t.Fatalf("wake tasks after second request = %d, want 1", wakeCount)
	}
}

// TestWakeDelegatorForCompletedWorkerStopsPropagation pins the one-hop
// propagation stop: a coordinator run that was itself spawned as a completion
// wake (its task carries the delegated_completion evidence kind) must never
// hand control further up, and a worker that a wake run delegated to must not
// wake it back — every A→B→A ping-pong is capped at one hop (#8753 concern).
func TestWakeDelegatorForCompletedWorkerStopsPropagation(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()

	// A worker task delegated from the coordinator's task, where the
	// coordinator task was itself spawned as a completion wake (marked by its
	// evidence kind).
	workerTaskID := f.insertWorkerTask(t, "completed", "comment", 1, 2)
	if _, err := f.pool.Exec(ctx, `
		UPDATE agent_task_queue SET trigger_evidence_kind = 'delegated_completion' WHERE id = $1`, f.sourceTask); err != nil {
		t.Fatalf("mark source task evidence: %v", err)
	}
	commentID := f.insertZeroRouteComment(t, workerTaskID, "wake run closing report")
	comment, err := svc.Queries.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	worker, err := svc.Queries.GetAgentTask(ctx, workerTaskID)
	if err != nil {
		t.Fatalf("load worker task: %v", err)
	}
	woke, err := svc.WakeDelegatorForCompletedWorker(ctx, worker, comment)
	if err != nil {
		t.Fatalf("wake: %v", err)
	}
	if woke {
		t.Fatal("a worker delegated from a wake run must not wake the wake run again")
	}
}

// TestWakeDelegatorForCompletedWorkerIgnoresFailedWorker pins the boundary
// with the failure path: a failed delegated worker has its own durable
// recovery (delegated_failure), and a failed row must never take the
// completion-wake path.
func TestWakeDelegatorForCompletedWorkerIgnoresFailedWorker(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	workerTaskID := f.insertWorkerTask(t, "failed", "comment", 1, 2)

	commentID := f.insertZeroRouteComment(t, workerTaskID, "report from a failed run")
	comment, err := svc.Queries.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	worker, err := svc.Queries.GetAgentTask(ctx, workerTaskID)
	if err != nil {
		t.Fatalf("load worker task: %v", err)
	}
	woke, err := svc.WakeDelegatorForCompletedWorker(ctx, worker, comment)
	if err != nil || woke {
		t.Fatalf("failed worker must not wake: woke=%v err=%v", woke, err)
	}
}
