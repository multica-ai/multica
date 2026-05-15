package handler

// CEREBRO-PATCH(chat-handler-chat-test): cerebro modification of upstream file

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newChatRequest builds a request with X-User-ID set to userID and the
// workspace context injected via middleware.SetMemberContext — required
// because chat handlers read workspaceID from the context (set by workspace
// middleware in production), not from headers.
func newChatRequest(method, path, userID string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	// Look up the member row for this user/workspace and inject it. For the
	// "user has no membership" case (which our tests don't exercise), callers
	// should bypass this helper and inject only the workspace ID.
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(req.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		// Fallback: only set workspace ID. The handler will skip member-bound code paths.
		ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{})
		return req.WithContext(ctx)
	}
	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, member)
	return req.WithContext(ctx)
}

// createWorkspaceMemberUser inserts a fresh user and adds them to the test
// workspace with the given role. Returns the user ID; cleanup is registered
// against t. Email is unique per test to avoid collisions when tests run in
// parallel or are re-invoked.
func createWorkspaceMemberUser(t *testing.T, role string) string {
	t.Helper()

	email := "chat-vis-" + t.Name() + "-" + role + "@multica.ai"
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Chat Visibility "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
			testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

// cleanupChatSession removes any chat sessions / messages tied to the given
// agent so successful-create cases don't leak state across runs.
func cleanupChatSessionsForAgent(t *testing.T, agentID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `
			DELETE FROM chat_message
			WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id = $1)
		`, agentID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE agent_id = $1`, agentID)
	})
}

// TestCreateChatSession_PrivateAgentBlockedForMember verifies the security
// fix for JEH-323: a regular workspace member cannot open a chat with a
// private agent. Without this gate, any member could start a chat with a
// privileged private agent and exfiltrate data via the agent's responses
// (only the chat creator sees the messages, so it's a perfect side-channel).
func TestCreateChatSession_PrivateAgentBlockedForMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Private agent owned by the workspace owner (testUserID).
	agentID := createHandlerTestAgent(t, "Chat Visibility Private Agent", []byte(`null`))

	// A plain workspace member — neither owner, admin, nor agent owner.
	memberID := createWorkspaceMemberUser(t, "member")

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodPost, "/api/chat-sessions", memberID, map[string]any{
		"agent_id": agentID,
		"title":    "should be blocked",
	})
	testHandler.CreateChatSession(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for member chatting with private agent, got %d: %s", w.Code, w.Body.String())
	}

	// Defense-in-depth: also verify no chat_session row was created.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_session WHERE agent_id = $1`, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count chat sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no chat_session rows on 403, got %d", count)
	}
}

// TestCreateChatSession_PrivateAgentAllowedForOwner verifies that the
// workspace owner can still start chats with private agents — the gate must
// not lock out admins/owners. Uses testUserID which is the workspace owner
// from the handler test fixture.
func TestCreateChatSession_PrivateAgentAllowedForOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Private agent — owner_id is testUserID by virtue of createHandlerTestAgent.
	// We re-set owner_id below to a different user so this test exercises the
	// "workspace owner role grants access" branch rather than the "agent owner"
	// branch (covered by the next test).
	agentID := createHandlerTestAgent(t, "Chat Visibility Owner Test Agent", []byte(`null`))

	// Reassign the agent to a different (non-owner) user so testUserID's
	// access is granted via workspace owner role, not agent ownership.
	otherOwner := createWorkspaceMemberUser(t, "member")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET owner_id = $1 WHERE id = $2`, otherOwner, agentID,
	); err != nil {
		t.Fatalf("reassign agent owner: %v", err)
	}
	// Revert ownership before cleanup runs; otherwise the user-delete cleanup
	// hits the agent.owner_id FK before the agent-delete cleanup fires (LIFO).
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE agent SET owner_id = $1 WHERE id = $2`, testUserID, agentID)
	})

	cleanupChatSessionsForAgent(t, agentID)

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodPost, "/api/chat-sessions", testUserID, map[string]any{
		"agent_id": agentID,
		"title":    "owner chat",
	})
	testHandler.CreateChatSession(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for workspace owner chatting with private agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateChatSession_PrivateAgentAllowedForAgentOwner verifies that an
// agent's owner can chat with their own private agent even when they don't
// have admin/owner role on the workspace.
func TestCreateChatSession_PrivateAgentAllowedForAgentOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Plain member of the workspace.
	agentOwnerID := createWorkspaceMemberUser(t, "member")

	// Private agent owned by the member (not by the workspace owner).
	agentID := createHandlerTestAgent(t, "Chat Visibility Agent-Owner Test", []byte(`null`))
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET owner_id = $1 WHERE id = $2`, agentOwnerID, agentID,
	); err != nil {
		t.Fatalf("reassign agent owner: %v", err)
	}
	// Revert ownership before cleanup runs; otherwise the user-delete cleanup
	// hits the agent.owner_id FK before the agent-delete cleanup fires (LIFO).
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE agent SET owner_id = $1 WHERE id = $2`, testUserID, agentID)
	})

	cleanupChatSessionsForAgent(t, agentID)

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodPost, "/api/chat-sessions", agentOwnerID, map[string]any{
		"agent_id": agentID,
		"title":    "agent owner chat",
	})
	testHandler.CreateChatSession(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for agent owner chatting with own private agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateChatSession_NonPrivateAgentNotAffected verifies the visibility
// gate is scoped to private agents only — workspace-visibility agents remain
// open to any member, which is the existing pre-fix behavior.
func TestCreateChatSession_NonPrivateAgentNotAffected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	memberID := createWorkspaceMemberUser(t, "member")

	// The fixture's "Handler Test Agent" has visibility='workspace'.
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find fixture agent: %v", err)
	}

	cleanupChatSessionsForAgent(t, agentID)

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodPost, "/api/chat-sessions", memberID, map[string]any{
		"agent_id": agentID,
		"title":    "workspace agent chat",
	})
	testHandler.CreateChatSession(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for member chatting with workspace-visibility agent, got %d: %s", w.Code, w.Body.String())
	}
}

// withChatTestWorkspaceCtx injects the workspace+member context that the
// real chi middleware chain would normally set. SendChatMessage (and most
// other chat handlers) read workspace ID from ctxWorkspaceID; without this
// the test harness, which calls handlers directly, gets "invalid workspace
// id" on the parseUUIDOrBadRequest call inside SendChatMessage.
func withChatTestWorkspaceCtx(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(testUserID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load test member row: %v", err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

// TestSendChatMessage_LinksAttachments verifies that attachments uploaded
// against a chat_session (chat_message_id NULL) are back-filled with the
// message_id when SendChatMessage receives the matching attachment_ids.
func TestSendChatMessage_LinksAttachments(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	agentID := createHandlerTestAgent(t, "ChatSendAttachAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	// 1. Upload a file against the chat session.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "send-link.png")
	part.Write([]byte("\x89PNG\r\n\x1a\nbytes"))
	writer.WriteField("chat_session_id", sessionID)
	writer.Close()

	uploadReq := httptest.NewRequest("POST", "/api/upload-file", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-ID", testUserID)
	uploadReq.Header.Set("X-Workspace-ID", testWorkspaceID)

	uploadW := httptest.NewRecorder()
	testHandler.UploadFile(uploadW, uploadReq)
	if uploadW.Code != http.StatusOK {
		t.Fatalf("upload precondition: %d %s", uploadW.Code, uploadW.Body.String())
	}
	var uploadResp AttachmentResponse
	if err := json.Unmarshal(uploadW.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	attachmentID := uploadResp.ID
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})

	// 2. Send a chat message that references the attachment.
	sendReq := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content":        "look at this ![](" + uploadResp.URL + ")",
		"attachment_ids": []string{attachmentID},
	})
	sendReq = withURLParam(sendReq, "sessionId", sessionID)
	sendReq = withChatTestWorkspaceCtx(t, sendReq)
	sendW := httptest.NewRecorder()
	testHandler.SendChatMessage(sendW, sendReq)
	if sendW.Code != http.StatusCreated {
		t.Fatalf("SendChatMessage: expected 201, got %d: %s", sendW.Code, sendW.Body.String())
	}

	var sendResp SendChatMessageResponse
	if err := json.Unmarshal(sendW.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("decode send: %v", err)
	}
	if sendResp.MessageID == "" {
		t.Fatal("expected non-empty message_id in send response")
	}

	// 3. Verify the attachment row now points at the new message.
	var dbMessageID *string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`,
		attachmentID,
	).Scan(&dbMessageID); err != nil {
		t.Fatalf("query attachment: %v", err)
	}
	if dbMessageID == nil {
		t.Fatal("chat_message_id is still NULL after send")
	}
	if *dbMessageID != sendResp.MessageID {
		t.Fatalf("chat_message_id mismatch: want %s, got %s", sendResp.MessageID, *dbMessageID)
	}
}

// TestUpdateChatSession_RenamesTitle confirms PATCH writes the new title,
// returns the updated row, and the server-side row reflects it.
func TestUpdateChatSession_RenamesTitle(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatRenameAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	req := newRequest("PATCH", "/api/chat/sessions/"+sessionID, map[string]any{
		"title": "  Renamed Session  ",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.UpdateChatSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateChatSession: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if resp.Title != "Renamed Session" {
		t.Fatalf("response title: want %q, got %q", "Renamed Session", resp.Title)
	}

	var dbTitle string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT title FROM chat_session WHERE id = $1`,
		sessionID,
	).Scan(&dbTitle); err != nil {
		t.Fatalf("query chat_session: %v", err)
	}
	if dbTitle != "Renamed Session" {
		t.Fatalf("db title: want %q, got %q", "Renamed Session", dbTitle)
	}
}

// TestUpdateChatSession_RejectsBlank refuses an empty/whitespace title with 400.
// (Untitled is a render-side fallback, not a stored value.)
func TestUpdateChatSession_RejectsBlank(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatRenameBlankAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	req := newRequest("PATCH", "/api/chat/sessions/"+sessionID, map[string]any{
		"title": "   ",
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.UpdateChatSession(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateChatSession blank: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSendChatMessage_InvalidAttachmentIDs rejects malformed UUIDs in
// attachment_ids with 400 before any side effects (no message row created).
func TestSendChatMessage_InvalidAttachmentIDs(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatBadAttachAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	req := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content":        "hi",
		"attachment_ids": []string{"not-a-uuid"},
	})
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("SendChatMessage with bad attachment id: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm no message row was created.
	var count int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM chat_message WHERE chat_session_id = $1`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count chat_message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 chat_message rows after rejected send, got %d", count)
	}
}
