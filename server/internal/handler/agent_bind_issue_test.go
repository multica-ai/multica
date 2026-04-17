package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type resumeErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func decodeResumeErrorResponse(t *testing.T, w *httptest.ResponseRecorder) resumeErrorResponse {
	t.Helper()

	var resp resumeErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v (body=%q)", err, w.Body.String())
	}
	return resp
}

func withRouteParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func mustLookupHandlerTestAgent(t *testing.T) (agentID string, runtimeID string) {
	t.Helper()
	ctx := context.Background()
	err := testPool.QueryRow(
		ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID,
		"Handler Test Agent",
	).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent/runtime: %v", err)
	}
	return agentID, runtimeID
}

func mustCreateIssueForWorkspace(t *testing.T, title string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(
		ctx,
		`INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id, number, position
		) VALUES (
			$1, $2, 'todo', 'none', 'member', $3,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1),
			0
		) RETURNING id`,
		testWorkspaceID,
		title,
		testUserID,
	).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	return issueID
}

func mustCreateTaskForAgent(t *testing.T, agentID string, runtimeID string, status string, issueID *string) string {
	t.Helper()
	ctx := context.Background()

	var issueArg any
	if issueID != nil {
		issueArg = *issueID
	} else {
		issueArg = nil
	}

	var taskID string
	err := testPool.QueryRow(
		ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context)
		 VALUES ($1, $2, $3, $4, 50, '{"resume_session_id":"019d9699-fbec-7f43-a3dd-67b8afd5f5d6"}'::jsonb)
		 RETURNING id`,
		agentID,
		runtimeID,
		issueArg,
		status,
	).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	return taskID
}

func TestBindAgentTaskIssue_BindsRunningTask(t *testing.T) {
	ctx := context.Background()
	agentID, runtimeID := mustLookupHandlerTestAgent(t)
	issueID := mustCreateIssueForWorkspace(t, "Bind running task issue")
	taskID := mustCreateTaskForAgent(t, agentID, runtimeID, "running", nil)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := newRequest("POST", "/api/agents/"+agentID+"/tasks/"+taskID+"/bind-issue", map[string]any{
		"issue_id": issueID,
	})
	req = withRouteParams(req, map[string]string{
		"id":     agentID,
		"taskId": taskID,
	})
	w := httptest.NewRecorder()
	testHandler.BindAgentTaskIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("BindAgentTaskIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.IssueID != issueID {
		t.Fatalf("expected issue_id %q, got %q", issueID, resp.IssueID)
	}

	var dbIssueID string
	if err := testPool.QueryRow(ctx, `SELECT COALESCE(issue_id::text, '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&dbIssueID); err != nil {
		t.Fatalf("query task issue_id: %v", err)
	}
	if dbIssueID != issueID {
		t.Fatalf("expected database issue_id %q, got %q", issueID, dbIssueID)
	}
}

func TestBindAgentTaskIssue_RejectsCompletedTask(t *testing.T) {
	ctx := context.Background()
	agentID, runtimeID := mustLookupHandlerTestAgent(t)
	issueID := mustCreateIssueForWorkspace(t, "Reject completed bind")
	taskID := mustCreateTaskForAgent(t, agentID, runtimeID, "completed", nil)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := newRequest("POST", "/api/agents/"+agentID+"/tasks/"+taskID+"/bind-issue", map[string]any{
		"issue_id": issueID,
	})
	req = withRouteParams(req, map[string]string{
		"id":     agentID,
		"taskId": taskID,
	})
	w := httptest.NewRecorder()
	testHandler.BindAgentTaskIssue(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("BindAgentTaskIssue: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResumeExternalSession_RejectsWhenWorkspaceHasNoRepos(t *testing.T) {
	ctx := context.Background()
	agentID, _ := mustLookupHandlerTestAgent(t)
	sessionID := "019d96b0-288d-7bc3-9488-275af8d26876"
	issueID := mustCreateIssueForWorkspace(t, "Resume should fail without repos")
	setHandlerTestWorkspaceRepos(t, []map[string]string{})

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := newRequest("POST", "/api/agents/"+agentID+"/resume-session", map[string]any{
		"session_id": sessionID,
		"issue_id":   issueID,
	})
	req = withRouteParams(req, map[string]string{
		"id": agentID,
	})
	w := httptest.NewRecorder()
	testHandler.ResumeExternalSession(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("ResumeExternalSession: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.ToLower(w.Body.String()), "no repositories configured") {
		t.Fatalf("ResumeExternalSession: expected no-repositories error, got %s", w.Body.String())
	}

	var createdCount int
	if err := testPool.QueryRow(
		ctx,
		`SELECT count(*)
		 FROM agent_task_queue
		 WHERE agent_id = $1
		   AND context->>'resume_session_id' = $2`,
		agentID,
		sessionID,
	).Scan(&createdCount); err != nil {
		t.Fatalf("query task count: %v", err)
	}
	if createdCount != 0 {
		t.Fatalf("expected no resume task enqueued, got %d", createdCount)
	}
}

func TestResumeExternalSession_RejectsMissingIssueID(t *testing.T) {
	agentID, _ := mustLookupHandlerTestAgent(t)
	sessionID := "019d96b2-4407-7291-9c75-355eb8a949d8"

	req := newRequest("POST", "/api/agents/"+agentID+"/resume-session", map[string]any{
		"session_id": sessionID,
	})
	req = withRouteParams(req, map[string]string{
		"id": agentID,
	})
	w := httptest.NewRecorder()
	testHandler.ResumeExternalSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("ResumeExternalSession: expected 400 for missing issue_id, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeResumeErrorResponse(t, w)
	if resp.Code != resumeErrorCodeIssueRequired {
		t.Fatalf("ResumeExternalSession: expected code %q, got %q", resumeErrorCodeIssueRequired, resp.Code)
	}
	if !strings.Contains(strings.ToLower(resp.Error), "issue_id is required") {
		t.Fatalf("ResumeExternalSession: expected issue_id required error, got %q", resp.Error)
	}
}
