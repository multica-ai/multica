package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func requireCollaborationRequestRouteTestTable(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("database not available")
	}
	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT to_regclass('public.collaboration_request') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("check collaboration_request table: %v", err)
	}
	if !exists {
		t.Skip("collaboration_request table not present; run migration 068 on a disposable DB to execute DB-backed route tests")
	}
}

func collaborationRouteRuntimeConfig(allowedTargets ...string) string {
	encodedTargets, err := json.Marshal(allowedTargets)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{
		"multica_policy": {
			"schema_version": "mhs19.v1",
			"mode": "supervised_collaboration",
			"collaboration": {
				"enabled": true,
				"scope": "same_issue",
				"allowed_agent_targets": %s,
				"raw_agent_mentions": "deny",
				"collaboration_requests": "allow_audited",
				"max_turns": 2,
				"max_depth": 2,
				"ttl_minutes": 60,
				"prevent_self_handoff": true,
				"prevent_cycles": true
			}
		}
	}`, string(encodedTargets))
}

func createCollaborationRouteAgent(t *testing.T, name string, runtimeConfig string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("load integration test runtime: %v", err)
	}
	uniqueName := fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', $3::jsonb, $4, 'private', 1, $5)
		RETURNING id
	`, testWorkspaceID, uniqueName, runtimeConfig, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create collaboration route agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func collaborationRouteAgentName(t *testing.T, agentID string) string {
	t.Helper()
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM agent WHERE id = $1`, agentID).Scan(&name); err != nil {
		t.Fatalf("load route agent name: %v", err)
	}
	return name
}

func createCollaborationRouteIssue(t *testing.T, title string) string {
	t.Helper()
	resp := authRequest(t, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  fmt.Sprintf("%s %d", title, time.Now().UnixNano()),
		"status": "todo",
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateIssue: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var issue map[string]any
	readJSON(t, resp, &issue)
	issueID := issue["id"].(string)
	t.Cleanup(func() {
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})
	return issueID
}

func createCollaborationRouteTask(t *testing.T, agentID, issueID, status string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load route agent runtime: %v", err)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, $4, 0)
		RETURNING id
	`, agentID, runtimeID, issueID, status).Scan(&taskID); err != nil {
		t.Fatalf("create route task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func authRequestWithAgentTask(t *testing.T, method, path string, body any, agentID, taskID string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}
	if taskID != "" {
		req.Header.Set("X-Task-ID", taskID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func newCollaborationRouteFixture(t *testing.T) (sourceID, targetID, sourceTaskID, issueID string) {
	t.Helper()
	targetID = createCollaborationRouteAgent(t, "Route Reviewer", collaborationRouteRuntimeConfig())
	targetName := collaborationRouteAgentName(t, targetID)
	sourceID = createCollaborationRouteAgent(t, "Route Builder", collaborationRouteRuntimeConfig(targetName))
	issueID = createCollaborationRouteIssue(t, "route collaboration request")
	sourceTaskID = createCollaborationRouteTask(t, sourceID, issueID, "running")
	return sourceID, targetID, sourceTaskID, issueID
}

func TestCollaborationRequestRoutesRejectMemberActor(t *testing.T) {
	requireCollaborationRequestRouteTestTable(t)
	_, targetID, _, issueID := newCollaborationRouteFixture(t)

	resp := authRequest(t, "POST", "/api/issues/"+issueID+"/collaboration-requests", map[string]any{
		"to_agent_id": targetID,
		"purpose":     "Please review this same issue.",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}
}

func TestCollaborationRequestRoutesSuccessAndList(t *testing.T) {
	requireCollaborationRequestRouteTestTable(t)
	sourceID, targetID, sourceTaskID, issueID := newCollaborationRouteFixture(t)

	resp := authRequestWithAgentTask(t, "POST", "/api/issues/"+issueID+"/collaboration-requests", map[string]any{
		"to_agent_id": targetID,
		"purpose":     "Please review this implementation on the same issue.",
	}, sourceID, sourceTaskID)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	if created["status"] != "queued" || created["mode"] != "discussion_only" {
		t.Fatalf("unexpected response: %+v", created)
	}
	if created["trigger_comment_id"] == nil || created["target_task_id"] == nil {
		t.Fatalf("expected trigger_comment_id and target_task_id: %+v", created)
	}

	resp = authRequest(t, "GET", "/api/issues/"+issueID+"/collaboration-requests", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected list 200, got %d: %s", resp.StatusCode, body)
	}
	var items []map[string]any
	readJSON(t, resp, &items)
	if len(items) == 0 || items[0]["id"] != created["id"] {
		t.Fatalf("expected list to include created request, got %+v", items)
	}
}
