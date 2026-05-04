package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestPauseUnpauseRuntime exercises the happy path: pause sets the columns,
// unpause clears them, and the response body reflects the state. Skipped
// when no DB is available — the handler test fixture wires testHandler/
// testPool from the shared TestMain.
func TestPauseUnpauseRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	// Pause with an explicit unpause_at 1 hour in the future.
	unpauseAt := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/pause", map[string]string{
		"unpause_at": unpauseAt,
		"reason":     "manual",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.PauseRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var paused AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&paused); err != nil {
		t.Fatalf("decode paused response: %v", err)
	}
	if paused.PausedAt == nil || *paused.PausedAt == "" {
		t.Fatalf("expected paused_at to be set, got %+v", paused)
	}
	if paused.UnpauseAt == nil || *paused.UnpauseAt == "" {
		t.Fatalf("expected unpause_at to be set, got %+v", paused)
	}
	if paused.PauseReason == nil || *paused.PauseReason != "manual" {
		t.Fatalf("expected pause_reason='manual', got %+v", paused.PauseReason)
	}

	// Unpause and verify all three columns clear.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/runtimes/"+runtimeID+"/unpause", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnpauseRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UnpauseRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var unpaused AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&unpaused); err != nil {
		t.Fatalf("decode unpaused response: %v", err)
	}
	if unpaused.PausedAt != nil && *unpaused.PausedAt != "" {
		t.Fatalf("expected paused_at to be cleared, got %v", unpaused.PausedAt)
	}
	if unpaused.UnpauseAt != nil && *unpaused.UnpauseAt != "" {
		t.Fatalf("expected unpause_at to be cleared, got %v", unpaused.UnpauseAt)
	}
	if unpaused.PauseReason != nil && *unpaused.PauseReason != "" {
		t.Fatalf("expected pause_reason to be cleared, got %v", unpaused.PauseReason)
	}
}

// TestPauseRuntime_DefaultReason verifies that pausing without a body
// defaults reason to 'manual' and leaves unpause_at NULL (= manual unpause
// only). Empty body is the common UI case ("just pause, I'll un-pause myself").
func TestPauseRuntime_DefaultReason(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	t.Cleanup(func() {
		// Unpause whatever state the test left behind so other cases are clean.
		testPool.Exec(context.Background(),
			`UPDATE agent_runtime SET paused_at=NULL, unpause_at=NULL, pause_reason=NULL WHERE id=$1`,
			runtimeID,
		)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/pause", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.PauseRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.PausedAt == nil || *resp.PausedAt == "" {
		t.Fatalf("expected paused_at set, got nil")
	}
	if resp.UnpauseAt != nil && *resp.UnpauseAt != "" {
		t.Fatalf("expected unpause_at nil for manual pause, got %v", resp.UnpauseAt)
	}
	if resp.PauseReason == nil || *resp.PauseReason != "manual" {
		t.Fatalf("expected pause_reason='manual', got %v", resp.PauseReason)
	}
}

// TestPauseRuntime_InvalidUnpauseAt checks that bad RFC3339 / past dates
// return 400 and don't mutate state.
func TestPauseRuntime_InvalidUnpauseAt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)

	cases := []struct {
		name      string
		unpauseAt string
	}{
		{"not rfc3339", "tomorrow"},
		{"past", time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/runtimes/"+runtimeID+"/pause", map[string]string{
				"unpause_at": tc.unpauseAt,
			})
			req = withURLParam(req, "runtimeId", runtimeID)
			testHandler.PauseRuntime(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

// TestPauseRuntime_SuspendsActiveTasks creates a dispatched task, pauses
// the runtime, and verifies the task is marked failed with
// failure_reason='runtime_paused' so the unpause path can resume it.
func TestPauseRuntime_SuspendsActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id=$1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'pause test issue', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'dispatched')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id=$1 OR parent_task_id=$1`, taskID) })
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET paused_at=NULL, unpause_at=NULL, pause_reason=NULL WHERE id=$1`, runtimeID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/pause", map[string]string{
		"reason": "rate_limit",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.PauseRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status, failureReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, COALESCE(failure_reason,'') FROM agent_task_queue WHERE id=$1`,
		taskID,
	).Scan(&status, &failureReason); err != nil {
		t.Fatalf("re-fetch task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status=failed after pause, got %q", status)
	}
	if failureReason != "runtime_paused" {
		t.Fatalf("expected failure_reason=runtime_paused, got %q", failureReason)
	}
}

// TestUnpauseRuntime_ResumesSuspendedTasks pauses a runtime with an active
// task, then unpauses, and verifies a child retry task was enqueued.
func TestUnpauseRuntime_ResumesSuspendedTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id=$1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'resume test issue', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, issueID) })

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id=$1 OR parent_task_id=$1`, parentID)
		testPool.Exec(ctx, `UPDATE agent_runtime SET paused_at=NULL, unpause_at=NULL, pause_reason=NULL WHERE id=$1`, runtimeID)
	})

	// Pause: marks parent failed with failure_reason='runtime_paused'.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/pause", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.PauseRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseRuntime: %d %s", w.Code, w.Body.String())
	}

	// Unpause: should create a child task.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/runtimes/"+runtimeID+"/unpause", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnpauseRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UnpauseRuntime: %d %s", w.Code, w.Body.String())
	}

	var childCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1 AND status='queued'`,
		parentID,
	).Scan(&childCount); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if childCount != 1 {
		t.Fatalf("expected 1 queued child task after unpause, got %d", childCount)
	}
}

// TestSweepUnpauseDue verifies the sweeper unpauses runtimes whose
// scheduled unpause_at has passed and resumes their work — the
// auto-unpause-at-time contract.
func TestSweepUnpauseDue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET paused_at=NULL, unpause_at=NULL, pause_reason=NULL WHERE id=$1`, runtimeID)
	})

	// Pause with unpause_at in the past so the sweeper picks it up.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime
		SET paused_at = now() - interval '1 hour',
		    unpause_at = now() - interval '1 minute',
		    pause_reason = 'rate_limit'
		WHERE id = $1
	`, runtimeID); err != nil {
		t.Fatalf("force-pause: %v", err)
	}

	count := testHandler.TaskService.SweepUnpauseDue(ctx)
	if count < 1 {
		t.Fatalf("expected sweep to unpause >=1 runtime, got %d", count)
	}

	// Verify the columns cleared.
	var pausedAt pgtype.Timestamptz
	if err := testPool.QueryRow(ctx,
		`SELECT paused_at FROM agent_runtime WHERE id=$1`,
		runtimeID,
	).Scan(&pausedAt); err != nil {
		t.Fatalf("re-fetch runtime: %v", err)
	}
	if pausedAt.Valid {
		t.Fatalf("expected paused_at NULL after sweep, got %v", pausedAt.Time)
	}
}

