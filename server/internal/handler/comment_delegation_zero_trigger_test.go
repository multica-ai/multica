package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// seedDelegationPair returns the leader and worker agent ids for one delegated
// run: the worker task carries delegated_from_task_id pointing at the leader's
// running task, both on the given issue.
func seedDelegationPair(t *testing.T, issueID string) (workerAgent, workerTaskID string) {
	t.Helper()
	leaderAgent := seededReadyAgentID(t)

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT runtime_id FROM agent WHERE id = $1
	`, leaderAgent).Scan(&runtimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}

	workerAgent = uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent (id, workspace_id, name, runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode)
		SELECT $1, workspace_id, 'delegation-worker-agent', runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode
		FROM agent WHERE id = $2
	`, workerAgent, leaderAgent); err != nil {
		t.Fatalf("insert worker agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, workerAgent)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		SELECT $1, target_type, target_id FROM agent_invocation_target WHERE agent_id = $2
	`, workerAgent, leaderAgent); err != nil {
		t.Fatalf("insert worker invocation: %v", err)
	}

	leaderTaskID := uuid.NewString()
	workerTaskID = uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (id, agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, $3, 'running', $4)
	`, leaderTaskID, leaderAgent, issueID, runtimeID); err != nil {
		t.Fatalf("insert leader task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id IN ($1, $2)`, leaderTaskID, workerTaskID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (id, agent_id, issue_id, status, runtime_id, delegated_from_task_id)
		VALUES ($1, $2, $3, 'running', $4, $5)
	`, workerTaskID, workerAgent, issueID, runtimeID, leaderTaskID); err != nil {
		t.Fatalf("insert worker task: %v", err)
	}
	return workerAgent, workerTaskID
}

func postCommentAsAgent(t *testing.T, issueID, agentID, taskID, content string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": content,
	}), "id", issueID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.CreateComment(w, req)
	return w
}

// TestDelegatedWorkerCommentWithNoRouteWarns pins the visibility half of
// #9720: a delegated worker's closing comment that resolves to zero triggers
// (no mention, no squad fallback) is exactly the moment a delegation chain
// silently stalls, so the daemon log must say so — with the delegation
// attribution — while a non-delegated agent's unrouted comment stays quiet.
func TestDelegatedWorkerCommentWithNoRouteWarns(t *testing.T) {
	issue := createIssueForTest(t, map[string]any{"title": "delegation zero trigger", "status": "todo"})
	workerAgent, workerTaskID := seedDelegationPair(t, issue.ID)

	var logs strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	w := postCommentAsAgent(t, issue.ID, workerAgent, workerTaskID, "closing report: work is done")
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(logs.String(), "delegat") {
		t.Fatalf("expected a delegation zero-route warning in the log, got:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), workerTaskID) {
		t.Fatalf("warning must carry the worker task id for attribution, got:\n%s", logs.String())
	}

	// The non-delegated control: the same unrouted comment from a plain agent
	// task is the quiet default (agents chat on issues all the time).
	issue2 := createIssueForTest(t, map[string]any{"title": "delegation zero trigger control", "status": "todo"})
	plainAgent := seededReadyAgentID(t)
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT runtime_id FROM agent WHERE id = $1
	`, plainAgent).Scan(&runtimeID); err != nil {
		t.Fatalf("load plain agent runtime: %v", err)
	}
	plainTaskID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (id, agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, $3, 'running', $4)
	`, plainTaskID, plainAgent, issue2.ID, runtimeID); err != nil {
		t.Fatalf("insert plain task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, plainTaskID)
	})

	var logs2 strings.Builder
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs2, &slog.HandlerOptions{Level: slog.LevelInfo})))
	w2 := postCommentAsAgent(t, issue2.ID, plainAgent, plainTaskID, "unrelated note without mention")
	if w2.Code != http.StatusCreated {
		t.Fatalf("CreateComment control: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}
	if strings.Contains(logs2.String(), "delegat") {
		t.Fatalf("non-delegated unrouted comment must stay quiet, got:\n%s", logs2.String())
	}
}

// JSON decode helper kept local so the test file stays self-contained.
func decodeJSON(t *testing.T, r *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
