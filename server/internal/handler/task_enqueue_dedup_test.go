package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

// createDedupTestIssue seeds a minimal issue for the enqueue-dedup tests,
// optionally assigned to an agent. Task rows created against it are cleaned
// up alongside the issue.
func createDedupTestIssue(t *testing.T, title, assigneeAgentID string) string {
	t.Helper()
	ctx := context.Background()

	var issueID string
	var assigneeType, assigneeID any
	if assigneeAgentID != "" {
		assigneeType = "agent"
		assigneeID = assigneeAgentID
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		VALUES ($1, $2, 'todo', 'medium', 'member', $3, $4, $5,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, title, testUserID, assigneeType, assigneeID).Scan(&issueID); err != nil {
		t.Fatalf("create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func countPendingTasks(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
	`, issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	return n
}

// TestEnqueueTaskForMention_DuplicatePendingIsTypedDedup pins the #5914 fix on
// the mention path: when a second enqueue for the same (issue, agent) pair
// trips idx_one_pending_task_per_issue_agent, the service must return the
// typed ErrTaskAlreadyPending — which the comment dispatch layer maps to
// DispatchDeferred/ReasonAlreadyActive — instead of a generic wrapped pg error
// logged at ERROR level.
func TestEnqueueTaskForMention_DuplicatePendingIsTypedDedup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "dedup-mention-agent", nil)
	issueID := createDedupTestIssue(t, "dedup mention issue", "")
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	if _, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(agentID), pgtype.UUID{}); err != nil {
		t.Fatalf("first enqueue should succeed, got: %v", err)
	}
	_, err = testHandler.TaskService.EnqueueTaskForMention(ctx, issue, parseUUID(agentID), pgtype.UUID{})
	if !errors.Is(err, service.ErrTaskAlreadyPending) {
		t.Fatalf("second enqueue: want ErrTaskAlreadyPending, got: %v", err)
	}

	if n := countPendingTasks(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 pending task after duplicate enqueue, got %d", n)
	}
}

// TestEnqueueTaskForIssue_DuplicatePendingIsTypedDedup pins the same contract
// on the issue-assignee path (enqueueIssueTaskWithCommentPlan) — the second
// write point of idx_one_pending_task_per_issue_agent. Without the shared
// isPendingTaskDedupViolation check the assignee path kept the pre-#5914
// behavior: ERROR log plus an untyped error.
func TestEnqueueTaskForIssue_DuplicatePendingIsTypedDedup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "dedup-assignee-agent", nil)
	issueID := createDedupTestIssue(t, "dedup assignee issue", agentID)
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	if _, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue); err != nil {
		t.Fatalf("first enqueue should succeed, got: %v", err)
	}
	_, err = testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
	if !errors.Is(err, service.ErrTaskAlreadyPending) {
		t.Fatalf("second enqueue: want ErrTaskAlreadyPending, got: %v", err)
	}

	if n := countPendingTasks(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 pending task after duplicate enqueue, got %d", n)
	}
}

// TestUpdateAgent_NameCollisionReturns409 pins the #5914 fix on the rename
// path: renaming an agent to a name already held in the workspace must
// surface the agent_workspace_name_unique violation as a structured 409
// Conflict (mirroring CreateAgent), not a 500 leaking the raw constraint.
func TestUpdateAgent_NameCollisionReturns409(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	createHandlerTestAgent(t, "dedup-rename-taken", nil)
	renamedID := createHandlerTestAgent(t, "dedup-rename-source", nil)

	body := map[string]any{"name": "dedup-rename-taken"}
	req := newRequest(http.MethodPut, "/api/agents/"+renamedID, body)
	req = withURLParam(req, "id", renamedID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateAgent name collision: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "already exists") {
		t.Errorf("409 body should name the conflict; got %s", w.Body.String())
	}
}
