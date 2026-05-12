package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueDoneUnblocksDependentIssueAndEnqueuesTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	ctx := context.Background()
	agentID := handlerTestAgentID(t)

	upstreamID := createIssueDependencyFixture(t, "dependency upstream", "todo", "")
	dependentID := createIssueDependencyFixture(t, "dependency dependent", "blocked", agentID)
	linkIssueDependency(t, dependentID, upstreamID)

	updateIssueStatus(t, upstreamID, "done")

	var dependentStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, dependentID).Scan(&dependentStatus); err != nil {
		t.Fatalf("load dependent issue status: %v", err)
	}
	if dependentStatus != "todo" {
		t.Fatalf("expected dependent issue to move to todo, got %q", dependentStatus)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, dependentID, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count dependent tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected one queued task for unblocked dependent issue, got %d", taskCount)
	}
}

func TestBatchIssueDoneUnblocksDependentIssueAndEnqueuesTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	ctx := context.Background()
	agentID := handlerTestAgentID(t)

	upstreamID := createIssueDependencyFixture(t, "batch dependency upstream", "todo", "")
	dependentID := createIssueDependencyFixture(t, "batch dependency dependent", "blocked", agentID)
	linkIssueDependency(t, dependentID, upstreamID)

	batchUpdateIssueStatus(t, []string{upstreamID}, "done")

	var dependentStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, dependentID).Scan(&dependentStatus); err != nil {
		t.Fatalf("load dependent issue status: %v", err)
	}
	if dependentStatus != "todo" {
		t.Fatalf("expected dependent issue to move to todo, got %q", dependentStatus)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, dependentID).Scan(&taskCount); err != nil {
		t.Fatalf("count dependent tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected one queued task for unblocked dependent issue, got %d", taskCount)
	}
}

func TestIssueDoneWaitsForAllDependenciesBeforeUnblocking(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	ctx := context.Background()
	agentID := handlerTestAgentID(t)

	firstUpstreamID := createIssueDependencyFixture(t, "first dependency upstream", "todo", "")
	secondUpstreamID := createIssueDependencyFixture(t, "second dependency upstream", "todo", "")
	dependentID := createIssueDependencyFixture(t, "multi dependency dependent", "blocked", agentID)
	linkIssueDependency(t, dependentID, firstUpstreamID)
	linkIssueDependency(t, dependentID, secondUpstreamID)

	updateIssueStatus(t, firstUpstreamID, "done")

	var dependentStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, dependentID).Scan(&dependentStatus); err != nil {
		t.Fatalf("load dependent issue status: %v", err)
	}
	if dependentStatus != "blocked" {
		t.Fatalf("expected dependent issue to stay blocked, got %q", dependentStatus)
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, dependentID).Scan(&taskCount); err != nil {
		t.Fatalf("count dependent tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no queued task while another dependency is unresolved, got %d", taskCount)
	}
}

func TestBacklogToTodoWithUnresolvedDependencyDoesNotEnqueueTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	ctx := context.Background()
	agentID := handlerTestAgentID(t)

	upstreamID := createIssueDependencyFixture(t, "unresolved backlog dependency", "todo", "")
	dependentID := createIssueDependencyFixture(t, "dependent backlog issue", "backlog", agentID)
	linkIssueDependency(t, dependentID, upstreamID)

	updateIssueStatus(t, dependentID, "todo")

	var taskCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, dependentID).Scan(&taskCount); err != nil {
		t.Fatalf("count dependent tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no task while dependency is unresolved, got %d", taskCount)
	}
}

func TestCreateBlockedAgentIssueDoesNotEnqueueTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	agentID := handlerTestAgentID(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "blocked issue should not enqueue",
		"status":        "blocked",
		"priority":      "none",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count blocked issue tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected blocked agent issue to skip enqueue, got %d tasks", taskCount)
	}
}

func TestMentionOnBlockedIssueDoesNotEnqueueTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	agentID := handlerTestAgentID(t)
	issueID := createIssueDependencyFixture(t, "blocked mention issue", "blocked", "")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "please look [@Agent](mention://agent/" + agentID + ")",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&taskCount); err != nil {
		t.Fatalf("count mention tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected blocked issue mention to skip enqueue, got %d tasks", taskCount)
	}
}

func TestMentionOnIssueWithUnresolvedDependencyDoesNotEnqueueTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not available")
	}
	agentID := handlerTestAgentID(t)
	upstreamID := createIssueDependencyFixture(t, "mention dependency upstream", "todo", "")
	issueID := createIssueDependencyFixture(t, "todo mention dependent", "todo", "")
	linkIssueDependency(t, issueID, upstreamID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "please look [@Agent](mention://agent/" + agentID + ")",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&taskCount); err != nil {
		t.Fatalf("count mention tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected unresolved dependency mention to skip enqueue, got %d tasks", taskCount)
	}
}

func handlerTestAgentID(t *testing.T) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent
		WHERE workspace_id = $1 AND archived_at IS NULL
		ORDER BY created_at ASC
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load handler test agent: %v", err)
	}
	return agentID
}

func createIssueDependencyFixture(t *testing.T, title, status, agentID string) string {
	t.Helper()
	body := map[string]any{
		"title":    title,
		"status":   status,
		"priority": "none",
	}
	if agentID == "" {
		// Leave the issue unassigned.
	} else {
		body["assignee_type"] = "agent"
		body["assignee_id"] = agentID
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue fixture %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue.ID
}

func linkIssueDependency(t *testing.T, issueID, dependsOnIssueID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
		VALUES ($1, $2, 'blocks')
	`, issueID, dependsOnIssueID); err != nil {
		t.Fatalf("link issue dependency: %v", err)
	}
}

func updateIssueStatus(t *testing.T, issueID, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": status})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue status=%s: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

func batchUpdateIssueStatus(t *testing.T, issueIDs []string, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": issueIDs,
		"updates": map[string]any{
			"status": status,
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues status=%s: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode batch update response: %v", err)
	}
	if resp.Updated != len(issueIDs) {
		t.Fatalf("expected updated=%d, got %d", len(issueIDs), resp.Updated)
	}
}
