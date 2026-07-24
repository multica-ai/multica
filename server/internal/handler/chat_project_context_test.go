package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func createChatProjectTestProject(t *testing.T, workspaceID, title, description string) string {
	t.Helper()

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, description)
		VALUES ($1, $2, NULLIF($3, ''))
		RETURNING id
	`, workspaceID, title, description).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func createChatSessionWithProjectForTest(t *testing.T, agentID, projectID string) string {
	t.Helper()

	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, project_id)
		VALUES ($1, $2, $3, 'Project context chat', 'active', $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, projectID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	return sessionID
}

func TestCreateChatSession_ProjectContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "ChatProjectContextAgent", []byte("[]"))
	projectID := createChatProjectTestProject(t, testWorkspaceID, "Chat project", "Durable context")

	t.Run("persists a workspace project", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{
			"agent_id":   agentID,
			"title":      "with project",
			"project_id": projectID,
		}))
		testHandler.CreateChatSession(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateChatSession: expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var response ChatSessionResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, response.ID)
		})
		if response.ProjectID == nil || *response.ProjectID != projectID {
			t.Fatalf("project_id = %v, want %s", response.ProjectID, projectID)
		}

		var storedProjectID *string
		if err := testPool.QueryRow(context.Background(), `
			SELECT project_id::text FROM chat_session WHERE id = $1
		`, response.ID).Scan(&storedProjectID); err != nil {
			t.Fatalf("load persisted project_id: %v", err)
		}
		if storedProjectID == nil || *storedProjectID != projectID {
			t.Fatalf("stored project_id = %v, want %s", storedProjectID, projectID)
		}

		listW := httptest.NewRecorder()
		listReq := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/chat/sessions", nil))
		testHandler.ListChatSessions(listW, listReq)
		if listW.Code != http.StatusOK {
			t.Fatalf("ListChatSessions: expected 200, got %d: %s", listW.Code, listW.Body.String())
		}
		var sessions []ChatSessionResponse
		if err := json.NewDecoder(listW.Body).Decode(&sessions); err != nil {
			t.Fatalf("decode session list: %v", err)
		}
		found := false
		for _, session := range sessions {
			if session.ID != response.ID {
				continue
			}
			found = true
			if session.ProjectID == nil || *session.ProjectID != projectID {
				t.Fatalf("listed project_id = %v, want %s", session.ProjectID, projectID)
			}
		}
		if !found {
			t.Fatalf("created session %s missing from list", response.ID)
		}
	})

	t.Run("rejects malformed project id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{
			"agent_id":   agentID,
			"project_id": "not-a-uuid",
		}))
		testHandler.CreateChatSession(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects a project from another workspace", func(t *testing.T) {
		var foreignWorkspaceID string
		if err := testPool.QueryRow(context.Background(), `
			INSERT INTO workspace (name, slug) VALUES ('Foreign chat context', $1) RETURNING id
		`, "chat-context-"+uuid.NewString()).Scan(&foreignWorkspaceID); err != nil {
			t.Fatalf("create foreign workspace: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
		})
		foreignProjectID := createChatProjectTestProject(t, foreignWorkspaceID, "Foreign project", "")

		w := httptest.NewRecorder()
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{
			"agent_id":   agentID,
			"project_id": foreignProjectID,
		}))
		testHandler.CreateChatSession(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("keeps project optional", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{
			"agent_id": agentID,
			"title":    "workspace context only",
		}))
		testHandler.CreateChatSession(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateChatSession: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var response ChatSessionResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, response.ID)
		})
		if response.ProjectID != nil {
			t.Fatalf("project_id = %v, want null", response.ProjectID)
		}
	})
}

func TestUpdateChatSession_UpdatesProjectContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	projectID := createChatProjectTestProject(t, testWorkspaceID, "Removable chat project", "")
	replacementProjectID := createChatProjectTestProject(t, testWorkspaceID, "Replacement chat project", "")
	agentID := createHandlerTestAgent(t, "RemoveChatProjectAgent", []byte("[]"))
	sessionID := createChatSessionWithProjectForTest(t, agentID, projectID)

	updateProject := func(projectID any) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(
			withChatTestWorkspaceCtx(t, newRequest(http.MethodPatch, "/api/chat/sessions/"+sessionID, map[string]any{
				"project_id": projectID,
			})),
			"sessionId",
			sessionID,
		)
		testHandler.UpdateChatSession(w, req)
		return w
	}

	w := updateProject(replacementProjectID)
	if w.Code != http.StatusOK {
		t.Fatalf("replace project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response ChatSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != sessionID {
		t.Fatalf("session id = %q, want %q", response.ID, sessionID)
	}
	if response.ProjectID == nil || *response.ProjectID != replacementProjectID {
		t.Fatalf("response project_id = %v, want %s", response.ProjectID, replacementProjectID)
	}

	var foreignWorkspaceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug) VALUES ('Foreign chat update', $1) RETURNING id
	`, "chat-update-"+uuid.NewString()).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})
	foreignProjectID := createChatProjectTestProject(t, foreignWorkspaceID, "Foreign replacement", "")
	foreignW := updateProject(foreignProjectID)
	if foreignW.Code != http.StatusNotFound {
		t.Fatalf("foreign project: expected 404, got %d: %s", foreignW.Code, foreignW.Body.String())
	}

	w = updateProject(nil)
	if w.Code != http.StatusOK {
		t.Fatalf("remove project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	response = ChatSessionResponse{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode remove response: %v", err)
	}
	if response.ProjectID != nil {
		t.Fatalf("response project_id = %v, want null", response.ProjectID)
	}

	var storedProjectID *string
	if err := testPool.QueryRow(context.Background(), `
		SELECT project_id::text FROM chat_session WHERE id = $1
	`, sessionID).Scan(&storedProjectID); err != nil {
		t.Fatalf("load updated chat session: %v", err)
	}
	if storedProjectID != nil {
		t.Fatalf("stored project_id = %v, want null", storedProjectID)
	}
}

func TestDeleteProject_ClearsChatSessionContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	projectID := createChatProjectTestProject(t, testWorkspaceID, "Deleted chat project", "")
	agentID := createHandlerTestAgent(t, "DeletedChatProjectAgent", []byte("[]"))
	sessionID := createChatSessionWithProjectForTest(t, agentID, projectID)

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest(http.MethodDelete, "/api/projects/"+projectID, nil),
		"id",
		projectID,
	)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var storedProjectID *string
	if err := testPool.QueryRow(context.Background(), `
		SELECT project_id::text FROM chat_session WHERE id = $1
	`, sessionID).Scan(&storedProjectID); err != nil {
		t.Fatalf("load chat session after project delete: %v", err)
	}
	if storedProjectID != nil {
		t.Fatalf("project_id = %v after project delete, want null", storedProjectID)
	}
}

func TestClaimTaskByRuntime_ChatProjectContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": "https://github.com/example/workspace-fallback"},
	})
	projectID := createChatProjectTestProject(
		t,
		testWorkspaceID,
		"Chat claim project",
		"Use the project design system and release branch.",
	)
	const projectRepoURL = "https://github.com/example/chat-project-repo"
	const projectRepoRef = "release/chat-context"
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_resource (
			project_id, workspace_id, resource_type, resource_ref, label, position
		) VALUES ($1, $2, 'github_repo', $3::jsonb, 'Chat repository', 0)
	`, projectID, testWorkspaceID, `{"url":"`+projectRepoURL+`","ref":"`+projectRepoRef+`"}`); err != nil {
		t.Fatalf("create project resource: %v", err)
	}

	agentID := createHandlerTestAgent(t, "ChatProjectClaimAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	sessionID := createChatSessionWithProjectForTest(t, agentID, projectID)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'Plan the project release')
	`, sessionID); err != nil {
		t.Fatalf("create chat message: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, chat_session_id
		) VALUES ($1, $2, 'queued', 1000, $3)
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+runtimeID+"/claim",
		nil,
		testWorkspaceID,
		"chat-project-context-test",
	)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Task *struct {
			ID                 string                `json:"id"`
			ProjectID          string                `json:"project_id"`
			ProjectTitle       string                `json:"project_title"`
			ProjectDescription string                `json:"project_description"`
			ProjectResources   []ProjectResourceData `json:"project_resources"`
			Repos              []RepoData            `json:"repos"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Task == nil || response.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", response.Task, taskID)
	}
	if response.Task.ProjectID != projectID {
		t.Fatalf("project_id = %q, want %q", response.Task.ProjectID, projectID)
	}
	if response.Task.ProjectTitle != "Chat claim project" {
		t.Fatalf("project_title = %q", response.Task.ProjectTitle)
	}
	if response.Task.ProjectDescription != "Use the project design system and release branch." {
		t.Fatalf("project_description = %q", response.Task.ProjectDescription)
	}
	if len(response.Task.ProjectResources) != 1 || response.Task.ProjectResources[0].Label != "Chat repository" {
		t.Fatalf("project_resources = %+v", response.Task.ProjectResources)
	}
	if len(response.Task.Repos) != 1 || response.Task.Repos[0].URL != projectRepoURL || response.Task.Repos[0].Ref != projectRepoRef {
		t.Fatalf("repos = %+v, want project repo only", response.Task.Repos)
	}
}
