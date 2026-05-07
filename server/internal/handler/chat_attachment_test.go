package handler

// CEREBRO-PATCH(chat-handler-chat-attachment-test): cerebro modification of upstream file

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTestChatMessage seeds a chat session and a single user message in the
// test database and returns (sessionID, messageID, agentID). The caller is
// responsible for cleanup; deleting the session cascades to messages and
// attachments.
func createTestChatMessage(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, "chat-attachment-test").Scan(&sessionID); err != nil {
		t.Fatalf("insert chat_session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	var messageID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'assistant', 'here is your file')
		RETURNING id
	`, sessionID).Scan(&messageID); err != nil {
		t.Fatalf("insert chat_message: %v", err)
	}

	return sessionID, messageID, agentID
}

func uploadChatAttachment(t *testing.T, messageID, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte(content))
	if err := writer.WriteField("chat_message_id", messageID); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	return w
}

// TestUploadFileChatMessage verifies the happy path: the chat session creator
// can upload a file linked to a chat message, the attachment row is persisted
// with chat_message_id set, and the response carries chat_message_id back to
// the client.
func TestUploadFileChatMessage(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	_, messageID, _ := createTestChatMessage(t)

	w := uploadChatAttachment(t, messageID, "report.html", "<h1>hello</h1>")
	if w.Code != http.StatusOK {
		t.Fatalf("upload chat attachment: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, w.Body.String())
	}
	gotMsgID, _ := resp["chat_message_id"].(string)
	if gotMsgID != messageID {
		t.Fatalf("response chat_message_id: want %s, got %v", messageID, resp["chat_message_id"])
	}
	attID, _ := resp["id"].(string)
	if attID == "" {
		t.Fatalf("response missing attachment id: %s", w.Body.String())
	}

	// DB row reflects the link.
	var dbMsgID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`, attID,
	).Scan(&dbMsgID); err != nil {
		t.Fatalf("select attachment: %v", err)
	}
	if dbMsgID != messageID {
		t.Fatalf("attachment chat_message_id row: want %s, got %s", messageID, dbMsgID)
	}
}

// TestUploadFileChatMessageNonOwner verifies that a workspace member who does
// not own the chat session cannot attach files to it. Chat is per-user, even
// inside a shared workspace.
func TestUploadFileChatMessageNonOwner(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	_, messageID, _ := createTestChatMessage(t)

	// Create a second user who is also a member of the workspace.
	ctx := context.Background()
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Other User', 'other-handler-test@multica.ai')
		RETURNING id
	`).Scan(&otherUserID); err != nil {
		t.Fatalf("insert other user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "leak.txt")
	part.Write([]byte("snoop"))
	writer.WriteField("chat_message_id", messageID)
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", otherUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner upload: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUploadFileInvalidChatMessage verifies that an unknown chat_message_id
// is rejected (no attachment row, no S3 write attempted past validation).
func TestUploadFileInvalidChatMessage(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	w := uploadChatAttachment(t, "00000000-0000-0000-0000-000000000099", "x.txt", "x")
	if w.Code != http.StatusForbidden {
		t.Fatalf("invalid chat_message_id: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestChatAttachmentDownloadFlow exercises the same code path the CLI hits
// when running `multica attachment download <id>` against a chat-message
// attachment: upload, then GetAttachmentByID returns a downloadable record.
// This is the integration assertion the task asked for.
func TestChatAttachmentDownloadFlow(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	_, messageID, _ := createTestChatMessage(t)

	upload := uploadChatAttachment(t, messageID, "findings.md", "# results")
	if upload.Code != http.StatusOK {
		t.Fatalf("upload: expected 200, got %d: %s", upload.Code, upload.Body.String())
	}
	var uploadResp map[string]any
	if err := json.Unmarshal(upload.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	attID, _ := uploadResp["id"].(string)
	if attID == "" {
		t.Fatalf("missing attachment id: %s", upload.Body.String())
	}

	// GET /api/attachments/{id} — what the CLI calls first.
	getReq := httptest.NewRequest("GET", "/api/attachments/"+attID, nil)
	getReq.Header.Set("X-User-ID", testUserID)
	getReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	getReq = withURLParam(getReq, "id", attID)
	getW := httptest.NewRecorder()
	testHandler.GetAttachmentByID(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("get attachment: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}

	var getResp map[string]any
	if err := json.Unmarshal(getW.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got, _ := getResp["chat_message_id"].(string); got != messageID {
		t.Fatalf("chat_message_id round-trip: want %s, got %v", messageID, getResp["chat_message_id"])
	}
	if dl, _ := getResp["download_url"].(string); dl == "" {
		t.Fatalf("response missing download_url: %s", getW.Body.String())
	}
	if fn, _ := getResp["filename"].(string); fn != "findings.md" {
		t.Fatalf("filename: want findings.md, got %v", getResp["filename"])
	}
}

// TestListChatMessageAttachments verifies the new list endpoint surfaces
// attachments uploaded against a message and gates by session ownership.
func TestListChatMessageAttachments(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	_, messageID, _ := createTestChatMessage(t)
	upload := uploadChatAttachment(t, messageID, "list-me.txt", "hi")
	if upload.Code != http.StatusOK {
		t.Fatalf("seed upload: %d: %s", upload.Code, upload.Body.String())
	}

	req := httptest.NewRequest("GET", "/api/chat/messages/"+messageID+"/attachments", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "messageId", messageID)
	w := httptest.NewRecorder()
	testHandler.ListChatMessageAttachments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("attachments: want 1, got %d", len(resp))
	}
	if got, _ := resp[0]["filename"].(string); got != "list-me.txt" {
		t.Fatalf("filename: want list-me.txt, got %v", resp[0]["filename"])
	}
}

