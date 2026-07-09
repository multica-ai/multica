package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if sendResp.TaskID == "" {
		t.Fatal("expected non-empty task_id in send response")
	}

	var messageTaskID string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT COALESCE(task_id::text, '') FROM chat_message WHERE id = $1`,
		sendResp.MessageID,
	).Scan(&messageTaskID); err != nil {
		t.Fatalf("query chat message task id: %v", err)
	}
	if messageTaskID != sendResp.TaskID {
		t.Fatalf("chat message task_id mismatch: want %s, got %s", sendResp.TaskID, messageTaskID)
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

// TestSendChatMessage_ArchivedAgent verifies that sending to a session whose
// agent was archived is rejected with 409 BEFORE any message is persisted.
// EnqueueChatTask rejects an archived agent, but only after CreateChatMessage;
// without the handler's preflight a stale client would leave an orphan user
// message with no task or reply.
func TestSendChatMessage_ArchivedAgent(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatArchivedAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	// Archive the agent out from under the (stale) client.
	if _, err := testPool.Exec(
		context.Background(),
		`UPDATE agent SET archived_at = now() WHERE id = $1`,
		agentID,
	); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	sendReq := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content": "still there?",
	})
	sendReq = withURLParam(sendReq, "sessionId", sessionID)
	sendReq = withChatTestWorkspaceCtx(t, sendReq)
	sendW := httptest.NewRecorder()
	testHandler.SendChatMessage(sendW, sendReq)

	if sendW.Code != http.StatusConflict {
		t.Fatalf("SendChatMessage to archived agent: expected 409, got %d: %s", sendW.Code, sendW.Body.String())
	}

	// The rejected send must not have persisted an orphan user message.
	var count int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM chat_message WHERE chat_session_id = $1`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count chat messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no chat_message rows after rejected send, got %d", count)
	}
}

// TestSendChatMessage_LinksUnattachedAttachments verifies the new compose
// path: upload creates a workspace-scoped unattached attachment, and chat send
// binds it to both the session and the user message.
func TestSendChatMessage_LinksUnattachedAttachments(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	agentID := createHandlerTestAgent(t, "ChatSendUnattachedAttachAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "send-unattached.png")
	part.Write([]byte("\x89PNG\r\n\x1a\nbytes"))
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
	if uploadResp.ChatSessionID != nil {
		t.Fatalf("pre-send chat_session_id should be nil, got %v", *uploadResp.ChatSessionID)
	}
	if uploadResp.ChatMessageID != nil {
		t.Fatalf("pre-send chat_message_id should be nil, got %v", *uploadResp.ChatMessageID)
	}

	sendReq := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content":        "look at this ![](" + uploadResp.MarkdownURL + ")",
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

	var dbSessionID, dbMessageID *string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT chat_session_id::text, chat_message_id::text FROM attachment WHERE id = $1`,
		attachmentID,
	).Scan(&dbSessionID, &dbMessageID); err != nil {
		t.Fatalf("query attachment: %v", err)
	}
	if dbSessionID == nil || *dbSessionID != sessionID {
		t.Fatalf("chat_session_id mismatch: want %s, got %v", sessionID, dbSessionID)
	}
	if dbMessageID == nil || *dbMessageID != sendResp.MessageID {
		t.Fatalf("chat_message_id mismatch: want %s, got %v", sendResp.MessageID, dbMessageID)
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

// TestSetChatSessionPinned_TogglesPin confirms PATCH /pin stamps pinned_at on
// pin and clears it on unpin, returns the new state, and does not bump
// updated_at (pinning is a list-ordering preference, not activity).
func TestSetChatSessionPinned_TogglesPin(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatPinAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var updatedBefore time.Time
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT updated_at FROM chat_session WHERE id = $1`,
		sessionID,
	).Scan(&updatedBefore); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}

	pin := func(pinned bool) ChatSessionResponse {
		req := newRequest("PATCH", "/api/chat/sessions/"+sessionID+"/pin", map[string]any{
			"pinned": pinned,
		})
		req = withURLParam(req, "sessionId", sessionID)
		req = withChatTestWorkspaceCtx(t, req)
		w := httptest.NewRecorder()
		testHandler.SetChatSessionPinned(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SetChatSessionPinned(%v): expected 200, got %d: %s", pinned, w.Code, w.Body.String())
		}
		var resp ChatSessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode pin: %v", err)
		}
		return resp
	}

	// Pin.
	if resp := pin(true); !resp.Pinned {
		t.Fatalf("pin=true response Pinned: want true, got false")
	}
	var pinnedAt *time.Time
	var updatedAfter time.Time
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT pinned_at, updated_at FROM chat_session WHERE id = $1`,
		sessionID,
	).Scan(&pinnedAt, &updatedAfter); err != nil {
		t.Fatalf("query pinned_at: %v", err)
	}
	if pinnedAt == nil {
		t.Fatalf("pinned_at: want non-null after pin, got null")
	}
	if !updatedAfter.Equal(updatedBefore) {
		t.Fatalf("updated_at must not change on pin: before %v, after %v", updatedBefore, updatedAfter)
	}

	// Unpin.
	if resp := pin(false); resp.Pinned {
		t.Fatalf("pin=false response Pinned: want false, got true")
	}
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT pinned_at FROM chat_session WHERE id = $1`,
		sessionID,
	).Scan(&pinnedAt); err != nil {
		t.Fatalf("query pinned_at after unpin: %v", err)
	}
	if pinnedAt != nil {
		t.Fatalf("pinned_at: want null after unpin, got %v", *pinnedAt)
	}
}

// TestSetChatSessionArchived_TogglesStatus archives then unarchives a session,
// asserting the response + DB status column flip and that updated_at is bumped
// (so the row re-sorts in whichever list it lands).
func TestSetChatSessionArchived_TogglesStatus(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatArchiveAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	archive := func(archived bool) ChatSessionResponse {
		req := newRequest("PATCH", "/api/chat/sessions/"+sessionID+"/archive", map[string]any{
			"archived": archived,
		})
		req = withURLParam(req, "sessionId", sessionID)
		req = withChatTestWorkspaceCtx(t, req)
		w := httptest.NewRecorder()
		testHandler.SetChatSessionArchived(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SetChatSessionArchived(%v): expected 200, got %d: %s", archived, w.Code, w.Body.String())
		}
		var resp ChatSessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode archive: %v", err)
		}
		return resp
	}

	dbStatus := func() string {
		var status string
		if err := testPool.QueryRow(
			context.Background(),
			`SELECT status FROM chat_session WHERE id = $1`,
			sessionID,
		).Scan(&status); err != nil {
			t.Fatalf("query status: %v", err)
		}
		return status
	}

	// Archive.
	if resp := archive(true); resp.Status != "archived" {
		t.Fatalf("archive=true response Status: want archived, got %q", resp.Status)
	}
	if got := dbStatus(); got != "archived" {
		t.Fatalf("db status after archive: want archived, got %q", got)
	}

	// Unarchive restores it to active.
	if resp := archive(false); resp.Status != "active" {
		t.Fatalf("archive=false response Status: want active, got %q", resp.Status)
	}
	if got := dbStatus(); got != "active" {
		t.Fatalf("db status after unarchive: want active, got %q", got)
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

func fetchChatMessagesPageForTest(t *testing.T, sessionID string, params url.Values) ChatMessagesPageResponse {
	t.Helper()
	target := "/api/chat/sessions/" + sessionID + "/messages/page"
	if encoded := params.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.ListChatMessagesPage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChatMessagesPage: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page ChatMessagesPageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page messages: %v", err)
	}
	return page
}

func TestListChatMessagesPage_UsesCursorWithoutChangingLegacyList(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatCursorPaginationAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	for i, content := range []string{"oldest", "middle", "newest"} {
		_, err := testPool.Exec(
			context.Background(),
			`INSERT INTO chat_message (chat_session_id, role, content, created_at)
			 VALUES ($1, 'user', $2, timestamp '2026-01-01 00:00:00' + ($3::int * interval '1 second'))`,
			sessionID,
			content,
			i,
		)
		if err != nil {
			t.Fatalf("insert chat message %d: %v", i, err)
		}
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/messages", nil)
	legacyReq.Header.Set("X-User-ID", testUserID)
	legacyReq = withURLParam(legacyReq, "sessionId", sessionID)
	legacyReq = withChatTestWorkspaceCtx(t, legacyReq)
	legacyW := httptest.NewRecorder()
	testHandler.ListChatMessages(legacyW, legacyReq)
	if legacyW.Code != http.StatusOK {
		t.Fatalf("ListChatMessages: expected 200, got %d: %s", legacyW.Code, legacyW.Body.String())
	}
	var legacy []ChatMessageResponse
	if err := json.Unmarshal(legacyW.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode legacy messages: %v", err)
	}
	if len(legacy) != 3 || legacy[0].Content != "oldest" || legacy[2].Content != "newest" {
		t.Fatalf("legacy messages = %#v", legacy)
	}

	latest := fetchChatMessagesPageForTest(t, sessionID, url.Values{"limit": {"2"}})
	if latest.Limit != 2 || !latest.HasMore || latest.NextCursor == nil {
		t.Fatalf("latest page metadata = %#v", latest)
	}
	if len(latest.Messages) != 2 || latest.Messages[0].Content != "middle" || latest.Messages[1].Content != "newest" {
		t.Fatalf("latest page messages = %#v", latest)
	}

	older := fetchChatMessagesPageForTest(t, sessionID, url.Values{
		"limit":             {"2"},
		"before_created_at": {latest.NextCursor.CreatedAt},
		"before_id":         {latest.NextCursor.ID},
	})
	if older.HasMore || older.NextCursor != nil {
		t.Fatalf("older page metadata = %#v", older)
	}
	if len(older.Messages) != 1 || older.Messages[0].Content != "oldest" {
		t.Fatalf("older page messages = %#v", older)
	}
}

func TestListChatMessagesPage_CursorTieBreaksSameTimestampWithoutDupesOrGaps(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatCursorTieBreakAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	contents := []string{"a", "b", "c", "d", "e"}
	for _, content := range contents {
		_, err := testPool.Exec(
			context.Background(),
			`INSERT INTO chat_message (chat_session_id, role, content, created_at)
			 VALUES ($1, 'user', $2, timestamp '2026-01-01 00:00:00')`,
			sessionID,
			content,
		)
		if err != nil {
			t.Fatalf("insert chat message %q: %v", content, err)
		}
	}

	seen := map[string]bool{}
	var ordered []string
	params := url.Values{"limit": {"2"}}
	for {
		page := fetchChatMessagesPageForTest(t, sessionID, params)
		for _, msg := range page.Messages {
			if seen[msg.ID] {
				t.Fatalf("duplicate message id %s across cursor pages", msg.ID)
			}
			seen[msg.ID] = true
			ordered = append(ordered, msg.Content)
		}
		if !page.HasMore {
			if page.NextCursor != nil {
				t.Fatalf("terminal page has next cursor: %#v", page.NextCursor)
			}
			break
		}
		if page.NextCursor == nil {
			t.Fatalf("has_more page missing next cursor: %#v", page)
		}
		params = url.Values{
			"limit":             {"2"},
			"before_created_at": {page.NextCursor.CreatedAt},
			"before_id":         {page.NextCursor.ID},
		}
	}

	if len(ordered) != len(contents) {
		t.Fatalf("expected %d messages across pages, got %d: %v", len(contents), len(ordered), ordered)
	}
	// Pages are newest-window first and chronological within each page. With all
	// timestamps equal, the id tie-break must still produce a deterministic,
	// gap-free traversal.
	for _, content := range contents {
		found := false
		for _, got := range ordered {
			if got == content {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing content %q across cursor pages: %v", content, ordered)
		}
	}
}

func TestListChatMessagesPage_RejectsInvalidLimit(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatPaginationBadLimitAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/messages/page?limit=0", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.ListChatMessagesPage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ListChatMessagesPage invalid limit: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteChatSession_PrunesChannelRows verifies the application-layer
// replacement for the channel_* chat_session-FK cascade (MUL-3515 §4): deleting a
// chat session prunes BOTH its channel_chat_session_binding and its
// channel_outbound_card_message rows in the same tx that deletes the session row.
// Both are keyed by chat_session_id with no FK and no reaper, so a miss leaves a
// permanent orphan (Elon's follow-up on #4810).
func TestDeleteChatSession_PrunesChannelRows(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChatDeleteBindingAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	ctx := context.Background()

	const appID = "cli_chat_delete_binding"
	const channelChatID = "oc_chat_delete_binding"
	const cardMsgID = "om_chat_delete_card"

	// channel_* rows have no FK to chat_session/workspace (MUL-3515 §4), so
	// they outlive the helper's chat_session cleanup; clear by deterministic
	// key before and after.
	cleanChannel := func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_chat_session_binding WHERE channel_chat_id = $1`, channelChatID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_outbound_card_message WHERE channel_card_message_id = $1`, cardMsgID)
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE channel_type = 'feishu' AND config->>'app_id' = $1`, appID)
	}
	cleanChannel()
	t.Cleanup(cleanChannel)

	var installID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4)
RETURNING id
`, testWorkspaceID, agentID, appID, testUserID).Scan(&installID); err != nil {
		t.Fatalf("insert channel_installation: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_chat_session_binding (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
VALUES ($1, $2, 'feishu', $3, 'p2p')
`, sessionID, installID, channelChatID); err != nil {
		t.Fatalf("insert channel_chat_session_binding: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_outbound_card_message (chat_session_id, channel_type, channel_chat_id, channel_card_message_id, status)
VALUES ($1, 'feishu', $2, $3, 'final')
`, sessionID, channelChatID, cardMsgID); err != nil {
		t.Fatalf("insert channel_outbound_card_message: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()
	testHandler.DeleteChatSession(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var bindingExists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_chat_session_binding WHERE channel_chat_id = $1)`, channelChatID).Scan(&bindingExists); err != nil {
		t.Fatalf("query chat session binding: %v", err)
	}
	if bindingExists {
		t.Fatal("deleted chat session's channel_chat_session_binding was not pruned")
	}

	var cardExists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_outbound_card_message WHERE channel_card_message_id = $1)`, cardMsgID).Scan(&cardExists); err != nil {
		t.Fatalf("query outbound card message: %v", err)
	}
	if cardExists {
		t.Fatal("deleted chat session's channel_outbound_card_message was not pruned (no reaper would ever collect it)")
	}
}
