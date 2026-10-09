package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func createRuntimeAccessDeniedAgent(t *testing.T, ctx context.Context, runtimeID, name string) string {
	t.Helper()
	foreignOwnerID := dbfx.User(t, name+" owner", "runtime-access-owner-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, testWorkspaceID, foreignOwnerID, "member")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, name)
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1, visibility = 'workspace', permission_mode = 'public_to' WHERE id = $2`, foreignOwnerID, agentID)
	dbfx.Exec(t, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID)
	return agentID
}

func TestCreateIssue_RuntimeAccessDeniedLeavesNoticeWithoutTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Assignment access denied runtime")
	agentID := createRuntimeAccessDeniedAgent(t, ctx, runtimeID, "Assignment access denied agent")

	w := testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "assignment access denied issue",
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})).Want(http.StatusCreated)
	var issue IssueResponse
	w.JSON(&issue)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID) })

	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issue.ID).Scan(&taskCount)
	if taskCount != 0 {
		t.Fatalf("assignment created %d task rows, want 0", taskCount)
	}
	var notice string
	dbfx.QueryRow(t, `
		SELECT content FROM comment
		WHERE issue_id = $1 AND author_type = 'system' AND type = 'system'
		ORDER BY created_at DESC LIMIT 1
	`, issue.ID).Scan(&notice)
	if !strings.Contains(notice, "public") || !strings.Contains(notice, "rebind/copy") {
		t.Fatalf("assignment notice = %q, want both recovery paths", notice)
	}
}

func TestSendChatMessage_RuntimeAccessDeniedReturnsStructuredConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Chat access denied runtime")
	agentID := createRuntimeAccessDeniedAgent(t, ctx, runtimeID, "Chat access denied agent")
	sessionID := createHandlerTestChatSession(t, agentID)

	req := newRequest(http.MethodPost, "/api/chat/sessions/"+sessionID+"/messages", map[string]any{"content": "please help"})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := testutil.Call(t, testHandler.SendChatMessage, req).Want(http.StatusConflict)
	var body struct {
		ReasonCode string `json:"reason_code"`
	}
	w.JSON(&body)
	if body.ReasonCode != string(ReasonRuntimeAccessDenied) {
		t.Fatalf("reason_code = %q, want %s", body.ReasonCode, ReasonRuntimeAccessDenied)
	}
	var taskCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1`, sessionID).Scan(&taskCount)
	if taskCount != 0 {
		t.Fatalf("blocked chat created %d task rows, want 0", taskCount)
	}
}

func TestCommentMention_RuntimeAccessDeniedReportsTargetAndNotice(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createCascadeFixtureRuntime(t, ctx, "Mention access denied runtime")
	agentID := createRuntimeAccessDeniedAgent(t, ctx, runtimeID, "Mention access denied agent")
	issueID := createMentionFixtureIssue(t, ctx, "mention access denied issue")

	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Agent](mention://agent/" + agentID + ") please help",
	})
	req = withURLParam(req, "id", issueID)
	w := testutil.Call(t, testHandler.CreateComment, req).WantOneOf(http.StatusOK, http.StatusCreated)
	var body struct {
		TriggerOutcomes []struct {
			TargetID   string `json:"target_id"`
			Status     string `json:"status"`
			ReasonCode string `json:"reason_code"`
		} `json:"trigger_outcomes"`
	}
	w.JSON(&body)
	found := false
	for _, outcome := range body.TriggerOutcomes {
		if outcome.TargetID == agentID {
			found = true
			if outcome.Status != string(DispatchBlocked) || outcome.ReasonCode != string(ReasonRuntimeAccessDenied) {
				t.Fatalf("target outcome = %+v, want blocked/runtime_access_denied", outcome)
			}
		}
	}
	if !found {
		t.Fatalf("no blocked outcome for agent %s: %s", agentID, w.Body.String())
	}
	var notice string
	dbfx.QueryRow(t, `
		SELECT content FROM comment
		WHERE issue_id = $1 AND author_type = 'system' AND type = 'system'
		ORDER BY created_at DESC LIMIT 1
	`, issueID).Scan(&notice)
	if !strings.Contains(notice, "public") || !strings.Contains(notice, "rebind/copy") {
		t.Fatalf("mention notice = %q, want both recovery paths", notice)
	}
}
