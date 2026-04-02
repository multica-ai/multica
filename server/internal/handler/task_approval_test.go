package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Helper to create a full test fixture: user, runtime, agent (with approval_required), issue.
// Returns cleanup function and IDs.
type approvalFixture struct {
	ownerID   string
	otherID   string
	runtimeID string
	agentID   string
	issueID   string
}

func setupApprovalFixture(t *testing.T) approvalFixture {
	t.Helper()
	ctx := context.Background()

	// Runtime owner
	var ownerID string
	err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('RuntimeOwner', 'approval-owner@test.ai') RETURNING id`).Scan(&ownerID)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	_, err = testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, ownerID)
	if err != nil {
		t.Fatalf("add owner member: %v", err)
	}

	// Another user (requester)
	var otherID string
	err = testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('TaskRequester', 'approval-requester@test.ai') RETURNING id`).Scan(&otherID)
	if err != nil {
		t.Fatalf("create requester: %v", err)
	}
	_, err = testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, otherID)
	if err != nil {
		t.Fatalf("add requester member: %v", err)
	}

	// Runtime owned by owner
	var runtimeID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility)
		VALUES ($1, 'approval-daemon', 'Approval Runtime', 'local', 'claude', 'online', '', '{}'::jsonb, $2, 'workspace')
		RETURNING id
	`, testWorkspaceID, ownerID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	// Agent with approval_required=true
	var agentID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, tools, triggers, approval_required)
		VALUES ($1, 'Approval Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, $3, '[]'::jsonb, '[{"type":"on_assign","enabled":true}]'::jsonb, true)
		RETURNING id
	`, testWorkspaceID, runtimeID, ownerID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Issue created by otherUser, assigned to the agent
	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number, position)
		VALUES ($1, 'Approval Test Issue', 'todo', 'none', 'member', $2, 'agent', $3, 9950, 0)
		RETURNING id
	`, testWorkspaceID, otherID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id IN ($1, $2)`, ownerID, otherID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id IN ($1, $2)`, ownerID, otherID)
	})

	return approvalFixture{
		ownerID:   ownerID,
		otherID:   otherID,
		runtimeID: runtimeID,
		agentID:   agentID,
		issueID:   issueID,
	}
}

// ---------- Cross-user task on approval_required agent -> pending_approval ----------

func TestEnqueueCrossUserTaskCreatesPendingApproval(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	// EnqueueTaskForIssue should create a pending_approval task when:
	// - agent.approval_required = true
	// - requester (otherID) != runtime owner (ownerID)
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(f.issueID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}

	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, f.otherID)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	if task.Status != "pending_approval" {
		t.Fatalf("expected status 'pending_approval', got '%s'", task.Status)
	}
	if uuidToString(task.RequestedBy) != f.otherID {
		t.Fatalf("expected requested_by=%s, got %s", f.otherID, uuidToString(task.RequestedBy))
	}
}

// ---------- Self-assigned task skips approval ----------

func TestEnqueueSelfAssignedTaskSkipsApproval(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(f.issueID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}

	// When the runtime owner triggers the task, it should skip approval
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, f.ownerID)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	if task.Status != "queued" {
		t.Fatalf("expected status 'queued' (self-assign skips approval), got '%s'", task.Status)
	}
}

// ---------- Agent without approval_required always queues ----------

func TestEnqueueNoApprovalRequiredAlwaysQueues(t *testing.T) {
	ctx := context.Background()

	// Create a fixture with approval_required=false
	var ownerID, otherID, runtimeID, agentID, issueID string
	err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('NoApprOwner', 'noappr-owner@test.ai') RETURNING id`).Scan(&ownerID)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	_, _ = testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, ownerID)

	err = testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('NoApprOther', 'noappr-other@test.ai') RETURNING id`).Scan(&otherID)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	_, _ = testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, otherID)

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'noappr-daemon', 'NoAppr Runtime', 'local', 'claude', 'online', '', '{}'::jsonb, $2) RETURNING id
	`, testWorkspaceID, ownerID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, tools, triggers, approval_required)
		VALUES ($1, 'NoAppr Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, $3, '[]'::jsonb, '[{"type":"on_assign","enabled":true}]'::jsonb, false)
		RETURNING id
	`, testWorkspaceID, runtimeID, ownerID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number, position)
		VALUES ($1, 'NoAppr Issue', 'todo', 'none', 'member', $2, 'agent', $3, 9951, 0) RETURNING id
	`, testWorkspaceID, otherID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id IN ($1, $2)`, ownerID, otherID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id IN ($1, $2)`, ownerID, otherID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}

	// Cross-user but approval_required=false -> should go straight to queued
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, otherID)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	if task.Status != "queued" {
		t.Fatalf("expected status 'queued' (no approval required), got '%s'", task.Status)
	}
}

// ---------- Approve task endpoint ----------

func TestApproveTask(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	issue, _ := testHandler.Queries.GetIssue(ctx, parseUUID(f.issueID))
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, f.otherID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if task.Status != "pending_approval" {
		t.Fatalf("precondition: expected pending_approval, got %s", task.Status)
	}

	taskID := uuidToString(task.ID)

	// Runtime owner approves
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/"+taskID+"/approve", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.ownerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "taskId", taskID)
	testHandler.ApproveTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ApproveTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify task is now queued
	approved, _ := testHandler.Queries.GetAgentTask(ctx, task.ID)
	if approved.Status != "queued" {
		t.Fatalf("after approve: expected 'queued', got '%s'", approved.Status)
	}
}

// ---------- Reject task endpoint ----------

func TestRejectTask(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	issue, _ := testHandler.Queries.GetIssue(ctx, parseUUID(f.issueID))
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, f.otherID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	taskID := uuidToString(task.ID)

	// Runtime owner rejects
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/"+taskID+"/reject", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.ownerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "taskId", taskID)
	testHandler.RejectTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RejectTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify task is cancelled
	rejected, _ := testHandler.Queries.GetAgentTask(ctx, task.ID)
	if rejected.Status != "cancelled" {
		t.Fatalf("after reject: expected 'cancelled', got '%s'", rejected.Status)
	}
}

// ---------- Non-owner cannot approve ----------

func TestNonOwnerCannotApprove(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	issue, _ := testHandler.Queries.GetIssue(ctx, parseUUID(f.issueID))
	task, _ := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, f.otherID)
	taskID := uuidToString(task.ID)

	// Other user (not runtime owner) tries to approve -- should fail
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/"+taskID+"/approve", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.otherID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "taskId", taskID)
	testHandler.ApproveTask(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("ApproveTask by non-owner: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------- pending_approval not claimable by daemon ----------

func TestPendingApprovalNotClaimable(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	issue, _ := testHandler.Queries.GetIssue(ctx, parseUUID(f.issueID))
	_, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue, f.otherID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Try to claim -- should return nil (no claimable task)
	claimed, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(f.runtimeID))
	if err != nil {
		t.Fatalf("ClaimTaskForRuntime: %v", err)
	}
	if claimed != nil {
		t.Fatalf("pending_approval task should not be claimable, got task %s", uuidToString(claimed.ID))
	}
}

// ---------- Agent response includes approval_required ----------

func TestAgentResponseIncludesApprovalRequired(t *testing.T) {
	f := setupApprovalFixture(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/agents/"+f.agentID, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.ownerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", f.agentID)
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	approvalRequired, ok := body["approval_required"]
	if !ok {
		t.Fatal("expected 'approval_required' field in agent response")
	}
	if approvalRequired != true {
		t.Fatalf("expected approval_required=true, got %v", approvalRequired)
	}
}

// ---------- Create agent with approval_required ----------

func TestCreateAgentWithApprovalRequired(t *testing.T) {
	ctx := context.Background()
	f := setupApprovalFixture(t)

	var runtimeID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'appr-create-daemon', 'Appr Create Runtime', 'local', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, f.ownerID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE runtime_id = $1`, runtimeID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]any{
		"name":              "Approval Create Agent",
		"runtime_id":        runtimeID,
		"approval_required": true,
	})
	req := httptest.NewRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.ownerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["approval_required"] != true {
		t.Fatalf("expected approval_required=true in response, got %v", resp["approval_required"])
	}
}

// ---------- Update agent approval_required ----------

func TestUpdateAgentApprovalRequired(t *testing.T) {
	f := setupApprovalFixture(t)

	// Verify it starts as true
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/agents/"+f.agentID, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.ownerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", f.agentID)
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update approval_required to false
	w = httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]any{
		"approval_required": false,
	})
	req = httptest.NewRequest("PUT", "/api/agents/"+f.agentID, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.ownerID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", f.agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated map[string]any
	json.NewDecoder(w.Body).Decode(&updated)
	if updated["approval_required"] != false {
		t.Fatalf("expected approval_required=false after update, got %v", updated["approval_required"])
	}
}
