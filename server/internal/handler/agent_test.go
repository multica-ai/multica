package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateAgent_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Clean up any agents created by this test.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "duplicate-name-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "duplicate-name-test-agent",
		"description":          "first description",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	// First call — creates the agent.
	w1 := httptest.NewRecorder()
	testHandler.CreateAgent(w1, newRequest(http.MethodPost, "/api/agents", body))
	if w1.Code != http.StatusCreated {
		t.Fatalf("first CreateAgent: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	agentID1, _ := resp1["id"].(string)
	if agentID1 == "" {
		t.Fatalf("first CreateAgent: no id in response: %v", resp1)
	}

	// Second call — same name must be rejected with 409 Conflict.
	// The unique constraint prevents silent duplicates; the UI shows a clear error.
	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusConflict {
		t.Fatalf("second CreateAgent with duplicate name: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestDuplicateAgent_CopiesConfigSkillsAndSkipsTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	sourceID := createHandlerTestAgent(t, "duplicate-source-agent", []byte(`{"mcpServers":{"filesystem":{"command":"mcp"}}}`))
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent SET
			description = 'Source description',
			instructions = 'Source instructions',
			runtime_config = '{"mode":"fast"}'::jsonb,
			visibility = 'workspace',
			max_concurrent_tasks = 7,
			custom_env = '{"SECRET":"value"}'::jsonb,
			custom_args = '["--foo","bar"]'::jsonb,
			model = 'test-model'
		WHERE id = $1
	`, sourceID); err != nil {
		t.Fatalf("failed to update source agent: %v", err)
	}

	var skillID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'duplicate-source-skill', 'skill description', 'skill content', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("failed to create source skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_skill (agent_id, skill_id)
		VALUES ($1, $2)
	`, sourceID, skillID); err != nil {
		t.Fatalf("failed to assign source skill: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id)
		VALUES ($1, 'Duplicate source task issue', 'todo', 'medium', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("failed to create source task issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'completed', 0)
	`, sourceID, testRuntimeID, issueID); err != nil {
		t.Fatalf("failed to create source task: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/agents/"+sourceID+"/duplicate", nil)
	req = withURLParam(req, "id", sourceID)
	testHandler.DuplicateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("DuplicateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var duplicated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&duplicated); err != nil {
		t.Fatalf("DuplicateAgent: decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, duplicated.ID)
	})

	if duplicated.Name != "duplicate-source-agent Copy" {
		t.Fatalf("DuplicateAgent: expected copy name, got %q", duplicated.Name)
	}
	if duplicated.Description != "Source description" || duplicated.Instructions != "Source instructions" {
		t.Fatalf("DuplicateAgent: source text fields not copied: %#v", duplicated)
	}
	if duplicated.Visibility != "workspace" || duplicated.MaxConcurrentTasks != 7 || duplicated.Model != "test-model" {
		t.Fatalf("DuplicateAgent: settings not copied: %#v", duplicated)
	}
	if duplicated.CustomEnv["SECRET"] != "value" {
		t.Fatalf("DuplicateAgent: custom_env not copied: %#v", duplicated.CustomEnv)
	}
	if len(duplicated.CustomArgs) != 2 || duplicated.CustomArgs[0] != "--foo" || duplicated.CustomArgs[1] != "bar" {
		t.Fatalf("DuplicateAgent: custom_args not copied: %#v", duplicated.CustomArgs)
	}
	if len(duplicated.Skills) != 1 || duplicated.Skills[0].ID != skillID {
		t.Fatalf("DuplicateAgent: skills not copied: %#v", duplicated.Skills)
	}
	assertJSONEqual(t, fetchAgentMcpConfig(t, duplicated.ID), `{"mcpServers":{"filesystem":{"command":"mcp"}}}`)

	var taskCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE agent_id = $1
	`, duplicated.ID).Scan(&taskCount); err != nil {
		t.Fatalf("failed to count duplicate tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("DuplicateAgent: expected no copied tasks, got %d", taskCount)
	}
}

func TestDuplicateAgent_ChoosesNextCopyName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	sourceID := createHandlerTestAgent(t, "duplicate-name-source", nil)
	createHandlerTestAgent(t, "duplicate-name-source Copy", nil)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/agents/"+sourceID+"/duplicate", nil)
	req = withURLParam(req, "id", sourceID)
	testHandler.DuplicateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("DuplicateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var duplicated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&duplicated); err != nil {
		t.Fatalf("DuplicateAgent: decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, duplicated.ID)
	})

	if duplicated.Name != "duplicate-name-source Copy 2" {
		t.Fatalf("DuplicateAgent: expected second copy name, got %q", duplicated.Name)
	}
}

func TestDuplicateAgent_RejectsNonOwnerMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	sourceID := createHandlerTestAgent(t, "duplicate-owned-by-original", nil)

	var otherUserID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ('Duplicate Other User', 'duplicate-other-user@multica.ai')
		RETURNING id
	`).Scan(&otherUserID); err != nil {
		t.Fatalf("failed to create other user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, otherUserID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("failed to add other workspace member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/agents/"+sourceID+"/duplicate", nil)
	req.Header.Set("X-User-ID", otherUserID)
	req = withURLParam(req, "id", sourceID)
	testHandler.DuplicateAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DuplicateAgent: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "only the agent owner") {
		t.Fatalf("DuplicateAgent: expected owner error, got %s", w.Body.String())
	}
}
