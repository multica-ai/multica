package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An unbound agent is refused by every execution entry point — that part already
// worked, because service.AgentReadiness and these gates all check
// RuntimeID.Valid. What did not work is what the user is told: the refusal reused
// runtime_offline, whose copy asks them to bring a machine back online. An
// unbound agent has no machine to bring back; the fix is to bind one. These tests
// pin the distinct reason code (MUL-5559).

// TestCommentMention_UnboundAgentReportsRuntimeRequired: @mentioning an unbound
// agent posts the comment, enqueues nothing, and reports agent_runtime_required
// rather than runtime_offline.
func TestCommentMention_UnboundAgentReportsRuntimeRequired(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Mention Unbound Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Mention Unbound Agent")
	// Public so the invocation gate (which runs first, and is enumeration-safe)
	// cannot be the reason we see a block.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET visibility = 'workspace', permission_mode = 'public_to' WHERE id = $1`,
		agentID); err != nil {
		t.Fatalf("make agent invocable: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		 VALUES ($1, 'workspace', $2, $3)`,
		agentID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("allow-list agent: %v", err)
	}

	issueID := createMentionFixtureIssue(t, ctx, "Mention unbound agent")

	// Bound but never mentioned yet: the same agent must be triggerable before we
	// unbind it, otherwise this test could pass for the wrong reason.
	if code := mentionAgentReasonCode(t, issueID, agentID); code != "" {
		t.Fatalf("bound agent: expected a successful trigger, got blocked with %q", code)
	}

	unbindRuntime(t, ctx, runtimeID, agentID)

	if code := mentionAgentReasonCode(t, issueID, agentID); code != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("unbound agent: reason_code = %q, want agent_runtime_required", code)
	}
}

// TestChatSend_UnboundAgentReturnsStructuredConflict: the chat gate already
// refused, but with a bare error string. The composer needs a code to offer
// "bind a runtime" instead of a generic failure.
func TestChatSend_UnboundAgentReturnsStructuredConflict(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Chat Unbound Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Chat Unbound Agent")
	sessionID := createHandlerTestChatSession(t, agentID)

	unbindRuntime(t, ctx, runtimeID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages",
		map[string]any{"content": "are you still there?"})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.SendChatMessage(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Error      string `json:"error"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ReasonCode != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("reason_code = %q, want agent_runtime_required", body.ReasonCode)
	}
	if body.Error == "" {
		t.Fatalf("expected a human-readable error alongside the code")
	}
}

func TestCreateAutopilot_UnboundAgentRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Autopilot Create Unbound Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Autopilot Create Unbound Agent")
	unbindRuntime(t, ctx, runtimeID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "must not start unbound",
		"assignee_id":    agentID,
		"execution_mode": "run_only",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var rows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM autopilot WHERE title = 'must not start unbound' AND assignee_id = $1`,
		agentID,
	).Scan(&rows); err != nil {
		t.Fatalf("count autopilots: %v", err)
	}
	if rows != 0 {
		t.Fatalf("unbound Agent received %d active Autopilots, want 0", rows)
	}
}

func TestUpdateAutopilot_UnboundAgentCannotResume(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Autopilot Resume Unbound Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Autopilot Resume Unbound Agent")

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id, status,
			execution_mode, created_by_type, created_by_id
		)
		VALUES ($1, 'cannot resume unbound', 'agent', $2, 'active',
			'run_only', 'member', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert paused autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})

	unbindRuntime(t, ctx, runtimeID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active"})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var status, pauseReason string
	if err := testPool.QueryRow(ctx,
		`SELECT status, pause_reason FROM autopilot WHERE id = $1`,
		autopilotID,
	).Scan(&status, &pauseReason); err != nil {
		t.Fatalf("read autopilot: %v", err)
	}
	if status != "paused" || pauseReason != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("autopilot = (%q, %q), want (paused, agent_runtime_required)", status, pauseReason)
	}
}

// mentionAgentReasonCode posts a comment mentioning the agent and returns the
// reason_code of its trigger outcome, or "" when the mention was admitted.
func mentionAgentReasonCode(t *testing.T, issueID, agentID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Agent](mention://agent/" + agentID + ") please take a look",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("CreateComment: expected 200/201, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		TriggerOutcomes []struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Status     string `json:"status"`
			ReasonCode string `json:"reason_code"`
		} `json:"trigger_outcomes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}
	for _, o := range body.TriggerOutcomes {
		if o.TargetID != agentID {
			continue
		}
		if o.Status == "blocked" {
			return o.ReasonCode
		}
		return ""
	}
	t.Fatalf("no trigger outcome for agent %s in %s", agentID, w.Body.String())
	return ""
}

func createMentionFixtureIssue(t *testing.T, ctx context.Context, title string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, number, title, description, status, creator_type, creator_id)
		VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1),
		        $2, '', 'todo', 'member', $3)
		RETURNING id
	`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert fixture issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}
