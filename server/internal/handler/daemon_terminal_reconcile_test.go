package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// claimOnWakeupNotifier is a test TaskWakeupNotifier that immediately advances
// the notified task from queued → dispatched, simulating the daemon claiming
// the retry child the instant it is woken. This reproduces the race that the
// "reconcile before notify" ordering fix was written to prevent.
type claimOnWakeupNotifier struct {
	pool   *pgxpool.Pool
	called bool
}

func (n *claimOnWakeupNotifier) NotifyTaskAvailable(_, taskID string) {
	if taskID == "" {
		return
	}
	_, _ = n.pool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1 AND status = 'queued'`,
		taskID)
	n.called = true
}

// mentionInAnyActivePlanForAgent reports whether commentID appears in the
// trigger or coalesced plan of ANY task for the (issue, agent) pair regardless
// of status. Used by the race regression test where the retry child may have
// been advanced to dispatched by the instant-claim wakeup notifier.
func mentionInAnyActivePlanForAgent(t *testing.T, issueID, agentID, commentID string) bool {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND ($3::uuid = ANY(coalesced_comment_ids) OR trigger_comment_id = $3::uuid)
	`, issueID, agentID, commentID).Scan(&n); err != nil {
		t.Fatalf("query mention in any plan: %v", err)
	}
	return n > 0
}

// These tests pin the #5278 invariant across EVERY terminal path that should
// allow subsequent work: an @agent hand-off dropped while the target had an
// in-flight task (merge-miss + active-task deferral) must still be replayed when
// that run ends by fail / user-cancel / sweeper-fail — not only on success.
// Reconciliation now runs inside the TaskService terminal transitions via the
// injected ReconcileTerminal seam, so these drive the real service/handler paths.

// failTaskWithReasonViaHandler drives the daemon FailTask endpoint with an
// explicit failure_reason (the shared failTaskViaHandler helper hardcodes a
// non-retryable agent_error; the retryable path here needs e.g. "timeout").
func failTaskWithReasonViaHandler(t *testing.T, taskID, failureReason string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail",
		map[string]any{"error": "boom", "failure_reason": failureReason},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.FailTask(w, req)
	return w
}

// cancelTaskViaHandler drives the issue-scoped daemon CancelTask endpoint
// (POST /api/issues/{id}/tasks/{taskId}/cancel).
func cancelTaskViaHandler(t *testing.T, issueID, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/tasks/"+taskID+"/cancel", nil)
	req = withURLParams(req, "id", issueID, "taskId", taskID)
	testHandler.CancelTask(w, req)
	return w
}

// mentionInQueuedPlanForAgent reports whether commentID appears in the trigger
// or coalesced plan of any QUEUED task for the (issue, agent) pair — i.e. the
// dropped mention was merged into a queued run (retry child or fresh follow-up).
func mentionInQueuedPlanForAgent(t *testing.T, issueID, agentID, commentID string) bool {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND ($3::uuid = ANY(coalesced_comment_ids) OR trigger_comment_id = $3::uuid)
	`, issueID, agentID, commentID).Scan(&n); err != nil {
		t.Fatalf("query mention in queued plan: %v", err)
	}
	return n > 0
}

// markTaskFailedDirect transitions a task to failed the way the sweepers and
// orphan recovery do — a direct row update — then returns the reloaded row to
// hand to TaskService.HandleFailedTasks, exactly as those pipelines do.
func markTaskFailedDirect(t *testing.T, taskID, reason string) db.AgentTaskQueue {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'failed', failure_reason = $2 WHERE id = $1`,
		taskID, reason); err != nil {
		t.Fatalf("mark task failed: %v", err)
	}
	row, err := testHandler.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("reload failed task: %v", err)
	}
	return row
}

// seedDroppedAgentMention reproduces the #5278 precondition: agent B has a
// DISPATCHED task on an issue when agent A posts an explicit @B mention, which
// hits the merge-miss + active-task drop at creation time and is deferred to
// terminal reconciliation. Returns the issue, B's (running) task, both agent
// ids, and the dropped mention's comment id, with the drop already asserted.
func seedDroppedAgentMention(t *testing.T, issueNumber int32) (issueID, taskID, agentA, agentB, mentionCommentID string) {
	t.Helper()
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("setup: get runtime: %v", err)
	}
	agentA = createHandlerTestAgent(t, "Terminal Reconcile Author A", nil)
	agentB = createHandlerTestAgent(t, "Terminal Reconcile Target B", nil)

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'terminal-reconcile fixture', 'in_progress', 'none', $2, 'member', $3, 0, 'agent', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, issueNumber, agentB).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID) })

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("setup: load issue: %v", err)
	}

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	// B's task is DISPATCHED — the state that makes an incoming mention hit the
	// merge-miss + active-task drop at creation time.
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, dispatched_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'dispatched', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentB, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: dispatched task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	// Agent A posts an explicit @B mention through the real trigger path while B
	// is dispatched.
	mention := "[@B](mention://agent/" + agentB + ") please also handle this"
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'agent', $3, $4, 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, agentA, mention).Scan(&mentionCommentID); err != nil {
		t.Fatalf("setup: agent @B comment: %v", err)
	}
	mentionComment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(mentionCommentID))
	if err != nil {
		t.Fatalf("setup: load mention comment: %v", err)
	}
	testHandler.triggerTasksForComment(ctx, issue, mentionComment, nil, "agent", agentA, "", "", nil)

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("expected the mention to be dropped at creation (0 queued follow-up), got %d", n)
	}

	// Advance B's task dispatched → running so the terminal endpoint has a live
	// run to end.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '1 minute' WHERE id = $1`, taskID); err != nil {
		t.Fatalf("advance task to running: %v", err)
	}
	return issueID, taskID, agentA, agentB, mentionCommentID
}

// TestFailTask_ReconcilesDroppedAgentMentionOnTerminalFail — non-retryable fail
// (agent_error) leaves no retry child, so the dropped mention earns exactly one
// fresh follow-up for B. Author A is never enqueued (loop guard).
func TestFailTask_ReconcilesDroppedAgentMentionOnTerminalFail(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999101)

	if w := failTaskViaHandler(t, taskID); w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 fresh follow-up for B after fail-path reconcile, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention not present in the follow-up's plan")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestFailTask_RetryableFail_MergesDroppedMentionIntoRetryChild — a retryable
// fail (timeout) creates the auto-retry child first; reconcile then merges the
// dropped mention INTO that single queued child rather than spawning a duplicate.
func TestFailTask_RetryableFail_MergesDroppedMentionIntoRetryChild(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999103)

	if w := failTaskWithReasonViaHandler(t, taskID, "timeout"); w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Exactly one queued child (the auto-retry), and the mention is folded into it.
	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 queued retry child, got %d (duplicate follow-up?)", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention not merged into the retry child's plan")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestCancelTask_ReconcilesDroppedAgentMentionOnCancel covers the issue-scoped
// daemon cancel route.
func TestCancelTask_ReconcilesDroppedAgentMentionOnCancel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999102)

	if w := cancelTaskViaHandler(t, issueID, taskID); w.Code != http.StatusOK {
		t.Fatalf("CancelTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for B after cancel-path reconcile, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention not present in the follow-up's plan")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestCancelTaskByUser_ReconcilesDroppedAgentMention is the maintainer's
// blocking case #1: the generic user-facing cancel route (POST
// /api/tasks/{taskId}/cancel, used by CLI / mobile / Chat / Activity) must also
// reconcile an issue-bound task's dropped hand-off.
func TestCancelTaskByUser_ReconcilesDroppedAgentMention(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999104)

	w := httptest.NewRecorder()
	testHandler.CancelTaskByUser(w, cancelTaskByUserRequest(t, testUserID, taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("CancelTaskByUser: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for B after user-cancel reconcile, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention not present in the follow-up's plan")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestHandleFailedTasks_RuntimeOffline_MergesDroppedMentionIntoRetryChild is the
// maintainer's blocking case #2/#3: the sweeper / offline-runtime / orphan-
// recovery pipeline (direct row update → TaskService.HandleFailedTasks) creates
// the retry child and then reconciles, folding the dropped mention into it.
func TestHandleFailedTasks_RuntimeOffline_MergesDroppedMentionIntoRetryChild(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999105)

	row := markTaskFailedDirect(t, taskID, "runtime_offline")
	testHandler.TaskService.HandleFailedTasks(context.Background(), []db.AgentTaskQueue{row})

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 queued retry child from the sweeper pipeline, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention not merged into the sweeper retry child's plan")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestHandleFailedTasks_NonRetryable_FreshFollowup is the maintainer's case #4:
// with no retry child (non-retryable reason), the sweeper pipeline still yields
// exactly one fresh follow-up carrying the dropped mention.
func TestHandleFailedTasks_NonRetryable_FreshFollowup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999106)

	row := markTaskFailedDirect(t, taskID, "agent_error")
	testHandler.TaskService.HandleFailedTasks(context.Background(), []db.AgentTaskQueue{row})

	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 fresh follow-up (no retry child), got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention not present in the fresh follow-up's plan")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestFailTask_IdempotentCallback_NoDuplicateFollowup is the maintainer's case
// #5: a repeated terminal callback must not introduce a second task or a
// duplicate coalesced comment id. The second FailTask hits the already-finalized
// early-return, so the reconciler never fires twice.
func TestFailTask_IdempotentCallback_NoDuplicateFollowup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, agentA, agentB, mention := seedDroppedAgentMention(t, 999107)

	if w := failTaskViaHandler(t, taskID); w.Code != http.StatusOK {
		t.Fatalf("FailTask #1: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := failTaskViaHandler(t, taskID); w.Code != http.StatusOK {
		t.Fatalf("FailTask #2: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Still exactly one follow-up — no duplicate concurrent run.
	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 follow-up after a repeated terminal callback, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention missing from the follow-up's plan")
	}
	// The mention appears across the queued plans exactly once — no duplicate
	// coalesced comment id from the second callback.
	var occurrences int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM agent_task_queue t, unnest(t.coalesced_comment_ids) AS c
		WHERE t.issue_id = $1 AND t.agent_id = $2 AND t.status = 'queued' AND c = $3::uuid
	`, issueID, agentB, mention).Scan(&occurrences); err != nil {
		t.Fatalf("count coalesced occurrences: %v", err)
	}
	if occurrences > 1 {
		t.Fatalf("dropped mention duplicated in the coalesced plan: %d occurrences", occurrences)
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("comment author A must not be enqueued, got %d A task(s)", n)
	}
}

// TestFailTask_RetryChildRace_MentionMergedBeforeWakeup is the race regression
// for the "reconcile before notify" ordering fix in FailTask. A wakeup notifier
// that immediately claims the retry child (advancing it to dispatched) is
// installed before FailTask runs. With the fix in place, reconcileTerminal
// merges the dropped mention into the queued child BEFORE NotifyTaskEnqueued
// wakes the daemon, so the mention survives even though the child is instantly
// dispatched.
func TestFailTask_RetryChildRace_MentionMergedBeforeWakeup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, _, agentB, mention := seedDroppedAgentMention(t, 999108)

	notifier := &claimOnWakeupNotifier{pool: testPool}
	orig := testHandler.TaskService.Wakeup
	testHandler.TaskService.Wakeup = notifier
	t.Cleanup(func() { testHandler.TaskService.Wakeup = orig })

	if w := failTaskWithReasonViaHandler(t, taskID, "timeout"); w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !notifier.called {
		t.Fatal("instant-claim notifier was never called — race scenario not exercised")
	}
	// The mention must be present even though the child was instantly dispatched.
	if !mentionInAnyActivePlanForAgent(t, issueID, agentB, mention) {
		t.Fatal("dropped mention lost — reconcile ran after notify; ordering fix not effective")
	}
}

// TestFailTask_IdempotentCallback_RecoveryFromSkippedReconcile covers the
// idempotency path introduced by the second review: if the first FailTask
// commits the terminal row but reconcileTerminal is skipped (simulating a
// process crash between commit and reconcile), the repeated callback must
// still produce exactly one follow-up carrying the dropped mention.
func TestFailTask_IdempotentCallback_RecoveryFromSkippedReconcile(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, _, agentB, mention := seedDroppedAgentMention(t, 999109)

	origReconcile := testHandler.TaskService.ReconcileTerminal
	testHandler.TaskService.ReconcileTerminal = nil
	// Safety net: restore even if a panic fires between now and the inline restore
	// below. Without this, a panic leaves the shared testHandler corrupted for
	// every subsequent test in the binary run.
	t.Cleanup(func() { testHandler.TaskService.ReconcileTerminal = origReconcile })
	if w := failTaskViaHandler(t, taskID); w.Code != http.StatusOK {
		t.Fatalf("FailTask #1 (no reconcile): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("expected 0 follow-ups before recovery, got %d", n)
	}

	// Restore before the second call so the early-exit path can reconcile.
	testHandler.TaskService.ReconcileTerminal = origReconcile

	// Second call hits the already-finalized early-exit, which calls
	// reconcileTerminal on the existing row → recovery.
	if w := failTaskViaHandler(t, taskID); w.Code != http.StatusOK {
		t.Fatalf("FailTask #2 (recovery): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 follow-up after idempotent recovery, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention missing from the recovery follow-up")
	}
}

// TestCompleteTask_IdempotentCallback_RecoveryFromSkippedReconcile covers the
// same idempotency recovery scenario on the CompleteTask path.
func TestCompleteTask_IdempotentCallback_RecoveryFromSkippedReconcile(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID, taskID, _, agentB, mention := seedDroppedAgentMention(t, 999110)

	origReconcile := testHandler.TaskService.ReconcileTerminal
	testHandler.TaskService.ReconcileTerminal = nil
	// Safety net: restore even if a panic fires between now and the inline restore
	// below. Without this, a panic leaves the shared testHandler corrupted for
	// every subsequent test in the binary run.
	t.Cleanup(func() { testHandler.TaskService.ReconcileTerminal = origReconcile })
	if _, err := testHandler.TaskService.CompleteTask(context.Background(),
		parseUUID(taskID), completeResult(t, "done"), "", ""); err != nil {
		t.Fatalf("CompleteTask #1 (no reconcile): %v", err)
	}
	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("expected 0 follow-ups before recovery, got %d", n)
	}

	// Restore before the second call so the early-exit path can reconcile.
	testHandler.TaskService.ReconcileTerminal = origReconcile

	if _, err := testHandler.TaskService.CompleteTask(context.Background(),
		parseUUID(taskID), completeResult(t, "done"), "", ""); err != nil {
		t.Fatalf("CompleteTask #2 (recovery): %v", err)
	}
	if n := queuedTaskCountForAgentIssue(t, issueID, agentB); n != 1 {
		t.Fatalf("expected exactly 1 follow-up after idempotent recovery, got %d", n)
	}
	if !mentionInQueuedPlanForAgent(t, issueID, agentB, mention) {
		t.Fatalf("dropped mention missing from the recovery follow-up")
	}
}
