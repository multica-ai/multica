package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedEvidenceGateRunningTask seeds a running MemoryHub execution: a task in
// status running carrying an execution snapshot (execution_id +
// memoryhub_run_id + memory_policy) plus its execution_ledger row. Returns the
// task id.
func seedEvidenceGateRunningTask(t *testing.T, ctx context.Context) string {
	t.Helper()
	agentID, runtimeID := func() (string, string) {
		var a, r string
		if err := testPool.QueryRow(ctx, `
			SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
		`, testWorkspaceID).Scan(&a, &r); err != nil {
			t.Fatalf("seed: get agent: %v", err)
		}
		return a, r
	}()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'evidence gate fixture', 'in_progress', 'none', $2, 'member', 20002, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	execID := uuid.New().String()
	runID := "run-" + uuid.New().String()

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, started_at,
			execution_id, memoryhub_run_id, memory_policy, review_policy
		)
		VALUES ($1, $2, $3, 'running', 0, now(), $4, $5, 'required', 'none')
		RETURNING id
	`, agentID, runtimeID, issueID, execID, runID).Scan(&taskID); err != nil {
		t.Fatalf("seed: create task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO execution_ledger (
			execution_id, attempt, task_id, task_version, workspace_id,
			scope_kind, run_id, agent_id, runtime_id, model, state, origin,
			idempotency_key, review_policy
		)
		VALUES ($1, 1, $2, 1, $3, 'workspace', $4, $5, $6, 'test-model', 'running',
		        'enqueue', $7, 'none')
	`, execID, taskID, testWorkspaceID, runID, agentID, runtimeID, "key-"+taskID); err != nil {
		t.Fatalf("seed: create execution ledger: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM execution_evidence_record WHERE execution_id = $1`, execID)
		testPool.Exec(context.Background(), `DELETE FROM execution_ledger WHERE execution_id = $1`, execID)
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func postComplete(t *testing.T, taskID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", body, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	return w
}

// TestCompleteTask_EvidenceGateBlocksEmptyOutputOnMemoryHubExecution is the B10
// handler-level fail-then-pass test. A MemoryHub execution completing through
// the daemon CompleteTask endpoint with an empty output must NOT become
// completed: the five-category runtime evidence gate routes it to the failure
// path with the empty_or_unparseable_output stop_reason.
//
// Before the B10 wiring, the daemon handler called CompleteTask directly and
// the task became completed despite the missing evidence (fail). After the
// wiring, the handler routes MemoryHub executions through
// CompleteTaskWithRuntimeEvidenceGate (pass).
func TestCompleteTask_EvidenceGateBlocksEmptyOutputOnMemoryHubExecution(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	taskID := seedEvidenceGateRunningTask(t, ctx)

	w := postComplete(t, taskID, map[string]any{"output": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status, failureReason string
	if err := testPool.QueryRow(ctx, `
		SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&status, &failureReason); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed — empty output on a MemoryHub execution must not transiently complete", status)
	}
	if failureReason != "empty_or_unparseable_output" {
		t.Fatalf("failure_reason = %q, want empty_or_unparseable_output", failureReason)
	}

	// The evidence record must be marked failed for the gate-failure path.
	var evidenceState string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_evidence_state FROM execution_evidence_record er
		JOIN agent_task_queue atq ON atq.execution_id = er.execution_id
		WHERE atq.id = $1
	`, taskID).Scan(&evidenceState); err != nil {
		t.Fatalf("read evidence record: %v", err)
	}
	if evidenceState != "failed" {
		t.Fatalf("runtime_evidence_state = %q, want failed", evidenceState)
	}
}

var _ = db.AgentTaskQueue{}
