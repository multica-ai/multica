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

// TestWakeDelegatorForZeroRouteComment pins the two structural guarantees of
// the completion-side wake (#9720): a completed delegated worker task's
// zero-route comment wakes the delegator agent exactly once, and a second
// zero-route comment from the same worker task wakes nobody.
func TestWakeDelegatorForZeroRouteComment(t *testing.T) {
	f, svc := seedDelegatedFailureFixture(t)
	ctx := context.Background()
	workerTaskID := f.insertWorkerTask(t, "completed", "comment", 1, 2)

	commentID := f.insertZeroRouteComment(t, workerTaskID, "closing report, no mention")
	comment, err := svc.Queries.GetComment(ctx, commentID)
	if err != nil {
		t.Fatalf("load zero-route comment: %v", err)
	}

	woke, err := svc.WakeDelegatorForZeroRouteComment(ctx, comment)
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

	// Second zero-route comment from the same worker task: no further wake.
	comment2ID := f.insertZeroRouteComment(t, workerTaskID, "one more note")
	comment2, err := svc.Queries.GetComment(ctx, comment2ID)
	if err != nil {
		t.Fatalf("load second zero-route comment: %v", err)
	}
	woke2, err := svc.WakeDelegatorForZeroRouteComment(ctx, comment2)
	if err != nil || woke2 {
		t.Fatalf("second zero-route comment must not wake again: woke=%v err=%v", woke2, err)
	}
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_completion' AND trigger_evidence_ref_id = $1`, workerTaskID).
		Scan(&wakeCount); err != nil {
		t.Fatalf("recount wake tasks: %v", err)
	}
	if wakeCount != 1 {
		t.Fatalf("wake tasks after second comment = %d, want 1", wakeCount)
	}
}
