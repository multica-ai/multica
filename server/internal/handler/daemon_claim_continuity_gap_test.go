package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// claimContinuityGapProbe decodes just the continuity-gap fields off a claim
// response so the two MUL-5305 disclosure paths can be asserted end-to-end.
type claimContinuityGapProbe struct {
	Task *struct {
		ID                            string `json:"id"`
		PriorSessionID                string `json:"prior_session_id"`
		PriorSessionResumeUnavailable bool   `json:"prior_session_resume_unavailable"`
	} `json:"task"`
}

func claimOneTaskForRuntime(t *testing.T, runtimeID, daemonID string) claimContinuityGapProbe {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var probe claimContinuityGapProbe
	if err := json.NewDecoder(w.Body).Decode(&probe); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if probe.Task == nil {
		t.Fatal("expected a claimed task in the response")
	}
	return probe
}

// TestClaimTaskByRuntime_ChatRolloutMissingDisclosesGap is the claim-response
// half of MUL-5305 Must-fix 2 for chat: when the most recent terminal task on a
// chat session withheld its Codex session (rollout missing), the next chat claim
// resumes the older pointer but MUST still set prior_session_resume_unavailable
// so the run discloses the gap instead of silently continuing.
func TestClaimTaskByRuntime_ChatRolloutMissingDisclosesGap(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "ChatGapClaimAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	// Chat session that still carries a good resume pointer from an earlier turn.
	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, runtime_id, session_id, work_dir)
		VALUES ($1, $2, $3, 'gap chat', 'active', $4, 'OLD-GOOD-CHAT-SESSION', '/tmp/chat')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID) })

	// Most recent terminal turn on this session withheld its Codex session.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, started_at, completed_at, chat_session_id, session_rollout_missing)
		VALUES ($1, $2, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', $3, TRUE)
	`, agentID, runtimeID, sessionID); err != nil {
		t.Fatalf("insert withheld chat task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'next turn')
	`, sessionID); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'queued', 1000, $3) RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("create chat follow-up task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	probe := claimOneTaskForRuntime(t, runtimeID, "chat-gap-claim-test")
	if !probe.Task.PriorSessionResumeUnavailable {
		t.Fatalf("expected chat claim to disclose the continuity gap; response task=%+v", *probe.Task)
	}
}

// TestClaimTaskByRuntime_RerunSourceRolloutMissingDisclosesGap is the
// claim-response half of MUL-5305 Must-fix 2 for manual rerun: when the exact
// source task withheld its Codex session, the rerun has nothing resumable from
// it and MUST disclose the gap rather than silently start fresh.
func TestClaimTaskByRuntime_RerunSourceRolloutMissingDisclosesGap(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "RerunGapClaimAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		VALUES ($1, 'rerun gap issue', 'todo', 'none', 'member', $2, 'agent', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Source task whose Codex session was withheld (rollout missing).
	var srcID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_rollout_missing)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', TRUE)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&srcID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}

	// A manual rerun of that source task, queued to claim (rerun carries
	// force_fresh_session=true; the disclosure must still fire from the source).
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, rerun_of_task_id, force_fresh_session)
		VALUES ($1, $2, $3, 'queued', 1000, $4, TRUE) RETURNING id
	`, agentID, runtimeID, issueID, srcID).Scan(&taskID); err != nil {
		t.Fatalf("create rerun task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	probe := claimOneTaskForRuntime(t, runtimeID, "rerun-gap-claim-test")
	if !probe.Task.PriorSessionResumeUnavailable {
		t.Fatalf("expected rerun claim to disclose the source task's continuity gap; response task=%+v", *probe.Task)
	}
}
