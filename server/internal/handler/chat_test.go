package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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

// withMobileChatCtx is the equivalent of withChatTestWorkspaceCtx but for an
// arbitrary workspace ID — used by TestListChats_WrongWorkspace to inject a
// different workspace context than the one the seeded sessions belong to.
// Falls back to a zero db.Member when no membership row exists (e.g. the
// "other" workspace the user has no role in), since ListChats reads only the
// workspace ID from context and never inspects the member row.
func withMobileChatCtx(t *testing.T, req *http.Request, workspaceID string) *http.Request {
	t.Helper()
	memberRow, _ := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(testUserID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	return req.WithContext(middleware.SetMemberContext(req.Context(), workspaceID, memberRow))
}

// createListChatsTestSession seeds a chat_session row owned by testUserID with
// an explicit updated_at so ORDER BY updated_at DESC is deterministic across
// rows created in the same test (without the explicit value, two rows inserted
// in rapid succession can share a now() timestamp).
func createListChatsTestSession(t *testing.T, workspaceID, agentID, title string, updatedAt time.Time) string {
	t.Helper()
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'active', $5, $5)
		RETURNING id
	`, workspaceID, agentID, testUserID, title, updatedAt).Scan(&sessionID); err != nil {
		t.Fatalf("failed to create test chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	return sessionID
}

// seedChatMessage inserts a chat_message row for the given session, returning
// the message id. Used by ListChats tests to verify last_message enrichment.
func seedChatMessage(t *testing.T, sessionID, content string, createdAt time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, created_at)
		VALUES ($1, 'user', $2, $3)
	`, sessionID, content, createdAt); err != nil {
		t.Fatalf("failed to seed chat_message: %v", err)
	}
}

// TestListChats_Empty: no sessions for this user → empty array, total=0,
// has_more=false. Empty sessions slice rather than null is part of the
// contract — the mobile client relies on .length being defined.
func TestListChats_Empty(t *testing.T) {
	// Use a freshly created agent so no prior test's sessions leak in.
	_ = createHandlerTestAgent(t, "ListChatsEmptyAgent", []byte("[]"))

	req := httptest.NewRequest("GET", "/api/chats", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.ListChats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChats empty: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChatListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Fatalf("expected total=0, got %d", resp.Total)
	}
	if resp.HasMore {
		t.Fatal("expected has_more=false for empty result")
	}
	if len(resp.Sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(resp.Sessions))
	}
	// Marshalled JSON should serialize sessions as [], never null — the
	// mobile client iterates blindly and would crash on null.
	if !strings.Contains(w.Body.String(), `"sessions":[]`) {
		t.Fatalf("expected sessions:[] in body, got %s", w.Body.String())
	}
}

// TestListChats_PopulatedReturnsEnrichedRows confirms the mobile-specific
// fields are present: each row carries agent_name (from the joined agent) and
// last_message / last_message_at (from the latest chat_message). Ordering is
// updated_at DESC.
func TestListChats_PopulatedReturnsEnrichedRows(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ListChatsEnrichAgent", []byte("[]"))

	older := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Millisecond)
	newer := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Millisecond)
	olderSession := createListChatsTestSession(t, testWorkspaceID, agentID, "Older Session", older)
	newerSession := createListChatsTestSession(t, testWorkspaceID, agentID, "Newer Session", newer)
	// Seed a message on the newer session only — the older session should
	// still appear in the list, just with last_message=nil. This proves the
	// LEFT JOIN preserves message-less sessions.
	seedChatMessage(t, newerSession, "hello mobile", newer)

	req := httptest.NewRequest("GET", "/api/chats", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.ListChats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChats populated: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChatListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected total=2, got %d (sessions: %+v)", resp.Total, resp.Sessions)
	}
	if resp.HasMore {
		t.Fatal("expected has_more=false (one page covers both sessions)")
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}
	// updated_at DESC → newerSession first.
	if resp.Sessions[0].ID != newerSession {
		t.Fatalf("expected newerSession (%s) first, got %s", newerSession, resp.Sessions[0].ID)
	}
	if resp.Sessions[1].ID != olderSession {
		t.Fatalf("expected olderSession (%s) second, got %s", olderSession, resp.Sessions[1].ID)
	}
	// Enriched fields on the first row.
	if resp.Sessions[0].AgentName != "ListChatsEnrichAgent" {
		t.Fatalf("expected agent_name=ListChatsEnrichAgent, got %q", resp.Sessions[0].AgentName)
	}
	if resp.Sessions[0].LastMessage == nil || *resp.Sessions[0].LastMessage != "hello mobile" {
		t.Fatalf("expected last_message=\"hello mobile\", got %v", resp.Sessions[0].LastMessage)
	}
	if resp.Sessions[0].LastMessageAt == nil {
		t.Fatal("expected last_message_at to be populated")
	}
	// Older session has no messages → last_message / last_message_at omitted.
	if resp.Sessions[1].LastMessage != nil {
		t.Fatalf("expected last_message=nil for message-less session, got %q", *resp.Sessions[1].LastMessage)
	}
	if resp.Sessions[1].LastMessageAt != nil {
		t.Fatalf("expected last_message_at=nil for message-less session, got %q", *resp.Sessions[1].LastMessageAt)
	}
}

// TestListChats_ScopedToWorkspace confirms a request that resolves to a
// different workspace cannot see this user's sessions from testWorkspaceID.
// Mirrors the production guarantee that ?workspace_slug=A and ?workspace_slug=B
// each see only their own sessions.
func TestListChats_ScopedToWorkspace(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ListChatsScopeAgent", []byte("[]"))
	_ = createListChatsTestSession(t, testWorkspaceID, agentID, "Session In Main WS", time.Now().UTC())

	// Stand up a second workspace owned by the same user, then call ListChats
	// with that workspace's context. The seeded session is in testWorkspaceID,
	// so the response should be empty.
	var otherWorkspaceID string
	otherSlug := "list-chats-other-" + fmt.Sprint(time.Now().UnixNano())
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Other WS', $1, 'Foreign workspace for scoping test', 'OTH')
		RETURNING id
	`, otherSlug).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	req := httptest.NewRequest("GET", "/api/chats", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withMobileChatCtx(t, req, otherWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListChats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChats scope: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChatListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Fatalf("expected total=0 from foreign workspace, got %d", resp.Total)
	}
	if len(resp.Sessions) != 0 {
		t.Fatalf("expected 0 sessions from foreign workspace, got %d", len(resp.Sessions))
	}
}

// TestListChats_Pagination: 3 sessions, limit=2 offset=0 → page 1 with
// has_more=true; offset=2 → page 2 with has_more=false. total stays at 3
// across both pages.
func TestListChats_Pagination(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ListChatsPageAgent", []byte("[]"))

	now := time.Now().UTC().Truncate(time.Millisecond)
	s1 := createListChatsTestSession(t, testWorkspaceID, agentID, "Page Session 1", now.Add(-3*time.Minute))
	s2 := createListChatsTestSession(t, testWorkspaceID, agentID, "Page Session 2", now.Add(-2*time.Minute))
	s3 := createListChatsTestSession(t, testWorkspaceID, agentID, "Page Session 3", now.Add(-1*time.Minute))

	// Page 1: limit=2 offset=0 → s3, s2 (DESC); has_more=true.
	req := httptest.NewRequest("GET", "/api/chats?limit=2&offset=0", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.ListChats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page1 ChatListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("decode page 1: %v", err)
	}
	if page1.Total != 3 {
		t.Fatalf("page 1 total: expected 3, got %d", page1.Total)
	}
	if !page1.HasMore {
		t.Fatal("page 1 has_more: expected true")
	}
	if len(page1.Sessions) != 2 {
		t.Fatalf("page 1 len: expected 2, got %d", len(page1.Sessions))
	}
	if page1.Sessions[0].ID != s3 || page1.Sessions[1].ID != s2 {
		t.Fatalf("page 1 order: expected [%s, %s], got [%s, %s]",
			s3, s2, page1.Sessions[0].ID, page1.Sessions[1].ID)
	}

	// Page 2: limit=2 offset=2 → s1 (only one row left); has_more=false.
	req = httptest.NewRequest("GET", "/api/chats?limit=2&offset=2", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withChatTestWorkspaceCtx(t, req)
	w = httptest.NewRecorder()
	testHandler.ListChats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page2 ChatListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if page2.Total != 3 {
		t.Fatalf("page 2 total: expected 3, got %d", page2.Total)
	}
	if page2.HasMore {
		t.Fatal("page 2 has_more: expected false")
	}
	if len(page2.Sessions) != 1 {
		t.Fatalf("page 2 len: expected 1, got %d", len(page2.Sessions))
	}
	if page2.Sessions[0].ID != s1 {
		t.Fatalf("page 2 id: expected %s, got %s", s1, page2.Sessions[0].ID)
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
