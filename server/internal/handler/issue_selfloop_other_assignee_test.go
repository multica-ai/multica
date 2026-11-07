package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestStatusWriteByRunningAgentEnqueuesADifferentAssignee pins the #9772
// report: an agent writing an issue's status from inside its own run (the
// X-Task-ID task lives on this issue) must not have the self-loop guard eat
// the run the write enqueues for a DIFFERENT agent. The guard exists to stop
// an agent re-waking itself; when the wake target is someone else, their run
// must start.
func TestStatusWriteByRunningAgentEnqueuesADifferentAssignee(t *testing.T) {
	actorAgent := seededReadyAgentID(t)

	// A second ready agent to hand the issue to: clone the first agent's
	// runtime binding so WillEnqueueRun sees it as ready.
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT runtime_id FROM agent WHERE id = $1
	`, actorAgent).Scan(&runtimeID); err != nil {
		t.Fatalf("load actor runtime: %v", err)
	}
	targetAgent := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent (id, workspace_id, name, runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode)
		SELECT $1, workspace_id, 'selfloop-target-agent', runtime_id, runtime_mode, model, custom_env, status, visibility, permission_mode
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

	issue := createIssueForTest(t, map[string]any{
		"title": "selfloop handoff", "status": "backlog",
		"assignee_type": "agent", "assignee_id": targetAgent,
	})

	// The actor's in-flight run on this very issue — what makes
	// isAgentRunningOnIssue true.
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

	// The actor's run promotes the issue out of backlog while the issue sits
	// on the target agent: the status write enqueues the target's run.
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issue.ID, map[string]any{"status": "todo"}), "id", issue.ID)
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Agent-ID", actorAgent)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if n := taskCountFor(t, issue.ID, targetAgent); n != 1 {
		t.Fatalf("target agent has %d queued runs for the issue, want 1 — the self-loop guard ate a run belonging to a different agent (#9772)", n)
	}
	// The actor's own running task row is expected; no NEW run for the actor.
	var actorNew int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
	`, issue.ID, actorAgent).Scan(&actorNew); err != nil {
		t.Fatalf("count actor tasks: %v", err)
	}
	if actorNew != 0 {
		t.Fatalf("actor agent has %d new runs for the issue, want 0 — the guard must still stop genuine self-wakes", actorNew)
	}
}
