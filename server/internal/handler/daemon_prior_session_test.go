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

func TestClaimTaskByRuntime_IssueTaskSkipsPriorSessionWithoutWorkDir(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, number, title, description, status, priority,
			assignee_type, assignee_id, creator_type, creator_id, position
		)
		VALUES ($1, 9901, 'prior session without workdir', '', 'todo', 'high', 'agent', $2, 'member', $3, 0)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	// Poison prior task: completed session_id exists, but work_dir is NULL.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), 'stale-session-123', NULL)
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("setup: create prior completed task: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create queued task: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-daemon-claim")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ID             string `json:"id"`
			PriorSessionID string `json:"prior_session_id"`
			PriorWorkDir   string `json:"prior_work_dir"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a task in response, got nil")
	}
	if !strings.EqualFold(resp.Task.ID, taskID) {
		t.Fatalf("expected task id %q, got %q", taskID, resp.Task.ID)
	}
	if resp.Task.PriorSessionID != "" {
		t.Fatalf("expected prior_session_id to be empty when prior work_dir is missing, got %q", resp.Task.PriorSessionID)
	}
	if resp.Task.PriorWorkDir != "" {
		t.Fatalf("expected prior_work_dir to be empty, got %q", resp.Task.PriorWorkDir)
	}
}
