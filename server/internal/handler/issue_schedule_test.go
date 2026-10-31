package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// createHandlerTestIssueForAgent creates an issue assigned directly to
// agentID via the real CreateIssue handler (so its trigger side effects and
// stored assignee columns match production behavior) and registers cleanup
// for both the issue and anything the schedule tests queue against it.
func createHandlerTestIssueForAgent(t *testing.T, agentID string) string {
	t.Helper()

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Schedule test issue",
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_scheduled_trigger WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	return created.ID
}

func TestCreateIssueScheduleSuccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Schedule Create Agent", nil)
	issueID := createHandlerTestIssueForAgent(t, agentID)

	runAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	req := newRequest("POST", "/api/issues/"+issueID+"/schedule", map[string]any{"run_at": runAt})
	req = withURLParam(req, "id", issueID)
	var resp IssueScheduleResponse
	testutil.Call(t, testHandler.CreateIssueSchedule, req).Want(http.StatusCreated).JSON(&resp)
	if resp.IssueID != issueID {
		t.Fatalf("issue_id = %q, want %q", resp.IssueID, issueID)
	}
	if resp.Status != "pending" {
		t.Fatalf("status = %q, want pending", resp.Status)
	}
	if resp.MissedPolicy != "notify" {
		t.Fatalf("missed_policy = %q, want notify", resp.MissedPolicy)
	}
	if resp.CreatedByUserID != testUserID {
		t.Fatalf("created_by_user_id = %q, want %q", resp.CreatedByUserID, testUserID)
	}

	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue_scheduled_trigger WHERE issue_id = $1 AND status = 'pending'`, issueID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 pending schedule row, got %d", count)
	}
}

func TestCreateIssueScheduleRejectsPastRunAt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Schedule Past Agent", nil)
	issueID := createHandlerTestIssueForAgent(t, agentID)

	runAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	req := newRequest("POST", "/api/issues/"+issueID+"/schedule", map[string]any{"run_at": runAt})
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.CreateIssueSchedule, req).Want(http.StatusBadRequest)
}

// A malformed request body must fail cleanly with 400, never panic or 500 —
// the API Compatibility validation-test convention (CLAUDE.md).
func TestCreateIssueScheduleRejectsMalformedBody(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Schedule Malformed Agent", nil)
	issueID := createHandlerTestIssueForAgent(t, agentID)

	req := httptest.NewRequest("POST", "/api/issues/"+issueID+"/schedule", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.CreateIssueSchedule, req).Want(http.StatusBadRequest)

	req2 := newRequest("POST", "/api/issues/"+issueID+"/schedule", map[string]any{"run_at": "not-a-timestamp"})
	req2 = withURLParam(req2, "id", issueID)
	testutil.Call(t, testHandler.CreateIssueSchedule, req2).Want(http.StatusBadRequest)
}

func TestCreateIssueScheduleRejectsNoAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Unassigned schedule test issue",
		"status": "backlog",
	})
	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	runAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	schedReq := newRequest("POST", "/api/issues/"+created.ID+"/schedule", map[string]any{"run_at": runAt})
	schedReq = withURLParam(schedReq, "id", created.ID)
	testutil.Call(t, testHandler.CreateIssueSchedule, schedReq).Want(http.StatusBadRequest)
}

func TestCreateIssueScheduleConflictWhenAlreadyPending(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Schedule Conflict Agent", nil)
	issueID := createHandlerTestIssueForAgent(t, agentID)

	runAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	first := newRequest("POST", "/api/issues/"+issueID+"/schedule", map[string]any{"run_at": runAt})
	first = withURLParam(first, "id", issueID)
	testutil.Call(t, testHandler.CreateIssueSchedule, first).Want(http.StatusCreated)

	second := newRequest("POST", "/api/issues/"+issueID+"/schedule", map[string]any{"run_at": runAt})
	second = withURLParam(second, "id", issueID)
	testutil.Call(t, testHandler.CreateIssueSchedule, second).Want(http.StatusConflict)
}

func TestGetIssueScheduleNotFoundThenLifecycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Schedule Lifecycle Agent", nil)
	issueID := createHandlerTestIssueForAgent(t, agentID)

	getReq := newRequest("GET", "/api/issues/"+issueID+"/schedule", nil)
	getReq = withURLParam(getReq, "id", issueID)
	testutil.Call(t, testHandler.GetIssueSchedule, getReq).Want(http.StatusNotFound)

	cancelReq := newRequest("DELETE", "/api/issues/"+issueID+"/schedule", nil)
	cancelReq = withURLParam(cancelReq, "id", issueID)
	testutil.Call(t, testHandler.CancelIssueSchedule, cancelReq).Want(http.StatusNotFound)

	runAt := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	createReq := newRequest("POST", "/api/issues/"+issueID+"/schedule", map[string]any{"run_at": runAt})
	createReq = withURLParam(createReq, "id", issueID)
	testutil.Call(t, testHandler.CreateIssueSchedule, createReq).Want(http.StatusCreated)

	getReq2 := newRequest("GET", "/api/issues/"+issueID+"/schedule", nil)
	getReq2 = withURLParam(getReq2, "id", issueID)
	var got IssueScheduleResponse
	testutil.Call(t, testHandler.GetIssueSchedule, getReq2).Want(http.StatusOK).JSON(&got)
	if got.Status != "pending" {
		t.Fatalf("status = %q, want pending", got.Status)
	}

	cancelReq2 := newRequest("DELETE", "/api/issues/"+issueID+"/schedule", nil)
	cancelReq2 = withURLParam(cancelReq2, "id", issueID)
	var cancelled IssueScheduleResponse
	testutil.Call(t, testHandler.CancelIssueSchedule, cancelReq2).Want(http.StatusOK).JSON(&cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}

	getReq3 := newRequest("GET", "/api/issues/"+issueID+"/schedule", nil)
	getReq3 = withURLParam(getReq3, "id", issueID)
	testutil.Call(t, testHandler.GetIssueSchedule, getReq3).Want(http.StatusNotFound)

	// Cancelling again is a no-op 404, not a re-cancel of the terminal row.
	cancelReq3 := newRequest("DELETE", "/api/issues/"+issueID+"/schedule", nil)
	cancelReq3 = withURLParam(cancelReq3, "id", issueID)
	testutil.Call(t, testHandler.CancelIssueSchedule, cancelReq3).Want(http.StatusNotFound)
}
