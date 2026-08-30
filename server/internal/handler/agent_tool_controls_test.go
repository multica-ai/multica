package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service/toolaction"
	"github.com/multica-ai/multica/server/internal/service/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const toolControlDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func cleanupAgentToolControlsForTest(t *testing.T, agentID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_tool_action_event WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_tool_approval_request WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_tool_policy_rule WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_tool_policy_revision WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_tool_policy WHERE agent_id = $1`, agentID)
	})
}

func replaceToolPolicyForTest(t *testing.T, actorID, agentID string, expected int64, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequestAs(actorID, http.MethodPut, "/api/agents/"+agentID+"/tool-policy", map[string]any{
		"expected_revision": expected,
		"rules": []map[string]any{
			{
				"transport_kind": "managed_mcp",
				"server_key":     " linear ",
				"tool_name":      " list_issues ",
				"schema_digest":  toolControlDigest,
				"effect":         "allow",
			},
		},
	})
	req = withURLParam(req, "id", agentID)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	testHandler.ReplaceAgentToolPolicy(w, req)
	return w
}

func TestAgentToolPolicyReplacementIsHumanOperatorOnlyAndRevisionGuarded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-policy-authz", nil)
	cleanupAgentToolControlsForTest(t, agentID)

	w := replaceToolPolicyForTest(t, testUserID, agentID, 0, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("initial replace status = %d: %s", w.Code, w.Body.String())
	}
	var policy struct {
		Configured bool  `json:"configured"`
		Revision   int64 `json:"revision"`
		Rules      []struct {
			ServerKey string `json:"server_key"`
			ToolName  string `json:"tool_name"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &policy); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if !policy.Configured || policy.Revision != 1 || len(policy.Rules) != 1 || policy.Rules[0].ServerKey != "linear" || policy.Rules[0].ToolName != "list_issues" {
		t.Fatalf("noncanonical policy response: %+v", policy)
	}

	stale := replaceToolPolicyForTest(t, testUserID, agentID, 0, nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale replace status = %d, want 409: %s", stale.Code, stale.Body.String())
	}

	memberID := createPermissionTestMember(t, "tool-policy-member@multica.test")
	member := replaceToolPolicyForTest(t, memberID, agentID, 1, nil)
	if member.Code != http.StatusForbidden {
		t.Fatalf("member replace status = %d, want 403: %s", member.Code, member.Body.String())
	}

	taskID := createHandlerTestTaskForAgent(t, agentID)
	task := replaceToolPolicyForTest(t, testUserID, agentID, 1, func(req *http.Request) {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
	})
	if task.Code != http.StatusForbidden {
		t.Fatalf("task replace status = %d, want 403: %s", task.Code, task.Body.String())
	}
	agentActor := replaceToolPolicyForTest(t, testUserID, agentID, 1, func(req *http.Request) {
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
	})
	if agentActor.Code != http.StatusForbidden {
		t.Fatalf("agent replace status = %d, want 403: %s", agentActor.Code, agentActor.Body.String())
	}

	daemon := replaceToolPolicyForTest(t, testUserID, agentID, 1, func(req *http.Request) {
		req.Header.Set("X-Actor-Source", "daemon")
	})
	if daemon.Code != http.StatusForbidden {
		t.Fatalf("daemon replace status = %d, want 403: %s", daemon.Code, daemon.Body.String())
	}

	otherWorkspaceID := createOtherTestWorkspace(t)
	crossWorkspace := replaceToolPolicyForTest(t, testUserID, agentID, 1, func(req *http.Request) {
		req.Header.Set("X-Workspace-ID", otherWorkspaceID)
	})
	if crossWorkspace.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace replace status = %d, want 404: %s", crossWorkspace.Code, crossWorkspace.Body.String())
	}
}

func TestAgentToolActionsNeverReturnRawValuesOrSecretCanaries(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-actions-metadata", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	var invocationID string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&invocationID); err != nil {
		t.Fatalf("create invocation id: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_tool_action_event (
			workspace_id, agent_id, task_id, invocation_id, transport_kind,
			server_key, tool_name, schema_digest, coverage_kind, event_type,
			argument_bytes, outcome_code
		) VALUES ($1, $2, $3, $4, 'managed_mcp', 'linear', 'list_issues', $5, 'managed_mcp', 'started', 42, 'started')
	`, testWorkspaceID, agentID, taskID, invocationID, toolControlDigest); err != nil {
		t.Fatalf("seed action event: %v", err)
	}

	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tool-actions", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ListAgentToolActions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list action status = %d: %s", w.Code, w.Body.String())
	}
	raw := w.Body.String()
	for _, forbidden := range []string{"arguments", "results", "headers", "url", "command_line", "environment", "provider_body", "secret"} {
		if strings.Contains(strings.ToLower(raw), `"`+forbidden+`"`) {
			t.Fatalf("response exposed forbidden raw field %q: %s", forbidden, raw)
		}
	}
}

func TestAgentToolControlReadsAllowOnlyTheOwnerOrTargetAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	targetID := createHandlerTestAgent(t, "tool-control-self-read", nil)
	cleanupAgentToolControlsForTest(t, targetID)
	if w := replaceToolPolicyForTest(t, testUserID, targetID, 0, nil); w.Code != http.StatusOK {
		t.Fatalf("seed policy status = %d: %s", w.Code, w.Body.String())
	}
	taskID := createHandlerTestTaskForAgent(t, targetID)
	memberID := createPermissionTestMember(t, "tool-control-read-member@multica.test")

	for _, tc := range []struct {
		name   string
		suffix string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "policy", suffix: "/tool-policy", call: testHandler.GetAgentToolPolicy},
		{name: "actions", suffix: "/tool-actions", call: testHandler.ListAgentToolActions},
	} {
		t.Run("owner_"+tc.name, func(t *testing.T) {
			req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+targetID+tc.suffix, nil), "id", targetID)
			w := httptest.NewRecorder()
			tc.call(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("agent owner read status = %d: %s", w.Code, w.Body.String())
			}
		})
		t.Run("member_"+tc.name, func(t *testing.T) {
			req := withURLParam(newRequestAs(memberID, http.MethodGet, "/api/agents/"+targetID+tc.suffix, nil), "id", targetID)
			w := httptest.NewRecorder()
			tc.call(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("plain member read status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}

	selfRequest := func(method, suffix string) *http.Request {
		req := withURLParam(newRequest(method, "/api/agents/"+targetID+suffix, nil), "id", targetID)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", targetID)
		req.Header.Set("X-Task-ID", taskID)
		return req
	}
	for _, tc := range []struct {
		name   string
		suffix string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "policy", suffix: "/tool-policy", call: testHandler.GetAgentToolPolicy},
		{name: "actions", suffix: "/tool-actions", call: testHandler.ListAgentToolActions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.call(w, selfRequest(http.MethodGet, tc.suffix))
			if w.Code != http.StatusOK {
				t.Fatalf("self read status = %d: %s", w.Code, w.Body.String())
			}
		})
	}

	siblingID := createHandlerTestAgent(t, "tool-control-sibling", nil)
	cleanupAgentToolControlsForTest(t, siblingID)
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+siblingID+"/tool-policy", nil), "id", siblingID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", targetID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.GetAgentToolPolicy(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-agent read status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestAgentToolPolicyReplacementIsAtomicWholesaleAndCancelsOldApprovals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-policy-atomic", nil)
	cleanupAgentToolControlsForTest(t, agentID)

	first := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/tool-policy", map[string]any{
		"expected_revision": 0,
		"rules": []map[string]any{
			{"transport_kind": "managed_mcp", "server_key": "zeta", "tool_name": "second", "schema_digest": toolControlDigest, "effect": "require_approval"},
			{"transport_kind": "managed_mcp", "server_key": "alpha", "tool_name": "first", "schema_digest": toolControlDigest, "effect": "allow"},
		},
	}), "id", agentID)
	firstRecorder := httptest.NewRecorder()
	testHandler.ReplaceAgentToolPolicy(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first replace status = %d: %s", firstRecorder.Code, firstRecorder.Body.String())
	}

	taskID := createHandlerTestTaskForAgent(t, agentID)
	var invocationID string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&invocationID); err != nil {
		t.Fatalf("create invocation id: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_tool_approval_request (
			workspace_id, agent_id, task_id, invocation_id, idempotency_key,
			transport_kind, server_key, tool_name, schema_digest, policy_revision,
			schema_field_names, argument_bytes, requested_at, expires_at
		) VALUES ($1, $2, $3, $4, 'replace-me', 'managed_mcp', 'zeta', 'second', $5, 1, '{}', 0, now(), now() + interval '1 hour')
	`, testWorkspaceID, agentID, taskID, invocationID, toolControlDigest); err != nil {
		t.Fatalf("seed approval: %v", err)
	}

	second := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/tool-policy", map[string]any{
		"expected_revision": 1,
		"rules": []map[string]any{
			{"transport_kind": "managed_mcp", "server_key": "new", "tool_name": "only", "schema_digest": toolControlDigest, "effect": "allow"},
		},
	}), "id", agentID)
	secondRecorder := httptest.NewRecorder()
	testHandler.ReplaceAgentToolPolicy(secondRecorder, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second replace status = %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}

	var revision int64
	var rules, revisions, cancellations int
	var approvalStatus, reason string
	if err := testPool.QueryRow(context.Background(), `SELECT revision FROM agent_tool_policy WHERE workspace_id = $1 AND agent_id = $2`, testWorkspaceID, agentID).Scan(&revision); err != nil {
		t.Fatalf("read policy revision: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy_rule WHERE workspace_id = $1 AND agent_id = $2 AND server_key = 'new' AND tool_name = 'only'`, testWorkspaceID, agentID).Scan(&rules); err != nil {
		t.Fatalf("count replacement rules: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy_revision WHERE workspace_id = $1 AND agent_id = $2`, testWorkspaceID, agentID).Scan(&revisions); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT status, reason_code FROM agent_tool_approval_request WHERE task_id = $1 AND invocation_id = $2`, taskID, invocationID).Scan(&approvalStatus, &reason); err != nil {
		t.Fatalf("read approval: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_action_event WHERE workspace_id = $1 AND task_id = $2 AND invocation_id = $3 AND event_type = 'cancelled'`, testWorkspaceID, taskID, invocationID).Scan(&cancellations); err != nil {
		t.Fatalf("count cancellation audit: %v", err)
	}
	if revision != 2 || rules != 1 || revisions != 2 || approvalStatus != "cancelled" || reason != "policy_replaced" || cancellations != 1 {
		t.Fatalf("replacement state revision=%d rules=%d revisions=%d approval=%s/%s cancellations=%d", revision, rules, revisions, approvalStatus, reason, cancellations)
	}
	var allRules int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy_rule WHERE workspace_id = $1 AND agent_id = $2`, testWorkspaceID, agentID).Scan(&allRules); err != nil {
		t.Fatalf("count all replacement rules: %v", err)
	}
	if allRules != 1 {
		t.Fatalf("wholesale replacement retained %d rules, want exactly 1", allRules)
	}
}

type failingPolicyAudit struct{}

func (failingPolicyAudit) RecordPolicyReplacement(context.Context, *db.Queries, toolpolicy.PolicyAudit) error {
	return errors.New("policy audit unavailable")
}

type failingActionRecorder struct{}

func (failingActionRecorder) RecordIn(context.Context, toolaction.EventQueries, toolaction.Event) (toolaction.Event, error) {
	return toolaction.Event{}, errors.New("action audit unavailable")
}

func TestAgentToolPolicyAuditFailureRollsBackReplacement(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-policy-audit-rollback", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	queries := db.New(testPool)
	repository := toolpolicy.NewSQLRepository(queries, testPool, toolaction.NewSQLService(queries), failingPolicyAudit{})
	service := toolpolicy.NewService(repository)
	_, err := service.Replace(context.Background(), toolpolicy.Replacement{
		WorkspaceID:      testWorkspaceID,
		AgentID:          agentID,
		Actor:            toolpolicy.Actor{Kind: toolpolicy.ActorHuman, UserID: testUserID, WorkspaceRole: "owner"},
		ExpectedRevision: 0,
		Rules:            []toolpolicy.Rule{{TransportKind: "managed_mcp", ServerKey: "linear", ToolName: "list_issues", SchemaDigest: toolControlDigest, Effect: "allow"}},
	})
	if err == nil {
		t.Fatal("replacement succeeded after policy audit failure")
	}
	var policies, rules, revisions int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy WHERE agent_id = $1`, agentID).Scan(&policies); err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy_rule WHERE agent_id = $1`, agentID).Scan(&rules); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy_revision WHERE agent_id = $1`, agentID).Scan(&revisions); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if policies != 0 || rules != 0 || revisions != 0 {
		t.Fatalf("audit failure committed policy state: policies=%d rules=%d revisions=%d", policies, rules, revisions)
	}
}

func TestAgentToolCancellationAuditFailureRollsBackReplacement(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-cancellation-audit-rollback", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	if w := replaceToolPolicyForTest(t, testUserID, agentID, 0, nil); w.Code != http.StatusOK {
		t.Fatalf("seed policy status = %d: %s", w.Code, w.Body.String())
	}
	taskID := createHandlerTestTaskForAgent(t, agentID)
	var invocationID string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&invocationID); err != nil {
		t.Fatalf("create invocation id: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_tool_approval_request (
			workspace_id, agent_id, task_id, invocation_id, idempotency_key,
			transport_kind, server_key, tool_name, schema_digest, policy_revision,
			schema_field_names, argument_bytes, requested_at, expires_at
		) VALUES ($1, $2, $3, $4, 'audit-fail', 'managed_mcp', 'linear', 'list_issues', $5, 1, '{}', 0, now(), now() + interval '1 hour')
	`, testWorkspaceID, agentID, taskID, invocationID, toolControlDigest); err != nil {
		t.Fatalf("seed approval: %v", err)
	}
	queries := db.New(testPool)
	repository := toolpolicy.NewSQLRepository(queries, testPool, failingActionRecorder{}, successfulPolicyAudit{})
	service := toolpolicy.NewService(repository)
	_, err := service.Replace(context.Background(), toolpolicy.Replacement{
		WorkspaceID:      testWorkspaceID,
		AgentID:          agentID,
		Actor:            toolpolicy.Actor{Kind: toolpolicy.ActorHuman, UserID: testUserID, WorkspaceRole: "owner"},
		ExpectedRevision: 1,
		Rules:            []toolpolicy.Rule{{TransportKind: "managed_mcp", ServerKey: "new", ToolName: "only", SchemaDigest: toolControlDigest, Effect: "allow"}},
	})
	if err == nil {
		t.Fatal("replacement succeeded after cancellation audit failure")
	}
	var revision int64
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT revision FROM agent_tool_policy WHERE agent_id = $1`, agentID).Scan(&revision); err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_tool_approval_request WHERE task_id = $1 AND invocation_id = $2`, taskID, invocationID).Scan(&status); err != nil {
		t.Fatalf("read approval: %v", err)
	}
	if revision != 1 || status != "pending" {
		t.Fatalf("failed cancellation audit committed transition: revision=%d status=%s", revision, status)
	}
}

type successfulPolicyAudit struct{}

func (successfulPolicyAudit) RecordPolicyReplacement(context.Context, *db.Queries, toolpolicy.PolicyAudit) error {
	return nil
}

func TestAgentToolActionReadFailsClosedOnStoredSecretCanary(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-action-canary", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_tool_action_event (
			workspace_id, agent_id, task_id, invocation_id, transport_kind,
			server_key, tool_name, schema_digest, coverage_kind, event_type
		) VALUES ($1, $2, $3, gen_random_uuid(), 'managed_mcp', 'linear', 'SECRET_CANARY', $4, 'managed_mcp', 'started')
	`, testWorkspaceID, agentID, taskID, toolControlDigest); err != nil {
		t.Fatalf("seed canary action: %v", err)
	}
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tool-actions", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ListAgentToolActions(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("canary read status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "canary") {
		t.Fatalf("error response returned the secret canary: %s", w.Body.String())
	}
}

func TestAgentToolPolicyReadFailsClosedOnStoredSecretCanary(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-policy-canary", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	if w := replaceToolPolicyForTest(t, testUserID, agentID, 0, nil); w.Code != http.StatusOK {
		t.Fatalf("seed policy status = %d: %s", w.Code, w.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_tool_policy_rule
		SET tool_name = 'SECRET_CANARY'
		WHERE workspace_id = $1 AND agent_id = $2
	`, testWorkspaceID, agentID); err != nil {
		t.Fatalf("seed canary policy rule: %v", err)
	}
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tool-policy", nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentToolPolicy(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("canary policy read status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "canary") {
		t.Fatalf("policy error response returned the secret canary: %s", w.Body.String())
	}
}

func TestAgentToolPolicyRejectsRawValuesAndDoesNotConsultOperatingMode(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "tool-policy-raw-values", nil)
	cleanupAgentToolControlsForTest(t, agentID)
	for _, mode := range []string{"coding", "operational", "hybrid"} {
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET operating_mode = $1 WHERE id = $2`, mode, agentID); err != nil {
			t.Fatalf("set operating mode: %v", err)
		}
		req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/tool-policy", map[string]any{
			"expected_revision": 0,
			"rules": []map[string]any{{
				"transport_kind": "managed_mcp",
				"server_key":     "linear",
				"tool_name":      "list_issues",
				"schema_digest":  toolControlDigest,
				"effect":         "allow",
				"arguments":      "raw-value",
			}},
		}), "id", agentID)
		w := httptest.NewRecorder()
		testHandler.ReplaceAgentToolPolicy(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("mode %s raw policy status = %d, want 400: %s", mode, w.Code, w.Body.String())
		}
	}
	var policies int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_tool_policy WHERE agent_id = $1`, agentID).Scan(&policies); err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if policies != 0 {
		t.Fatalf("raw policy payload persisted %d policies", policies)
	}
}

func TestRuntimeSystemAgentCleanupCancelsApprovalsAndRemovesCurrentPolicy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Tool Policy Cleanup Runtime")
	userAgentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Tool Policy Cleanup User Agent")
	systemAgentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Tool Policy Cleanup System Agent")
	cleanupAgentToolControlsForTest(t, systemAgentID)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET kind = 'system', system_key = 'tool_policy_cleanup_probe'
		WHERE id = $1
	`, systemAgentID); err != nil {
		t.Fatalf("make system agent: %v", err)
	}
	taskID := insertFixtureTask(t, ctx, runtimeID, systemAgentID, "completed", true)

	var policyID, invocationID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_tool_policy (
			workspace_id, agent_id, revision, status, policy_digest,
			created_by_user_id, updated_by_user_id
		) VALUES ($1, $2, 1, 'active', $3, $4, $4)
		RETURNING id
	`, testWorkspaceID, systemAgentID, toolControlDigest, testUserID).Scan(&policyID); err != nil {
		t.Fatalf("seed system agent policy: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_tool_policy_rule (
			workspace_id, agent_id, policy_id, transport_kind,
			server_key, tool_name, schema_digest, effect
		) VALUES ($1, $2, $3, 'managed_mcp', 'linear', 'list_issues', $4, 'allow')
	`, testWorkspaceID, systemAgentID, policyID, toolControlDigest); err != nil {
		t.Fatalf("seed system agent policy rule: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&invocationID); err != nil {
		t.Fatalf("create cleanup invocation id: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_tool_approval_request (
			workspace_id, agent_id, task_id, invocation_id, idempotency_key,
			transport_kind, server_key, tool_name, schema_digest, policy_revision,
			schema_field_names, argument_bytes, requested_at, expires_at
		) VALUES (
			$1, $2, $3, $4, 'runtime-cleanup', 'managed_mcp', 'linear',
			'list_issues', $5, 1, '{}', 0, now(), now() + interval '1 hour'
		)
	`, testWorkspaceID, systemAgentID, taskID, invocationID, toolControlDigest); err != nil {
		t.Fatalf("seed system agent approval: %v", err)
	}

	unbindRuntime(t, ctx, runtimeID, userAgentID)

	var policies, rules, cancellationEvents int
	var approvalStatus, reasonCode string
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_tool_policy WHERE agent_id = $1`, systemAgentID).Scan(&policies); err != nil {
		t.Fatalf("count cleaned policies: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_tool_policy_rule WHERE agent_id = $1`, systemAgentID).Scan(&rules); err != nil {
		t.Fatalf("count cleaned rules: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT status, reason_code FROM agent_tool_approval_request
		WHERE agent_id = $1 AND invocation_id = $2
	`, systemAgentID, invocationID).Scan(&approvalStatus, &reasonCode); err != nil {
		t.Fatalf("read cleanup approval: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_tool_action_event
		WHERE agent_id = $1 AND invocation_id = $2 AND event_type = 'cancelled'
	`, systemAgentID, invocationID).Scan(&cancellationEvents); err != nil {
		t.Fatalf("count cleanup cancellation events: %v", err)
	}
	if policies != 0 || rules != 0 || approvalStatus != "cancelled" || reasonCode != "agent_cleanup" || cancellationEvents != 1 {
		t.Fatalf("system cleanup state policies=%d rules=%d approval=%s/%s events=%d", policies, rules, approvalStatus, reasonCode, cancellationEvents)
	}
}
