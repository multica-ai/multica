package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedDispatchAuditTask creates a running task with an optional
// dispatch_runtime_audit jsonb. It mirrors seedNULTask's issue-backed shape so
// requireDaemonTaskAccess resolves the workspace and the daemon report endpoint
// is reachable.
func seedDispatchAuditTask(t *testing.T, label, audit string) (taskID string) {
	t.Helper()
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, testWorkspaceID, label, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, $2, 'in_progress', 'none', $3, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id`, testWorkspaceID, label+" fixture", testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// audit == "" seeds a NULL column (the common no-fallback dispatch);
	// otherwise the provided jsonb literal seeds the failover evidence.
	var auditArg any
	if audit != "" {
		auditArg = []byte(audit)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, dispatch_runtime_audit)
		VALUES ($1, $2, $3, 'running', 0, now(), $4)
		RETURNING id`, agentID, handlerTestRuntimeID(t), issueID, auditArg).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// TestSetTaskDispatchModelActionMergesOnlyExistingAudit pins the F6 merge
// contract: the daemon's model_action enriches an existing failover audit but
// must never fabricate one on a plain (no-fallback) dispatch. The query gates
// on dispatch_runtime_audit IS NOT NULL for exactly that reason.
func TestSetTaskDispatchModelActionMergesOnlyExistingAudit(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("no database")
	}
	ctx := context.Background()

	withAudit := seedDispatchAuditTask(t, "dispatch-audit-merge",
		`{"reason":"runtime_failover","target_provider":"codex"}`)
	withoutAudit := seedDispatchAuditTask(t, "dispatch-audit-noop", "")

	// Merge into the failover audit.
	if err := testHandler.Queries.SetTaskDispatchModelAction(ctx, db.SetTaskDispatchModelActionParams{
		ID:          parseUUID(withAudit),
		ModelAction: "qualified",
	}); err != nil {
		t.Fatalf("SetTaskDispatchModelAction (with audit): %v", err)
	}
	var merged []byte
	testPool.QueryRow(ctx, `SELECT dispatch_runtime_audit FROM agent_task_queue WHERE id = $1`, withAudit).Scan(&merged)
	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged audit is not json: %v (%s)", err, merged)
	}
	if got["model_action"] != "qualified" {
		t.Errorf("model_action = %v, want qualified", got["model_action"])
	}
	if got["reason"] != "runtime_failover" {
		t.Errorf("reason clobbered = %v, want runtime_failover preserved", got["reason"])
	}

	// No-op against a task without an audit: the column stays NULL, no stray
	// {"model_action":...} object is stamped onto a plain dispatch.
	if err := testHandler.Queries.SetTaskDispatchModelAction(ctx, db.SetTaskDispatchModelActionParams{
		ID:          parseUUID(withoutAudit),
		ModelAction: "cleared_incompatible",
	}); err != nil {
		t.Fatalf("SetTaskDispatchModelAction (without audit): %v", err)
	}
	var after []byte
	testPool.QueryRow(ctx, `SELECT dispatch_runtime_audit FROM agent_task_queue WHERE id = $1`, withoutAudit).Scan(&after)
	if after != nil {
		t.Errorf("no-fallback task gained an audit = %s, want NULL", after)
	}
}

// TestReportTaskDispatchModelActionEndpoint drives the daemon report endpoint
// end to end: a valid action returns 200 and lands in the audit; an action
// outside the closed set is rejected 400 before any write.
func TestReportTaskDispatchModelActionEndpoint(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("no database")
	}
	taskID := seedDispatchAuditTask(t, "dispatch-audit-endpoint",
		`{"reason":"runtime_failover","target_provider":"codex"}`)

	// Invalid model_action is rejected before touching the row.
	badReq := daemonTaskRequest(t,
		"/api/daemon/tasks/"+taskID+"/dispatch-model-action", taskID,
		map[string]string{"model_action": "bogus"})
	testutil.Call(t, testHandler.ReportTaskDispatchModelAction, badReq).Want(http.StatusBadRequest)

	// Valid action is accepted and persisted into the existing audit.
	okReq := daemonTaskRequest(t,
		"/api/daemon/tasks/"+taskID+"/dispatch-model-action", taskID,
		map[string]string{"model_action": "cleared_unresolved"})
	testutil.Call(t, testHandler.ReportTaskDispatchModelAction, okReq).Want(http.StatusOK)

	var raw []byte
	testPool.QueryRow(context.Background(),
		`SELECT dispatch_runtime_audit FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("audit not json: %v (%s)", err, raw)
	}
	if got["model_action"] != "cleared_unresolved" {
		t.Errorf("model_action = %v, want cleared_unresolved", got["model_action"])
	}
}

// TestTaskToResponseSerializesDispatchAudit is a pure serialization check (no
// DB): the durable evidence must surface in task detail so "run_only evidence
// lives in task/run detail" (F6). A task without an audit omits the field.
func TestTaskToResponseSerializesDispatchAudit(t *testing.T) {
	audit := []byte(`{"reason":"runtime_failover","target_provider":"codex","model_action":"qualified"}`)
	withAudit := taskToResponse(db.AgentTaskQueue{DispatchRuntimeAudit: audit}, testWorkspaceID)
	if len(withAudit.DispatchRuntimeAudit) == 0 {
		t.Fatal("dispatch_runtime_audit not serialized")
	}
	var got map[string]any
	if err := json.Unmarshal(withAudit.DispatchRuntimeAudit, &got); err != nil {
		t.Fatalf("serialized audit not json: %v", err)
	}
	if got["model_action"] != "qualified" || got["target_provider"] != "codex" {
		t.Errorf("serialized audit = %v, want model_action/target_provider preserved", got)
	}

	// omitempty: a plain dispatch carries no audit field on the wire.
	plain := taskToResponse(db.AgentTaskQueue{}, testWorkspaceID)
	if len(plain.DispatchRuntimeAudit) != 0 {
		t.Errorf("plain task emitted an audit = %s, want omitted", plain.DispatchRuntimeAudit)
	}
}
