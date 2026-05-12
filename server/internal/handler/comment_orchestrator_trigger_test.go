package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestCommentTriggersOrchestratorTask asserts the orchestrator wake-up
// trigger:
//   - workspace.orchestrator_agent_id is set
//   - an agent (the assignee, in this fixture) posts a comment
//   - the orchestrator is NOT the assignee (avoids the on_comment double-fire)
//   - the issue is not in a terminal status
//
// Expected: a new task is enqueued for the orchestrator agent with the
// triggering comment as trigger_comment_id.
func TestCommentTriggersOrchestratorTask(t *testing.T) {
	ctx := context.Background()

	// Two agents in the workspace: the assignee, and the orchestrator.
	// "Handler Test Agent" already exists from the test fixture. We create
	// a separate orchestrator.
	orchestratorID := createHandlerTestAgent(t, "Orchestrator Test Agent", nil)

	// Resolve the assignee agent id (the existing fixture).
	var assigneeAgentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&assigneeAgentID); err != nil {
		t.Fatalf("find assignee agent: %v", err)
	}

	// Set the workspace's orchestrator. Restored on cleanup.
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET orchestrator_agent_id = $1 WHERE id = $2`,
		orchestratorID, testWorkspaceID,
	); err != nil {
		t.Fatalf("set orchestrator_agent_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET orchestrator_agent_id = NULL WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Issue assigned to the assignee agent.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Orchestrator trigger fixture",
		"assignee_type": "agent",
		"assignee_id":   assigneeAgentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// Snapshot existing tasks so we can isolate the new ones the trigger
	// creates. The act of assigning an issue to an agent enqueues an
	// on_assign task; we want to compare against post-comment delta.
	existingTaskIDs := map[string]bool{}
	rows, err := testPool.Query(ctx, `SELECT id FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
	if err != nil {
		t.Fatalf("snapshot tasks: %v", err)
	}
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		existingTaskIDs[uuidToString(id)] = true
	}
	rows.Close()

	// Agent (the assignee) posts a comment via X-Agent-ID — the trigger
	// only fires for agent-authored comments.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Work complete. Ready for review.",
	})
	req = setAgentActor(t, req, assigneeAgentID)
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: %d %s", w.Code, w.Body.String())
	}
	var comment CommentResponse
	json.NewDecoder(w.Body).Decode(&comment)

	// Find the new task. The trigger goes through the service's
	// notifyTaskAvailable channel which is async, but the DB row is
	// committed before the handler returns, so a direct SELECT is safe.
	var orchestratorTaskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND trigger_comment_id = $3
	`, issue.ID, orchestratorID, comment.ID).Scan(&orchestratorTaskCount); err != nil {
		t.Fatalf("count orchestrator tasks: %v", err)
	}
	if orchestratorTaskCount != 1 {
		t.Fatalf("expected 1 orchestrator task, got %d", orchestratorTaskCount)
	}

	// And the assignee got their normal on_comment task (not part of this
	// PR's logic — just confirming the orchestrator trigger doesn't break
	// the pre-existing flow).
	var assigneeTaskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issue.ID, assigneeAgentID).Scan(&assigneeTaskCount); err != nil {
		t.Fatalf("count assignee tasks: %v", err)
	}
	if assigneeTaskCount < 1 {
		t.Fatalf("expected at least 1 assignee task, got %d", assigneeTaskCount)
	}
}

// TestCommentDoesNotTriggerOrchestratorWhenAuthorIsMember covers the
// most-important suppression path. Member-authored comments must NOT wake
// the orchestrator — the orchestrator pattern is about cross-agent
// workflow, not "an LLM reacts to every human reply."
func TestCommentDoesNotTriggerOrchestratorWhenAuthorIsMember(t *testing.T) {
	ctx := context.Background()

	orchestratorID := createHandlerTestAgent(t, "Member-Comment Suppression Orchestrator", nil)

	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET orchestrator_agent_id = $1 WHERE id = $2`,
		orchestratorID, testWorkspaceID,
	); err != nil {
		t.Fatalf("set orchestrator_agent_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET orchestrator_agent_id = NULL WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Issue with no assignee (irrelevant to this suppression).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Member-suppression fixture",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// Member-authored comment (no X-Agent-ID).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Hey team, anyone want to take this?",
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: %d %s", w.Code, w.Body.String())
	}

	// Orchestrator must NOT have a task — comment was member-authored.
	var n int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issue.ID, orchestratorID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 orchestrator tasks for member-authored comment, got %d", n)
	}
}

// TestCommentDoesNotTriggerOrchestratorOnSelfLoop covers the agent==orchestrator
// suppression: if the orchestrator IS the comment author, we don't re-wake
// it on its own comment.
func TestCommentDoesNotTriggerOrchestratorOnSelfLoop(t *testing.T) {
	ctx := context.Background()

	orchestratorID := createHandlerTestAgent(t, "Self-Loop Suppression Orchestrator", nil)

	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET orchestrator_agent_id = $1 WHERE id = $2`,
		orchestratorID, testWorkspaceID,
	); err != nil {
		t.Fatalf("set orchestrator_agent_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET orchestrator_agent_id = NULL WHERE id = $1`,
			testWorkspaceID,
		)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Self-loop fixture",
	})
	testHandler.CreateIssue(w, req)
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// Orchestrator posts on its own.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Routing this to Claude Code.",
	})
	req = setAgentActor(t, req, orchestratorID)
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: %d %s", w.Code, w.Body.String())
	}

	// No orchestrator task — self-loop suppression.
	var n int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
	`, issue.ID, orchestratorID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 orchestrator tasks on self-loop, got %d", n)
	}
}

// TestUpdateWorkspaceSetOrchestratorAgent covers the API contract:
// PATCH /api/workspaces/:id with {orchestrator_agent_id_set:true,
// orchestrator_agent_id:"<uuid>"} sets the field; passing null clears it.
func TestUpdateWorkspaceSetOrchestratorAgent(t *testing.T) {
	ctx := context.Background()
	orchestratorID := createHandlerTestAgent(t, "API Set Orchestrator", nil)

	// Set.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID, map[string]any{
		"orchestrator_agent_id_set": true,
		"orchestrator_agent_id":     orchestratorID,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWorkspace (set): %d %s", w.Code, w.Body.String())
	}
	var resp WorkspaceResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.OrchestratorAgentID == nil || *resp.OrchestratorAgentID != orchestratorID {
		got := "nil"
		if resp.OrchestratorAgentID != nil {
			got = *resp.OrchestratorAgentID
		}
		t.Fatalf("expected orchestrator_agent_id=%q, got %q", orchestratorID, got)
	}

	// Clear.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/workspaces/"+testWorkspaceID, map[string]any{
		"orchestrator_agent_id_set": true,
		"orchestrator_agent_id":     nil,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWorkspace (clear): %d %s", w.Code, w.Body.String())
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.OrchestratorAgentID != nil {
		t.Fatalf("expected orchestrator_agent_id=null after clear, got %q", *resp.OrchestratorAgentID)
	}

	// Reset to NULL on cleanup just in case the clear failed.
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE workspace SET orchestrator_agent_id = NULL WHERE id = $1`, testWorkspaceID)
	})
}

// TestOrchestratorLoopCircuitBreaker asserts that the circuit breaker suppresses
// the orchestrator wake-up when 5+ tasks for the orchestrator on this issue
// were already triggered by agent-authored comments in the last 5 minutes.
func TestOrchestratorLoopCircuitBreaker(t *testing.T) {
	ctx := context.Background()

	posterID := createHandlerTestAgent(t, "Loop Poster Agent", nil)
	orchestratorID := createHandlerTestAgent(t, "Loop Orchestrator Agent", nil)

	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET orchestrator_agent_id = $1 WHERE id = $2`,
		orchestratorID, testWorkspaceID,
	); err != nil {
		t.Fatalf("set orchestrator_agent_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET orchestrator_agent_id = NULL WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Issue unassigned (orchestrator is not the assignee, so suppression doesn't fire).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Circuit-breaker orchestrator fixture",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// Seed 5 agent-authored comments + orchestrator tasks pointing at them to
	// simulate a loop that already hit the threshold.
	for i := 0; i < 5; i++ {
		var seedCommentID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_id, author_type, content)
			VALUES ($1, $2, $3, 'agent', 'loop comment')
			RETURNING id
		`, issue.ID, testWorkspaceID, posterID).Scan(&seedCommentID); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id, trigger_comment_id, status)
			VALUES ($1, $2, $3, $4, 'completed')
		`, issue.ID, orchestratorID, handlerTestRuntimeID(t), seedCommentID); err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}

	// Snapshot task count for the orchestrator before the triggering comment.
	var beforeCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, orchestratorID).Scan(&beforeCount)

	// Agent posts a comment — this would normally wake the orchestrator.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": "Triggering comment that should be circuit-broken.",
	})
	req = setAgentActor(t, req, posterID)
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: %d %s", w.Code, w.Body.String())
	}

	// Orchestrator task count must NOT have increased.
	var afterCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, orchestratorID).Scan(&afterCount)
	if afterCount != beforeCount {
		t.Fatalf("circuit breaker did not fire: orchestrator task count went %d → %d (expected no change)", beforeCount, afterCount)
	}
}

// TestMentionLoopCircuitBreaker asserts that the circuit breaker suppresses
// @mention task enqueuing when 5+ tasks for the target agent on this issue
// were already triggered by agent-authored comments in the last 5 minutes.
func TestMentionLoopCircuitBreaker(t *testing.T) {
	ctx := context.Background()

	callerID := createHandlerTestAgent(t, "Loop Caller Agent", nil)
	targetID := createHandlerTestAgent(t, "Loop Target Agent", nil)

	// Issue unassigned.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Circuit-breaker mention fixture",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// Get the target agent's mention link format so we can @mention them.
	var targetMentionID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE id = $1`, targetID,
	).Scan(&targetMentionID); err != nil {
		t.Fatalf("look up target agent: %v", err)
	}

	// Seed 5 agent-authored comments + tasks for the target to simulate a loop.
	for i := 0; i < 5; i++ {
		var seedCommentID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_id, author_type, content)
			VALUES ($1, $2, $3, 'agent', 'loop mention comment')
			RETURNING id
		`, issue.ID, testWorkspaceID, callerID).Scan(&seedCommentID); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (issue_id, agent_id, runtime_id, trigger_comment_id, status)
			VALUES ($1, $2, $3, $4, 'completed')
		`, issue.ID, targetID, handlerTestRuntimeID(t), seedCommentID); err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}

	// Snapshot task count for the target before the new @mention.
	var beforeCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, targetID).Scan(&beforeCount)

	// Agent posts a comment that @mentions the target.
	mentionContent := "[Target](mention://agent/" + targetID + ") please do the next step."
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/comments", map[string]any{
		"content": mentionContent,
	})
	req = setAgentActor(t, req, callerID)
	req = withURLParam(req, "id", issue.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: %d %s", w.Code, w.Body.String())
	}

	// Target task count must NOT have increased.
	var afterCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		issue.ID, targetID).Scan(&afterCount)
	if afterCount != beforeCount {
		t.Fatalf("mention circuit breaker did not fire: task count went %d → %d (expected no change)", beforeCount, afterCount)
	}
}

// TestUpdateWorkspaceRejectsCrossWorkspaceOrchestratorAgent covers a
// security-relevant guard: a malicious or misconfigured client can't point
// the orchestrator at an agent in a DIFFERENT workspace, which would
// otherwise let comments in workspace A wake an agent in workspace B.
func TestUpdateWorkspaceRejectsCrossWorkspaceOrchestratorAgent(t *testing.T) {
	ctx := context.Background()

	// Create a sibling workspace + an agent in it. Sibling agent_id is
	// what we'll try (and fail) to point our test workspace at.
	var siblingWorkspaceID, siblingRuntimeID, siblingAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix, settings)
		VALUES ('Sibling', 'sibling-orch', 'SIB', '{}'::jsonb)
		RETURNING id
	`).Scan(&siblingWorkspaceID); err != nil {
		t.Fatalf("create sibling workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, siblingWorkspaceID)
	})
	// agent_runtime has no max_concurrent_tasks column; status is checked
	// to be 'online' | 'offline'; runtime_mode is 'local' | 'cloud'. The
	// fixture exists only to give the sibling agent a runtime FK so the
	// cross-workspace pointer attempt has something to point at.
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		VALUES ($1, 'sibling-rt', 'cloud', 'claude', 'online') RETURNING id
	`, siblingWorkspaceID).Scan(&siblingRuntimeID); err != nil {
		t.Fatalf("create sibling runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'sibling-orch-agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id
	`, siblingWorkspaceID, siblingRuntimeID, testUserID).Scan(&siblingAgentID); err != nil {
		t.Fatalf("create sibling agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID, map[string]any{
		"orchestrator_agent_id_set": true,
		"orchestrator_agent_id":     siblingAgentID,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-workspace orchestrator, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "in this workspace") {
		t.Fatalf("expected cross-workspace error body, got: %s", w.Body.String())
	}
}
