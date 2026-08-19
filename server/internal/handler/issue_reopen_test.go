package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// workerReopenFixture creates a terminal issue assigned to agentID and a
// completed task that can authenticate the worker without occupying the issue
// claim. A new task is expected only when the reopen authorization succeeds.
func workerReopenFixture(t *testing.T, name string) (agentID, issueID, taskID string) {
	t.Helper()

	agentID = createHandlerTestAgent(t, name, []byte("[]"))
	issueID = dbfx.Issue(t, name+" issue", testutil.Cols{
		"status":        "done",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   handlerTestRuntimeID(t),
		"status":       "completed",
		"issue_id":     issueID,
		"completed_at": testutil.Raw("now()"),
	})
	return agentID, issueID, taskID
}

func workerReopenRequest(t *testing.T, agentID, taskID, issueID, status string) *http.Request {
	t.Helper()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": status,
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return withURLParam(req, "id", issueID)
}

func issueStatusForTest(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
	return status
}

func TestUpdateIssueWorkerCanReopenAssignedIssueWhenClaimIsUnused(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenUnusedClaim")
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, workerReopenRequest(t, agentID, taskID, issueID, "in_progress"))

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "in_progress" {
		t.Fatalf("reopen status = %q, want in_progress", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("accepted worker reopen must renew the claim with one queued task, got %d", got)
	}
}

func TestUpdateIssueWorkerReopenRollsBackWhenRenewalEnqueueFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenRenewalEnqueueFailure")
	// Authorization only proves ownership and claim occupancy. The runtime can
	// disappear before the write transaction reaches the required renewal; that
	// failure must abort the status change rather than leave an active issue with
	// no replacement task.
	dbfx.Exec(t, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID)

	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, workerReopenRequest(t, agentID, taskID, issueID, "in_progress"))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("worker reopen enqueue failure: expected 503, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("failed worker reopen changed issue status to %q, want done", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("failed worker reopen queued %d replacement tasks", got)
	}
}

func TestUpdateIssueWorkerConcurrentReopenRenewsClaimOnce(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenConcurrent")
	start := make(chan struct{})
	responses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			testHandler.UpdateIssue(w, workerReopenRequest(t, agentID, taskID, issueID, "in_progress"))
			responses <- w.Code
		}()
	}
	close(start)
	wg.Wait()
	close(responses)

	var okCount, conflictCount int
	for status := range responses {
		switch status {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		default:
			t.Fatalf("concurrent worker reopen status = %d, want one 200 and one 409", status)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent worker reopen results = %d success, %d conflict; want 1/1", okCount, conflictCount)
	}
	if got := issueStatusForTest(t, issueID); got != "in_progress" {
		t.Fatalf("concurrent reopen status = %q, want in_progress", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("concurrent reopen must renew once, got %d queued tasks", got)
	}
}

func TestUpdateIssueWorkerCannotReopenAssignedIssueWithActiveClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenActiveClaim")
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
		"issue_id":   issueID,
		"started_at": testutil.Raw("now()"),
	})

	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, workerReopenRequest(t, agentID, taskID, issueID, "in_progress"))

	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateIssue active-claim reopen: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("active-claim denial changed issue status to %q", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("active-claim denial queued %d replacement tasks", got)
	}
}

func TestUpdateIssueWorkerCannotReopenIssueAssignedToAnotherWorker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerID, issueID, _ := workerReopenFixture(t, "WorkerReopenUnauthorizedOwner")
	actorID := createHandlerTestAgent(t, "WorkerReopenUnauthorizedActor", []byte("[]"))
	actorTaskID := dbfx.Task(t, actorID, testutil.Cols{
		"runtime_id":   handlerTestRuntimeID(t),
		"status":       "completed",
		"issue_id":     issueID,
		"completed_at": testutil.Raw("now()"),
	})

	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, workerReopenRequest(t, actorID, actorTaskID, issueID, "in_progress"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateIssue unauthorized reopen: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("unauthorized worker changed issue status to %q (owner %s)", got, ownerID)
	}
}

func TestUpdateIssueWorkerCannotHijackAssignmentWhileReopening(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerID, issueID, _ := workerReopenFixture(t, "WorkerReopenAssignmentHijack")
	actorID := createHandlerTestAgent(t, "WorkerReopenAssignmentHijacker", []byte("[]"))
	actorTaskID := dbfx.Task(t, actorID, testutil.Cols{
		"runtime_id":   handlerTestRuntimeID(t),
		"status":       "completed",
		"issue_id":     issueID,
		"completed_at": testutil.Raw("now()"),
	})

	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   actorID,
	})
	req.Header.Set("X-Agent-ID", actorID)
	req.Header.Set("X-Task-ID", actorTaskID)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateIssue assignment hijack: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("assignment hijack denial changed issue status to %q", got)
	}
	var assigneeID string
	dbfx.QueryRow(t, `SELECT assignee_id FROM issue WHERE id = $1`, issueID).Scan(&assigneeID)
	if assigneeID != ownerID {
		t.Fatalf("assignment hijack denial changed assignee to %q, want original owner %q", assigneeID, ownerID)
	}
}

func TestUpdateIssueWorkerCannotReopenToBacklogWithoutRenewingClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenBacklog")
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, workerReopenRequest(t, agentID, taskID, issueID, "backlog"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateIssue backlog reopen: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("backlog reopen denial changed issue status to %q", got)
	}
}

func TestUpdateIssueMemberReopenKeepsExistingControlPlaneBehavior(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, _ := workerReopenFixture(t, "MemberReopenControlPlane")
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": "in_progress",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("member reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "in_progress" {
		t.Fatalf("member reopen status = %q, want in_progress", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("member reopen must not synthesize a worker renewal, got %d queued tasks", got)
	}
}

func TestUpdateIssueWorkerCannotReopenWithoutRenewingClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenSuppressed")
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status":       "in_progress",
		"suppress_run": true,
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("suppressed worker reopen: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("suppressed worker reopen changed issue status to %q", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 0 {
		t.Fatalf("suppressed worker reopen queued %d replacement tasks", got)
	}
}

func TestPreviewIssueTriggerWorkerReopenMatchesRenewalAuthorization(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "WorkerReopenPreview")
	req := newRequest("POST", "/api/issues/preview-trigger?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{issueID},
		"status":    "in_progress",
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	var resp IssueTriggerPreviewResponse
	testutil.Call(t, testHandler.PreviewIssueTrigger, req).Want(http.StatusOK).JSON(&resp)
	if resp.TotalCount != 1 || len(resp.Triggers) != 1 || resp.Triggers[0].Source != string(service.RunSourceReopen) {
		t.Fatalf("worker reopen preview = %+v, want one reopen trigger", resp)
	}
}

func TestBatchUpdateIssuesWorkerCannotReopenAssignedIssueWithActiveClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "BatchWorkerReopenActiveClaim")
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
		"issue_id":   issueID,
		"started_at": testutil.Raw("now()"),
	})

	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates": map[string]any{
			"status": "in_progress",
		},
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("BatchUpdateIssues active-claim reopen: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "done" {
		t.Fatalf("batch active-claim denial changed issue status to %q", got)
	}
}

func TestBatchUpdateIssuesWorkerCanReopenAssignedIssueWhenClaimIsUnused(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "BatchWorkerReopenUnusedClaim")
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates": map[string]any{
			"status": "in_progress",
		},
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusForTest(t, issueID); got != "in_progress" {
		t.Fatalf("batch reopen status = %q, want in_progress", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("accepted batch worker reopen must renew the claim with one queued task, got %d", got)
	}
}

func TestBatchUpdateIssuesWorkerReopenRetainsEarlierItemWhenLaterItemIsMissing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "BatchWorkerReopenPerItemScope")
	missingIssueID := "00000000-0000-0000-0000-000000000001"
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID, missingIssueID},
		"updates": map[string]any{
			"status": "in_progress",
		},
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues per-item scope: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode per-item scope response: %v", err)
	}
	if resp.Updated != 1 {
		t.Fatalf("per-item scope updated=%d, want 1 successful item", resp.Updated)
	}
	if got := issueStatusForTest(t, issueID); got != "in_progress" {
		t.Fatalf("earlier successful item status = %q, want in_progress", got)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("earlier successful worker reopen queued %d tasks, want 1", got)
	}
}

func TestBatchUpdateIssuesWorkerReopenDeduplicatesIssueIDs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "BatchWorkerReopenDuplicateIDs")
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID, issueID},
		"updates": map[string]any{
			"status": "in_progress",
		},
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues duplicate reopen: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("duplicate batch reopen must renew once, got %d queued tasks", got)
	}
}

func TestBatchUpdateIssuesWorkerConcurrentReopenRenewsClaimOnce(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, issueID, taskID := workerReopenFixture(t, "BatchWorkerReopenConcurrent")
	start := make(chan struct{})
	responses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := newRequest("POST", "/api/issues/batch-update", map[string]any{
				"issue_ids": []string{issueID},
				"updates":   map[string]any{"status": "in_progress"},
			})
			req.Header.Set("X-Agent-ID", agentID)
			req.Header.Set("X-Task-ID", taskID)
			w := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(w, req)
			responses <- w.Code
		}()
	}
	close(start)
	wg.Wait()
	close(responses)

	var okCount, conflictCount int
	for status := range responses {
		switch status {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		default:
			t.Fatalf("concurrent batch worker reopen status = %d, want one 200 and one 409", status)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent batch worker reopen results = %d success, %d conflict; want 1/1", okCount, conflictCount)
	}
	if got := queuedTaskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("concurrent batch reopen must renew once, got %d queued tasks", got)
	}
}
