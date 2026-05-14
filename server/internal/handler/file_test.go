package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createHandlerTestChatSession seeds a chat_session row owned by testUserID
// targeting the given agent and returns the session UUID. Cleanup runs after
// the test. Used by attachment / chat tests that need an existing session.
func createHandlerTestChatSession(t *testing.T, agentID string) string {
	t.Helper()

	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, "Handler Test Chat Session").Scan(&sessionID); err != nil {
		t.Fatalf("failed to create handler test chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})
	return sessionID
}

type mockStorage struct{}

func (m *mockStorage) Upload(_ context.Context, key string, _ []byte, _ string, _ string) (string, error) {
	return fmt.Sprintf("https://cdn.example.com/%s", key), nil
}

func (m *mockStorage) Delete(_ context.Context, _ string)        {}
func (m *mockStorage) DeleteKeys(_ context.Context, _ []string)  {}
func (m *mockStorage) KeyFromURL(rawURL string) string            { return rawURL }
func (m *mockStorage) CdnDomain() string                         { return "cdn.example.com" }

func TestUploadFileForeignWorkspace(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("hello world"))
	writer.Close()

	foreignWorkspaceID := "00000000-0000-0000-0000-000000000099"
	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", foreignWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UploadFile with foreign workspace: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUploadFileResolvesWorkspaceViaSlugHeader is a regression test for the
// v2 workspace URL refactor (#1141). The frontend switched from sending
// X-Workspace-ID (UUID) to X-Workspace-Slug. For endpoints that sit outside
// the workspace middleware — like /api/upload-file — the handler-side
// resolver must accept the slug and translate it to a UUID, otherwise the
// handler silently falls through to the "no workspace context" branch and
// skips creating the DB attachment record. Files end up in S3 with no row
// in the attachment table, invisible to the UI.
func TestUploadFileResolvesWorkspaceViaSlugHeader(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "slug-upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("hello via slug"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	// Intentionally NOT setting X-Workspace-ID — post-v2 clients only send slug.
	req.Header.Set("X-Workspace-Slug", handlerTestWorkspaceSlug)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UploadFile with slug header: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The workspace-aware branch returns the full AttachmentResponse (with
	// id, workspace_id, uploader, etc.). The no-workspace-context branch
	// returns only {filename, link}. Distinguish by checking the shape.
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, w.Body.String())
	}
	if _, ok := resp["id"]; !ok {
		t.Fatalf("expected attachment response with 'id' field (DB row created); got fallback link-only response: %s", w.Body.String())
	}
	if gotWs, _ := resp["workspace_id"].(string); gotWs != testWorkspaceID {
		t.Fatalf("attachment workspace_id mismatch: want %s, got %v", testWorkspaceID, resp["workspace_id"])
	}

	// Verify the row actually exists in the database.
	var count int
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM attachment WHERE workspace_id = $1 AND filename = $2`,
		testWorkspaceID,
		"slug-upload.txt",
	).Scan(&count); err != nil {
		t.Fatalf("query attachment count: %v", err)
	}
	if count != 1 {
		t.Fatalf("attachment row count: want 1, got %d", count)
	}

	// Clean up so reruns don't accumulate rows.
	if _, err := testPool.Exec(
		context.Background(),
		`DELETE FROM attachment WHERE workspace_id = $1 AND filename = $2`,
		testWorkspaceID,
		"slug-upload.txt",
	); err != nil {
		t.Fatalf("cleanup attachment: %v", err)
	}
}

// TestUploadFileResolvesWorkspaceViaIDHeaderStill confirms the legacy path
// (CLI / daemon clients sending X-Workspace-ID as a UUID) still works after
// the refactor. Prevents a regression in the CLI/daemon compat branch.
func TestUploadFileResolvesWorkspaceViaIDHeaderStill(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "uuid-upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("hello via uuid"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UploadFile with UUID header: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Clean up.
	if _, err := testPool.Exec(
		context.Background(),
		`DELETE FROM attachment WHERE workspace_id = $1 AND filename = $2`,
		testWorkspaceID,
		"uuid-upload.txt",
	); err != nil {
		t.Fatalf("cleanup attachment: %v", err)
	}
}

// TestUploadFile_AttachesToChatSession verifies that a multipart upload with
// a chat_session_id form field creates an attachment row linked to that chat
// session (chat_message_id remains NULL — it is back-filled on send).
func TestUploadFile_AttachesToChatSession(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	agentID := createHandlerTestAgent(t, "ChatUploadAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "chat-upload.png")
	if err != nil {
		t.Fatal(err)
	}
	// Minimal PNG signature so content-type sniffs as image/png.
	part.Write([]byte("\x89PNG\r\n\x1a\nrest-of-bytes"))
	if err := writer.WriteField("chat_session_id", sessionID); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UploadFile with chat_session_id: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, w.Body.String())
	}
	if resp.ChatSessionID == nil || *resp.ChatSessionID != sessionID {
		t.Fatalf("chat_session_id in response: want %s, got %v", sessionID, resp.ChatSessionID)
	}
	if resp.ChatMessageID != nil {
		t.Fatalf("chat_message_id should be NULL before send, got %v", resp.ChatMessageID)
	}
	if resp.IssueID != nil || resp.CommentID != nil {
		t.Fatalf("issue_id/comment_id should be NULL for chat-only upload: %+v", resp)
	}
	if resp.URL == "" {
		t.Fatal("expected non-empty url")
	}

	// Verify the DB row directly.
	var dbSession, dbMessage *string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT chat_session_id::text, chat_message_id::text FROM attachment WHERE id = $1`,
		resp.ID,
	).Scan(&dbSession, &dbMessage); err != nil {
		t.Fatalf("query attachment row: %v", err)
	}
	if dbSession == nil || *dbSession != sessionID {
		t.Fatalf("DB chat_session_id mismatch: want %s, got %v", sessionID, dbSession)
	}
	if dbMessage != nil {
		t.Fatalf("DB chat_message_id should be NULL, got %v", dbMessage)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, resp.ID)
	})
}

// TestUploadFile_RejectsForeignChatSession verifies a chat_session in another
// workspace (or owned by another user) is rejected with 403/404, preventing
// cross-tenant attachment binding.
func TestUploadFile_RejectsForeignChatSession(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "evil.txt")
	part.Write([]byte("payload"))
	// Random non-existent UUID.
	writer.WriteField("chat_session_id", "00000000-0000-0000-0000-0000deadbeef")
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
		t.Fatalf("UploadFile with unknown chat_session_id: expected 4xx, got %d: %s", w.Code, w.Body.String())
	}
}
