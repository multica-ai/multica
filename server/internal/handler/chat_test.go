package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// withWorkspaceContext sets the workspace ID in the request context.
func withWorkspaceContext(req *http.Request, workspaceID string) *http.Request {
	member := db.Member{
		UserID:      util.MustParseUUID(testUserID),
		WorkspaceID: util.MustParseUUID(workspaceID),
		Role:        "owner",
	}
	ctx := middleware.SetMemberContext(req.Context(), workspaceID, member)
	return req.WithContext(ctx)
}

// setupChatTestFixture creates a test chat session with messages.
func setupChatTestFixture(t *testing.T) (sessionID, userMessageID, assistantMessageID string) {
	t.Helper()
	ctx := context.Background()

	// Create chat session
	var sid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		SELECT workspace_id, id, $1, 'Test Chat Session', 'active'
		FROM agent
		WHERE workspace_id = $2
		LIMIT 1
		RETURNING id
	`, testUserID, testWorkspaceID).Scan(&sid); err != nil {
		t.Fatalf("failed to create chat session: %v", err)
	}
	sessionID = util.UUIDToString(util.MustParseUUID(sid))

	// Create user message
	var umid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'Test user message')
		RETURNING id
	`, sid).Scan(&umid); err != nil {
		t.Fatalf("failed to create user message: %v", err)
	}
	userMessageID = util.UUIDToString(util.MustParseUUID(umid))

	// Create assistant message
	var amid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'assistant', 'Test assistant response')
		RETURNING id
	`, sid).Scan(&amid); err != nil {
		t.Fatalf("failed to create assistant message: %v", err)
	}
	assistantMessageID = util.UUIDToString(util.MustParseUUID(amid))

	return
}

// cleanupChatTestFixture removes the test chat session.
func cleanupChatTestFixture(t *testing.T, sessionID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.MustParseUUID(sessionID)); err != nil {
		t.Logf("warning: failed to cleanup chat session: %v", err)
	}
}

func createChatSessionForAgent(t *testing.T, agentID, creatorID, title string) string {
	t.Helper()

	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, creatorID, title).Scan(&sessionID); err != nil {
		t.Fatalf("failed to create chat session: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, util.MustParseUUID(sessionID))
	})

	return sessionID
}

func createOtherUserPrivateAgent(t *testing.T, name string) (agentID, ownerID string) {
	t.Helper()

	ctx := context.Background()
	emailLocal := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	email := fmt.Sprintf("%s-%s@multica.ai", emailLocal, strings.ToLower(strings.ReplaceAll(name, " ", "-")))

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, name, email).Scan(&ownerID); err != nil {
		t.Fatalf("failed to create other user: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(ownerID)); err != nil {
		t.Fatalf("failed to add other user to workspace: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("failed to create private agent: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, util.MustParseUUID(agentID))
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(ownerID))
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, util.MustParseUUID(ownerID))
	})

	return agentID, ownerID
}

type queryRowOverrideDB struct {
	base     db.DBTX
	matchSQL func(string) bool
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (d queryRowOverrideDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return d.base.Exec(ctx, sql, args...)
}

func (d queryRowOverrideDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return d.base.Query(ctx, sql, args...)
}

func (d queryRowOverrideDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if d.matchSQL != nil && d.matchSQL(sql) && d.queryRow != nil {
		return d.queryRow(ctx, sql, args...)
	}
	return d.base.QueryRow(ctx, sql, args...)
}

// TestCreateChatSession_Error_PrivateAgentNotOwned verifies that creating a
// chat session with a private agent owned by another user returns 403.
func TestCreateChatSession_Error_PrivateAgentNotOwned(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create another user who will own the private agent.
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Private Agent Owner', 'private-owner@multica.ai')
		RETURNING id
	`).Scan(&otherUserID); err != nil {
		t.Fatalf("setup: create other user: %v", err)
	}
	otherUserIDStr := util.UUIDToString(util.MustParseUUID(otherUserID))
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, util.MustParseUUID(otherUserIDStr)) })

	// Add them to the workspace.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(otherUserIDStr)); err != nil {
		t.Fatalf("setup: add other member: %v", err)
	}

	// Create a private agent owned by the other user.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("setup: get runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, 'Other Private Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, otherUserIDStr).Scan(&agentID); err != nil {
		t.Fatalf("setup: create private agent: %v", err)
	}
	agentIDStr := util.UUIDToString(util.MustParseUUID(agentID))
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, util.MustParseUUID(agentIDStr)) })

	// testUserID tries to create a chat session with the other user's private agent → 403
	req := newRequest("POST", "/api/chat/sessions?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentIDStr,
		"title":    "should fail",
	})
	req = withWorkspaceContext(req, testWorkspaceID)

	rr := httptest.NewRecorder()
	testHandler.CreateChatSession(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestCreateChatSession_Success_OwnPrivateAgent verifies that creating a chat
// session with your own private agent succeeds.
func TestCreateChatSession_Success_OwnPrivateAgent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create a private agent owned by testUserID.
	agentID := createHandlerTestAgent(t, "My Private Chat Agent", []byte("{}"))

	req := newRequest("POST", "/api/chat/sessions?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentID,
		"title":    "my chat",
	})
	req = withWorkspaceContext(req, testWorkspaceID)

	rr := httptest.NewRecorder()
	testHandler.CreateChatSession(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Cleanup the session.
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	if sid, ok := resp["id"].(string); ok {
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.MustParseUUID(sid))
		})
	}
}

func TestListChatSessions_FiltersPrivateSessionsOwnedByOthers(t *testing.T) {
	t.Parallel()

	visibleAgentID := createHandlerTestAgent(t, "Visible Private Chat Agent", []byte("{}"))
	visibleSessionID := createChatSessionForAgent(t, visibleAgentID, testUserID, "Visible chat")

	hiddenAgentID, _ := createOtherUserPrivateAgent(t, "Hidden Private Chat Agent")
	hiddenSessionID := createChatSessionForAgent(t, hiddenAgentID, testUserID, "Hidden chat")

	assertVisibleSessions := func(path string) {
		t.Helper()

		req := newRequest("GET", path, nil)
		req = withWorkspaceContext(req, testWorkspaceID)

		rr := httptest.NewRecorder()
		testHandler.ListChatSessions(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var sessions []ChatSessionResponse
		if err := json.NewDecoder(rr.Body).Decode(&sessions); err != nil {
			t.Fatalf("failed to decode sessions: %v", err)
		}

		foundVisible := false
		for _, session := range sessions {
			if session.ID == hiddenSessionID {
				t.Fatalf("hidden private session should not be listed for %s", path)
			}
			if session.ID == visibleSessionID {
				foundVisible = true
			}
		}
		if !foundVisible {
			t.Fatalf("visible private session missing for %s", path)
		}
	}

	assertVisibleSessions("/api/chat/sessions?workspace_id=" + testWorkspaceID)
	assertVisibleSessions("/api/chat/sessions?workspace_id=" + testWorkspaceID + "&status=all")
}

func TestSendChatMessage_Error_PrivateSessionOwnedByOtherUser(t *testing.T) {
	t.Parallel()

	hiddenAgentID, _ := createOtherUserPrivateAgent(t, "Hidden Session Agent")
	sessionID := createChatSessionForAgent(t, hiddenAgentID, testUserID, "Old hidden session")

	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages", map[string]any{
		"content": "should fail",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withWorkspaceContext(req, testWorkspaceID)

	rr := httptest.NewRecorder()
	testHandler.SendChatMessage(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "private agent belongs to another user" {
		t.Fatalf("expected private-agent error, got %q", resp["error"])
	}
}

// TestDeleteChatMessage_Success_DeleteUserMessage tests successful deletion of a user message by the creator.
func TestDeleteChatMessage_Success_DeleteUserMessage(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+messageID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify message is deleted
	ctx := context.Background()
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_message WHERE id = $1`, util.MustParseUUID(messageID)).Scan(&count); err != nil {
		t.Fatalf("failed to query message: %v", err)
	}
	if count != 0 {
		t.Fatalf("message still exists after deletion, count: %d", count)
	}
}

// TestDeleteChatMessage_Success_DeleteAssistantMessage_AsOwner tests successful deletion of an assistant message by the agent owner.
func TestDeleteChatMessage_Success_DeleteAssistantMessage_AsOwner(t *testing.T) {
	t.Parallel()

	sessionID, _, assistantMessageID := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+assistantMessageID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", assistantMessageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify message is deleted
	ctx := context.Background()
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_message WHERE id = $1`, util.MustParseUUID(assistantMessageID)).Scan(&count); err != nil {
		t.Fatalf("failed to query message: %v", err)
	}
	if count != 0 {
		t.Fatalf("assistant message still exists after deletion, count: %d", count)
	}
}

// TestDeleteChatMessage_Error_SessionNotFound tests deletion when session does not exist.
func TestDeleteChatMessage_Error_SessionNotFound(t *testing.T) {
	t.Parallel()

	fakeSessionID := "00000000-0000-0000-0000-000000000099"
	fakeMessageID := "00000000-0000-0000-0000-000000000098"
	req := newRequest("DELETE", "/api/chat/sessions/"+fakeSessionID+"/messages/"+fakeMessageID, nil)
	req = withURLParam(req, "sessionId", fakeSessionID)
	req = withURLParam(req, "messageId", fakeMessageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "chat session not found" {
		t.Fatalf("expected 'chat session not found', got '%s'", resp["error"])
	}
}

// TestDeleteChatMessage_Error_MessageNotFound tests deletion when message does not exist.
func TestDeleteChatMessage_Error_MessageNotFound(t *testing.T) {
	t.Parallel()

	sessionID, _, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	fakeMessageID := "00000000-0000-0000-0000-000000000097"
	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+fakeMessageID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", fakeMessageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "message not found" {
		t.Fatalf("expected 'message not found', got '%s'", resp["error"])
	}
}

// TestDeleteChatMessage_Error_MessageFromDifferentSession tests deletion when message belongs to a different session.
func TestDeleteChatMessage_Error_MessageFromDifferentSession(t *testing.T) {
	t.Parallel()

	sessionID1, messageID1, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID1)

	sessionID2, _, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID2)

	// Try to delete message from session1 using session2's path
	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID2+"/messages/"+messageID1, nil)
	req = withURLParam(req, "sessionId", sessionID2)
	req = withURLParam(req, "messageId", messageID1)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteChatMessage_Error_NotAuthenticated tests deletion without authentication.
func TestDeleteChatMessage_Error_NotAuthenticated(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	req := httptest.NewRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+messageID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)
	// Don't set X-User-ID header

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRetryChatMessage_Success tests successful retry of a message.
func TestRetryChatMessage_Success(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages/"+messageID+"/retry", nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.RetryChatMessage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp SendChatMessageResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.MessageID == "" {
		t.Fatal("expected non-empty message_id")
	}
	if resp.TaskID == "" {
		t.Fatal("expected non-empty task_id")
	}

	// Verify new message was created with original content
	ctx := context.Background()
	var content string
	if err := testPool.QueryRow(ctx, `SELECT content FROM chat_message WHERE id = $1`, util.MustParseUUID(resp.MessageID)).Scan(&content); err != nil {
		t.Fatalf("failed to query new message: %v", err)
	}
	if content != "Test user message" {
		t.Fatalf("expected content 'Test user message', got '%s'", content)
	}

	// Verify original message still exists
	var originalCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_message WHERE id = $1`, util.MustParseUUID(messageID)).Scan(&originalCount); err != nil {
		t.Fatalf("failed to query original message: %v", err)
	}
	if originalCount != 1 {
		t.Fatalf("original message should still exist, count: %d", originalCount)
	}
}

// TestRetryChatMessage_Error_SessionNotFound tests retry when session does not exist.
func TestRetryChatMessage_Error_SessionNotFound(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	fakeSessionID := "00000000-0000-0000-0000-000000000096"
	req := newRequest("POST", "/api/chat/sessions/"+fakeSessionID+"/messages/"+messageID+"/retry", nil)
	req = withURLParam(req, "sessionId", fakeSessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.RetryChatMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "chat session not found" {
		t.Fatalf("expected 'chat session not found', got '%s'", resp["error"])
	}
}

// TestRetryChatMessage_Error_MessageNotFound tests retry when message does not exist.
func TestRetryChatMessage_Error_MessageNotFound(t *testing.T) {
	t.Parallel()

	sessionID, _, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	fakeMessageID := "00000000-0000-0000-0000-000000000095"
	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages/"+fakeMessageID+"/retry", nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", fakeMessageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.RetryChatMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "message not found" {
		t.Fatalf("expected 'message not found', got '%s'", resp["error"])
	}
}

// TestRetryChatMessage_Error_MessageFromDifferentSession tests retry when message belongs to a different session.
func TestRetryChatMessage_Error_MessageFromDifferentSession(t *testing.T) {
	t.Parallel()

	sessionID1, messageID1, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID1)

	sessionID2, _, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID2)

	req := newRequest("POST", "/api/chat/sessions/"+sessionID2+"/messages/"+messageID1+"/retry", nil)
	req = withURLParam(req, "sessionId", sessionID2)
	req = withURLParam(req, "messageId", messageID1)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.RetryChatMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRetryChatMessage_Error_SessionArchived tests retry when session is archived.
func TestRetryChatMessage_Error_SessionArchived(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	// Archive the session
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE chat_session SET status = 'archived' WHERE id = $1`, util.MustParseUUID(sessionID)); err != nil {
		t.Fatalf("failed to archive session: %v", err)
	}

	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages/"+messageID+"/retry", nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.RetryChatMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "chat session is archived" {
		t.Fatalf("expected 'chat session is archived', got '%s'", resp["error"])
	}
}

// TestRetryChatMessage_Error_NotAuthenticated tests retry without authentication.
func TestRetryChatMessage_Error_NotAuthenticated(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	req := httptest.NewRequest("POST", "/api/chat/sessions/"+sessionID+"/messages/"+messageID+"/retry", nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)
	// Don't set X-User-ID header

	w := httptest.NewRecorder()
	testHandler.RetryChatMessage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteChatMessage_Error_InvalidRole tests deletion when message has invalid role.
func TestDeleteChatMessage_Error_InvalidRole(t *testing.T) {
	t.Parallel()

	sessionID, messageID, _ := setupChatTestFixture(t)
	defer cleanupChatTestFixture(t, sessionID)

	handlerWithInvalidRole := *testHandler
	handlerWithInvalidRole.Queries = db.New(queryRowOverrideDB{
		base: testPool,
		matchSQL: func(sql string) bool {
			return strings.Contains(sql, "SELECT id, chat_session_id, role, content, task_id, created_at, failure_reason, elapsed_ms FROM chat_message")
		},
		queryRow: func(ctx context.Context, _ string, args ...any) pgx.Row {
			return testPool.QueryRow(ctx, `
				SELECT id, chat_session_id, 'invalid_role' AS role, content, task_id, created_at, failure_reason, elapsed_ms
				FROM chat_message
				WHERE id = $1
			`, args...)
		},
	})

	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+messageID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	handlerWithInvalidRole.DeleteChatMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid message role" {
		t.Fatalf("expected 'invalid message role', got '%s'", resp["error"])
	}
}

// TestDeleteChatMessage_Error_UserNotCreator tests deletion when user is not the creator.
func TestDeleteChatMessage_Error_UserNotCreator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create another user
	var otherUserID string
	otherEmail := fmt.Sprintf("other-%s@multica.ai", t.Name())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Other User', $1)
		RETURNING id
	`, otherEmail).Scan(&otherUserID); err != nil {
		t.Fatalf("failed to create other user: %v", err)
	}
	otherUserIDStr := util.UUIDToString(util.MustParseUUID(otherUserID))

	// Add other user to workspace
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, util.MustParseUUID(testWorkspaceID), util.MustParseUUID(otherUserIDStr)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	// Create chat session with testUserID
	var sid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		SELECT workspace_id, id, $1, 'Test Chat Session', 'active'
		FROM agent
		WHERE workspace_id = $2
		LIMIT 1
		RETURNING id
	`, testUserID, testWorkspaceID).Scan(&sid); err != nil {
		t.Fatalf("failed to create chat session: %v", err)
	}
	sessionID := util.UUIDToString(util.MustParseUUID(sid))

	// Create user message
	var mid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'Test message')
		RETURNING id
	`, sid).Scan(&mid); err != nil {
		t.Fatalf("failed to create message: %v", err)
	}
	messageID := util.UUIDToString(util.MustParseUUID(mid))

	defer func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, util.MustParseUUID(otherUserIDStr))
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.MustParseUUID(sessionID))
	}()

	// Try to delete with other user (not the creator)
	req := httptest.NewRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+messageID, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", otherUserIDStr)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "not your chat session" {
		t.Fatalf("expected 'not your chat session', got '%s'", resp["error"])
	}
}
