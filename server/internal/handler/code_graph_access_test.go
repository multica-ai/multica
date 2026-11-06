package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// These cases exercise the real database authorization boundary, not a provider.
func TestCodeGraphUsageScopes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtime := dbfx.Runtime(t, "Graph permission runtime", testutil.Cols{"runtime_mode": "local", "provider": "codex"})
	agent := dbfx.Agent(t, "Graph permission agent", runtime)
	project := dbfx.Project(t, "Graph project")
	otherProject := dbfx.Project(t, "Other graph project")
	issue := dbfx.Issue(t, "Graph issue", testutil.Cols{"project_id": project})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": runtime})
	repo := dbfx.Insert(t, "code_graph_repository", testutil.Cols{"workspace_id": testWorkspaceID, "repo_url": "https://example.com/test.git", "repo_name": "scope-boundary", "branch": "main"})
	access := service.CodeGraphAccessService{DB: testPool, WorkspaceID: parseUUID(testWorkspaceID), AgentID: parseUUID(agent), TaskID: parseUUID(task)}
	check := func(want ...string) {
		t.Helper()
		rows, err := access.Repositories(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(want) == 0 {
			if len(rows) != 0 {
				t.Fatalf("unexpected grant: %+v", rows)
			}
			return
		}
		if len(rows) != 1 || !slices.Equal(rows[0].Sources, want) {
			t.Fatalf("sources=%+v, want %v", rows, want)
		}
	}
	check()
	dbfx.InsertNoID(t, "project_code_graph_repository", testutil.Cols{"repository_id": repo, "project_id": project}, "repository_id=$1 AND project_id=$2", repo, project)
	check("project")
	dbfx.InsertNoID(t, "agent_code_graph_repository", testutil.Cols{"repository_id": repo, "agent_id": agent}, "repository_id=$1 AND agent_id=$2", repo, agent)
	check("agent", "project")
	if _, err := testPool.Exec(ctx, `UPDATE code_graph_repository SET is_workspace_shared=true WHERE id=$1`, repo); err != nil {
		t.Fatal(err)
	}
	check("workspace", "agent", "project")
	if _, err := testPool.Exec(ctx, `DELETE FROM agent_code_graph_repository WHERE repository_id=$1`, repo); err != nil {
		t.Fatal(err)
	}
	check("workspace", "project")
	if _, err := testPool.Exec(ctx, `UPDATE code_graph_repository SET is_workspace_shared=false WHERE id=$1`, repo); err != nil {
		t.Fatal(err)
	}
	check("project")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id=$2 WHERE id=$1`, issue, otherProject); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id=$2 WHERE id=$1`, issue, project); err != nil {
		t.Fatal(err)
	}
	check("project")
	// An ordinary conversation never inherits an unrelated project's grants.
	access.TaskID = parseUUID(dbfx.Task(t, agent, testutil.Cols{"runtime_id": runtime}))
	check()
	// Project conversations use their stored project relation.
	chat := dbfx.Insert(t, "chat_session", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": agent, "creator_id": testUserID, "title": "Project graph conversation", "project_id": project})
	access.TaskID = parseUUID(dbfx.Task(t, agent, testutil.Cols{"chat_session_id": chat, "runtime_id": runtime}))
	check("project")
	// Even workspace sharing must not make a different agent's task valid.
	if _, err := testPool.Exec(ctx, `UPDATE code_graph_repository SET is_workspace_shared=true WHERE id=$1`, repo); err != nil {
		t.Fatal(err)
	}
	otherAgent := dbfx.Agent(t, "Other permission agent", runtime)
	access.TaskID = parseUUID(dbfx.Task(t, otherAgent, testutil.Cols{"runtime_id": runtime}))
	check()
	access.TaskID = parseUUID(task)
	access.WorkspaceID = parseUUID(dbfx.Workspace(t, "Other graph workspace", "other-codegraph-scope"))
	check()
}

func TestCodeGraphAssignmentOwnerPermissions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	user := dbfx.User(t, "Graph agent owner", "graph-agent-owner@example.test")
	dbfx.Member(t, testWorkspaceID, user, "member")
	runtime := dbfx.Runtime(t, "Permission runtime", testutil.Cols{"runtime_mode": "local", "provider": "codex"})
	own := dbfx.Agent(t, "Owned graph agent", runtime, testutil.Cols{"owner_id": user})
	other := dbfx.Agent(t, "Another graph agent", runtime)
	repo := dbfx.Insert(t, "code_graph_repository", testutil.Cols{"workspace_id": testWorkspaceID, "repo_url": "https://example.com/owner.git", "repo_name": "owner-boundary", "branch": "main"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_code_graph_repository WHERE repository_id=$1`, repo)
	})
	for _, tc := range []struct {
		agent string
		want  int
	}{{own, 200}, {other, 403}} {
		req := withURLParams(newRequest(http.MethodPost, "/", map[string]any{"agent_id": tc.agent}), "id", testWorkspaceID, "codeGraphId", repo)
		req.Header.Set("X-User-ID", user)
		testutil.Call(t, testHandler.AssignWorkspaceCodeGraphToAgent, req).Want(tc.want)
	}
	req := withURLParams(newRequest(http.MethodPatch, "/", map[string]any{"is_workspace_shared": true}), "id", testWorkspaceID, "codeGraphId", repo)
	req.Header.Set("X-User-ID", user)
	response := httptest.NewRecorder()
	testHandler.UpdateCodeGraphWorkspaceScope(response, req)
	if response.Code != 403 {
		t.Fatalf("member sharing: %d %s", response.Code, response.Body.String())
	}
}
