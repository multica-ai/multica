package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type terminalWakeupRecorder struct {
	calls []struct{ runtimeID, taskID string }
}

func (r *terminalWakeupRecorder) NotifyTaskAvailable(runtimeID, taskID string) {
	r.calls = append(r.calls, struct{ runtimeID, taskID string }{runtimeID, taskID})
}

func failTaskViaHandler(t *testing.T, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail",
		map[string]any{"error": "agent failed", "failure_reason": "agent_error"},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.FailTask(w, req)
	return w
}

// TestTerminalTransitionsNotifyRuntime verifies the server-side half of the
// queued-successor fix. Completion wakes only after reconciliation, while fail
// and cancel wake after their terminal side effects, so an old daemon can retry
// its normal atomic claim path without waiting for the 30-second fallback.
func TestTerminalTransitionsNotifyRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'terminal wakeup fixture', 'in_progress', 'none', $2, 'member', 999099, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: running task: %v", err)
	}

	recorder := &terminalWakeupRecorder{}
	previous := testHandler.TaskService.Wakeup
	testHandler.TaskService.Wakeup = recorder
	t.Cleanup(func() { testHandler.TaskService.Wakeup = previous })

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(recorder.calls); got != 1 {
		t.Fatalf("expected 1 terminal runtime wakeup, got %d", got)
	}
	if recorder.calls[0].runtimeID != runtimeID {
		t.Fatalf("wakeup runtime = %q, want %q", recorder.calls[0].runtimeID, runtimeID)
	}
	if recorder.calls[0].taskID != "" {
		t.Fatalf("terminal wakeup must omit completed task id, got %q", recorder.calls[0].taskID)
	}

	// Failure uses the daemon callback handler, matching production. It must
	// send the same runtime-only hint after FailTask commits.
	recorder.calls = nil
	var failedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&failedTaskID); err != nil {
		t.Fatalf("setup: failed task: %v", err)
	}
	if w := failTaskViaHandler(t, failedTaskID); w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(recorder.calls); got != 1 || recorder.calls[0].runtimeID != runtimeID || recorder.calls[0].taskID != "" {
		t.Fatalf("unexpected failure wakeups: %#v", recorder.calls)
	}

	// Cancellation is finalized in TaskService because both issue and chat
	// handlers share that path.
	recorder.calls = nil
	var cancelledTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&cancelledTaskID); err != nil {
		t.Fatalf("setup: cancelled task: %v", err)
	}
	cancelled, err := testHandler.TaskService.CancelTask(ctx, parseUUID(cancelledTaskID))
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("CancelTask status = %q, want cancelled", cancelled.Status)
	}
	if got := len(recorder.calls); got != 1 || recorder.calls[0].runtimeID != runtimeID || recorder.calls[0].taskID != "" {
		t.Fatalf("unexpected cancellation wakeups: %#v", recorder.calls)
	}
}
