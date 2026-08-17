package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertRunningIssueTask inserts an in-flight (running) task for the given agent
// on the given issue and returns its id, registering cleanup.
func insertRunningIssueTask(t *testing.T, agentID, issueID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, started_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, now())
		RETURNING id
	`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// insertAgentAssignedIssue inserts an issue assigned to the given agent and
// returns its id, registering cleanup.
func insertAgentAssignedIssue(t *testing.T, agentID string, number int, title string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, $2, 'todo', 'medium', $3, 'member', $4, 0, 'agent', $5)
		RETURNING id
	`, testWorkspaceID, title, testUserID, number, agentID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })
	return issueID
}

// TestUpdateIssueReassignDoesNotCancelActiveTasks locks in the #4963 / MUL-4113
// decision: changing an issue's assignee cancels nothing. Both the previous
// assignee's own in-flight run and an unrelated (mention-triggered) run for a
// different agent must survive the reassignment.
func TestUpdateIssueReassignDoesNotCancelActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "ReassignNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "ReassignNoCancelMention", []byte("[]"))

	issueID := insertAgentAssignedIssue(t, ownerAgent, 92110, "reassign-no-cancel")
	ownerTask := insertRunningIssueTask(t, ownerAgent, issueID)
	mentionTask := insertRunningIssueTask(t, mentionAgent, issueID)

	// Reassign from ownerAgent to a member — a genuine assignee change that
	// does not itself enqueue a new agent run.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue reassign: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := taskStatus(t, ownerTask); got != "running" {
		t.Fatalf("previous assignee's own task must survive reassignment, got status %q", got)
	}
	if got := taskStatus(t, mentionTask); got != "running" {
		t.Fatalf("unrelated agent's task must survive reassignment (no collateral cancel), got status %q", got)
	}
}

// TestBatchUpdateIssueReassignDoesNotCancelActiveTasks is the batch-path mirror
// of TestUpdateIssueReassignDoesNotCancelActiveTasks — BatchUpdateIssues shares
// the same no-cancel-on-reassign behavior.
func TestBatchUpdateIssueReassignDoesNotCancelActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "BatchReassignNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "BatchReassignNoCancelMention", []byte("[]"))

	issueID := insertAgentAssignedIssue(t, ownerAgent, 92111, "batch-reassign-no-cancel")
	ownerTask := insertRunningIssueTask(t, ownerAgent, issueID)
	mentionTask := insertRunningIssueTask(t, mentionAgent, issueID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates": map[string]any{
			"assignee_type": "member",
			"assignee_id":   testUserID,
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues reassign: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := taskStatus(t, ownerTask); got != "running" {
		t.Fatalf("previous assignee's own task must survive batch reassignment, got status %q", got)
	}
	if got := taskStatus(t, mentionTask); got != "running" {
		t.Fatalf("unrelated agent's task must survive batch reassignment, got status %q", got)
	}
}

// queuedTaskCountFor returns how many queued tasks the agent holds on the issue.
func queuedTaskCountFor(t *testing.T, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return n
}

// TestUpdateIssueReassignToAgentKeepsOldTaskAndEnqueuesNew covers the core
// handoff path the member-target tests above do not: reassigning from one agent
// to ANOTHER agent. The previous assignee's in-flight run must survive (the
// #4963 / MUL-4113 no-cancel guarantee), and the new assignee must still get
// its run enqueued by WillEnqueueRun — the two effects are independent.
func TestUpdateIssueReassignToAgentKeepsOldTaskAndEnqueuesNew(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "ReassignAgentToAgentOwner", []byte("[]"))
	newAgent := createHandlerTestAgent(t, "ReassignAgentToAgentNew", []byte("[]"))

	issueID := insertAgentAssignedIssue(t, ownerAgent, 92112, "reassign-agent-to-agent")
	ownerTask := insertRunningIssueTask(t, ownerAgent, issueID)

	// Reassign from ownerAgent to newAgent — an agent→agent ownership handoff.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   newAgent,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue reassign: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := taskStatus(t, ownerTask); got != "running" {
		t.Fatalf("previous agent's own task must survive agent→agent reassignment, got status %q", got)
	}
	if got := queuedTaskCountFor(t, issueID, newAgent); got != 1 {
		t.Fatalf("new assignee must get exactly one run enqueued, got %d queued tasks", got)
	}
}
