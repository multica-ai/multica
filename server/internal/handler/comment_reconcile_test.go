package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// completeTaskViaHandler drives the daemon CompleteTask endpoint for taskID.
func completeTaskViaHandler(t *testing.T, taskID, output string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	return w
}

// pendingTaskCountForAgentIssue counts claimable (queued/dispatched) tasks for
// an (issue, agent) pair.
func pendingTaskCountForAgentIssue(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`,
		issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	return n
}

// TestCompleteTask_ReconcilesMemberCommentPostedDuringRun proves the MUL-4195
// completion-reconciliation guarantee: a deliberate member comment that lands
// while the agent is busy (after the run's started_at) must earn a follow-up
// run instead of being silently lost.
func TestCompleteTask_ReconcilesMemberCommentPostedDuringRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	// Issue assigned to the agent so a plain member comment routes to it.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-e2e fixture', 'in_progress', 'none', $2, 'member', 999001, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Trigger comment created BEFORE the run starts.
	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	// A running task whose started_at is in the past.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	// A deliberate member comment that arrived DURING the run (after started_at).
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'wait, also handle this', 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: mid-run member comment: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// A follow-up run must now be queued for the agent.
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up task after reconciliation, got %d", n)
	}
}

// TestCompleteTask_NoReconcileWhenNoNewMemberComment guards against spurious
// follow-ups: when no member comment arrived after the run started, completion
// must not enqueue any new task.
func TestCompleteTask_NoReconcileWhenNoNewMemberComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-negative fixture', 'in_progress', 'none', $2, 'member', 999002, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'the only request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 0 {
		t.Fatalf("expected no follow-up task when no new member comment, got %d", n)
	}
}

// TestCompleteTask_DoesNotReTriggerOtherAgentMentionedDuringRun is the MUL-4195
// review must-fix #2 regression test. Agent A is running on an issue when a
// member posts a comment that @-mentions a DIFFERENT agent B. B is triggered at
// comment-creation time (not exercised here). When A's run completes, the
// completion reconcile must NOT replay that comment through the full trigger
// pipeline and spawn a SECOND B run — reconcile is scoped to the agent that
// just ran (A). Before the fix, reconcile fanned the latest member comment out
// to every routed agent, so completing A re-woke B (and any other agent the
// comment mentioned), breaking the bounded-follow-up guarantee.
func TestCompleteTask_DoesNotReTriggerOtherAgentMentionedDuringRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentA, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentA, &runtimeID); err != nil {
		t.Fatalf("setup: get agent A: %v", err)
	}
	// A second, workspace-invocable agent that a member can @mention.
	agentB := createHandlerTestAgent(t, "Reconcile Other Agent B", nil)

	// Issue assigned to A so A's completion is the one that reconciles.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'reconcile-other-agent fixture', 'in_progress', 'none', $2, 'member', 999003, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentA).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// A's trigger comment, created before the run starts.
	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	// A running task for A whose started_at is in the past.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, $4, 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentA, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	// A member comment posted DURING A's run that @-mentions agent B.
	mention := "[@B](mention://agent/" + agentB + ") please take a look"
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - interval '1 minute')
	`, issueID, testWorkspaceID, testUserID, mention); err != nil {
		t.Fatalf("setup: mid-run @B comment: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The @B comment routes to B, not A, so scoping reconcile to A means
	// NEITHER agent gets a completion-driven follow-up. B in particular must
	// not be re-woken by A's completion.
	if n := pendingTaskCountForAgentIssue(t, issueID, agentB); n != 0 {
		t.Fatalf("agent B must not be re-triggered by agent A's completion, got %d B task(s)", n)
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentA); n != 0 {
		t.Fatalf("agent A must not enqueue a follow-up for a comment addressed to B, got %d A task(s)", n)
	}
}

// handlerWorkspaceMember inserts a fresh user + workspace member and returns
// the user id (for a second distinct originator).
func handlerWorkspaceMember(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := slug + "-" + time.Now().Format("150405.000000") + "@example.test"
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reconcile Test "+slug, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`,
		testWorkspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// TestConsecutiveCommentsDifferentOriginatorsFullEnqueuePath is the MUL-4195
// second-round must-fix #1 regression test, driving the FULL handler enqueue
// path (computeCommentAgentTriggers → enqueueCommentAgentTriggers → merge), not
// just the SQL. Member A's comment creates a queued task; member B (a different
// originator) then comments before the run starts. The earlier build returned
// ErrNoRows from the originator gate and fell through to a fresh enqueue that
// tripped the one-pending-per-(issue,agent) unique index, silently dropping B's
// comment. With recompute-on-merge, B's comment folds into the single task:
// still one task (no drop, no collision), trigger repointed to B, originator
// re-stamped to B, and A's comment preserved as coalesced.
func TestConsecutiveCommentsDifferentOriginatorsFullEnqueuePath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}
	userB := handlerWorkspaceMember(t, "originatorB")

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'diff-originator fixture', 'in_progress', 'none', $2, 'member', 999004, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	insertMemberComment := func(authorID, content string) db.Comment {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'member', $3, $4, 'comment') RETURNING id
		`, issueID, testWorkspaceID, authorID, content).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		c, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(id))
		if err != nil {
			t.Fatalf("load comment: %v", err)
		}
		return c
	}

	// A's comment → creates the queued task (originator A).
	cA := insertMemberComment(testUserID, "first, from A")
	testHandler.triggerTasksForComment(ctx, issue, cA, nil, "member", testUserID, testUserID, nil)
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("after A's comment expected exactly 1 queued task, got %d", n)
	}

	// B's comment (different originator) before start → must fold in, NOT drop.
	cB := insertMemberComment(userB, "second, from B — different user")
	testHandler.triggerTasksForComment(ctx, issue, cB, nil, "member", userB, userB, nil)

	// Still exactly one task (bounded concurrency, no unique-index collision).
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("after B's comment expected still exactly 1 task (folded in, not dropped/duplicated), got %d", n)
	}
	// Trigger repointed to B, originator re-stamped to B, A coalesced.
	trigger, originator, coalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
	if trigger != uuidToString(cB.ID) {
		t.Errorf("expected trigger repointed to B's comment %s, got %s", uuidToString(cB.ID), trigger)
	}
	if originator != userB {
		t.Errorf("expected originator re-stamped to B (%s), got %s", userB, originator)
	}
	if !containsUUID(coalesced, uuidToString(cA.ID)) {
		t.Errorf("expected A's comment %s preserved as coalesced, got %v", uuidToString(cA.ID), coalesced)
	}
}

// TestCompleteTask_ReconcilesDispatchedWindowComment is the MUL-4195
// second-round must-fix #2 regression test. A member comment that lands AFTER
// the claim response was built (after dispatched_at) but BEFORE StartTask
// (before started_at) must still earn a follow-up. The earlier reconcile
// anchored on started_at and missed this window; anchoring on dispatched_at
// catches it.
func TestCompleteTask_ReconcilesDispatchedWindowComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'dispatched-window fixture', 'in_progress', 'none', $2, 'member', 999005, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'initial request', 'comment', now() - interval '10 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("setup: trigger comment: %v", err)
	}

	// Running task: dispatched 5m ago, started 2m ago. The claim response was
	// built at dispatch.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'running', 0, now() - interval '10 minutes', now() - interval '5 minutes', now() - interval '2 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	// A member comment in the dispatch→start window: after dispatched_at
	// (5m ago), before started_at (2m ago). A started_at anchor would miss it.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, 'squeezed in before start', 'comment', now() - interval '3 minutes')
	`, issueID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: dispatch-window comment: %v", err)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for the dispatch-window comment, got %d", n)
	}
}

// taskTriggerOriginatorCoalesced returns (trigger_comment_id, originator_user_id,
// coalesced_comment_ids) as text for the most recent task of (issue, agent).
func taskTriggerOriginatorCoalesced(t *testing.T, issueID, agentID string) (string, string, []string) {
	t.Helper()
	var trigger, originator string
	var coalesced []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(trigger_comment_id::text, ''),
		       COALESCE(originator_user_id::text, ''),
		       coalesced_comment_ids::text[]
		  FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2
		 ORDER BY created_at DESC
		 LIMIT 1
	`, issueID, agentID).Scan(&trigger, &originator, &coalesced); err != nil {
		t.Fatalf("read task trigger/originator/coalesced: %v", err)
	}
	return trigger, originator, coalesced
}

func containsUUID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestCompleteTask_ReconcilesPreDispatchMergeRaceComment is the MUL-4195
// round-3 must-fix regression test. A member comment is created while the task
// is still queued, but its merge loses the race to the daemon claiming the task
// (queued→dispatched); the merge then finds no pre-claim row and the enqueue
// path defers to reconcile. The comment's created_at is BEFORE dispatched_at,
// so a dispatched_at-anchored reconcile would skip it and it would vanish. The
// created_at anchor + delivered-set exclusion must catch it — while NOT
// re-firing a comment that WAS delivered as a pre-claim coalesced entry.
func TestCompleteTask_ReconcilesPreDispatchMergeRaceComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'pre-dispatch race fixture', 'in_progress', 'none', $2, 'member', 999006, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Timeline: task created 10m ago; the run's trigger 10m ago; a delivered
	// pre-claim coalesced comment 8m ago; the RACE comment 7m ago (still before
	// dispatch); dispatched 6m ago; started 5m ago.
	insertComment := func(content, age string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', now() - $5::interval) RETURNING id
		`, issueID, testWorkspaceID, testUserID, content, age).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	triggerCommentID := insertComment("initial request", "10 minutes")
	deliveredCoalescedID := insertComment("folded in while queued (delivered)", "8 minutes")
	raceCommentID := insertComment("posted before dispatch, merge lost the race", "7 minutes")

	// The running task: created before every comment window, with the delivered
	// comment recorded in coalesced_comment_ids, dispatched after the race
	// comment, started later still.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, issue_id, trigger_comment_id, coalesced_comment_ids, status, priority, created_at, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, ARRAY[$5::uuid], 'running', 0,
			now() - interval '10 minutes', now() - interval '6 minutes', now() - interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID, deliveredCoalescedID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Exactly one follow-up, and it must be for the RACE comment — the
	// delivered coalesced comment must be excluded (not re-fired).
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up for the pre-dispatch race comment, got %d", n)
	}
	trigger, _, coalesced := taskTriggerOriginatorCoalesced(t, issueID, agentID)
	if trigger != raceCommentID {
		t.Errorf("follow-up trigger must be the race comment %s, got %s", raceCommentID, trigger)
	}
	if containsUUID(coalesced, deliveredCoalescedID) || trigger == deliveredCoalescedID {
		t.Errorf("the already-delivered coalesced comment %s must be excluded from the follow-up, got trigger=%s coalesced=%v",
			deliveredCoalescedID, trigger, coalesced)
	}
}
