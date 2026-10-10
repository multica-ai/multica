package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// agentConversationFixture creates a plain member and an agent owned by that
// member. Ownership (not membership) is what opens the monitoring endpoints.
func agentConversationFixture(t *testing.T, name, ownerEmail string) (agentID, ownerID string) {
	t.Helper()
	ownerID = createPermissionTestMember(t, ownerEmail)
	agentID = createHandlerTestAgent(t, name, nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET owner_id = $1 WHERE id = $2`, ownerID, agentID); err != nil {
		t.Fatalf("assign agent owner: %v", err)
	}
	return agentID, ownerID
}

// createAgentConversationSession seeds a chat_session created by creatorID,
// optionally archived. explicitly_created_at makes it visible to the monitor
// projection even without a message.
func createAgentConversationSession(t *testing.T, agentID, creatorID, title, status string) string {
	t.Helper()
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, explicitly_created_at)
		VALUES ($1, $2, $3, $4, $5, now())
		RETURNING id
	`, testWorkspaceID, agentID, creatorID, title, status).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	return sessionID
}

func appendAgentConversationMessage(t *testing.T, sessionID, role, content string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, $2, $3)
	`, sessionID, role, content); err != nil {
		t.Fatalf("append chat message: %v", err)
	}
}

func listAgentConversations(t *testing.T, actorID, agentID, query string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/agents/" + agentID + "/chat-sessions" + query
	req := withURLParam(newRequestAs(actorID, http.MethodGet, path, nil), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ListAgentChatSessions(w, req)
	return w
}

// TestAgentConversations_OwnerMonitorsEveryMember covers the read surface: the
// agent owner and a workspace owner see every member's sessions, while an
// unrelated member is refused. It also pins the active/archived default.
func TestAgentConversations_OwnerMonitorsEveryMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID := agentConversationFixture(t, "conversations-owner-agent", "conversations-owner@multica.test")
	otherMemberID := createPermissionTestMember(t, "conversations-other@multica.test")

	ownerSession := createAgentConversationSession(t, agentID, ownerID, "owned", "active")
	otherSession := createAgentConversationSession(t, agentID, otherMemberID, "other member", "active")
	archivedSession := createAgentConversationSession(t, agentID, otherMemberID, "archived", "archived")

	// Default: active only.
	w := listAgentConversations(t, ownerID, agentID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var active []ChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &active); err != nil {
		t.Fatalf("decode owner list: %v", err)
	}
	ids := map[string]bool{}
	for _, s := range active {
		ids[s.ID] = true
	}
	if !ids[ownerSession] || !ids[otherSession] {
		t.Errorf("owner must see their own and other members' sessions, got %v", ids)
	}
	if ids[archivedSession] {
		t.Errorf("active-only list must not include an archived session")
	}

	// status=all includes archived.
	w = listAgentConversations(t, ownerID, agentID, "?status=all")
	if w.Code != http.StatusOK {
		t.Fatalf("owner list (all): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var all []ChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode owner list (all): %v", err)
	}
	foundArchived := false
	for _, s := range all {
		if s.ID == archivedSession {
			foundArchived = true
		}
	}
	if !foundArchived {
		t.Errorf("status=all must include the archived session, got %d rows", len(all))
	}

	// Workspace owner (testUserID) is admitted by role even though it owns no
	// agent here.
	if w := listAgentConversations(t, testUserID, agentID, ""); w.Code != http.StatusOK {
		t.Fatalf("workspace owner list: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Unrelated plain member is refused.
	if w := listAgentConversations(t, otherMemberID, agentID, ""); w.Code != http.StatusForbidden {
		t.Fatalf("unrelated member list: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentConversations_MessagesAreAgentScoped verifies the transcript read:
// the owner can read a member's messages, but the per-agent privilege cannot
// be used to read another agent's session, and unrelated members are refused.
func TestAgentConversations_MessagesAreAgentScoped(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, ownerID := agentConversationFixture(t, "conversations-messages-agent", "conversations-messages-owner@multica.test")
	strangerID := createPermissionTestMember(t, "conversations-messages-stranger@multica.test")

	sessionID := createAgentConversationSession(t, agentID, strangerID, "monitored", "active")
	appendAgentConversationMessage(t, sessionID, "user", "hello from the member")
	appendAgentConversationMessage(t, sessionID, "assistant", "hello from the agent")

	readMessages := func(actorID, urlAgentID, session string) *httptest.ResponseRecorder {
		req := withURLParams(newRequestAs(actorID, http.MethodGet, "/api/agents/"+urlAgentID+"/chat-sessions/"+session+"/messages", nil), "id", urlAgentID, "sessionId", session)
		// respondChatMessagesPage reads the workspace from request context, set
		// by the workspace middleware in production. This test calls the handler
		// directly, so inject it the same way chat_test.go does.
		req = withChatTestWorkspaceCtx(t, req)
		w := httptest.NewRecorder()
		testHandler.ListAgentChatSessionMessages(w, req)
		return w
	}

	w := readMessages(ownerID, agentID, sessionID)
	if w.Code != http.StatusOK {
		t.Fatalf("owner read messages: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page ChatMessagesPageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode messages page: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(page.Messages))
	}
	if page.Messages[0].Content != "hello from the member" || page.Messages[1].Content != "hello from the agent" {
		t.Errorf("messages out of order or wrong content: %+v", page.Messages)
	}

	if w := readMessages(strangerID, agentID, sessionID); w.Code != http.StatusForbidden {
		t.Fatalf("unrelated member read messages: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// A second agent owned by the same owner: the session belongs to the first
	// agent, so addressing it through the second must 404 rather than leak.
	otherAgentID := createHandlerTestAgent(t, "conversations-messages-other-agent", nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET owner_id = $1 WHERE id = $2`, ownerID, otherAgentID); err != nil {
		t.Fatalf("assign second agent owner: %v", err)
	}
	if w := readMessages(ownerID, otherAgentID, sessionID); w.Code != http.StatusNotFound {
		t.Fatalf("cross-agent read: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentConversations_AgentActorRejected pins the boundary: an agent process
// stamped with its owner's identity must not read the monitoring endpoints.
func TestAgentConversations_AgentActorRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID := agentConversationFixture(t, "conversations-actor-target", "conversations-actor-owner@multica.test")
	hostAgentID := createHandlerTestAgent(t, "conversations-actor-host", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, ownerID, hostAgentID); err != nil {
		t.Fatalf("assign host agent owner: %v", err)
	}
	hostTaskID := createHandlerTestTaskForAgent(t, hostAgentID)

	req := withURLParam(newRequestAs(ownerID, http.MethodGet, "/api/agents/"+agentID+"/chat-sessions", nil), "id", agentID)
	req.Header.Set("X-Agent-ID", hostAgentID)
	req.Header.Set("X-Task-ID", hostTaskID)
	w := httptest.NewRecorder()
	testHandler.ListAgentChatSessions(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent actor list: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
