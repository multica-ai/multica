package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCodeGraphMCPDynamicAssignments(t *testing.T) {
	ctx := context.Background()
	code, repo, raw := createCodeGraphViaAPI(t, map[string]any{"repo_url": "https://github.com/multica-ai/multica-cli.git", "branch": "main", "auth_mode": "anonymous"})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, raw)
	}
	runtimeID := dbfx.Runtime(t, "CodeGraph MCP runtime", testutil.Cols{"runtime_mode": "local", "provider": "codex"})
	agentID := dbfx.Agent(t, "dynamic-codegraph", runtimeID)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	token, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatal(err)
	}
	_, err = testHandler.Queries.CreateTaskToken(ctx, db.CreateTaskTokenParams{
		TokenHash: auth.HashToken(token), AgentID: parseUUID(agentID), TaskID: parseUUID(taskID),
		WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testHandler.Queries.DeleteTaskTokensByTask(ctx, parseUUID(taskID)) })
	endpoint := middleware.Auth(testHandler.Queries, nil, nil, nil)(http.HandlerFunc(testHandler.CodeGraphMCP))
	call := func(name string, args map[string]any) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		request := httptest.NewRequest(http.MethodPost, "/api/codegraph/mcp", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response := httptest.NewRecorder()
		endpoint.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("MCP: %d %s", response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	if result := call("codegraph_list_repositories", map[string]any{}); strings.Contains(result, repo.ID) {
		t.Fatal("unbound repository exposed")
	}
	if code, raw := assignCodeGraphRepositoryForTest(t, repo.ID, agentID); code != http.StatusOK {
		t.Fatalf("assign: %d %s", code, raw)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, "DELETE FROM agent_code_graph_repository WHERE repository_id = $1", repo.ID)
	})
	if result := call("codegraph_list_repositories", map[string]any{}); !strings.Contains(result, repo.ID) {
		t.Fatalf("new assignment not visible: %s", result)
	}
	var buildID string
	if err := testPool.QueryRow(ctx, "INSERT INTO code_graph_build(workspace_id,repository_id,status,commit_hash) VALUES($1,$2,'ready','boundary-fixture') RETURNING id::text", testWorkspaceID, repo.ID).Scan(&buildID); err != nil {
		t.Fatal(err)
	}
	dir := codeGraphBuildDir(testWorkspaceID, repo.ID, buildID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, "example.go"), []byte("package example\nfunc BoundExample() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, "UPDATE code_graph_repository SET status = 'ready', active_build_id=$2 WHERE id = $1", repo.ID, buildID); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"repository_id": repo.ID, "path": "example.go"}
	if result := call("codegraph_read_file", args); !strings.Contains(result, "BoundExample") {
		t.Fatalf("read: %s", result)
	}
	if result := call("codegraph_search", map[string]any{"repository_id": repo.ID, "query": "BoundExample"}); !strings.Contains(result, "example.go") {
		t.Fatalf("search: %s", result)
	}
	req := withURLParams(newRequest(http.MethodGet, "/", nil), "id", testWorkspaceID, "codeGraphId", repo.ID)
	details := httptest.NewRecorder()
	testHandler.GetWorkspaceCodeGraph(details, req)
	if details.Code != http.StatusOK || !strings.Contains(details.Body.String(), agentID) {
		t.Fatalf("details: %d %s", details.Code, details.Body.String())
	}
	unbind := withURLParams(newRequest(http.MethodDelete, "/", nil), "id", testWorkspaceID, "codeGraphId", repo.ID, "agentId", agentID)
	response := httptest.NewRecorder()
	testHandler.UnassignWorkspaceCodeGraphAgent(response, unbind)
	if response.Code != http.StatusNoContent {
		t.Fatalf("unbind: %d %s", response.Code, response.Body.String())
	}
	if result := call("codegraph_read_file", args); !strings.Contains(result, `"isError":true`) || strings.Contains(result, "BoundExample") {
		t.Fatalf("revoked grant still readable: %s", result)
	}
	if result := call("codegraph_list_repositories", map[string]any{}); strings.Contains(result, repo.ID) {
		t.Fatal("revoked repository listed")
	}
	// Spoofed agent headers without a task credential must not reach the tools.
	if _, err := testPool.Exec(ctx, `UPDATE code_graph_repository SET is_workspace_shared=true WHERE id=$1`, repo.ID); err != nil {
		t.Fatal(err)
	}
	if result := call("codegraph_read_file", args); !strings.Contains(result, "BoundExample") {
		t.Fatalf("workspace scope unreadable: %s", result)
	}
	if _, err := testPool.Exec(ctx, `UPDATE code_graph_repository SET is_workspace_shared=false WHERE id=$1`, repo.ID); err != nil {
		t.Fatal(err)
	}
	project := dbfx.Project(t, "MCP project scope")
	issue := dbfx.Issue(t, "MCP project task", testutil.Cols{"project_id": project})
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET issue_id=$2 WHERE id=$1`, taskID, issue); err != nil {
		t.Fatal(err)
	}
	dbfx.InsertNoID(t, "project_code_graph_repository", testutil.Cols{"repository_id": repo.ID, "project_id": project}, "repository_id=$1 AND project_id=$2", repo.ID, project)
	if result := call("codegraph_read_file", args); !strings.Contains(result, "BoundExample") {
		t.Fatalf("project scope unreadable: %s", result)
	}
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id=NULL WHERE id=$1`, issue); err != nil {
		t.Fatal(err)
	}
	if result := call("codegraph_read_file", args); !strings.Contains(result, `"isError":true`) || strings.Contains(result, "BoundExample") {
		t.Fatalf("moved project still readable: %s", result)
	}
	// Spoofed agent headers without a task credential must not reach the tools.
	forged := httptest.NewRequest(http.MethodPost, "/api/codegraph/mcp", nil)
	forged.Header.Set("X-Agent-ID", agentID)
	forged.Header.Set("X-Actor-Source", "task_token")
	rejected := httptest.NewRecorder()
	endpoint.ServeHTTP(rejected, forged)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("forged identity accepted: %d", rejected.Code)
	}
}
