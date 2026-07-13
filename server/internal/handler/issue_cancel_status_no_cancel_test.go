package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateIssueCancelStatusDoesNotCancelActiveTasks locks in MUL-4465:
// moving an issue to `cancelled` no longer stops its in-flight agent runs.
// A user clicking "cancel" has no expectation that it interrupts running
// tasks, so that implicit coupling was removed. Deleting an issue still
// cancels its tasks (covered elsewhere); a status change never does.
func TestUpdateIssueCancelStatusDoesNotCancelActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "CancelStatusNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "CancelStatusNoCancelMention", []byte("[]"))

	issueID := insertAgentAssignedIssue(t, ownerAgent, 92130, "cancel-status-no-cancel")
	ownerTask := insertRunningIssueTask(t, ownerAgent, issueID)
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

	if got := taskStatus(t, ownerTask); got != "running" {
		t.Fatalf("assignee's own task must survive issue → cancelled, got status %q", got)
	}
	if got := taskStatus(t, mentionTask); got != "running" {
		t.Fatalf("unrelated agent's task must survive issue → cancelled, got status %q", got)
	}
}

// TestBatchUpdateIssueCancelStatusDoesNotCancelActiveTasks is the batch-path
// mirror — BatchUpdateIssues shares the same no-cancel-on-cancelled behavior.
func TestBatchUpdateIssueCancelStatusDoesNotCancelActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "BatchCancelStatusNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "BatchCancelStatusNoCancelMention", []byte("[]"))

	issueID := insertAgentAssignedIssue(t, ownerAgent, 92131, "batch-cancel-status-no-cancel")
	ownerTask := insertRunningIssueTask(t, ownerAgent, issueID)
	mentionTask := insertRunningIssueTask(t, mentionAgent, issueID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates": map[string]any{
			"status": "cancelled",
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := taskStatus(t, ownerTask); got != "running" {
		t.Fatalf("assignee's own task must survive batch issue → cancelled, got status %q", got)
	}
	if got := taskStatus(t, mentionTask); got != "running" {
		t.Fatalf("unrelated agent's task must survive batch issue → cancelled, got status %q", got)
	}
}
