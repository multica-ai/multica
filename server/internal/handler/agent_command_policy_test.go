package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const operatorControlledRuntimeConfig = `{
	"multica_policy": {
		"mode": "operator_controlled",
		"deny_commands": [
			"issue.create",
			"issue.update.status",
			"issue.status",
			"issue.update.assignee",
			"issue.assign"
		],
		"deny_agent_mentions": true,
		"allow_comment_without_agent_mentions": true
	}
}`

func createHandlerTestAgentWithRuntimeConfig(t *testing.T, name, runtimeConfig string) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', $3::jsonb, $4, 'private', 1, $5, '', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, runtimeConfig, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("failed to create policy test agent: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func createAgentPolicyTestIssue(t *testing.T, title string) IssueResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}

	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})
	return created
}

func TestAgentCommandPolicyDeniesIssueCreate(t *testing.T) {
	agentID := createHandlerTestAgentWithRuntimeConfig(t, "Policy Create Agent", operatorControlledRuntimeConfig)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "agent policy should deny create",
	})
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.CreateIssue(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateIssue: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentCommandPolicyDeniesStatusAndAssigneeUpdates(t *testing.T) {
	agentID := createHandlerTestAgentWithRuntimeConfig(t, "Policy Update Agent", operatorControlledRuntimeConfig)
	created := createAgentPolicyTestIssue(t, "agent policy update target")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+created.ID, map[string]any{
		"status": "done",
	})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateIssue status: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, created.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "todo" {
		t.Fatalf("expected issue status to remain todo, got %q", status)
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+created.ID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateIssue assignee: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentCommandPolicyDeniesAgentMentionsButAllowsPlainComment(t *testing.T) {
	agentID := createHandlerTestAgentWithRuntimeConfig(t, "Policy Comment Agent", operatorControlledRuntimeConfig)
	created := createAgentPolicyTestIssue(t, "agent policy comment target")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+created.ID+"/comments", map[string]any{
		"content": "Loop [@Policy Comment Agent](mention://agent/" + agentID + ")",
	})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateComment with agent mention: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+created.ID+"/comments", map[string]any{
		"content": "Plain implementation result with no agent mention.",
	})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment plain: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentCommandPolicyDeniesBatchStatusAndAssigneeUpdates(t *testing.T) {
	agentID := createHandlerTestAgentWithRuntimeConfig(t, "Policy Batch Agent", operatorControlledRuntimeConfig)
	created := createAgentPolicyTestIssue(t, "agent policy batch target")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{created.ID},
		"updates":   map[string]any{"status": "done"},
	})
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("BatchUpdateIssues status: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, created.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "todo" {
		t.Fatalf("expected issue status to remain todo, got %q", status)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{created.ID},
		"updates": map[string]any{
			"assignee_type": "agent",
			"assignee_id":   agentID,
		},
	})
	req.Header.Set("X-Agent-ID", agentID)
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("BatchUpdateIssues assignee: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
