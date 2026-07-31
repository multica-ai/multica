package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
)

func taskTokenRequest(t *testing.T, token, method, path string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("create task-token request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task-token request: %v", err)
	}
	return resp
}

func createTaskScopedPropertyFixture(t *testing.T) (token, ownIssueID, otherIssueID, propertyID string) {
	t.Helper()

	propertyResp := authRequest(t, http.MethodPost, "/api/properties", map[string]any{
		"name": "Task Scope " + uuid.NewString()[:8],
		"type": "number",
	})
	if propertyResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(propertyResp.Body)
		propertyResp.Body.Close()
		t.Fatalf("create property: expected 201, got %d: %s", propertyResp.StatusCode, body)
	}
	var property map[string]any
	readJSON(t, propertyResp, &property)
	propertyID, _ = property["id"].(string)

	createIssue := func(title string) string {
		resp := authRequest(t, http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":    title,
			"status":   "todo",
			"priority": "none",
		})
		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("create issue: expected 201, got %d: %s", resp.StatusCode, body)
		}
		var issue map[string]any
		readJSON(t, resp, &issue)
		id, _ := issue["id"].(string)
		return id
	}
	ownIssueID = createIssue("Task-scoped property owner")
	otherIssueID = createIssue("Task-scoped property other")

	ctx := context.Background()
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text, runtime_id::text
		FROM agent
		WHERE workspace_id = $1 AND runtime_id IS NOT NULL
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id
	`, agentID, runtimeID, ownIssueID).Scan(&taskID); err != nil {
		t.Fatalf("create fixture task: %v", err)
	}
	token = "mat_" + uuid.NewString()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, auth.HashToken(token), taskID, agentID, testWorkspaceID, testUserID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create fixture task token: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := testPool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID); err != nil {
			t.Errorf("delete fixture task: %v", err)
		}
		if _, err := testPool.Exec(cleanupCtx, `DELETE FROM issue WHERE id IN ($1, $2)`, ownIssueID, otherIssueID); err != nil {
			t.Errorf("delete fixture issues: %v", err)
		}
		if _, err := testPool.Exec(cleanupCtx, `DELETE FROM issue_property WHERE id = $1`, propertyID); err != nil {
			t.Errorf("delete fixture property: %v", err)
		}
		if _, err := testPool.Exec(cleanupCtx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID); err != nil {
			t.Errorf("reset fixture agent: %v", err)
		}
	})

	return token, ownIssueID, otherIssueID, propertyID
}

// CEREBRO-PATCH(task-scope-hotfix): prove the FIR-4076 compatibility boundary.
func TestTaskTokenCompatibilityModeRestoresGeneralRoutes(t *testing.T) {
	token, ownIssueID, otherIssueID, propertyID := createTaskScopedPropertyFixture(t)

	resp := taskTokenRequest(t, token, http.MethodGet, "/api/properties", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list properties: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = taskTokenRequest(t, token, http.MethodPost, "/api/properties", map[string]any{
		"name": "Agent-created definition",
		"type": "number",
	})
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create property definition: expected 403, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = taskTokenRequest(t, token, http.MethodPut, "/api/issues/"+ownIssueID+"/properties/"+propertyID, map[string]any{"value": 42})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("set own issue property: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = taskTokenRequest(t, token, http.MethodDelete, "/api/issues/"+ownIssueID+"/properties/"+propertyID, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("unset own issue property: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = taskTokenRequest(t, token, http.MethodPut, "/api/issues/"+otherIssueID+"/properties/"+propertyID, map[string]any{"value": 99})
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("set other issue property: expected 403, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// CEREBRO-PATCH(task-scope-hotfix): general platform actions must pass again.
	resp = taskTokenRequest(t, token, http.MethodGet, "/api/issues?workspace_id="+testWorkspaceID, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list issues: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = taskTokenRequest(t, token, http.MethodPut, "/api/issues/"+ownIssueID, map[string]any{
		"title": "Task-scoped issue updated",
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("update own issue: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = taskTokenRequest(t, token, http.MethodPost, "/api/issues/"+ownIssueID+"/comments", map[string]any{
		"content": "FIR-4076 rollback regression check",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("comment on own issue: expected 201, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()
}
