package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type recoverOrphanFixture struct {
	runtimeID string
	agentID   string
	issueID   string
	taskID    string
}

func createRecoverOrphanFixture(t *testing.T, ctx context.Context, name string) recoverOrphanFixture {
	t.Helper()

	runtimeID := createClaimReclaimRuntime(t, ctx, name+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" agent")
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET last_seen_at = now() - interval '10 minutes' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("age runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1`, runtimeID)
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("seed agent status: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at, max_attempts
		)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours', 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	return recoverOrphanFixture{
		runtimeID: runtimeID,
		agentID:   agentID,
		issueID:   issueID,
		taskID:    taskID,
	}
}

func assertRecoverOrphanedTaskState(t *testing.T, ctx context.Context, agentID, issueID, taskID string) {
	t.Helper()

	var afterAgent, afterIssue, afterTask, afterFailure string
	var afterActive, afterQueued, afterFailed int
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&afterAgent); err != nil {
		t.Fatalf("read agent after: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&afterIssue); err != nil {
		t.Fatalf("read issue after: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&afterTask, &afterFailure); err != nil {
		t.Fatalf("read task after: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
	`, issueID).Scan(&afterActive); err != nil {
		t.Fatalf("count active after: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status = 'queued'
	`, issueID).Scan(&afterQueued); err != nil {
		t.Fatalf("count queued after: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status = 'failed'
	`, issueID).Scan(&afterFailed); err != nil {
		t.Fatalf("count failed after: %v", err)
	}

	if afterTask != "failed" {
		t.Fatalf("parent task status = %s, want failed", afterTask)
	}
	if afterFailure != "runtime_recovery" {
		t.Fatalf("parent failure_reason = %s, want runtime_recovery", afterFailure)
	}
	if afterAgent != "idle" {
		t.Fatalf("agent status = %s, want idle after recovery drained live work", afterAgent)
	}
	if afterIssue != "in_progress" {
		t.Fatalf("issue status = %s, want in_progress with a queued retry", afterIssue)
	}
	if afterActive != 0 {
		t.Fatalf("active task count = %d, want 0", afterActive)
	}
	if afterQueued != 1 {
		t.Fatalf("queued task count = %d, want 1 retry", afterQueued)
	}
	if afterFailed != 1 {
		t.Fatalf("failed task count = %d, want 1 terminal parent", afterFailed)
	}
}

func TestRecoverOrphanedTasks_RequeuesDeadSession(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	fixture := createRecoverOrphanFixture(t, ctx, "Recover orphan")

	var beforeAgent, beforeIssue, beforeTask string
	var beforeActive int
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent WHERE id = $1`, fixture.agentID).Scan(&beforeAgent); err != nil {
		t.Fatalf("read agent before: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, fixture.issueID).Scan(&beforeIssue); err != nil {
		t.Fatalf("read issue before: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&beforeTask); err != nil {
		t.Fatalf("read task before: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
	`, fixture.issueID).Scan(&beforeActive); err != nil {
		t.Fatalf("count active before: %v", err)
	}
	t.Logf("before recovery: agent=%s issue=%s task=%s active=%d", beforeAgent, beforeIssue, beforeTask, beforeActive)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+fixture.runtimeID+"/recover-orphans", nil, testWorkspaceID, "recover-orphans-test")
	req = withURLParams(req, "runtimeId", fixture.runtimeID)
	testHandler.RecoverOrphanedTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RecoverOrphanedTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Orphaned int `json:"orphaned"`
		Retried  int `json:"retried"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode recover response: %v", err)
	}
	if resp.Orphaned != 1 || resp.Retried != 1 {
		t.Fatalf("recover response = %+v, want one recovered and one retry", resp)
	}

	assertRecoverOrphanedTaskState(t, ctx, fixture.agentID, fixture.issueID, fixture.taskID)
}

func TestRecoverOrphanedTasks_AllowsWorkspaceOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	fixture := createRecoverOrphanFixture(t, ctx, "Recover owner")

	w := httptest.NewRecorder()
	req := newRequestAs(testUserID, "POST", "/api/daemon/runtimes/"+fixture.runtimeID+"/recover-orphans", nil)
	req = withURLParams(req, "runtimeId", fixture.runtimeID)
	testHandler.RecoverOrphanedTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RecoverOrphanedTasks owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Orphaned int `json:"orphaned"`
		Retried  int `json:"retried"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode recover response: %v", err)
	}
	if resp.Orphaned != 1 || resp.Retried != 1 {
		t.Fatalf("recover response = %+v, want one recovered and one retry", resp)
	}

	assertRecoverOrphanedTaskState(t, ctx, fixture.agentID, fixture.issueID, fixture.taskID)
}

func TestRecoverOrphanedTasks_RejectsPlainMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	fixture := createRecoverOrphanFixture(t, ctx, "Recover member")
	memberID := createPlainMember(t, "recover-orphan-member@multica.test")

	w := httptest.NewRecorder()
	req := newRequestAs(memberID, "POST", "/api/daemon/runtimes/"+fixture.runtimeID+"/recover-orphans", nil)
	req = withURLParams(req, "runtimeId", fixture.runtimeID)
	testHandler.RecoverOrphanedTasks(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("RecoverOrphanedTasks member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var taskStatus, failureReason string
	if err := testPool.QueryRow(ctx, `SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&taskStatus, &failureReason); err != nil {
		t.Fatalf("read task after member rejection: %v", err)
	}
	if taskStatus != "running" || failureReason != "" {
		t.Fatalf("task changed after member rejection: status=%s failure=%s", taskStatus, failureReason)
	}
}

func TestRecoverOrphanedTasks_ConcurrentRequestsStayAtMostOnce(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	fixture := createRecoverOrphanFixture(t, ctx, "Recover concurrent")

	type result struct {
		orphaned int
		retried  int
		body     string
		code     int
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+fixture.runtimeID+"/recover-orphans", nil, testWorkspaceID, "recover-orphans-concurrent")
			req = withURLParams(req, "runtimeId", fixture.runtimeID)
			testHandler.RecoverOrphanedTasks(w, req)
			var resp struct {
				Orphaned int `json:"orphaned"`
				Retried  int `json:"retried"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			results <- result{
				orphaned: resp.Orphaned,
				retried:  resp.Retried,
				body:     w.Body.String(),
				code:     w.Code,
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var totalOrphaned, totalRetried int
	var bodies []string
	for r := range results {
		if r.code != http.StatusOK {
			t.Fatalf("RecoverOrphanedTasks code = %d, want 200: %s", r.code, r.body)
		}
		totalOrphaned += r.orphaned
		totalRetried += r.retried
		bodies = append(bodies, r.body)
	}
	t.Logf("concurrent recover responses: %v", bodies)

	if totalOrphaned != 1 {
		t.Fatalf("total orphaned = %d, want exactly 1 recovered task", totalOrphaned)
	}
	if totalRetried != 1 {
		t.Fatalf("total retried = %d, want exactly 1 retry", totalRetried)
	}

	var failedCount, queuedCount, activeCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status = 'failed'
	`, fixture.issueID).Scan(&failedCount); err != nil {
		t.Fatalf("count failed after concurrent recovery: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status = 'queued'
	`, fixture.issueID).Scan(&queuedCount); err != nil {
		t.Fatalf("count queued after concurrent recovery: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE issue_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
	`, fixture.issueID).Scan(&activeCount); err != nil {
		t.Fatalf("count active after concurrent recovery: %v", err)
	}

	if failedCount != 1 || queuedCount != 1 || activeCount != 0 {
		t.Fatalf("post-recovery counts = failed:%d queued:%d active:%d, want 1/1/0", failedCount, queuedCount, activeCount)
	}
}

// TestRecoverOrphanedTasks_RejectsWrongWorkspace makes the access check visible
// in tests so a stale runtime can't be recovered from the wrong tenant.
func TestRecoverOrphanedTasks_RejectsWrongWorkspace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility)
		VALUES ($1, NULL, 'Wrong workspace recovery runtime', 'cloud', 'handler_test_runtime', 'online', 'recover wrong workspace fixture', '{}'::jsonb, now(), 'private')
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("setup runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/recover-orphans", nil, "00000000-0000-0000-0000-000000000000", "recover-orphans-wrong-workspace")
	req = withURLParams(req, "runtimeId", runtimeID)
	testHandler.RecoverOrphanedTasks(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("RecoverOrphanedTasks cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
