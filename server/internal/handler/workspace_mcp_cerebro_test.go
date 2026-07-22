package handler

// CEREBRO-PATCH(workspace-mcp-http): TECH-3405 workspace-scoped MCP endpoint regression tests.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/middleware"
)

type workspaceMCPApprovalFake struct {
	calls  int
	params approvals.IntakeParams
}

func (f *workspaceMCPApprovalFake) Intake(_ context.Context, p approvals.IntakeParams) (cerebrodb.CerebroApprovalRequest, error) {
	f.calls++
	f.params = p
	id, _ := pgtypeUUID(uuid.NewString())
	return cerebrodb.CerebroApprovalRequest{ID: id, WorkspaceID: p.WorkspaceID, Status: approvals.StatusPending}, nil
}

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

func TestWorkspaceMCPListsRequestApprovalTool(t *testing.T) {
	rec := exerciseWorkspaceMCP(t, testWorkspaceID, testUserID, map[string]any{
		"jsonrpc": "2.0",
		"id":      20,
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
		if tool.Name == "request_approval" {
			return
		}
	}
	t.Fatalf("request_approval tool not listed: %+v", resp.Result.Tools)
}

func TestWorkspaceMCPRequestApprovalCreatesPendingWithTaskContext(t *testing.T) {
	fake := &workspaceMCPApprovalFake{}
	previous := testHandler.ApprovalRequester
	testHandler.ApprovalRequester = fake
	t.Cleanup(func() { testHandler.ApprovalRequester = previous })

	agentID := createHandlerTestAgent(t, "workspace-mcp-approval-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgent(t, agentID)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      23,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "request_approval",
			"arguments": map[string]any{
				"capability": "publish_campaign",
				"resource":   "campaign:summer",
				"reason":     "Needs owner review",
				"surface":    "chat",
			},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	rec := httptest.NewRecorder()
	workspaceMCPTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"isError":true`) {
		t.Fatalf("request_approval response = %d %s", rec.Code, rec.Body.String())
	}
	if fake.calls != 1 {
		t.Fatalf("approval intake calls = %d, want 1", fake.calls)
	}
	if fake.params.RequesterType != approvals.RequesterAgent || fake.params.Capability != "publish_campaign" || fake.params.Resource != "campaign:summer" {
		t.Fatalf("approval params = %+v", fake.params)
	}
	if fake.params.Context["task_id"] != taskID || fake.params.Context["surface"] != "issue" {
		t.Fatalf("approval context = %#v", fake.params.Context)
	}
}

func pgtypeUUID(raw string) (pgtype.UUID, error) {
	id, err := uuid.Parse(raw)
	return pgtype.UUID{Bytes: id, Valid: err == nil}, err
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

func TestWorkspaceMCPCreateIssuePermission_DenyBlocksWithoutMutation(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingDeny)
	title := "Workspace MCP denied " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	rec := exerciseWorkspaceMCPAsAgent(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      21,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "create_issue",
			"arguments": map[string]any{"title": title, "allow_duplicate": true},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("Workspace MCP Deny status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "platform_action_denied") {
		t.Fatalf("Workspace MCP Deny response missing platform_action_denied: %s", rec.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("Workspace MCP Deny mutated %d issue rows, want 0", got)
	}
}

func TestWorkspaceMCPCreateIssuePermission_AskReturnsPendingWithoutMutation(t *testing.T) {
	setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
	title := "Workspace MCP pending " + uuid.NewString()
	cleanupIssuesByTitle(t, title)

	rec := exerciseWorkspaceMCPAsAgent(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      22,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "create_issue",
			"arguments": map[string]any{"title": title, "allow_duplicate": true},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("Workspace MCP Ask status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "platform_action_pending") {
		t.Fatalf("Workspace MCP Ask response missing platform_action_pending: %s", rec.Body.String())
	}
	if got := issueCountByTitle(t, title); got != 0 {
		t.Fatalf("Workspace MCP Ask mutated %d issue rows before approval, want 0", got)
	}
}

func TestWorkspaceMCPCreateIssuePermission_AskDecisionControlsSingleMutation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		decision   string
		wantIssues int
		wantError  bool
	}{
		{name: "approve once", decision: approvals.StatusApproved, wantIssues: 1},
		{name: "reject", decision: approvals.StatusRejected, wantIssues: 0, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCreateIssueWorkspacePolicy(t, toolpolicy.SettingAsk)
			title := "Workspace MCP decided " + uuid.NewString()
			cleanupIssuesByTitle(t, title)

			rec := exerciseWorkspaceMCPAsAgentWithDecision(t, tc.decision, map[string]any{
				"jsonrpc": "2.0",
				"id":      24,
				"method":  "tools/call",
				"params": map[string]any{
					"name":      "create_issue",
					"arguments": map[string]any{"title": title, "allow_duplicate": true},
				},
			})

			if rec.Code != http.StatusOK {
				t.Fatalf("Workspace MCP decided status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if gotError := strings.Contains(rec.Body.String(), `"isError":true`); gotError != tc.wantError {
				t.Fatalf("Workspace MCP decided isError = %v, want %v; body=%s", gotError, tc.wantError, rec.Body.String())
			}
			if got := issueCountByTitle(t, title); got != tc.wantIssues {
				t.Fatalf("Workspace MCP decided mutated %d issue rows, want %d", got, tc.wantIssues)
			}
		})
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

func exerciseWorkspaceMCPAsAgent(t *testing.T, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	agentID := createHandlerTestAgent(t, "workspace-mcp-permission-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgent(t, agentID)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	// Give a loaded CI database enough time to persist the pending approval
	// before the request timeout exercises the MCP pending response.
	ctx, cancel := context.WithTimeout(req.Context(), 500*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	workspaceMCPTestRouter().ServeHTTP(rec, req)
	return rec
}

func exerciseWorkspaceMCPAsAgentWithDecision(t *testing.T, decision string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	agentID := createHandlerTestAgent(t, "workspace-mcp-decided-"+uuid.NewString(), []byte(`{}`))
	taskID := createHandlerTestTaskForAgent(t, agentID)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/mcp", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	ctx, cancel := context.WithTimeout(req.Context(), 4*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		workspaceMCPTestRouter().ServeHTTP(rec, req)
		close(done)
	}()

	var approvalID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := testPool.QueryRow(ctx, `
			SELECT id::text
			FROM cerebro_approval_request
			WHERE requester_id = $1 AND capability = 'create_issue' AND status = 'pending'
			ORDER BY created_at DESC
			LIMIT 1`, agentID).Scan(&approvalID)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approvalID == "" {
		t.Fatal("Workspace MCP did not create a pending create_issue approval")
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE cerebro_approval_request
		SET status = $2, expires_at = NOW() + INTERVAL '1 minute', updated_at = NOW()
		WHERE id = $1`, approvalID, decision); err != nil {
		t.Fatalf("decide Workspace MCP approval: %v", err)
	}

	select {
	case <-done:
		return rec
	case <-ctx.Done():
		t.Fatalf("Workspace MCP did not resume after %s: %v", decision, ctx.Err())
		return rec
	}
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
