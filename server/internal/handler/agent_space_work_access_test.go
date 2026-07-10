package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	authmiddleware "github.com/multica-ai/multica/server/internal/middleware"
)

func taskTokenRequest(method, path string, body any, agentID, spaceID string) *http.Request {
	r := newRequest(method, path, body)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", "00000000-0000-0000-0000-000000000001")
	if spaceID != "" {
		r.Header.Set("X-Space-ID", spaceID)
	}
	return r
}

func TestAgentTaskTokenIsBoundToOneAssignedSpace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	spaceA := createSpaceForAccessTest(t, "Agent Work A", "AGWKA", "open")
	spaceB := createSpaceForAccessTest(t, "Agent Work B", "AGWKB", "open")

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, availability_mode,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		)
		VALUES (
			$1, 'Space-bound Agent', 'cloud', '{}'::jsonb, $2,
			'workspace', 'public_to', 'selected_spaces',
			1, $3, '', '{}'::jsonb, '[]'::jsonb
		)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_available_space (agent_id, workspace_id, space_id, created_by)
		VALUES ($1, $2, $3, $4)
	`, agentID, testWorkspaceID, spaceA.ID, testUserID); err != nil {
		t.Fatalf("assign agent to Space A: %v", err)
	}

	createIssue := func(title, spaceID string) IssueResponse {
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title":    title,
			"space_id": spaceID,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create issue %q: %d %s", title, w.Code, w.Body.String())
		}
		var issue IssueResponse
		if err := json.Unmarshal(w.Body.Bytes(), &issue); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
		})
		return issue
	}
	issueA := createIssue("Space A work", spaceA.ID)
	issueB := createIssue("Space B work", spaceB.ID)
	createProject := func(title, spaceID string) ProjectResponse {
		w := httptest.NewRecorder()
		testHandler.CreateProject(w, newRequest(http.MethodPost, "/api/projects", map[string]any{
			"title":    title,
			"space_id": spaceID,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create project %q: %d %s", title, w.Code, w.Body.String())
		}
		var project ProjectResponse
		if err := json.Unmarshal(w.Body.Bytes(), &project); err != nil {
			t.Fatalf("decode project: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
		})
		return project
	}
	projectA := createProject("Agent boundary project A", spaceA.ID)
	projectB := createProject("Agent boundary project B", spaceB.ID)

	// Assignment to A grants this running Agent read and write access to A.
	w := httptest.NewRecorder()
	testHandler.GetIssue(w, withURLParam(taskTokenRequest(http.MethodGet, "/api/issues/"+issueA.ID, nil, agentID, spaceA.ID), "id", issueA.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("get bound-Space issue: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, taskTokenRequest(http.MethodPost, "/api/issues", map[string]any{
		"title":    "Created by Space-bound Agent",
		"space_id": spaceA.ID,
	}, agentID, spaceA.ID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue in bound Space: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode agent-created issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// The same task cannot move laterally to B, even though its runtime owner is
	// a Workspace owner and could access B with a human credential.
	w = httptest.NewRecorder()
	testHandler.GetIssue(w, withURLParam(taskTokenRequest(http.MethodGet, "/api/issues/"+issueB.ID, nil, agentID, spaceA.ID), "id", issueB.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-Space get: got %d, want 403: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, taskTokenRequest(http.MethodPost, "/api/issues", map[string]any{
		"title":    "Must not cross Spaces",
		"space_id": spaceB.ID,
	}, agentID, spaceA.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-Space create: got %d, want 403: %s", w.Code, w.Body.String())
	}

	// Workspace-wide list endpoints are forced to the token's one Space.
	w = httptest.NewRecorder()
	testHandler.ListIssues(w, taskTokenRequest(http.MethodGet, "/api/issues", nil, agentID, spaceA.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("list issues: %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode issues: %v", err)
	}
	foundA, foundB := false, false
	for _, issue := range listed.Issues {
		foundA = foundA || issue.ID == issueA.ID
		foundB = foundB || issue.ID == issueB.ID
		if issue.SpaceID == nil || *issue.SpaceID != spaceA.ID {
			t.Fatalf("task-token issue list leaked Space %v", issue.SpaceID)
		}
	}
	if !foundA || foundB {
		t.Fatalf("list scope: found A=%v B=%v, want true/false", foundA, foundB)
	}

	w = httptest.NewRecorder()
	testHandler.SearchIssues(w, taskTokenRequest(http.MethodGet, "/api/issues/search?q=Space", nil, agentID, spaceA.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("search issues: %d %s", w.Code, w.Body.String())
	}
	var searchedIssues struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &searchedIssues); err != nil {
		t.Fatalf("decode searched issues: %v", err)
	}
	for _, issue := range searchedIssues.Issues {
		if issue.SpaceID == nil || *issue.SpaceID != spaceA.ID {
			t.Fatalf("task-token issue search leaked Space %v", issue.SpaceID)
		}
	}

	w = httptest.NewRecorder()
	testHandler.ListProjects(w, taskTokenRequest(http.MethodGet, "/api/projects", nil, agentID, spaceA.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("list projects: %d %s", w.Code, w.Body.String())
	}
	var listedProjects struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listedProjects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	foundProjectA, foundProjectB := false, false
	for _, project := range listedProjects.Projects {
		foundProjectA = foundProjectA || project.ID == projectA.ID
		foundProjectB = foundProjectB || project.ID == projectB.ID
		if project.SpaceID != spaceA.ID {
			t.Fatalf("task-token project list leaked Space %s", project.SpaceID)
		}
	}
	if !foundProjectA || foundProjectB {
		t.Fatalf("project list scope: found A=%v B=%v, want true/false", foundProjectA, foundProjectB)
	}

	w = httptest.NewRecorder()
	testHandler.SearchProjects(w, taskTokenRequest(http.MethodGet, "/api/projects/search?q=Agent+boundary", nil, agentID, spaceA.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("search projects: %d %s", w.Code, w.Body.String())
	}
	var searchedProjects struct {
		Projects []SearchProjectResponse `json:"projects"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &searchedProjects); err != nil {
		t.Fatalf("decode searched projects: %v", err)
	}
	for _, project := range searchedProjects.Projects {
		if project.SpaceID != spaceA.ID {
			t.Fatalf("task-token project search leaked Space %s", project.SpaceID)
		}
	}

	w = httptest.NewRecorder()
	testHandler.ListSpaces(w, taskTokenRequest(http.MethodGet, "/api/spaces", nil, agentID, spaceA.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("list Spaces: %d %s", w.Code, w.Body.String())
	}
	var spaces struct {
		Spaces []SpaceResponse `json:"spaces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spaces); err != nil {
		t.Fatalf("decode Spaces: %v", err)
	}
	if len(spaces.Spaces) != 1 || spaces.Spaces[0].ID != spaceA.ID {
		t.Fatalf("task-token Spaces = %+v, want only Space A", spaces.Spaces)
	}

	// Auth, not the Agent process, is authoritative for X-Space-ID. A forged
	// client header is discarded and replaced with the token row's bound Space.
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, testRuntimeID).Scan(&taskID); err != nil {
		t.Fatalf("create auth probe task: %v", err)
	}
	rawToken := "mat_space_work_access_probe"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, space_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, auth.HashToken(rawToken), taskID, agentID, testWorkspaceID, spaceA.ID, testUserID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create task token: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	var stampedSpace string
	authHandler := authmiddleware.Auth(testHandler.Queries, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stampedSpace = r.Header.Get("X-Space-ID")
		w.WriteHeader(http.StatusNoContent)
	}))
	authReq := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	authReq.Header.Set("Authorization", "Bearer "+rawToken)
	authReq.Header.Set("X-Space-ID", spaceB.ID)
	authReq.Header.Set("X-Actor-Source", "member")
	authW := httptest.NewRecorder()
	authHandler.ServeHTTP(authW, authReq)
	if authW.Code != http.StatusNoContent || stampedSpace != spaceA.ID {
		t.Fatalf("auth stamped Space = %q status=%d, want %q/204", stampedSpace, authW.Code, spaceA.ID)
	}

	// A context-free Chat token has no Space data access.
	w = httptest.NewRecorder()
	testHandler.GetIssue(w, withURLParam(taskTokenRequest(http.MethodGet, "/api/issues/"+issueA.ID, nil, agentID, ""), "id", issueA.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("context-free token get: got %d, want 403: %s", w.Code, w.Body.String())
	}

	// Removing the assignment revokes an already-running token immediately.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_available_space WHERE agent_id = $1`, agentID); err != nil {
		t.Fatalf("revoke Space assignment: %v", err)
	}
	w = httptest.NewRecorder()
	testHandler.GetIssue(w, withURLParam(taskTokenRequest(http.MethodGet, "/api/issues/"+issueA.ID, nil, agentID, spaceA.ID), "id", issueA.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("revoked assignment get: got %d, want 403: %s", w.Code, w.Body.String())
	}
}
