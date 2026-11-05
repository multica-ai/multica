package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func createCodeGraphViaAPI(t *testing.T, body any) (int, CodeGraphRepositoryResponse, string) {
	t.Helper()
	if os.Getenv("MULTICA_CODEGRAPH_ENGINE_URL") == "" {
		t.Skip("real CodeGraph engine configuration is required for repository registration")
	}

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/code-graphs", body)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceCodeGraph(w, req)

	var resp CodeGraphRepositoryResponse
	raw := w.Body.String()
	if w.Code == http.StatusCreated || w.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_code_graph_repository WHERE repository_id = $1`, resp.ID)
			testPool.Exec(context.Background(), `DELETE FROM code_graph_build WHERE repository_id = $1`, resp.ID)
			testPool.Exec(context.Background(), `DELETE FROM code_graph_repository WHERE id = $1`, resp.ID)
		})
	}
	return w.Code, resp, raw
}

func TestCreateWorkspaceCodeGraph_IsIdempotentByRepoAndBranch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	body := map[string]any{
		"repo_url":  "https://github.com/expressjs/express.git",
		"auth_mode": "anonymous",
		"repo_name": "repo",
		"branch":    "master",
	}

	code, first, raw := createCodeGraphViaAPI(t, body)
	if code != http.StatusCreated {
		t.Fatalf("expected first create 201, got %d: %s", code, raw)
	}
	if first.Status != "pending" {
		t.Fatalf("status = %q, want pending", first.Status)
	}

	code, second, raw := createCodeGraphViaAPI(t, body)
	if code != http.StatusOK {
		t.Fatalf("expected duplicate create 200, got %d: %s", code, raw)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate registration created a new row: first=%s second=%s", first.ID, second.ID)
	}

	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/code-graphs", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListWorkspaceCodeGraphs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", w.Code, w.Body.String())
	}
	var list CodeGraphRepositoryListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if list.Stats.Total < 1 || len(list.Items) != 1 || list.Items[0].ID != first.ID {
		t.Fatalf("stats did not include registered repository: %+v", list)
	}
}

func assignCodeGraphRepositoryForTest(t *testing.T, repoID, agentID string) (int, string) {
	t.Helper()

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/code-graphs/"+repoID+"/agents",
		map[string]any{"agent_id": agentID})
	req = withURLParams(req, "id", testWorkspaceID, "codeGraphId", repoID)
	w := httptest.NewRecorder()
	testHandler.AssignWorkspaceCodeGraphToAgent(w, req)
	return w.Code, w.Body.String()
}

func TestAssignWorkspaceCodeGraphToAgent_PersistsBinding(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	code, repo, raw := createCodeGraphViaAPI(t, map[string]any{
		"repo_url":  "https://github.com/expressjs/express.git",
		"auth_mode": "anonymous",
		"repo_name": "assignable",
		"branch":    "master",
	})
	if code != http.StatusCreated {
		t.Fatalf("create repo: expected 201, got %d: %s", code, raw)
	}
	// This DB-level authorization case requires a tool-capable runtime.
	// This test does not execute a provider.
	runtimeID := dbfx.Runtime(t, "CodeGraph binding runtime", testutil.Cols{"runtime_mode": "local", "provider": "codex"})
	agentID := dbfx.Agent(t, "code-graph-assigned-agent", runtimeID)

	code, raw = assignCodeGraphRepositoryForTest(t, repo.ID, agentID)
	if code != http.StatusOK {
		t.Fatalf("assign: expected 200, got %d: %s", code, raw)
	}
	code, raw = assignCodeGraphRepositoryForTest(t, repo.ID, agentID)
	if code != http.StatusOK {
		t.Fatalf("duplicate assign: expected 200, got %d: %s", code, raw)
	}

	var bindings int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_code_graph_repository
		WHERE repository_id = $1 AND agent_id = $2
	`, repo.ID, agentID).Scan(&bindings); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindings != 1 {
		t.Fatalf("expected exactly one binding, got %d", bindings)
	}
}

func TestAssignWorkspaceCodeGraphToAgent_RejectsAgentFromAnotherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	code, repo, raw := createCodeGraphViaAPI(t, map[string]any{
		"repo_url":  "https://github.com/expressjs/express.git",
		"auth_mode": "anonymous",
		"repo_name": "tenant",
		"branch":    "master",
	})
	if code != http.StatusCreated {
		t.Fatalf("create repo: expected 201, got %d: %s", code, raw)
	}

	var otherWorkspaceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('CodeGraph Other WS', 'code-graph-other-ws', '', 'CGO')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	var otherAgentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, visibility, status, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'foreign code graph agent', 'local', '{}'::jsonb, 'workspace', 'offline', 1, $2, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, otherWorkspaceID, testUserID).Scan(&otherAgentID); err != nil {
		t.Fatalf("create other agent: %v", err)
	}

	code, raw = assignCodeGraphRepositoryForTest(t, repo.ID, otherAgentID)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for an agent from another workspace, got %d: %s", code, raw)
	}
}
