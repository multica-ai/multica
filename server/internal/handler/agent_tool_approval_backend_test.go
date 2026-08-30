package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service/toolapproval"
)

func requireApprovalPolicyForTest(t *testing.T, agentID string) {
	t.Helper()
	req := newRequestAs(testUserID, http.MethodPut, "/api/agents/"+agentID+"/tool-policy", map[string]any{
		"expected_revision": 0,
		"rules": []map[string]any{{
			"transport_kind": "managed_mcp",
			"server_key":     "linear",
			"tool_name":      "list_issues",
			"schema_digest":  toolControlDigest,
			"effect":         "require_approval",
		}},
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ReplaceAgentToolPolicy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("replace approval policy status = %d: %s", w.Code, w.Body.String())
	}
}

func createApprovalForTest(t *testing.T, service *toolapproval.Service, agentID, taskID, invocationID, idempotencyKey string) toolapproval.Approval {
	t.Helper()
	approval, err := service.CreateOrGet(context.Background(), toolapproval.Creation{
		WorkspaceID:      testWorkspaceID,
		AgentID:          agentID,
		TaskID:           taskID,
		InvocationID:     invocationID,
		IdempotencyKey:   idempotencyKey,
		TransportKind:    "managed_mcp",
		ServerKey:        "linear",
		ToolName:         "list_issues",
		SchemaDigest:     toolControlDigest,
		PolicyRevision:   1,
		SchemaFieldNames: []string{"limit", "team_id"},
		ArgumentBytes:    128,
		ExpiresAt:        time.Now().UTC().Add(15 * time.Minute).Truncate(time.Microsecond),
		Actor:            toolapproval.Actor{Kind: toolapproval.ActorDaemon},
	})
	if err != nil {
		t.Fatalf("CreateOrGet() error = %v", err)
	}
	return approval
}

func TestAgentToolApprovalCreateOrGetIsIdempotentOnlyForIdenticalMetadata(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-create-or-get", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)

	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	creation := toolapproval.Creation{
		WorkspaceID:      testWorkspaceID,
		AgentID:          agentID,
		TaskID:           taskID,
		InvocationID:     "00000000-0000-4000-8000-000000000040",
		IdempotencyKey:   "invocation-1",
		TransportKind:    "managed_mcp",
		ServerKey:        "linear",
		ToolName:         "list_issues",
		SchemaDigest:     toolControlDigest,
		PolicyRevision:   1,
		SchemaFieldNames: []string{"limit", "team_id"},
		ArgumentBytes:    128,
		ExpiresAt:        time.Now().UTC().Add(15 * time.Minute).Truncate(time.Microsecond),
		Actor:            toolapproval.Actor{Kind: toolapproval.ActorDaemon},
	}

	first, err := service.CreateOrGet(context.Background(), creation)
	if err != nil {
		t.Fatalf("first CreateOrGet() error = %v", err)
	}
	second, err := service.CreateOrGet(context.Background(), creation)
	if err != nil {
		t.Fatalf("second CreateOrGet() error = %v", err)
	}
	if first.ID == "" || second.ID != first.ID {
		t.Fatalf("approval IDs = %q, %q", first.ID, second.ID)
	}

	mismatched := creation
	mismatched.ToolName = "get_issue"
	if _, err := service.CreateOrGet(context.Background(), mismatched); !errors.Is(err, toolapproval.ErrIdentityConflict) {
		t.Fatalf("mismatched CreateOrGet() error = %v, want %v", err, toolapproval.ErrIdentityConflict)
	}

	var approvals, requestedEvents int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_approval_request WHERE task_id = $1`, taskID).Scan(&approvals); err != nil {
		t.Fatalf("count approvals: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_action_event WHERE task_id = $1 AND event_type = 'approval_requested'`, taskID).Scan(&requestedEvents); err != nil {
		t.Fatalf("count approval events: %v", err)
	}
	if approvals != 1 || requestedEvents != 1 {
		t.Fatalf("approval rows/events = %d/%d, want 1/1", approvals, requestedEvents)
	}
}

func TestAgentToolApprovalDecisionCommitsStateActivityAndActionAtomically(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-decision-atomic", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)

	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	approval := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000041", "decision-1")
	decided, err := service.Decide(context.Background(), toolapproval.Decision{
		WorkspaceID: testWorkspaceID,
		ApprovalID:  approval.ID,
		Actor: toolapproval.Actor{
			Kind:          toolapproval.ActorHuman,
			UserID:        testUserID,
			WorkspaceRole: "owner",
		},
		Decision: toolapproval.DecisionApproved,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decided.Status != toolapproval.DecisionApproved || decided.ReasonCode != "operator_approved" || decided.DecidedAt == nil || decided.DecidedByUserID != testUserID {
		t.Fatalf("decision = %#v", decided)
	}

	var state, reason string
	var activityCount, actionCount int
	if err := testPool.QueryRow(context.Background(), `SELECT status, reason_code FROM agent_tool_approval_request WHERE id = $1`, approval.ID).Scan(&state, &reason); err != nil {
		t.Fatalf("read decision: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM activity_log WHERE workspace_id = $1 AND action = 'agent_tool_approval_approved' AND details->>'approval_id' = $2`, testWorkspaceID, approval.ID).Scan(&activityCount); err != nil {
		t.Fatalf("count approval activity: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_action_event WHERE approval_request_id = $1 AND event_type = 'approval_approved'`, approval.ID).Scan(&actionCount); err != nil {
		t.Fatalf("count approval action: %v", err)
	}
	if state != "approved" || reason != "operator_approved" || activityCount != 1 || actionCount != 1 {
		t.Fatalf("state/reason/activity/action = %q/%q/%d/%d", state, reason, activityCount, actionCount)
	}

	if _, err := service.Decide(context.Background(), toolapproval.Decision{
		WorkspaceID: testWorkspaceID,
		ApprovalID:  approval.ID,
		Actor: toolapproval.Actor{
			Kind:          toolapproval.ActorHuman,
			UserID:        testUserID,
			WorkspaceRole: "owner",
		},
		Decision: toolapproval.DecisionDenied,
	}); !errors.Is(err, toolapproval.ErrStateConflict) {
		t.Fatalf("second Decide() error = %v, want %v", err, toolapproval.ErrStateConflict)
	}
}

func approveForTest(t *testing.T, service *toolapproval.Service, approvalID string) {
	t.Helper()
	if _, err := service.Decide(context.Background(), toolapproval.Decision{
		WorkspaceID: testWorkspaceID,
		ApprovalID:  approvalID,
		Actor: toolapproval.Actor{
			Kind:          toolapproval.ActorHuman,
			UserID:        testUserID,
			WorkspaceRole: "owner",
		},
		Decision: toolapproval.DecisionApproved,
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
}

func TestAgentToolApprovalConsumptionIsExactlyOnceUnderConcurrency(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-consume-race", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)
	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	approval := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000042", "consume-1")
	approveForTest(t, service, approval.ID)

	const contenders = 16
	var successes atomic.Int32
	errorsByCall := make(chan error, contenders)
	var wg sync.WaitGroup
	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Consume(context.Background(), toolapproval.Consumption{
				WorkspaceID: testWorkspaceID,
				TaskID:      taskID,
				ApprovalID:  approval.ID,
				Actor:       toolapproval.Actor{Kind: toolapproval.ActorDaemon},
			})
			if err == nil {
				successes.Add(1)
				return
			}
			errorsByCall <- err
		}()
	}
	wg.Wait()
	close(errorsByCall)
	if successes.Load() != 1 {
		t.Fatalf("successful consumptions = %d, want 1", successes.Load())
	}
	for err := range errorsByCall {
		if !errors.Is(err, toolapproval.ErrStateConflict) {
			t.Fatalf("losing consumption error = %v, want %v", err, toolapproval.ErrStateConflict)
		}
	}
	var state string
	var events int
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_tool_approval_request WHERE id = $1`, approval.ID).Scan(&state); err != nil {
		t.Fatalf("read consumed approval: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_action_event WHERE approval_request_id = $1 AND event_type = 'approval_consumed'`, approval.ID).Scan(&events); err != nil {
		t.Fatalf("count consumed events: %v", err)
	}
	if state != toolapproval.StatusConsumed || events != 1 {
		t.Fatalf("state/events = %q/%d, want consumed/1", state, events)
	}
}

func TestAgentToolApprovalConsumptionRevalidatesPolicyRevision(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-policy-revalidation", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)
	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	approval := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000043", "policy-revalidate-1")
	approveForTest(t, service, approval.ID)

	req := newRequestAs(testUserID, http.MethodPut, "/api/agents/"+agentID+"/tool-policy", map[string]any{
		"expected_revision": 1,
		"rules": []map[string]any{{
			"transport_kind": "managed_mcp",
			"server_key":     "linear",
			"tool_name":      "list_issues",
			"schema_digest":  toolControlDigest,
			"effect":         "allow",
		}},
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ReplaceAgentToolPolicy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("replace policy status = %d: %s", w.Code, w.Body.String())
	}

	if _, err := service.Consume(context.Background(), toolapproval.Consumption{
		WorkspaceID: testWorkspaceID,
		TaskID:      taskID,
		ApprovalID:  approval.ID,
		Actor:       toolapproval.Actor{Kind: toolapproval.ActorDaemon},
	}); !errors.Is(err, toolapproval.ErrStateConflict) {
		t.Fatalf("Consume() error = %v, want %v", err, toolapproval.ErrStateConflict)
	}
	var state, reason string
	if err := testPool.QueryRow(context.Background(), `SELECT status, reason_code FROM agent_tool_approval_request WHERE id = $1`, approval.ID).Scan(&state, &reason); err != nil {
		t.Fatalf("read cancelled approval: %v", err)
	}
	if state != toolapproval.StatusCancelled || reason != "policy_replaced" {
		t.Fatalf("state/reason = %q/%q, want cancelled/policy_replaced", state, reason)
	}
}

func TestAgentToolApprovalCancellationIsGuardedAndAudited(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-cancel-guard", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)
	service := toolapproval.NewSQLService(testHandler.Queries, testPool)

	cancellable := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000044", "cancel-1")
	approveForTest(t, service, cancellable.ID)
	cancelled, err := service.Cancel(context.Background(), toolapproval.Cancellation{
		WorkspaceID: testWorkspaceID,
		TaskID:      taskID,
		ApprovalID:  cancellable.ID,
		Actor:       toolapproval.Actor{Kind: toolapproval.ActorDaemon},
		ReasonCode:  "task_cancelled",
	})
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelled.Status != toolapproval.StatusCancelled || cancelled.ReasonCode != "task_cancelled" {
		t.Fatalf("cancelled approval = %#v", cancelled)
	}
	if _, err := service.Cancel(context.Background(), toolapproval.Cancellation{
		WorkspaceID: testWorkspaceID,
		TaskID:      taskID,
		ApprovalID:  cancellable.ID,
		Actor:       toolapproval.Actor{Kind: toolapproval.ActorDaemon},
		ReasonCode:  "task_cancelled",
	}); !errors.Is(err, toolapproval.ErrStateConflict) {
		t.Fatalf("second Cancel() error = %v, want %v", err, toolapproval.ErrStateConflict)
	}
	var cancelledEvents int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_action_event WHERE approval_request_id = $1 AND event_type = 'cancelled'`, cancellable.ID).Scan(&cancelledEvents); err != nil {
		t.Fatalf("count cancellation events: %v", err)
	}
	if cancelledEvents != 1 {
		t.Fatalf("cancellation events = %d, want 1", cancelledEvents)
	}

	expired := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000045", "cancel-expired-1")
	approveForTest(t, service, expired.ID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_tool_approval_request
		SET requested_at = now() - interval '2 hours', expires_at = now() - interval '1 hour'
		WHERE id = $1
	`, expired.ID); err != nil {
		t.Fatalf("age approval: %v", err)
	}
	if _, err := service.Cancel(context.Background(), toolapproval.Cancellation{
		WorkspaceID: testWorkspaceID,
		TaskID:      taskID,
		ApprovalID:  expired.ID,
		Actor:       toolapproval.Actor{Kind: toolapproval.ActorDaemon},
		ReasonCode:  "task_cancelled",
	}); !errors.Is(err, toolapproval.ErrStateConflict) {
		t.Fatalf("expired Cancel() error = %v, want %v", err, toolapproval.ErrStateConflict)
	}
}

func TestAgentToolApprovalHandlersRejectMachineDecisionsAndFreeFormNotes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-handler-auth", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)
	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	approval := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000046", "handler-auth-1")

	machine := newRequestAs(testUserID, http.MethodPost, "/api/approvals/"+approval.ID+"/decision", map[string]any{"decision": "approve", "reason_code": "operator_approved", "expected_status": "pending"})
	machine = withURLParam(machine, "approvalId", approval.ID)
	machine.Header.Set("X-Actor-Source", "task_token")
	machine.Header.Set("X-Agent-ID", agentID)
	machine.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.DecideAgentToolApproval(w, machine)
	if w.Code != http.StatusForbidden {
		t.Fatalf("machine decision status = %d, want 403: %s", w.Code, w.Body.String())
	}

	freeForm := newRequestAs(testUserID, http.MethodPost, "/api/approvals/"+approval.ID+"/decision", map[string]any{
		"decision": "deny", "reason_code": "risk_too_high", "expected_status": "pending",
		"note": "raw command and arguments looked risky",
	})
	freeForm = withURLParam(freeForm, "approvalId", approval.ID)
	w = httptest.NewRecorder()
	testHandler.DecideAgentToolApproval(w, freeForm)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("free-form decision status = %d, want 400: %s", w.Code, w.Body.String())
	}

	valid := newRequestAs(testUserID, http.MethodPost, "/api/approvals/"+approval.ID+"/decision", map[string]any{"decision": "approve", "reason_code": "operator_approved", "expected_status": "pending"})
	valid = withURLParam(valid, "approvalId", approval.ID)
	w = httptest.NewRecorder()
	testHandler.DecideAgentToolApproval(w, valid)
	if w.Code != http.StatusOK {
		t.Fatalf("human decision status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestDaemonToolApprovalHandlersUseTaskScopedIdentityAndStrictMetadata(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-daemon-handler", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	issueID := createIssueForTimeline(t, "Approval daemon handler")
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = 'approval-test-daemon' WHERE id = (SELECT runtime_id FROM agent_task_queue WHERE id = $1)`, taskID)
	requireApprovalPolicyForTest(t, agentID)

	path := "/api/daemon/tasks/" + taskID + "/tool-invocations"
	requestBody := map[string]any{
		"idempotency_key": "daemon-handler-1", "transport_kind": "managed_mcp",
		"server_key": "linear", "tool_name": "list_issues", "schema_digest": toolControlDigest,
		"policy_revision": 1, "argument_bytes": 64,
	}
	strictBody := make(map[string]any, len(requestBody)+1)
	for key, value := range requestBody {
		strictBody[key] = value
	}
	strictBody["summary"] = "raw tool arguments"
	req := newDaemonTokenRequest(http.MethodPost, path, strictBody, testWorkspaceID, "approval-test-daemon")
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.CreateDaemonToolInvocation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("raw summary create status = %d, want 400: %s", w.Code, w.Body.String())
	}

	req = newDaemonTokenRequest(http.MethodPost, path, requestBody, testWorkspaceID, "approval-test-daemon")
	req = withURLParam(req, "taskId", taskID)
	w = httptest.NewRecorder()
	testHandler.CreateDaemonToolInvocation(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("daemon create status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var response struct {
		InvocationID      string `json:"invocation_id"`
		ApprovalRequestID string `json:"approval_request_id"`
		Status            string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode daemon approval: %v", err)
	}
	if response.InvocationID == "" || response.ApprovalRequestID == "" || response.Status != toolapproval.StatusPending {
		t.Fatalf("daemon approval response = %#v", response)
	}
	wrongDaemon := newDaemonTokenRequest(http.MethodPost, path, requestBody, testWorkspaceID, "different-daemon")
	wrongDaemon = withURLParam(wrongDaemon, "taskId", taskID)
	w = httptest.NewRecorder()
	testHandler.CreateDaemonToolInvocation(w, wrongDaemon)
	if w.Code != http.StatusNotFound {
		t.Fatalf("different daemon status = %d, want 404: %s", w.Code, w.Body.String())
	}

	consume := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/tool-approvals/"+response.ApprovalRequestID+"/consume", map[string]any{
		"invocation_id": response.InvocationID, "transport_kind": "managed_mcp", "server_key": "linear",
		"tool_name": "list_issues", "schema_digest": toolControlDigest, "policy_revision": 1,
	}, testWorkspaceID, "approval-test-daemon")
	consume = withURLParams(consume, "taskId", taskID, "approvalId", response.ApprovalRequestID)
	w = httptest.NewRecorder()
	testHandler.ConsumeDaemonToolApproval(w, consume)
	if w.Code != http.StatusConflict {
		t.Fatalf("pending consume status = %d, want 409: %s", w.Code, w.Body.String())
	}
	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	approveForTest(t, service, response.ApprovalRequestID)
	consume = newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/tool-approvals/"+response.ApprovalRequestID+"/consume", map[string]any{
		"invocation_id": response.InvocationID, "transport_kind": "managed_mcp", "server_key": "linear",
		"tool_name": "list_issues", "schema_digest": toolControlDigest, "policy_revision": 1,
	}, testWorkspaceID, "approval-test-daemon")
	consume = withURLParams(consume, "taskId", taskID, "approvalId", response.ApprovalRequestID)
	w = httptest.NewRecorder()
	testHandler.ConsumeDaemonToolApproval(w, consume)
	if w.Code != http.StatusOK {
		t.Fatalf("approved consume status = %d, want 200: %s", w.Code, w.Body.String())
	}

	eventBody := map[string]any{
		"event_type": "started", "transport_kind": "managed_mcp", "server_key": "linear",
		"tool_name": "list_issues", "schema_digest": toolControlDigest, "policy_revision": 1,
		"argument_bytes": 64,
		"task_message":   map[string]any{"invocation_id": response.InvocationID, "type": "tool_use", "tool": "list_issues"},
	}
	for range 2 {
		event := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/tool-invocations/"+response.InvocationID+"/events", eventBody, testWorkspaceID, "approval-test-daemon")
		event = withURLParams(event, "taskId", taskID, "invocationId", response.InvocationID)
		w = httptest.NewRecorder()
		testHandler.CommitDaemonToolInvocationEvent(w, event)
		if w.Code != http.StatusNoContent {
			t.Fatalf("started event status = %d, want 204: %s", w.Code, w.Body.String())
		}
	}
	var startedEvents, toolMessages int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_action_event WHERE task_id = $1 AND invocation_id = $2 AND event_type = 'started'`, taskID, response.InvocationID).Scan(&startedEvents); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM task_message WHERE task_id = $1 AND type = 'tool_use' AND tool = 'list_issues'`, taskID).Scan(&toolMessages); err != nil {
		t.Fatal(err)
	}
	if startedEvents != 1 || toolMessages != 1 {
		t.Fatalf("started events/messages = %d/%d, want 1/1", startedEvents, toolMessages)
	}
}

func TestAgentToolApprovalListAndDaemonGetExposeMetadataOnlyWithinScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "approval-list-get", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	issueID := createIssueForTimeline(t, "Approval list and get")
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = 'approval-test-daemon' WHERE id = (SELECT runtime_id FROM agent_task_queue WHERE id = $1)`, taskID)
	requireApprovalPolicyForTest(t, agentID)
	service := toolapproval.NewSQLService(testHandler.Queries, testPool)
	approval := createApprovalForTest(t, service, agentID, taskID, "00000000-0000-4000-8000-000000000048", "opaque-idempotency-key")

	list := newRequestAs(testUserID, http.MethodGet, "/api/approvals", nil)
	w := httptest.NewRecorder()
	testHandler.ListAgentToolApprovals(w, list)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"opaque-idempotency-key", "idempotency_key", "note", "summary"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("list response exposed forbidden field %q: %s", forbidden, body)
		}
	}
	var listed struct {
		Items []toolapproval.Approval `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode approval list: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != approval.ID {
		t.Fatalf("listed approvals = %#v", listed.Items)
	}

	get := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/tool-approvals/"+approval.ID, nil, testWorkspaceID, "approval-test-daemon")
	get = withURLParams(get, "taskId", taskID, "approvalId", approval.ID)
	w = httptest.NewRecorder()
	testHandler.GetDaemonToolApproval(w, get)
	if w.Code != http.StatusOK {
		t.Fatalf("daemon get status = %d, want 200: %s", w.Code, w.Body.String())
	}

	otherIssueID := createIssueForTimeline(t, "Approval wrong task")
	otherTaskID := createHandlerTestTaskForAgentOnIssue(t, agentID, otherIssueID)
	get = newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+otherTaskID+"/tool-approvals/"+approval.ID, nil, testWorkspaceID, "approval-test-daemon")
	get = withURLParams(get, "taskId", otherTaskID, "approvalId", approval.ID)
	w = httptest.NewRecorder()
	testHandler.GetDaemonToolApproval(w, get)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-task get status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestManagedMCPPolicyClaimPayloadRequiresAdvertisedPreTransportCapability(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "managed-policy-claim", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	requireApprovalPolicyForTest(t, agentID)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), task.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Provider = "claude"
	capability := managedMCPPreTransportCapability(runtime.Provider)
	runtime.Metadata, err = json.Marshal(map[string]any{"capabilities": []string{capability}})
	if err != nil {
		t.Fatal(err)
	}
	policy, failure := testHandler.managedMCPPolicyForClaim(context.Background(), &task, runtime)
	if failure != nil || policy == nil {
		t.Fatalf("policy/failure = %#v/%#v", policy, failure)
	}
	if policy.Capability != capability || policy.Revision != 1 || len(policy.Rules) != 1 || policy.Rules[0].Effect != "require_approval" {
		t.Fatalf("managed policy payload = %#v", policy)
	}
	raw, params, err := remoteMCPDaemonTokenForClaim(AgentTaskResponse{ManagedMCPPolicy: policy}, runtime)
	if err != nil || raw == "" || len(params) != 1 {
		t.Fatalf("managed policy daemon token = %q %#v, err=%v", raw, params, err)
	}
	if runtimeHasCapability([]byte(`{"capabilities":[]}`), capability) {
		t.Fatal("runtime without managed pre-transport capability was accepted")
	}
}
