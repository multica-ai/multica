package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// activeTaskStatuses are the non-terminal states CancelAgentTasksByIssue sweeps
// (server/pkg/db/queries/agent.sql). MUL-4465's contract is that a status flip
// to `cancelled` leaves every one of them untouched, so the tests below drive
// each state, not just `running`.
var activeTaskStatuses = []string{"queued", "dispatched", "running", "waiting_local_directory", "deferred"}

// insertIssueTaskWithStatus inserts one active task for the agent on the issue
// in the given status, populating the per-status columns the schema expects
// (started_at for running, fire_at for deferred, wait_reason for
// waiting_local_directory), and registers cleanup.
func insertIssueTaskWithStatus(t *testing.T, agentID, issueID, status string) string {
	t.Helper()
	var startedAt, fireAt, waitReason any // nil => SQL NULL
	switch status {
	case "running":
		startedAt = time.Now()
	case "deferred":
		fireAt = time.Now().Add(time.Hour)
	case "waiting_local_directory":
		waitReason = "waiting for local directory"
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, started_at, fire_at, wait_reason)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 0, $3, $4, $5, $6)
		RETURNING id
	`, agentID, status, issueID, startedAt, fireAt, waitReason).Scan(&taskID); err != nil {
		t.Fatalf("insert %s task: %v", status, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestUpdateIssueCancelStatusDoesNotCancelActiveTasks locks in MUL-4465:
// moving an issue to `cancelled` no longer stops its in-flight agent runs. A
// user clicking "cancel" has no expectation that it interrupts running tasks,
// so that implicit coupling was removed. Every active task state must survive,
// as must an unrelated @-mention run for a different agent on the same issue.
// Deleting an issue still cancels its tasks (covered elsewhere).
func TestUpdateIssueCancelStatusDoesNotCancelActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "CancelStatusNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "CancelStatusNoCancelMention", []byte("[]"))

	for i, status := range activeTaskStatuses {
		t.Run(status, func(t *testing.T) {
			issueID := insertAgentAssignedIssue(t, ownerAgent, 92130+i, "cancel-status-no-cancel-"+status)
			ownerTask := insertIssueTaskWithStatus(t, ownerAgent, issueID, status)
			mentionTask := insertRunningIssueTask(t, mentionAgent, issueID)

			w := httptest.NewRecorder()
			req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
				"status": "cancelled",
			})
			req = withURLParam(req, "id", issueID)
			testHandler.UpdateIssue(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("UpdateIssue cancel: expected 200, got %d: %s", w.Code, w.Body.String())
			}

			if got := taskStatus(t, ownerTask); got != status {
				t.Fatalf("assignee's %s task must survive issue → cancelled, got status %q", status, got)
			}
			if got := taskStatus(t, mentionTask); got != "running" {
				t.Fatalf("unrelated agent's task must survive issue → cancelled, got status %q", got)
			}
		})
	}
}

// TestBatchUpdateIssueCancelStatusDoesNotCancelActiveTasks is the batch-path
// mirror — BatchUpdateIssues shares the same no-cancel-on-cancelled behavior.
// One issue per active state is cancelled in a single batch call; every task
// must keep its original status.
func TestBatchUpdateIssueCancelStatusDoesNotCancelActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "BatchCancelStatusNoCancelOwner", []byte("[]"))

	issueIDs := make([]string, 0, len(activeTaskStatuses))
	taskByStatus := make(map[string]string, len(activeTaskStatuses))
	for i, status := range activeTaskStatuses {
		issueID := insertAgentAssignedIssue(t, ownerAgent, 92140+i, "batch-cancel-status-no-cancel-"+status)
		taskByStatus[status] = insertIssueTaskWithStatus(t, ownerAgent, issueID, status)
		issueIDs = append(issueIDs, issueID)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": issueIDs,
		"updates": map[string]any{
			"status": "cancelled",
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for status, taskID := range taskByStatus {
		if got := taskStatus(t, taskID); got != status {
			t.Fatalf("%s task must survive batch issue → cancelled, got status %q", status, got)
		}
	}
}
