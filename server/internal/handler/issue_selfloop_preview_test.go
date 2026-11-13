package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestPreviewTriggerForRunningAgentSeesAnotherAgentsRun pins the preview side
// of the #9772 guard: an agent asking what its status write will start, while
// its own task lives on the issue and the issue sits on a DIFFERENT agent,
// must see that other agent's run in the preview — the write path enqueues it,
// and the two surfaces must not drift (MUL-3375).
func TestPreviewTriggerForRunningAgentSeesAnotherAgentsRun(t *testing.T) {
	actorAgent := seededReadyAgentID(t)

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT runtime_id FROM agent WHERE id = $1
	`, actorAgent).Scan(&runtimeID); err != nil {
		t.Fatalf("load actor runtime: %v", err)
	}
	targetAgent := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent (id, workspace_id, name, runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode)
		SELECT $1, workspace_id, 'selfloop-preview-target', runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode
		FROM agent WHERE id = $2
	`, targetAgent, actorAgent); err != nil {
		t.Fatalf("insert target agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		SELECT $1, target_type, target_id FROM agent_invocation_target WHERE agent_id = $2
	`, targetAgent, actorAgent); err != nil {
		t.Fatalf("insert target invocation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, targetAgent)
	})

	// The issue sits in backlog on the target agent; promoting it out of
	// backlog is the write under preview.
	issue := createIssueForTest(t, map[string]any{
		"title": "selfloop preview", "status": "backlog",
		"assignee_type": "agent", "assignee_id": targetAgent,
	})

	taskID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (id, agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, $3, 'running', $4)
	`, taskID, actorAgent, issue.ID, runtimeID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/preview-trigger?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{issue.ID},
		"status":    "todo",
	})
	req.Header.Set("X-Agent-ID", actorAgent)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.PreviewIssueTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewIssueTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if resp.TotalCount != 1 || len(resp.Triggers) != 1 || resp.Triggers[0].AgentID != targetAgent {
		t.Fatalf("preview for a running agent must show the target agent's run, got %+v", resp)
	}
}
