package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertAssignedIssueWithStatus inserts an issue in the given status with the
// given assignee, bypassing CreateIssue so the seeding itself never enqueues a
// run. assigneeType/assigneeID may be empty for an unassigned issue.
func insertAssignedIssueWithStatus(t *testing.T, assigneeType, assigneeID string, number int, title, status string) string {
	t.Helper()
	var (
		issueID  string
		typeArg  any = nil
		idArg    any = nil
		nullUUID     = "00000000-0000-0000-0000-000000000000"
	)
	if assigneeType != "" && assigneeID != "" && assigneeID != nullUUID {
		typeArg, idArg = assigneeType, assigneeID
	}
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, $2, $3, 'medium', $4, 'member', $5, 0, $6, $7)
		RETURNING id
	`, testWorkspaceID, title, status, testUserID, number, typeArg, idArg).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

// setIssueStatus drives the real single-issue write path with a status-only
// change — exactly what `multica issue status <id> <status>` sends.
func setIssueStatus(t *testing.T, issueID, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": status}), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue status=%s: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// TestUpdateIssueReopenToTodoEnqueuesRun is the reopen regression: an issue
// already assigned to a ready agent that is reopened from a terminal or review
// status back into `todo` must hand the agent a fresh run, with no assignee
// change and no @mention. Before the fix the rework request sat silently in the
// queue because only `backlog` → active enqueued.
func TestUpdateIssueReopenToTodoEnqueuesRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	cases := []struct {
		name       string
		prevStatus string
		number     int
	}{
		{"in_review", "in_review", 92300},
		{"done", "done", 92301},
		{"cancelled", "cancelled", 92302},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := createHandlerTestAgent(t, "ReopenWake-"+tc.name, []byte("[]"))
			issueID := insertAssignedIssueWithStatus(t, "agent", agentID, tc.number, "reopen-"+tc.name, tc.prevStatus)

			// Preview must promise the run before the write makes it.
			pv := previewIssueTrigger(t, map[string]any{
				"issue_ids": []string{issueID},
				"status":    "todo",
			})
			if pv.TotalCount != 1 {
				t.Fatalf("preview %s → todo: expected 1 trigger, got %+v", tc.prevStatus, pv)
			}
			if pv.Triggers[0].AgentID != agentID || pv.Triggers[0].Source != "status" {
				t.Fatalf("preview %s → todo: wrong trigger %+v", tc.prevStatus, pv.Triggers[0])
			}

			setIssueStatus(t, issueID, "todo")

			if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
				t.Fatalf("%s → todo: expected exactly 1 queued task for the unchanged assignee, got %d", tc.prevStatus, got)
			}
		})
	}
}

// TestUpdateIssueReopenToTodoIsIdempotent locks the dedup contract: a retried
// or duplicated reopen must not stack runs, and a reopen against an agent that
// already holds a pending task must coalesce onto that one.
func TestUpdateIssueReopenToTodoIsIdempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	t.Run("repeated reopen", func(t *testing.T) {
		agentID := createHandlerTestAgent(t, "ReopenWakeRetry", []byte("[]"))
		issueID := insertAssignedIssueWithStatus(t, "agent", agentID, 92310, "reopen-retry", "in_review")

		setIssueStatus(t, issueID, "todo")
		// Same write again: status is unchanged now, so it is not a transition.
		setIssueStatus(t, issueID, "todo")
		// And a full round trip back and forth while the first run is still queued.
		setIssueStatus(t, issueID, "in_review")
		setIssueStatus(t, issueID, "todo")

		if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
			t.Fatalf("repeated reopen must coalesce onto one pending run, got %d queued tasks", got)
		}
	})

	t.Run("existing pending task", func(t *testing.T) {
		agentID := createHandlerTestAgent(t, "ReopenWakePending", []byte("[]"))
		issueID := insertAssignedIssueWithStatus(t, "agent", agentID, 92311, "reopen-pending", "in_review")
		// A mention-raised run is already waiting for this agent on this issue.
		insertRunningIssueTask(t, agentID, issueID)

		pv := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issueID}, "status": "todo"})
		if pv.TotalCount != 1 {
			t.Fatalf("running task is not a pending queue slot; expected preview 1, got %+v", pv)
		}

		setIssueStatus(t, issueID, "todo")
		if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
			t.Fatalf("expected 1 queued task alongside the running one, got %d", got)
		}
	})
}

// TestUpdateIssueReopenNonAgentAssigneeEnqueuesNothing covers the two negative
// assignee shapes: no assignee at all, and a member assignee.
func TestUpdateIssueReopenNonAgentAssigneeEnqueuesNothing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	unassigned := insertAssignedIssueWithStatus(t, "", "", 92320, "reopen-unassigned", "done")
	setIssueStatus(t, unassigned, "todo")
	if got := taskCountForIssue(t, unassigned); got != 0 {
		t.Fatalf("unassigned reopen must enqueue nothing, got %d tasks", got)
	}

	member := insertAssignedIssueWithStatus(t, "member", testUserID, 92321, "reopen-member", "done")
	setIssueStatus(t, member, "todo")
	if got := taskCountForIssue(t, member); got != 0 {
		t.Fatalf("member-assignee reopen must enqueue nothing, got %d tasks", got)
	}
}

// TestUpdateIssueReopenPreservesBacklogAndNonTodoSemantics guards the blast
// radius: parking an issue in `backlog` still never wakes anyone, and a move
// out of `in_review` into a status other than `todo` is not a re-dispatch.
func TestUpdateIssueReopenPreservesBacklogAndNonTodoSemantics(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	parkAgent := createHandlerTestAgent(t, "ReopenWakePark", []byte("[]"))
	parked := insertAssignedIssueWithStatus(t, "agent", parkAgent, 92330, "reopen-park", "in_review")
	setIssueStatus(t, parked, "backlog")
	if got := queuedTaskCountFor(t, parked, parkAgent); got != 0 {
		t.Fatalf("in_review → backlog must not wake the assignee, got %d queued tasks", got)
	}

	blockedAgent := createHandlerTestAgent(t, "ReopenWakeBlocked", []byte("[]"))
	blocked := insertAssignedIssueWithStatus(t, "agent", blockedAgent, 92331, "reopen-blocked", "in_review")
	setIssueStatus(t, blocked, "blocked")
	if got := queuedTaskCountFor(t, blocked, blockedAgent); got != 0 {
		t.Fatalf("in_review → blocked must not wake the assignee, got %d queued tasks", got)
	}
}

// TestBatchUpdateIssueReopenToTodoEnqueuesRun mirrors the single-issue reopen
// on the batch write path, which shares the same predicate.
func TestBatchUpdateIssueReopenToTodoEnqueuesRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ReopenWakeBatch", []byte("[]"))
	i1 := insertAssignedIssueWithStatus(t, "agent", agentID, 92340, "reopen-batch-1", "in_review")
	i2 := insertAssignedIssueWithStatus(t, "agent", agentID, 92341, "reopen-batch-2", "done")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{i1, i2},
		"updates":   map[string]any{"status": "todo"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, id := range []string{i1, i2} {
		if got := queuedTaskCountFor(t, id, agentID); got != 1 {
			t.Fatalf("batch reopen: expected 1 queued task on %s, got %d", id, got)
		}
	}
}

// TestUpdateIssueReopenSuppressRunSkipsEnqueue verifies the reopen source
// honours suppress_run like every other enqueue source.
func TestUpdateIssueReopenSuppressRunSkipsEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ReopenWakeSuppress", []byte("[]"))
	issueID := insertAssignedIssueWithStatus(t, "agent", agentID, 92350, "reopen-suppress", "in_review")

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": "todo", "suppress_run": true,
	}), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue suppressed reopen: %d %s", w.Code, w.Body.String())
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("suppress_run must skip the reopen enqueue, got %d queued tasks", got)
	}
}

// TestUpdateIssueReopenSelfLoopIsSuppressed verifies the reopen source keeps
// the existing self-loop guard: an agent flipping its own in-flight issue back
// to `todo` from inside its own run must not re-trigger itself.
func TestUpdateIssueReopenSelfLoopIsSuppressed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ReopenWakeSelfLoop", []byte("[]"))
	issueID := insertAssignedIssueWithStatus(t, "agent", agentID, 92360, "reopen-self-loop", "in_review")
	taskID := insertRunningIssueTask(t, agentID, issueID)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "todo"}), "id", issueID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue self-loop reopen: %d %s", w.Code, w.Body.String())
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("agent reopening its own running issue must not self-trigger, got %d queued tasks", got)
	}
}

// taskCountForIssue counts every task on the issue regardless of agent — used
// by the negative assignee cases where no agent id exists to key on.
func taskCountForIssue(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}
