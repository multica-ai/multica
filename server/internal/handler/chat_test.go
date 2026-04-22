package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// withWorkspaceContext sets the workspace ID in the request context.
func withWorkspaceContext(req *http.Request, workspaceID string) *http.Request {
	member := db.Member{
		UserID:      util.ParseUUID(testUserID),
		WorkspaceID: util.ParseUUID(workspaceID),
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
		SELECT id, id, $1, 'Test Chat Session', 'active'
		FROM agent
		WHERE workspace_id = $2
		LIMIT 1
		RETURNING id
	`, testUserID, testWorkspaceID).Scan(&sid); err != nil {
		t.Fatalf("failed to create chat session: %v", err)
	}
	sessionID = util.UUIDToString(util.ParseUUID(sid))

	// Create user message
	var umid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'Test user message')
		RETURNING id
	`, sid).Scan(&umid); err != nil {
		t.Fatalf("failed to create user message: %v", err)
	}
	userMessageID = util.UUIDToString(util.ParseUUID(umid))

	// Create assistant message
	var amid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'assistant', 'Test assistant response')
		RETURNING id
	`, sid).Scan(&amid); err != nil {
		t.Fatalf("failed to create assistant message: %v", err)
	}
	assistantMessageID = util.UUIDToString(util.ParseUUID(amid))

	return
}

// cleanupChatTestFixture removes the test chat session.
func cleanupChatTestFixture(t *testing.T, sessionID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.ParseUUID(sessionID)); err != nil {
		t.Logf("warning: failed to cleanup chat session: %v", err)
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
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_message WHERE id = $1`, util.ParseUUID(messageID)).Scan(&count); err != nil {
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
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_message WHERE id = $1`, util.ParseUUID(assistantMessageID)).Scan(&count); err != nil {
		t.Fatalf("failed to query message: %v", err)
	}
	if count != 0 {
		t.Fatalf("assistant message still exists after deletion, count: %d", count)
	}
}

// TestDeleteChatMessage_Error_SessionNotFound tests deletion when session does not exist.
func TestDeleteChatMessage_Error_SessionNotFound(t *testing.T) {
	t.Parallel()

	req := newRequest("DELETE", "/api/chat/sessions/nonexistent/messages/xyz", nil)
	req = withURLParam(req, "sessionId", "nonexistent")
	req = withURLParam(req, "messageId", "xyz")
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

	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/nonexistent", nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", "nonexistent")
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
	if err := testPool.QueryRow(ctx, `SELECT content FROM chat_message WHERE id = $1`, util.ParseUUID(resp.MessageID)).Scan(&content); err != nil {
		t.Fatalf("failed to query new message: %v", err)
	}
	if content != "Test user message" {
		t.Fatalf("expected content 'Test user message', got '%s'", content)
	}

	// Verify original message still exists
	var originalCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_message WHERE id = $1`, util.ParseUUID(messageID)).Scan(&originalCount); err != nil {
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

	req := newRequest("POST", "/api/chat/sessions/nonexistent/messages/"+messageID+"/retry", nil)
	req = withURLParam(req, "sessionId", "nonexistent")
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

	req := newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages/nonexistent/retry", nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", "nonexistent")
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
	if _, err := testPool.Exec(ctx, `UPDATE chat_session SET status = 'archived' WHERE id = $1`, util.ParseUUID(sessionID)); err != nil {
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

	ctx := context.Background()

	// Create chat session
	var sid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		SELECT id, id, $1, 'Test Chat Session', 'active'
		FROM agent
		WHERE workspace_id = $2
		LIMIT 1
		RETURNING id
	`, testUserID, testWorkspaceID).Scan(&sid); err != nil {
		t.Fatalf("failed to create chat session: %v", err)
	}
	sessionID := util.UUIDToString(util.ParseUUID(sid))

	// Create message with invalid role
	var mid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'invalid_role', 'Test message')
		RETURNING id
	`, sid).Scan(&mid); err != nil {
		t.Fatalf("failed to create message: %v", err)
	}
	messageID := util.UUIDToString(util.ParseUUID(mid))

	defer func() {
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.ParseUUID(sessionID))
	}()

	req := newRequest("DELETE", "/api/chat/sessions/"+sessionID+"/messages/"+messageID, nil)
	req = withURLParam(req, "sessionId", sessionID)
	req = withURLParam(req, "messageId", messageID)
	req = withWorkspaceContext(req, testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.DeleteChatMessage(w, req)

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
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Other User', 'other-test@multica.ai')
		RETURNING id
	`).Scan(&otherUserID); err != nil {
		t.Fatalf("failed to create other user: %v", err)
	}
	otherUserIDStr := util.UUIDToString(util.ParseUUID(otherUserID))

	// Add other user to workspace
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, util.ParseUUID(testWorkspaceID), util.ParseUUID(otherUserIDStr)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	// Create chat session with testUserID
	var sid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		SELECT id, id, $1, 'Test Chat Session', 'active'
		FROM agent
		WHERE workspace_id = $2
		LIMIT 1
		RETURNING id
	`, testUserID, testWorkspaceID).Scan(&sid); err != nil {
		t.Fatalf("failed to create chat session: %v", err)
	}
	sessionID := util.UUIDToString(util.ParseUUID(sid))

	// Create user message
	var mid string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'Test message')
		RETURNING id
	`, sid).Scan(&mid); err != nil {
		t.Fatalf("failed to create message: %v", err)
	}
	messageID := util.UUIDToString(util.ParseUUID(mid))

	defer func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, util.ParseUUID(otherUserIDStr))
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, util.ParseUUID(sessionID))
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
	if resp["error"] != "not your message" {
		t.Fatalf("expected 'not your message', got '%s'", resp["error"])
	}
}
