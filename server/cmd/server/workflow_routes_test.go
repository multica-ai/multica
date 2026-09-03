package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

func workflowTestUser(t *testing.T, role string) (userID, token string) {
	t.Helper()
	email := fmt.Sprintf("workflow-%s-%d@multica.test", role, time.Now().UnixNano())
	if err := testPool.QueryRow(context.Background(), `INSERT INTO "user" (name,email) VALUES ('Workflow Route Test',$1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,$3)`, testWorkspaceID, userID, role); err != nil {
		t.Fatal(err)
	}
	var err error
	token, err = generateTestJWT(userID, email, "Workflow Route Test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, userID)
	})
	return userID, token
}

func workflowAuthRequest(t *testing.T, token, method, path string, body any) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, testServer.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func workflowTaskToken(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id::text FROM agent WHERE workspace_id=$1 ORDER BY created_at LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	taskID := ensureAgentTask(t, agentID)
	token, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatal(err)
	}
	var tokenID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id::text
	`, auth.HashToken(token), taskID, agentID, testWorkspaceID, testUserID, time.Now().Add(time.Hour)).Scan(&tokenID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_token WHERE id=$1`, tokenID)
	})
	return token
}

func workflowRouteStartFixture(t *testing.T) (definitionID, parentID string) {
	t.Helper()
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workflow_definition (workspace_id,name,version,definition,created_by)
		VALUES ($1,$2,1,$3::jsonb,$4) RETURNING id::text
	`, testWorkspaceID, fmt.Sprintf("Task token flow %d", time.Now().UnixNano()), `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`, testUserID).Scan(&definitionID); err != nil {
		t.Fatal(err)
	}
	var base int32
	if err := testPool.QueryRow(ctx, `SELECT COALESCE(max(number),0)+10 FROM issue WHERE workspace_id=$1`, testWorkspaceID).Scan(&base); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number)
		VALUES ($1,'Workflow Route Parent','todo','none','member',$2,0,$3) RETURNING id::text
	`, testWorkspaceID, testUserID, base).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,parent_issue_id,stage,position,number)
		VALUES ($1,'Workflow Route Child','backlog','none','member',$2,$3,1,0,$4)
	`, testWorkspaceID, testUserID, parentID, base+1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id=$1 AND parent_issue_id=$2`, testWorkspaceID, parentID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id=$1 AND id=$2`, testWorkspaceID, parentID)
		testPool.Exec(context.Background(), `DELETE FROM workflow_definition WHERE workspace_id=$1 AND id=$2`, testWorkspaceID, definitionID)
	})
	return definitionID, parentID
}

func TestWorkflowRoutesRoleAndMachineActorGuards(t *testing.T) {
	_, memberToken := workflowTestUser(t, "member")
	_, adminToken := workflowTestUser(t, "admin")
	valid := map[string]any{
		"name": fmt.Sprintf("Route Flow %d", time.Now().UnixNano()),
		"definition": map[string]any{
			"schema_version": 1,
			"stages":         []map[string]string{{"key": "build", "name": "Build"}},
		},
	}

	resp := workflowAuthRequest(t, memberToken, http.MethodGet, "/api/workflow-definitions", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member list status = %d, want 200", resp.StatusCode)
	}
	resp = workflowAuthRequest(t, memberToken, http.MethodPost, "/api/workflow-definitions", valid)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member create status = %d, want 403", resp.StatusCode)
	}
	resp = workflowAuthRequest(t, adminToken, http.MethodPost, "/api/workflow-definitions", valid)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("admin create status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workflow_definition WHERE id=$1`, created.ID)
	})

	taskToken := workflowTaskToken(t)
	resp = workflowAuthRequest(t, taskToken, http.MethodPost, "/api/workflow-definitions", valid)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("task-token definition create status = %d, want 403", resp.StatusCode)
	}

	definitionID, parentID := workflowRouteStartFixture(t)
	resp = workflowAuthRequest(t, taskToken, http.MethodPost, "/api/issues/"+parentID+"/workflow/start", map[string]any{"workflow_definition_id": definitionID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task-token start status = %d, want 200", resp.StatusCode)
	}
	resp = workflowAuthRequest(t, taskToken, http.MethodPost, "/api/issues/"+parentID+"/workflow/resume", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task-token resume status = %d, want 200", resp.StatusCode)
	}
	resp = workflowAuthRequest(t, taskToken, http.MethodPost, "/api/issues/"+parentID+"/workflow/cancel", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("task-token cancel status = %d, want 403", resp.StatusCode)
	}
}
