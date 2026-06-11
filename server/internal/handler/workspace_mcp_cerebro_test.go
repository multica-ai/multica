package handler

// CEREBRO-PATCH(workspace-mcp-http): TECH-3405 workspace-scoped MCP endpoint regression tests.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
)

func TestWorkspaceMCPListsCreateIssueTool(t *testing.T) {
	rec := exerciseWorkspaceMCP(t, testWorkspaceID, testUserID, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("WorkspaceMCP tools/list status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, tool := range resp.Result.Tools {
		if tool.Name == "create_issue" {
			return
		}
	}
	t.Fatalf("create_issue tool not listed: %+v", resp.Result.Tools)
}

func TestWorkspaceMCPCreateIssueCreatesInURLWorkspace(t *testing.T) {
	title := "Workspace MCP create " + uuid.NewString()
	rec := exerciseWorkspaceMCP(t, testWorkspaceID, testUserID, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "create_issue",
			"arguments": map[string]any{
				"title":       title,
				"description": "Created through workspace MCP",
				"priority":    "high",
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("WorkspaceMCP tools/call status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Result.IsError {
		t.Fatalf("create_issue returned MCP error: %s", rec.Body.String())
	}
	var issue IssueResponse
	if err := json.Unmarshal(resp.Result.StructuredContent, &issue); err != nil {
		t.Fatalf("decode structured issue: %v; body=%s", err, rec.Body.String())
	}
	if issue.Title != title {
		t.Fatalf("created title = %q, want %q", issue.Title, title)
	}
	if issue.WorkspaceID != testWorkspaceID {
		t.Fatalf("created workspace = %q, want %q", issue.WorkspaceID, testWorkspaceID)
	}
}

func TestWorkspaceMCPTaskTokenCannotCrossWorkspaceDirectly(t *testing.T) {
	otherWorkspaceID := uuid.NewString()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+otherWorkspaceID+"/mcp", bytes.NewReader(mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	router := workspaceMCPTestRouter()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("task-token cross-workspace status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func exerciseWorkspaceMCP(t *testing.T, workspaceID, userID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/mcp", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	rec := httptest.NewRecorder()
	workspaceMCPTestRouter().ServeHTTP(rec, req)
	return rec
}

func workspaceMCPTestRouter() http.Handler {
	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.With(middleware.RequireWorkspaceMemberFromURL(testHandler.Queries, "id")).Post("/mcp", testHandler.WorkspaceMCP)
	})
	return r
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
}
