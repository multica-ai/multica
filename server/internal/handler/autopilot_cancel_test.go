package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCancelAutopilotRun_RunOnlyCancelsRunAndTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "cancel-run-only-agent", []byte(`[]`))

	var autopilotID, runID, taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id, status)
		VALUES ($1, 'cancel run only', $2, 'run_only', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, status, priority, autopilot_run_id)
		VALUES ($1, 'running', 0, $2)
		RETURNING id
	`, agentID, runID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE autopilot_run SET task_id = $1 WHERE id = $2`, taskID, runID); err != nil {
		t.Fatalf("link task: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilot-runs/"+runID+"/cancel?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "runId", runID)
	testHandler.CancelAutopilotRun(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("CancelAutopilotRun: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CancelAutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Cancelled || resp.AlreadyTerminal || resp.CancelledTasks != 1 {
		t.Fatalf("unexpected cancel response: %#v", resp)
	}
	if resp.Run.Status != "cancelled" {
		t.Fatalf("run status in response = %q, want cancelled", resp.Run.Status)
	}

	var runStatus, taskStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot_run WHERE id = $1`, runID).Scan(&runStatus); err != nil {
		t.Fatalf("select run status: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus); err != nil {
		t.Fatalf("select task status: %v", err)
	}
	if runStatus != "cancelled" || taskStatus != "cancelled" {
		t.Fatalf("statuses = run:%s task:%s, want cancelled/cancelled", runStatus, taskStatus)
	}
}

func TestCancelAutopilotRun_TerminalRunIsIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "cancel-terminal-agent", []byte(`[]`))

	var autopilotID, runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id, status)
		VALUES ($1, 'cancel terminal', $2, 'run_only', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status, completed_at)
		VALUES ($1, 'manual', 'completed', now())
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilot-runs/"+runID+"/cancel?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "runId", runID)
	testHandler.CancelAutopilotRun(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("CancelAutopilotRun: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CancelAutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Cancelled || !resp.AlreadyTerminal || resp.CancelledTasks != 0 {
		t.Fatalf("unexpected idempotent response: %#v", resp)
	}
	if resp.Run.Status != "completed" {
		t.Fatalf("run status in response = %q, want completed", resp.Run.Status)
	}
}

func TestCancelAutopilotRun_MissingOrWrongWorkspaceReturns404(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "cancel-wrong-workspace-agent", []byte(`[]`))

	var autopilotID, runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id, status)
		VALUES ($1, 'cancel wrong workspace', $2, 'run_only', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	t.Run("missing run", func(t *testing.T) {
		missingRunID := "11111111-2222-3333-4444-555555555555"
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/autopilot-runs/"+missingRunID+"/cancel?workspace_id="+testWorkspaceID, nil)
		r = withURLParam(r, "runId", missingRunID)
		testHandler.CancelAutopilotRun(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("missing run: expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("wrong workspace", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/autopilot-runs/"+runID+"/cancel?workspace_id=00000000-0000-0000-0000-000000000001", nil)
		r = withURLParam(r, "runId", runID)
		testHandler.CancelAutopilotRun(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("wrong workspace: expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestCancelAutopilotRun_PrivateAgentPlainMemberForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, _, memberID := privateAgentTestFixture(t)

	var autopilotID, runID, taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id, status)
		VALUES ($1, 'cancel private agent', $2, 'run_only', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, status, priority, autopilot_run_id)
		VALUES ($1, 'running', 0, $2)
		RETURNING id
	`, agentID, runID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE autopilot_run SET task_id = $1 WHERE id = $2`, taskID, runID); err != nil {
		t.Fatalf("link task: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/autopilot-runs/"+runID+"/cancel?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "runId", runID)
	testHandler.CancelAutopilotRun(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var runStatus, taskStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot_run WHERE id = $1`, runID).Scan(&runStatus); err != nil {
		t.Fatalf("select run status: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus); err != nil {
		t.Fatalf("select task status: %v", err)
	}
	if runStatus != "running" || taskStatus != "running" {
		t.Fatalf("forbidden cancel mutated statuses: run:%s task:%s", runStatus, taskStatus)
	}
}
