package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// createWorkflowRerunFixture sets up a workflow node run in the working state
// with a failed worker task that carries workflow context. It returns the IDs
// needed to trigger and verify a manual rerun.
type workflowRerunFixture struct {
	runtimeID    string
	agentID      string
	workflowID   string
	nodeID       string
	runID        string
	nodeRunID    string
	issueID      string
	sourceTaskID string
}

func createWorkflowRerunFixture(t *testing.T, ctx context.Context, suffix string) workflowRerunFixture {
	t.Helper()
	f := workflowRerunFixture{}

	// Runtime
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility
		)
		VALUES ($1, NULL, $2, 'cloud', 'handler_test_runtime', 'online', 'workflow rerun fixture', '{}'::jsonb, now(), 'private')
		RETURNING id
	`, testWorkspaceID, "rerun runtime "+suffix).Scan(&f.runtimeID); err != nil {
		t.Fatalf("setup: create runtime: %v", err)
	}

	// Agent
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, "rerun agent "+suffix, f.runtimeID, testUserID).Scan(&f.agentID); err != nil {
		t.Fatalf("setup: create agent: %v", err)
	}

	// Workflow
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow (workspace_id, title, description, status, created_by_type, created_by_id)
		VALUES ($1, $2, '', 'active', 'member', $3)
		RETURNING id
	`, testWorkspaceID, "rerun workflow "+suffix, parseUUID(testUserID)).Scan(&f.workflowID); err != nil {
		t.Fatalf("setup: create workflow: %v", err)
	}

	// Node
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node (
			workflow_id, title, description, worker_type, worker_id,
			critic_type, critic_id, sort_order
		)
		VALUES ($1, 'Rerun Node', '', 'agent', $2, 'human', NULL, 0)
		RETURNING id
	`, parseUUID(f.workflowID), parseUUID(f.agentID)).Scan(&f.nodeID); err != nil {
		t.Fatalf("setup: create node: %v", err)
	}

	// Workflow run
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_run (
			workflow_id, workspace_id, workflow_title, status,
			triggered_by_type, triggered_by_id, input
		)
		VALUES ($1, $2, 'Rerun Run', 'running', 'member', $3, '{}'::jsonb)
		RETURNING id
	`, parseUUID(f.workflowID), parseUUID(testWorkspaceID), parseUUID(testUserID)).Scan(&f.runID); err != nil {
		t.Fatalf("setup: create workflow run: %v", err)
	}

	// Node run in working state
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node_run (
			workflow_run_id, workflow_node_id, node_title, status,
			worker_type, worker_id, critic_type, critic_id
		)
		VALUES ($1, $2, 'Rerun Node', 'working', 'agent', $3, 'human', NULL)
		RETURNING id
	`, parseUUID(f.runID), parseUUID(f.nodeID), parseUUID(f.agentID)).Scan(&f.nodeRunID); err != nil {
		t.Fatalf("setup: create node run: %v", err)
	}

	// Sub-issue assigned to the agent
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_issue (
			workspace_id, title, status, priority,
			assignee_type, assignee_id,
			creator_id, creator_type, number, position
		)
		VALUES (
			$1, $2, 'in_progress', 'none',
			'agent', $3,
			$4, 'member',
			(SELECT COALESCE(MAX(number), 95000) + 1 FROM multica_issue WHERE workspace_id = $1),
			0
		)
		RETURNING id
	`, testWorkspaceID, "rerun issue "+suffix, f.agentID, testUserID).Scan(&f.issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}

	// Link issue origin to node run
	if _, err := testPool.Exec(ctx, `
		UPDATE multica_issue
		SET origin_type = 'workflow', origin_id = $1
		WHERE id = $2
	`, f.nodeRunID, f.issueID); err != nil {
		t.Fatalf("setup: link issue origin: %v", err)
	}

	// Failed source task with workflow context
	contextJSON, _ := json.Marshal(map[string]any{
		"type":                   "workflow",
		"workflow_id":            f.workflowID,
		"workflow_title":         "Rerun Run",
		"workflow_run_id":        f.runID,
		"workflow_node_id":       f.nodeID,
		"node_title":             "Rerun Node",
		"node_run_id":            f.nodeRunID,
		"phase":                  "worker",
		"worker_can_await_input": true,
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			workflow_node_run_id, context, error, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, $4, $5, 'setup failure', 'agent_error', 1, 2)
		RETURNING id
	`, parseUUID(f.agentID), parseUUID(f.runtimeID), parseUUID(f.issueID), parseUUID(f.nodeRunID), contextJSON).Scan(&f.sourceTaskID); err != nil {
		t.Fatalf("setup: create source task: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_agent_task_queue WHERE issue_id = $1 OR workflow_node_run_id = $2`, f.issueID, f.nodeRunID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_issue WHERE id = $1`, f.issueID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow_node_run WHERE id = $1`, f.nodeRunID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow_run WHERE id = $1`, f.runID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow_node WHERE id = $1`, f.nodeID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, f.workflowID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_agent WHERE id = $1`, f.agentID)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_agent_runtime WHERE id = $1`, f.runtimeID)
	})

	return f
}

// TestRerunIssue_PreservesWorkflowContext verifies that manually rerunning a
// workflow-bound task copies the workflow context (including phase) to the new
// task. Without this, HandleWorkflowTaskCompletion cannot identify the task as
// a worker-phase workflow task and the node run never advances.
func TestRerunIssue_PreservesWorkflowContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := createWorkflowRerunFixture(t, ctx, "ctx-preservation")

	// Trigger manual rerun targeting the source task row.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+f.issueID+"/rerun", map[string]any{"task_id": f.sourceTaskID})
	req = withURLParam(req, "id", f.issueID)
	testHandler.RerunIssue(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("RerunIssue: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		TaskID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rerun response: %v", err)
	}
	if resp.TaskID == "" {
		t.Fatal("rerun response missing task id")
	}

	// Load the newly created task and assert it inherited workflow context.
	var contextBytes []byte
	if err := testPool.QueryRow(ctx, `SELECT context FROM multica_agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextBytes); err != nil {
		t.Fatalf("load rerun task: %v", err)
	}
	if len(contextBytes) == 0 {
		t.Fatalf("rerun task context is empty")
	}
	var taskCtx map[string]any
	if err := json.Unmarshal(contextBytes, &taskCtx); err != nil {
		t.Fatalf("parse rerun task context: %v", err)
	}
	if taskCtx["type"] != "workflow" {
		t.Errorf("context.type = %v, want workflow", taskCtx["type"])
	}
	if taskCtx["phase"] != "worker" {
		t.Errorf("context.phase = %v, want worker", taskCtx["phase"])
	}
	if taskCtx["node_run_id"] != f.nodeRunID {
		t.Errorf("context.node_run_id = %v, want %s", taskCtx["node_run_id"], f.nodeRunID)
	}

	// The rerun should also link the node run to the new task so the UI shows
	// the correct active task.
	var workerTaskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT worker_agent_task_id FROM multica_workflow_node_run WHERE id = $1`, f.nodeRunID).Scan(&workerTaskID); err != nil {
		t.Fatalf("load node run: %v", err)
	}
	if util.UUIDToString(workerTaskID) != resp.TaskID {
		t.Errorf("node run worker_agent_task_id = %v, want %s", util.UUIDToString(workerTaskID), resp.TaskID)
	}
}

// TestRerunIssue_RebuildsEmptyWorkflowContext guards the defensive path: if the
// source task somehow has no workflow context (e.g. it was created by an older
// buggy rerun), the rerun must rebuild it from the node run so the completion
// gateway can still route the task.
func TestRerunIssue_RebuildsEmptyWorkflowContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := createWorkflowRerunFixture(t, ctx, "rebuild-ctx")

	// Simulate a source task that lost its context.
	if _, err := testPool.Exec(ctx, `UPDATE multica_agent_task_queue SET context = NULL WHERE id = $1`, f.sourceTaskID); err != nil {
		t.Fatalf("clear source task context: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+f.issueID+"/rerun", map[string]any{"task_id": f.sourceTaskID})
	req = withURLParam(req, "id", f.issueID)
	testHandler.RerunIssue(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("RerunIssue: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		TaskID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rerun response: %v", err)
	}

	var contextBytes []byte
	if err := testPool.QueryRow(ctx, `SELECT context FROM multica_agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextBytes); err != nil {
		t.Fatalf("load rerun task: %v", err)
	}
	if len(contextBytes) == 0 {
		t.Fatalf("rerun task context is empty")
	}
	var taskCtx map[string]any
	if err := json.Unmarshal(contextBytes, &taskCtx); err != nil {
		t.Fatalf("parse rerun task context: %v", err)
	}
	if taskCtx["type"] != "workflow" {
		t.Errorf("context.type = %v, want workflow", taskCtx["type"])
	}
	if taskCtx["phase"] != "worker" {
		t.Errorf("context.phase = %v, want worker", taskCtx["phase"])
	}
	if taskCtx["node_run_id"] != f.nodeRunID {
		t.Errorf("context.node_run_id = %v, want %s", taskCtx["node_run_id"], f.nodeRunID)
	}
}
