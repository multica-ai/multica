package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedCollabNodeRun creates a minimal workflow → node → run → node_run chain in
// the given status and returns the node_run UUID (string). All rows are cleaned
// up on test completion. Used by the takeover/handback/finalize/binding tests.
func seedCollabNodeRun(t *testing.T, status string) string {
	t.Helper()
	ctx := context.Background()

	var workflowID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow (workspace_id, title, status, created_by_type, created_by_id)
		VALUES ($1, 'Collab Test WF', 'active', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&workflowID); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, workflowID) })

	var nodeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node (workflow_id, title, worker_type, critic_type)
		VALUES ($1, 'Collab Node', 'agent', 'api')
		RETURNING id
	`, workflowID).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_run (workflow_id, workspace_id, workflow_title, status, triggered_by_type, triggered_by_id)
		VALUES ($1, $2, 'Collab Test WF', 'running', 'member', $3)
		RETURNING id
	`, workflowID, testWorkspaceID, testUserID).Scan(&runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	var nodeRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node_run (workflow_run_id, workflow_node_id, node_title, status, worker_type, critic_type)
		VALUES ($1, $2, 'Collab Node', $3, 'agent', 'api')
		RETURNING id
	`, runID, nodeID, status).Scan(&nodeRunID); err != nil {
		t.Fatalf("seed node run: %v", err)
	}
	return nodeRunID
}

// seedHandbackNodeRun creates a blocked node_run whose worker is a real agent
// bound to testRuntimeID, with the node_run already carrying a bound CSC
// session_id + runtime_id (as if the daemon wrote the binding before takeover).
// Returns (nodeRunID, agentID, sessionID). Used by the handback-continuation
// test, which requires a dispatchable worker.
func seedHandbackNodeRun(t *testing.T) (nodeRunID, agentID, sessionID string) {
	t.Helper()
	ctx := context.Background()
	sessionID = "csc-handback-resume-001"

	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Handback Worker', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM multica_agent WHERE id = $1`, agentID) })

	var workflowID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow (workspace_id, title, status, created_by_type, created_by_id)
		VALUES ($1, 'Handback WF', 'active', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&workflowID); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, workflowID) })

	var nodeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node (workflow_id, title, worker_type, worker_id, critic_type)
		VALUES ($1, 'Handback Node', 'agent', $2, 'api')
		RETURNING id
	`, workflowID, agentID).Scan(&nodeID); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_run (workflow_id, workspace_id, workflow_title, status, triggered_by_type, triggered_by_id)
		VALUES ($1, $2, 'Handback WF', 'running', 'member', $3)
		RETURNING id
	`, workflowID, testWorkspaceID, testUserID).Scan(&runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO multica_workflow_node_run (
			workflow_run_id, workflow_node_id, node_title, status, worker_type, critic_type,
			runtime_id, session_id
		)
		VALUES ($1, $2, 'Handback Node', 'blocked', 'agent', 'api', $3, $4)
		RETURNING id
	`, runID, nodeID, testRuntimeID, sessionID).Scan(&nodeRunID); err != nil {
		t.Fatalf("seed node run: %v", err)
	}
	return nodeRunID, agentID, sessionID
}

func fetchNodeRun(t *testing.T, nodeRunID string) db.MulticaWorkflowNodeRun {
	t.Helper()
	nr, err := testHandler.Queries.GetWorkflowNodeRun(context.Background(), parseUUID(nodeRunID))
	if err != nil {
		t.Fatalf("fetch node run %s: %v", nodeRunID, err)
	}
	return nr
}

// TestTakeoverNodeRun_PausesWorkingNode covers the happy path: a working node
// transitions to blocked with completed_at left NULL (the "paused, resumable"
// discriminator) rather than set (the "stuck" rework-exhausted case).
func TestTakeoverNodeRun_PausesWorkingNode(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "working")

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/blocked", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.TakeoverNodeRun(w, req)

	if w.Code != 200 {
		t.Fatalf("takeover: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp WorkflowNodeRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "blocked" {
		t.Fatalf("expected status blocked, got %s", resp.Status)
	}
	nr := fetchNodeRun(t, nodeRunID)
	if nr.Status != "blocked" {
		t.Fatalf("db status: expected blocked, got %s", nr.Status)
	}
	if nr.CompletedAt.Valid {
		t.Fatalf("takeover must leave completed_at NULL (paused, not stuck), got %v", nr.CompletedAt)
	}
}

// TestTakeoverNodeRun_RejectsTerminalNode verifies an already-completed node
// cannot be taken over (invalid transition → 400), so the state machine guard
// is enforced at the HTTP boundary, not just in the pure-function unit test.
func TestTakeoverNodeRun_RejectsTerminalNode(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "completed")

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/blocked", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.TakeoverNodeRun(w, req)

	if w.Code != 400 {
		t.Fatalf("takeover of completed node: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandbackNodeRun_ResumesBlockedNode covers blocked → working: control
// returns to the agent and completed_at stays NULL so the run remains active.
func TestHandbackNodeRun_ResumesBlockedNode(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "blocked")

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/working", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.HandbackNodeRun(w, req)

	if w.Code != 200 {
		t.Fatalf("handback: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	nr := fetchNodeRun(t, nodeRunID)
	if nr.Status != "working" {
		t.Fatalf("db status: expected working, got %s", nr.Status)
	}
	if nr.CompletedAt.Valid {
		t.Fatalf("handback must keep completed_at NULL, got %v", nr.CompletedAt)
	}
}

// TestHandbackNodeRun_RejectsNonBlocked verifies handback only applies to a
// blocked node; a working node yields 400.
func TestHandbackNodeRun_RejectsNonBlocked(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "working")

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/working", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.HandbackNodeRun(w, req)

	if w.Code != 400 {
		t.Fatalf("handback of working node: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestFinalizeNodeRun_ApprovedCompletes covers a human concluding a taken-over
// node positively (blocked → completed) instead of handing it back.
func TestFinalizeNodeRun_ApprovedCompletes(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "blocked")
	approved := true

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/finalize", FinalizeNodeRunRequest{Approved: &approved}),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.FinalizeNodeRun(w, req)

	if w.Code != 200 {
		t.Fatalf("finalize(approved): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	nr := fetchNodeRun(t, nodeRunID)
	if nr.Status != "completed" {
		t.Fatalf("db status: expected completed, got %s", nr.Status)
	}
}

// TestFinalizeNodeRun_RejectedFails covers the negative outcome
// (blocked → failed) when the human rejects the work.
func TestFinalizeNodeRun_RejectedFails(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "blocked")
	approved := false

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/finalize", FinalizeNodeRunRequest{Approved: &approved}),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.FinalizeNodeRun(w, req)

	if w.Code != 200 {
		t.Fatalf("finalize(rejected): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	nr := fetchNodeRun(t, nodeRunID)
	if nr.Status != "failed" {
		t.Fatalf("db status: expected failed, got %s", nr.Status)
	}
}

// TestBindNodeRunSession_DaemonWritesBinding covers the daemon write-back: the
// runtime/device/session triple is persisted and locatable by session_id so
// Cloud Web can later attach to the live CSC session.
func TestBindNodeRunSession_DaemonWritesBinding(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "working")
	const sessionID = "csc-session-collab-test-001"
	const deviceID = "device-collab-test-001"

	body := BindNodeRunSessionRequest{
		RuntimeID: testRuntimeID,
		DeviceID:  deviceID,
		SessionID: sessionID,
	}
	req := withURLParam(
		newDaemonTokenRequest("POST", "/api/daemon/node-runs/"+nodeRunID+"/session", body, testWorkspaceID, ""),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.BindNodeRunSession(w, req)

	if w.Code != 204 {
		t.Fatalf("bind session: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	nr := fetchNodeRun(t, nodeRunID)
	if nr.SessionID.String != sessionID {
		t.Fatalf("session_id: expected %q, got %q", sessionID, nr.SessionID.String)
	}
	if nr.DeviceID.String != deviceID {
		t.Fatalf("device_id: expected %q, got %q", deviceID, nr.DeviceID.String)
	}
	if uuidToString(nr.RuntimeID) != testRuntimeID {
		t.Fatalf("runtime_id: expected %q, got %q", testRuntimeID, uuidToString(nr.RuntimeID))
	}

	// The binding must be locatable by session_id — this is the lookup Cloud
	// Web uses to attach to the live session.
	found, err := testHandler.Queries.GetWorkflowNodeRunBySessionID(
		context.Background(), pgtype.Text{String: sessionID, Valid: true},
	)
	if err != nil {
		t.Fatalf("lookup by session_id: %v", err)
	}
	if uuidToString(found.ID) != nodeRunID {
		t.Fatalf("lookup by session_id returned wrong node run: %s != %s", uuidToString(found.ID), nodeRunID)
	}
}

// TestBlockedNodeRun_DoesNotAutoAdvanceOnWorkerOutput is the core B3 contract:
// while a node is taken over (status=blocked), the daemon's agent may still
// finish its turn and submit worker output — but that output must NOT advance
// the workflow (the human, via takeover, owns the node now). The existing
// state-machine guard (worker output only advances from working/worker_assigned)
// enforces this; this test pins it so a future refactor can't silently let a
// taken-over node auto-progress past the human.
func TestBlockedNodeRun_DoesNotAutoAdvanceOnWorkerOutput(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "blocked")

	err := testHandler.WorkflowService.SubmitWorkerOutput(
		context.Background(), parseUUID(nodeRunID), json.RawMessage(`{"output":"agent finished during takeover"}`),
	)
	if err == nil {
		t.Fatalf("expected SubmitWorkerOutput to reject a blocked node, got nil error")
	}

	nr := fetchNodeRun(t, nodeRunID)
	if nr.Status != "blocked" {
		t.Fatalf("taken-over node must stay blocked, advanced to %s", nr.Status)
	}
}

// to a different workspace cannot bind a session onto this workspace's node run
// (404, not a silent success — guarding the #1661-class bug).
func TestBindNodeRunSession_RejectsForeignWorkspace(t *testing.T) {
	nodeRunID := seedCollabNodeRun(t, "working")

	body := BindNodeRunSessionRequest{SessionID: "should-not-bind"}
	// Daemon token for a random other workspace.
	req := withURLParam(
		newDaemonTokenRequest("POST", "/api/daemon/node-runs/"+nodeRunID+"/session", body, "00000000-0000-0000-0000-0000000000ff", ""),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.BindNodeRunSession(w, req)

	if w.Code != 404 {
		t.Fatalf("foreign-workspace bind: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	nr := fetchNodeRun(t, nodeRunID)
	if nr.SessionID.Valid {
		t.Fatalf("foreign bind must not write session_id, got %q", nr.SessionID.String)
	}
}

// TestGetSessionPermission_AllowsWorkspaceMember covers the cross-system seam:
// a session_id bound to a node_run can be resolved by cs-cloud to check whether
// the calling Casdoor user belongs to the workspace.
func TestGetSessionPermission_AllowsWorkspaceMember(t *testing.T) {
	nodeRunID, _, sessionID := seedHandbackNodeRun(t)

	req := withURLParam(
		newRequest("GET", "/api/sessions/"+sessionID+"/permission", nil),
		"sessionId", sessionID,
	)
	w := httptest.NewRecorder()
	testHandler.GetSessionPermission(w, req)

	if w.Code != 200 {
		t.Fatalf("get session permission: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SessionPermissionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.HasAccess {
		t.Fatalf("workspace member should have access")
	}
	if resp.NodeRunID != nodeRunID {
		t.Fatalf("node_run_id: expected %q, got %q", nodeRunID, resp.NodeRunID)
	}
	if resp.SessionID != sessionID {
		t.Fatalf("session_id: expected %q, got %q", sessionID, resp.SessionID)
	}
}

// TestGetSessionPermission_NotFound covers the case where cs-cloud asks about a
// session_id that is not bound to any node_run.
func TestGetSessionPermission_NotFound(t *testing.T) {
	req := withURLParam(
		newRequest("GET", "/api/sessions/non-existent-session/permission", nil),
		"sessionId", "non-existent-session",
	)
	w := httptest.NewRecorder()
	testHandler.GetSessionPermission(w, req)

	if w.Code != 404 {
		t.Fatalf("unknown session: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandbackNodeRun_DispatchesWorkerResume is the B3 contract: handing a
// taken-over node back to its agent (blocked → working) must re-dispatch a
// worker task linked to the node_run, so the daemon resumes execution. The
// dispatched task targets the node_run's bound runtime; the daemon claim
// handler resolves the resume session from the node_run's bound session_id.
func TestHandbackNodeRun_DispatchesWorkerResume(t *testing.T) {
	nodeRunID, _, _ := seedHandbackNodeRun(t)

	req := withURLParam(
		newRequest("POST", "/api/node-runs/"+nodeRunID+"/working", nil),
		"nodeRunId", nodeRunID,
	)
	w := httptest.NewRecorder()
	testHandler.HandbackNodeRun(w, req)

	if w.Code != 200 {
		t.Fatalf("handback: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	nr := fetchNodeRun(t, nodeRunID)
	if nr.Status != "working" {
		t.Fatalf("expected status working, got %s", nr.Status)
	}
	if !nr.WorkerAgentTaskID.Valid {
		t.Fatalf("handback must link a worker task (worker_agent_task_id), got NULL")
	}

	// The dispatched task must target the bound runtime so the daemon that owns
	// the CSC session is the one that claims the resume.
	task, err := testHandler.Queries.GetAgentTask(context.Background(), nr.WorkerAgentTaskID)
	if err != nil {
		t.Fatalf("fetch dispatched task: %v", err)
	}
	if uuidToString(task.RuntimeID) != testRuntimeID {
		t.Fatalf("dispatched task runtime: expected %q, got %q", testRuntimeID, uuidToString(task.RuntimeID))
	}
	if uuidToString(task.WorkflowNodeRunID) != nodeRunID {
		t.Fatalf("dispatched task must carry node_run_id %q, got %q", nodeRunID, uuidToString(task.WorkflowNodeRunID))
	}
}
