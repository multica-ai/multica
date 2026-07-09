package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

// mockStorage is a tiny in-memory Storage stand-in. Upload records the bytes
// keyed by the storage key so GetReader can round-trip them in tests; KeyFromURL
// strips the synthetic CDN host so consumers can pass either the URL or the
// raw key.
type mockStorage struct {
	mu                  sync.Mutex
	files               map[string][]byte
	presignCalls        []string
	presignDispositions []string
}

func (m *mockStorage) Upload(_ context.Context, key string, data []byte, _ string, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[key] = append([]byte(nil), data...)
	return fmt.Sprintf("https://cdn.example.com/%s", key), nil
}

func (m *mockStorage) Delete(_ context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
}
func (m *mockStorage) DeleteKeys(_ context.Context, _ []string) {}
func (m *mockStorage) KeyFromURL(rawURL string) string {
	for _, prefix := range []string{
		"https://cdn.example.com/",
		"http://rustfs:9000/test-bucket/",
		"https://s3.example.com/test-bucket/",
	} {
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}
	return rawURL
}
func (m *mockStorage) CdnDomain() string { return "cdn.example.com" }

// mockStorageNoCdn is a mockStorage variant that returns an empty CdnDomain
// to simulate a private S3 / R2 / MinIO deployment where the operator has
// NOT configured a public-facing CDN domain. buildMarkdownURL must not
// persist `a.Url` for this shape — it would write a private bucket URL
// into markdown that no client can load.
type mockStorageNoCdn struct{ mockStorage }

func (m *mockStorageNoCdn) CdnDomain() string { return "" }
func (m *mockStorage) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.files[key]; ok {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil, fmt.Errorf("mockStorage GetReader: key not found: %q", key)
}
func (m *mockStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.presignCalls = append(m.presignCalls, key)
	return "https://signed.example.com/" + key + "?X-Amz-Signature=mock", nil
}
func (m *mockStorage) PresignGetWithContentDisposition(_ context.Context, key string, _ time.Duration, contentDisposition string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.presignCalls = append(m.presignCalls, key)
	m.presignDispositions = append(m.presignDispositions, contentDisposition)
	u := url.URL{
		Scheme: "https",
		Host:   "signed.example.com",
		Path:   "/" + key,
	}
	q := u.Query()
	q.Set("X-Amz-Signature", "mock")
	if contentDisposition != "" {
		q.Set("response-content-disposition", contentDisposition)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func (m *mockStorage) put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[key] = append([]byte(nil), data...)
}

// seekableReadCloser adapts a *bytes.Reader (which is an io.ReadSeeker) into an
// io.ReadCloser, mirroring what LocalStorage.GetReader returns (an *os.File is
// seekable). Used to exercise proxyAttachmentDownload's http.ServeContent path.
type seekableReadCloser struct{ *bytes.Reader }

func (seekableReadCloser) Close() error { return nil }

// seekableMockStorage is a mockStorage whose GetReader returns a seekable body,
// so proxyAttachmentDownload takes the http.ServeContent branch (the local-disk
// shape) instead of the manual single-range fallback.
type seekableMockStorage struct{ mockStorage }

func (m *seekableMockStorage) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.files[key]; ok {
		return seekableReadCloser{bytes.NewReader(data)}, nil
	}
	return nil, fmt.Errorf("seekableMockStorage GetReader: key not found: %q", key)
}

// failingReader serves up to failAfter bytes and then errors on the next Read.
// It is forward-only (no Seek), so proxyAttachmentDownload routes it through the
// manual serveProxyRange path — letting us simulate a storage backend that dies
// mid-stream while the handler is discarding bytes to reach a Range start.
type failingReader struct {
	data      []byte
	pos       int
	failAfter int
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.pos >= f.failAfter {
		return 0, fmt.Errorf("simulated storage read failure at offset %d", f.pos)
	}
	if remaining := f.failAfter - f.pos; len(p) > remaining {
		p = p[:remaining]
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *failingReader) Close() error { return nil }

// failingMockStorage returns a failingReader so the non-seekable Range path hits
// a read error partway through the skip-to-start CopyN.
type failingMockStorage struct {
	mockStorage
	failAfter int
}

func (m *failingMockStorage) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.files[key]; ok {
		return &failingReader{data: data, failAfter: m.failAfter}, nil
	}
	return nil, fmt.Errorf("failingMockStorage GetReader: key not found: %q", key)
}

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

// ---------------------------------------------------------------------------
// GetAttachmentContent tests (preview proxy)
// ---------------------------------------------------------------------------

// seedPreviewAttachment inserts an attachment row + writes the bytes into the
// active mockStorage. Returns the new attachment id. Caller is responsible for
// installing the mockStorage on testHandler before calling.
func seedPreviewAttachment(t *testing.T, store *mockStorage, key, filename, contentType string, body []byte) string {
	t.Helper()
	// Register the body so GetReader can find it via KeyFromURL → key.
	url, err := store.Upload(context.Background(), key, body, contentType, filename)
	if err != nil {
		t.Fatalf("seed Upload: %v", err)
	}

	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, $3, $4, $5, $6)
		RETURNING id::text
	`, testWorkspaceID, testUserID, filename, url, contentType, len(body)).Scan(&id); err != nil {
		t.Fatalf("seed attachment row: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return id
}

func seedAttachmentURL(t *testing.T, rawURL, filename, contentType string, sizeBytes int64) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, $3, $4, $5, $6)
		RETURNING id::text
	`, testWorkspaceID, testUserID, filename, rawURL, contentType, sizeBytes).Scan(&id); err != nil {
		t.Fatalf("seed attachment row: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return id
}

func newPreviewRequest(t *testing.T, attachmentID, workspaceID string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/content", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", attachmentID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req, httptest.NewRecorder()
}

func newDownloadRequest(t *testing.T, attachmentID, workspaceID string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/attachments/"+attachmentID+"/download", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", attachmentID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req, httptest.NewRecorder()
}

// requireAttachmentDownloadHeaders asserts the security / disposition headers
// that proxyAttachmentDownload sets before choosing a Range branch are preserved
// on the final response, whether it went out as a 206 (partial) or a full 200.
// The Range/206 work must not drop Content-Type / Content-Disposition /
// Cache-Control: no-store / X-Content-Type-Options / preview CSP — the seekable
// (http.ServeContent) path in particular could silently clobber them.
func requireAttachmentDownloadHeaders(t *testing.T, header http.Header, wantFilename string) {
	t.Helper()
	if got, want := header.Get("Content-Type"), "application/octet-stream"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got := header.Get("Content-Disposition"); got == "" || !strings.Contains(got, wantFilename) {
		t.Fatalf("Content-Disposition = %q, want non-empty containing %q", got, wantFilename)
	}
	if got, want := header.Get("Cache-Control"), "no-store"; got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
	if got, want := header.Get("X-Content-Type-Options"), "nosniff"; got != want {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, want)
	}
	requireAttachmentPreviewCSP(t, header)
}

func requireAttachmentPreviewCSP(t *testing.T, header http.Header, extraAncestors ...string) {
	t.Helper()
	csp := header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
	for _, directive := range []string{
		"default-src 'none'",
		"frame-ancestors 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("Content-Security-Policy missing %q; got %q", directive, csp)
		}
	}
	for _, ancestor := range extraAncestors {
		if !strings.Contains(csp, ancestor) {
			t.Fatalf("Content-Security-Policy missing frame ancestor %q; got %q", ancestor, csp)
		}
	}
	if strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy still blocks same-origin previews: %q", csp)
	}
}

func TestAttachmentPreviewCSPHeader_AllowsConfiguredFrontendOrigins(t *testing.T) {
	csp := attachmentPreviewCSPHeader([]string{
		"https://app.example.test",
		" https://App.Example.Test/some/path ",
		"http://localhost:3000",
		"*",
		"javascript:alert(1)",
		"not a url",
	})

	for _, want := range []string{
		"frame-ancestors 'self' https://app.example.test http://localhost:3000",
		"default-src 'none'",
		"object-src 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("Content-Security-Policy missing %q; got %q", want, csp)
		}
	}
	for _, reject := range []string{"*", "javascript:", "not a url", "some/path"} {
		if strings.Contains(csp, reject) {
			t.Fatalf("Content-Security-Policy includes rejected source %q; got %q", reject, csp)
		}
	}
	if strings.Count(csp, "https://app.example.test") != 1 {
		t.Fatalf("Content-Security-Policy should dedupe origins; got %q", csp)
	}
}

func newDownloadRouter() http.Handler {
	// Mirrors the production router after MUL-3130: the download
	// route is registered under Auth-only with no
	// RequireWorkspaceMember wrapper. The handler self-resolves the
	// workspace from the attachment row and enforces membership
	// internally, so a native browser <img>/<video> resource load
	// with no X-Workspace-* headers is the supported call shape.
	r := chi.NewRouter()
	r.Get("/api/attachments/{id}/download", testHandler.DownloadAttachment)
	return r
}

func testCloudFrontSigner(t *testing.T) *auth.CloudFrontSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CloudFront test key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	t.Setenv("CLOUDFRONT_KEY_PAIR_ID", "KTEST")
	t.Setenv("CLOUDFRONT_DOMAIN", "static.example.test")
	t.Setenv("COOKIE_DOMAIN", ".example.test")
	t.Setenv("CLOUDFRONT_PRIVATE_KEY", base64.StdEncoding.EncodeToString(pemBytes))
	t.Setenv("CLOUDFRONT_PRIVATE_KEY_SECRET", "")
	signer := auth.NewCloudFrontSignerFromEnv()
	if signer == nil {
		t.Fatal("expected CloudFront signer")
	}
	return signer
}

func TestAttachmentToResponse_NonCloudFrontUsesDownloadEndpoint(t *testing.T) {
	origSigner := testHandler.CFSigner
	testHandler.CFSigner = nil
	t.Cleanup(func() { testHandler.CFSigner = origSigner })

	id := seedAttachmentURL(t, "http://rustfs:9000/test-bucket/private.txt", "private.txt", "text/plain", 5)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	if resp.URL != "http://rustfs:9000/test-bucket/private.txt" {
		t.Fatalf("stored url changed: %q", resp.URL)
	}
	if resp.DownloadURL != "/api/attachments/"+id+"/download" {
		t.Fatalf("download_url = %q, want unified endpoint", resp.DownloadURL)
	}
}

func TestDownloadAttachment_CloudFrontRedirectSignsAttachmentDisposition(t *testing.T) {
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = &mockStorage{}
	testHandler.cfg.AttachmentDownloadMode = "cloudfront"
	testHandler.cfg.AttachmentFrameAncestors = []string{"https://app.example.test"}
	testHandler.CFSigner = testCloudFrontSigner(t)
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	id := seedAttachmentURL(t, "https://static.example.test/downloads/cloudfront.md", "cloud front.md", "text/markdown", 10)

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := parsed.Query().Get("response-content-disposition"); got != `attachment; filename="cloud front.md"` {
		t.Fatalf("response-content-disposition = %q", got)
	}
	if got := parsed.Query().Get("Key-Pair-Id"); got != "KTEST" {
		t.Fatalf("Key-Pair-Id = %q", got)
	}
	requireAttachmentPreviewCSP(t, w.Header(), "https://app.example.test")
}

func TestDownloadAttachment_BareNavigationWithWorkspaceSlugQueryPassesMiddleware(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	key := "downloads/bare-nav.txt"
	body := []byte("download body")
	store.put(key, body)
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "bare-nav.txt", "text/plain", int64(len(body)))

	req := httptest.NewRequest("GET", "/api/attachments/"+id+"/download?workspace_slug="+url.QueryEscape(handlerTestWorkspaceSlug), nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	newDownloadRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if req.Header.Get("X-Workspace-ID") != "" || req.Header.Get("X-Workspace-Slug") != "" {
		t.Fatalf("bare navigation test must not set custom workspace headers")
	}
}

// TestDownloadAttachment_BareNavigationServesMemberWithoutWorkspaceHeaders
// is the regression test for MUL-3130: a markdown image rendered as
// `<img src="/api/attachments/<id>/download">` produces a native browser
// resource load that cannot attach X-Workspace-Slug / X-Workspace-ID
// headers. After the fix the handler self-resolves the workspace from
// the attachment row, so a bare URL succeeds for a workspace member.
func TestDownloadAttachment_BareNavigationServesMemberWithoutWorkspaceHeaders(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	key := "downloads/bare-nav.txt"
	body := []byte("download body")
	store.put(key, body)
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "bare-nav.txt", "text/plain", int64(len(body)))

	// Bare URL — no workspace_slug / workspace_id query, no
	// X-Workspace-* headers. This is what a browser <img> tag emits
	// when the markdown stores `/api/attachments/<id>/download`.
	req := httptest.NewRequest("GET", "/api/attachments/"+id+"/download", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	newDownloadRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if req.Header.Get("X-Workspace-ID") != "" || req.Header.Get("X-Workspace-Slug") != "" {
		t.Fatalf("bare navigation test must not set custom workspace headers")
	}
}

// TestDownloadAttachment_BareNavigationDeniesNonMemberWith404 covers the
// IDOR boundary: a stray attachment ID belonging to a workspace the
// requester is NOT a member of must return 404, not 200 (would leak
// bytes) and not 403 (would confirm the ID exists). Mirrors
// ServeLocalUpload's deny shape.
func TestDownloadAttachment_BareNavigationDeniesNonMemberWith404(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	// Seed an attachment that lives in a workspace testUserID is NOT
	// a member of. The workspace row has to exist so the FK on
	// attachment.workspace_id resolves; we tear both down on
	// cleanup.
	ctx := context.Background()
	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Bare-Nav Foreign', 'bare-nav-foreign', '', 'BNF')
		RETURNING id::text
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID) })

	key := "downloads/bare-nav-foreign.txt"
	store.put(key, []byte("foreign-body"))
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, $3, $4, $5, $6)
		RETURNING id::text
	`, foreignWorkspaceID, testUserID, "foreign.txt", "https://s3.example.com/test-bucket/"+key, "text/plain", 12).Scan(&id); err != nil {
		t.Fatalf("seed foreign attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id) })

	req := httptest.NewRequest("GET", "/api/attachments/"+id+"/download", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()

	newDownloadRouter().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for non-member; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "foreign-body") {
		t.Fatalf("response body leaked file contents: %q", w.Body.String())
	}
}

func TestDownloadAttachment_AutoInternalEndpointProxies(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "auto"
	testHandler.cfg.AttachmentFrameAncestors = []string{"https://app.example.test"}
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	key := "downloads/proxy-private.txt"
	body := []byte("private object")
	store.put(key, body)
	id := seedAttachmentURL(t, "http://rustfs:9000/test-bucket/"+key, "report.txt", "text/plain", int64(len(body)))

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if got := w.Header().Get("Location"); got != "" {
		t.Fatalf("Location should be empty for proxy download, got %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="report.txt"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	requireAttachmentPreviewCSP(t, w.Header(), "https://app.example.test")
	if len(store.presignCalls) != 0 {
		t.Fatalf("internal endpoint should not presign, calls=%v", store.presignCalls)
	}
}

func TestDownloadAttachment_AutoPublicEndpointPresigns(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "auto"
	testHandler.cfg.AttachmentFrameAncestors = []string{"https://app.example.test"}
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	key := "downloads/public-private.txt"
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "public.txt", "text/plain", 10)

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "X-Amz-Signature=mock") {
		t.Fatalf("Location = %q, want fake S3 signature", loc)
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := parsed.Query().Get("response-content-disposition"); got != `attachment; filename="public.txt"` {
		t.Fatalf("response-content-disposition = %q", got)
	}
	if len(store.presignCalls) != 1 || store.presignCalls[0] != key {
		t.Fatalf("presign calls = %v, want [%s]", store.presignCalls, key)
	}
	if len(store.presignDispositions) != 1 || store.presignDispositions[0] != `attachment; filename="public.txt"` {
		t.Fatalf("presign dispositions = %v", store.presignDispositions)
	}
	requireAttachmentPreviewCSP(t, w.Header(), "https://app.example.test")
}

func TestDownloadAttachment_ExplicitProxyStreamsPublicEndpoint(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	key := "downloads/forced-proxy.png"
	body := []byte("\x89PNG\r\n\x1a\nimage")
	store.put(key, body)
	id := seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "image.png", "image/png", int64(len(body)))

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.Bytes(); !bytes.Equal(got, body) {
		t.Fatalf("body mismatch: got %q want %q", got, body)
	}
	if got := w.Header().Get("Content-Disposition"); got != `inline; filename="image.png"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	requireAttachmentPreviewCSP(t, w.Header())
	if len(store.presignCalls) != 0 {
		t.Fatalf("forced proxy should not presign, calls=%v", store.presignCalls)
	}
}

// setProxyDownloadHandler swaps the handler into forced-proxy mode with the
// given storage backend and returns the seeded attachment id + body. It wires
// t.Cleanup to restore the original handler fields.
func setProxyDownloadHandler(t *testing.T, store storage.Storage, body []byte) string {
	t.Helper()
	origStorage := testHandler.Storage
	origCfg := testHandler.cfg
	origSigner := testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage = origStorage
		testHandler.cfg = origCfg
		testHandler.CFSigner = origSigner
	})

	key := "downloads/range-sample.bin"
	if putter, ok := store.(interface{ put(string, []byte) }); ok {
		putter.put(key, body)
	} else {
		t.Fatalf("store %T does not support put()", store)
	}
	return seedAttachmentURL(t, "https://s3.example.com/test-bucket/"+key, "sample.bin", "application/octet-stream", int64(len(body)))
}

// rangeBody is a deterministic 4 KiB payload used by the Range tests so we can
// assert that a partial response's bytes match the corresponding slice of the
// full object.
func rangeBody() []byte {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte(i % 251) // 251 is prime → no alignment with 256 boundaries
	}
	return b
}

// runProxyRangeMatrix exercises the three acceptance paths (Range hit, 416,
// no-Range) against whichever storage backend is supplied, so both the
// http.ServeContent (seekable) and manual (non-seekable) branches are covered
// by identical assertions.
func runProxyRangeMatrix(t *testing.T, newStore func() storage.Storage) {
	body := rangeBody()

	t.Run("RangeHitReturns206", func(t *testing.T) {
		id := setProxyDownloadHandler(t, newStore(), body)
		req, w := newDownloadRequest(t, id, testWorkspaceID)
		req.Header.Set("Range", "bytes=0-1023")
		testHandler.DownloadAttachment(w, req)

		if w.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206; body=%s", w.Code, w.Body.String())
		}
		if got, want := w.Header().Get("Accept-Ranges"), "bytes"; got != want {
			t.Fatalf("Accept-Ranges = %q, want %q", got, want)
		}
		if got, want := w.Header().Get("Content-Range"), fmt.Sprintf("bytes 0-1023/%d", len(body)); got != want {
			t.Fatalf("Content-Range = %q, want %q", got, want)
		}
		if got, want := w.Header().Get("Content-Length"), "1024"; got != want {
			t.Fatalf("Content-Length = %q, want %q", got, want)
		}
		if got := w.Body.Bytes(); !bytes.Equal(got, body[:1024]) {
			t.Fatalf("partial body mismatch: len(got)=%d, want first 1024 bytes of object", len(got))
		}
		requireAttachmentDownloadHeaders(t, w.Header(), "sample.bin")
	})

	t.Run("MidStreamRangeMatchesFullSlice", func(t *testing.T) {
		id := setProxyDownloadHandler(t, newStore(), body)
		req, w := newDownloadRequest(t, id, testWorkspaceID)
		req.Header.Set("Range", "bytes=1000-1999")
		testHandler.DownloadAttachment(w, req)

		if w.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206; body=%s", w.Code, w.Body.String())
		}
		if got, want := w.Header().Get("Content-Range"), fmt.Sprintf("bytes 1000-1999/%d", len(body)); got != want {
			t.Fatalf("Content-Range = %q, want %q", got, want)
		}
		if got := w.Body.Bytes(); !bytes.Equal(got, body[1000:2000]) {
			t.Fatalf("mid-stream range bytes do not match object[1000:2000]")
		}
	})

	t.Run("SuffixRangeReturnsTail", func(t *testing.T) {
		id := setProxyDownloadHandler(t, newStore(), body)
		req, w := newDownloadRequest(t, id, testWorkspaceID)
		req.Header.Set("Range", "bytes=-500")
		testHandler.DownloadAttachment(w, req)

		if w.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206; body=%s", w.Code, w.Body.String())
		}
		start := len(body) - 500
		if got, want := w.Header().Get("Content-Range"), fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)); got != want {
			t.Fatalf("Content-Range = %q, want %q", got, want)
		}
		if got := w.Body.Bytes(); !bytes.Equal(got, body[start:]) {
			t.Fatalf("suffix range bytes do not match object tail")
		}
	})

	t.Run("UnsatisfiableRangeReturns416", func(t *testing.T) {
		id := setProxyDownloadHandler(t, newStore(), body)
		req, w := newDownloadRequest(t, id, testWorkspaceID)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", len(body)+10, len(body)+20))
		testHandler.DownloadAttachment(w, req)

		if w.Code != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("status = %d, want 416; body=%s", w.Code, w.Body.String())
		}
		if got, want := w.Header().Get("Content-Range"), fmt.Sprintf("bytes */%d", len(body)); got != want {
			t.Fatalf("Content-Range = %q, want %q", got, want)
		}
	})

	t.Run("NoRangeReturnsFull200", func(t *testing.T) {
		id := setProxyDownloadHandler(t, newStore(), body)
		req, w := newDownloadRequest(t, id, testWorkspaceID)
		testHandler.DownloadAttachment(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if got, want := w.Header().Get("Accept-Ranges"), "bytes"; got != want {
			t.Fatalf("Accept-Ranges = %q, want %q", got, want)
		}
		if got := w.Body.Bytes(); !bytes.Equal(got, body) {
			t.Fatalf("full body mismatch: len(got)=%d, want %d", len(got), len(body))
		}
		requireAttachmentDownloadHeaders(t, w.Header(), "sample.bin")
	})
}

// TestDownloadAttachment_ProxyRange_Seekable covers the http.ServeContent path
// taken when the storage backend returns a seekable reader (local disk).
func TestDownloadAttachment_ProxyRange_Seekable(t *testing.T) {
	runProxyRangeMatrix(t, func() storage.Storage { return &seekableMockStorage{} })
}

// TestDownloadAttachment_ProxyRange_NonSeekable covers the manual single-range
// fallback taken when the backend returns a forward-only stream (S3/MinIO).
func TestDownloadAttachment_ProxyRange_NonSeekable(t *testing.T) {
	runProxyRangeMatrix(t, func() storage.Storage { return &mockStorage{} })
}

// TestDownloadAttachment_NonSeekableRangeSkipFailureReturns502 is the must-fix
// regression (RAS-31 ①). When the storage read fails while the handler is
// skipping forward to the Range start — before any response header is written —
// it must reply with an honest 502, NOT net/http's default 200 OK + empty body.
// A resuming client reads that default 200 as "Range ignored, this is the full
// object", silently turning a transient storage error into a corrupt/empty
// download and defeating the resume feature itself.
func TestDownloadAttachment_NonSeekableRangeSkipFailureReturns502(t *testing.T) {
	body := rangeBody()
	// Die at offset 500, well before the Range start (1000), so the failure lands
	// inside the skip CopyN rather than the payload copy.
	store := &failingMockStorage{failAfter: 500}
	id := setProxyDownloadHandler(t, store, body)

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	req.Header.Set("Range", "bytes=1000-1999")
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to read attachment range") {
		t.Fatalf("502 body should carry the honest error, got %q", w.Body.String())
	}
	if got := w.Body.Bytes(); bytes.Contains(got, body[:64]) {
		t.Fatalf("502 response must not leak object bytes; got %q", got)
	}
}

// TestDownloadAttachment_NonSeekableMultiRangeServesFull200 is the should-fix
// (RAS-31 ②). A multi-range request the non-seekable path cannot serve must be
// ignored and answered with a full 200 (RFC 7233), not 416. Paired with the
// seekable test below, this proves the two backends no longer disagree on
// success vs failure for the same multi-range request.
func TestDownloadAttachment_NonSeekableMultiRangeServesFull200(t *testing.T) {
	body := rangeBody()
	id := setProxyDownloadHandler(t, &mockStorage{}, body)

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	req.Header.Set("Range", "bytes=0-10,20-30")
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (multi-range ignored); body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.Bytes(); !bytes.Equal(got, body) {
		t.Fatalf("multi-range should serve full body: len(got)=%d, want %d", len(got), len(body))
	}
	if got := w.Header().Get("Content-Range"); got != "" {
		t.Fatalf("full 200 response must not carry Content-Range, got %q", got)
	}
	if got, want := w.Header().Get("Accept-Ranges"), "bytes"; got != want {
		t.Fatalf("Accept-Ranges = %q, want %q", got, want)
	}
	requireAttachmentDownloadHeaders(t, w.Header(), "sample.bin")
}

// TestDownloadAttachment_SeekableMultiRangeServes206 documents the other half of
// the ②-divergence: the seekable (http.ServeContent) path answers the same
// multi-range request with a successful 206 multipart. Together with the
// non-seekable 200 test above, it shows neither backend fails the request with
// 416 — the divergence the maintainer flagged is gone.
func TestDownloadAttachment_SeekableMultiRangeServes206(t *testing.T) {
	body := rangeBody()
	id := setProxyDownloadHandler(t, &seekableMockStorage{}, body)

	req, w := newDownloadRequest(t, id, testWorkspaceID)
	req.Header.Set("Range", "bytes=0-10,20-30")
	testHandler.DownloadAttachment(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 (multipart); body len=%d", w.Code, w.Body.Len())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "multipart/byteranges") {
		t.Fatalf("Content-Type = %q, want multipart/byteranges", ct)
	}
}

// TestDownloadAttachment_NonSeekableEmptyObjectRangeServesFull200 covers the
// zero-length nit the maintainer flagged during re-review (same class as RAS-31
// ②). A WELL-FORMED Range against a 0-byte attachment used to collapse to 416 on
// the non-seekable path (parseSingleByteRange → rangeUnsatisfiable) while the
// seekable http.ServeContent path ignores the Range and returns an empty 200.
// parseSingleByteRange now classifies a well-formed range against an empty object
// as rangeUnsupported (→ empty 200), so neither backend hard-fails it. Malformed
// ranges are covered by the 416 companion test below.
func TestDownloadAttachment_NonSeekableEmptyObjectRangeServesFull200(t *testing.T) {
	// Both a start-based range and a suffix range must be ignored on an empty
	// object; the suffix form ("bytes=-100" against size 0) is the exact case the
	// maintainer referenced.
	for _, rangeHeader := range []string{"bytes=0-", "bytes=-100", "bytes=0-99"} {
		t.Run(rangeHeader, func(t *testing.T) {
			id := setProxyDownloadHandler(t, &mockStorage{}, []byte{})

			req, w := newDownloadRequest(t, id, testWorkspaceID)
			req.Header.Set("Range", rangeHeader)
			testHandler.DownloadAttachment(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (Range ignored on empty object); body=%s", w.Code, w.Body.String())
			}
			if got := w.Body.Len(); got != 0 {
				t.Fatalf("empty object must serve an empty body, got %d bytes", got)
			}
			if got := w.Header().Get("Content-Length"); got != "0" {
				t.Fatalf("Content-Length = %q, want %q for an empty 200", got, "0")
			}
			if got := w.Header().Get("Content-Range"); got != "" {
				t.Fatalf("full 200 response must not carry Content-Range, got %q", got)
			}
			if got, want := w.Header().Get("Accept-Ranges"), "bytes"; got != want {
				t.Fatalf("Accept-Ranges = %q, want %q", got, want)
			}
			requireAttachmentDownloadHeaders(t, w.Header(), "sample.bin")
		})
	}
}

// TestDownloadAttachment_NonSeekableEmptyObjectMalformedRangeReturns416 is the
// guard against over-correcting the zero-length nit: a MALFORMED / unknown-unit
// Range against a 0-byte object must still return 416, not be swallowed into a
// 200. stdlib http.ServeContent returns 416 for these even on an empty object
// (it rejects the range before the empty-content short-circuit), so returning
// 200 here would introduce a NEW backend-dependent divergence — the exact
// regression a blanket "total == 0 → 200" would cause.
func TestDownloadAttachment_NonSeekableEmptyObjectMalformedRangeReturns416(t *testing.T) {
	for _, rangeHeader := range []string{"items=0-10", "bytes=abc-def", "bytes=100"} {
		t.Run(rangeHeader, func(t *testing.T) {
			id := setProxyDownloadHandler(t, &mockStorage{}, []byte{})

			req, w := newDownloadRequest(t, id, testWorkspaceID)
			req.Header.Set("Range", rangeHeader)
			testHandler.DownloadAttachment(w, req)

			if w.Code != http.StatusRequestedRangeNotSatisfiable {
				t.Fatalf("status = %d, want 416 for malformed range on empty object; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestParseSingleByteRange(t *testing.T) {
	const size = 1000
	tests := []struct {
		name        string
		header      string
		size        int64
		wantOutcome rangeParseOutcome
		wantStart   int64
		wantLength  int64
	}{
		{"full closed range", "bytes=0-1023", 2000, rangeSatisfiable, 0, 1024},
		{"open-ended range", "bytes=500-", size, rangeSatisfiable, 500, 500},
		{"end clamped to eof", "bytes=900-5000", size, rangeSatisfiable, 900, 100},
		{"suffix range", "bytes=-200", size, rangeSatisfiable, 800, 200},
		{"suffix larger than size clamps", "bytes=-5000", size, rangeSatisfiable, 0, 1000},
		{"single byte", "bytes=0-0", size, rangeSatisfiable, 0, 1},
		{"last byte", "bytes=999-999", size, rangeSatisfiable, 999, 1},
		{"start at eof unsatisfiable", "bytes=1000-1001", size, rangeUnsatisfiable, 0, 0},
		{"start past eof unsatisfiable", "bytes=2000-", size, rangeUnsatisfiable, 0, 0},
		{"end before start", "bytes=500-499", size, rangeUnsatisfiable, 0, 0},
		{"missing unit", "0-100", size, rangeUnsatisfiable, 0, 0},
		// Multi-range is a valid-but-unsupported form: ignored → full body (200),
		// not 416. This is the must-fix that stops the non-seekable path from
		// diverging from the seekable (ServeContent) path on multi-range.
		{"multi-range ignored", "bytes=0-10,20-30", size, rangeUnsupported, 0, 0},
		{"empty spec", "bytes=", size, rangeUnsatisfiable, 0, 0},
		{"no dash", "bytes=100", size, rangeUnsatisfiable, 0, 0},
		{"suffix zero", "bytes=-0", size, rangeUnsatisfiable, 0, 0},
		{"negative garbage", "bytes=abc-def", size, rangeUnsatisfiable, 0, 0},
		// Empty object (size == 0): a WELL-FORMED range has no bytes to satisfy it,
		// so it is ignored → full (empty) 200, matching stdlib http.ServeContent
		// rather than diverging with a 416. This covers the maintainer's
		// zero-length nit. Genuinely malformed / unknown-unit ranges against an
		// empty object still yield rangeUnsatisfiable (416), also matching stdlib.
		{"suffix on empty object", "bytes=-100", 0, rangeUnsupported, 0, 0},
		{"start range on empty object", "bytes=0-", 0, rangeUnsupported, 0, 0},
		{"mid range on empty object", "bytes=5-10", 0, rangeUnsupported, 0, 0},
		{"closed range on empty object", "bytes=0-99", 0, rangeUnsupported, 0, 0},
		// stdlib checks start-vs-size before validating the end, so on an empty
		// object it ignores (200) even a range with a bad end or reversed bounds;
		// mirror that. The paired non-empty rows below lock the other half of the
		// contract: once the start is IN range, the end IS validated → 416.
		{"bad end on empty object", "bytes=0-abc", 0, rangeUnsupported, 0, 0},
		{"reversed on empty object", "bytes=10-5", 0, rangeUnsupported, 0, 0},
		{"bad end on non-empty", "bytes=0-abc", size, rangeUnsatisfiable, 0, 0},
		{"reversed on non-empty", "bytes=10-5", size, rangeUnsatisfiable, 0, 0},
		{"unknown unit on empty object", "items=0-10", 0, rangeUnsatisfiable, 0, 0},
		{"malformed start on empty object", "bytes=abc-def", 0, rangeUnsatisfiable, 0, 0},
		{"no dash on empty object", "bytes=100", 0, rangeUnsatisfiable, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, length, outcome := parseSingleByteRange(tc.header, tc.size)
			if outcome != tc.wantOutcome {
				t.Fatalf("outcome = %v, want %v (start=%d length=%d)", outcome, tc.wantOutcome, start, length)
			}
			if outcome != rangeSatisfiable {
				return
			}
			if start != tc.wantStart || length != tc.wantLength {
				t.Fatalf("got (start=%d, length=%d), want (start=%d, length=%d)", start, length, tc.wantStart, tc.wantLength)
			}
		})
	}
}

func TestShouldProxyAttachmentURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"http://rustfs:9000/test-bucket/file.txt", true},
		{"http://localhost:9000/test-bucket/file.txt", true},
		{"http://127.0.0.1:9000/test-bucket/file.txt", true},
		{"http://10.0.2.15/test-bucket/file.txt", true},
		{"https://minio.internal/test-bucket/file.txt", true},
		{"/uploads/workspaces/abc/file.txt", true},
		{"https://s3.example.com/test-bucket/file.txt", false},
		{"https://bucket.s3.us-east-1.amazonaws.com/file.txt", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if got := shouldProxyAttachmentURL(tc.raw); got != tc.want {
				t.Fatalf("shouldProxyAttachmentURL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestGetAttachmentContent_HappyPath_Markdown(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	body := []byte("# heading\n\nbody text\n")
	id := seedPreviewAttachment(t, store, "preview-md-key.md", "preview.md", "text/markdown", body)

	req, w := newPreviewRequest(t, id, testWorkspaceID)
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
	testHandler.GetAttachmentContent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != string(body) {
		t.Errorf("body = %q, want %q", got, body)
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if got := w.Header().Get("X-Original-Content-Type"); got != "text/markdown" {
		t.Errorf("X-Original-Content-Type = %q, want text/markdown", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	requireAttachmentPreviewCSP(t, w.Header())
}

// Even when http.DetectContentType returned "text/plain" instead of "text/markdown"
// (a known sniffer quirk), the extension whitelist still grants access.
func TestGetAttachmentContent_AcceptsByExtensionWhenContentTypeIsGeneric(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	body := []byte("package main\n")
	id := seedPreviewAttachment(t, store, "main-go-key.go", "main.go", "application/octet-stream", body)

	req, w := newPreviewRequest(t, id, testWorkspaceID)
	testHandler.GetAttachmentContent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestGetAttachmentContent_Unsupported_PDF(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	id := seedPreviewAttachment(t, store, "pdf-key.pdf", "manual.pdf", "application/pdf", []byte("%PDF-1.4\n"))

	req, w := newPreviewRequest(t, id, testWorkspaceID)
	testHandler.GetAttachmentContent(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body=%s", w.Code, w.Body.String())
	}
}

func TestGetAttachmentContent_TooLarge(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	// One byte over the limit. Allocate ASCII so io.ReadAll has work to do.
	big := bytes.Repeat([]byte("a"), maxPreviewTextSize+1)
	id := seedPreviewAttachment(t, store, "huge-key.txt", "huge.txt", "text/plain", big)

	req, w := newPreviewRequest(t, id, testWorkspaceID)
	testHandler.GetAttachmentContent(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
}

func TestGetAttachmentContent_ForeignWorkspace(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	id := seedPreviewAttachment(t, store, "ws-mismatch.md", "note.md", "text/markdown", []byte("# secret\n"))

	// Same attachment id, but request comes in scoped to a different workspace.
	foreign := "00000000-0000-0000-0000-000000000099"
	req, w := newPreviewRequest(t, id, foreign)
	testHandler.GetAttachmentContent(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestGetAttachmentContent_NotFound(t *testing.T) {
	store := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = store
	defer func() { testHandler.Storage = origStorage }()

	req, w := newPreviewRequest(t, "00000000-0000-0000-0000-000000000abc", testWorkspaceID)
	testHandler.GetAttachmentContent(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// isTextPreviewable is the whitelist linkpin between the proxy and the
// client-side dispatcher. Regress against the most common content types so
// drifting one of the lists alone fails loud.
func TestIsTextPreviewable(t *testing.T) {
	t.Helper()
	cases := []struct {
		name        string
		contentType string
		filename    string
		want        bool
	}{
		{"markdown by ext", "application/octet-stream", "README.md", true},
		{"markdown by mime", "text/markdown", "README", true},
		{"plain text", "text/plain", "log.txt", true},
		{"json by mime", "application/json", "data.json", true},
		{"yaml by ext", "application/octet-stream", "config.yml", true},
		{"go source", "text/plain", "main.go", true},
		{"typescript", "application/octet-stream", "index.ts", true},
		{"html", "text/html", "page.html", true},
		{"dockerfile no ext", "application/octet-stream", "Dockerfile", true},
		{"makefile no ext", "application/octet-stream", "Makefile", true},
		{"env dotfile", "application/octet-stream", ".env", true},
		{"gitignore dotfile", "application/octet-stream", ".gitignore", true},
		{"dockerfile extension", "application/octet-stream", "service.dockerfile", true},
		{"makefile extension", "application/octet-stream", "rules.makefile", true},

		{"pdf rejected", "application/pdf", "doc.pdf", false},
		{"png rejected", "image/png", "shot.png", false},
		{"video rejected", "video/mp4", "clip.mp4", false},
		{"binary fallthrough", "application/octet-stream", "blob.bin", false},
		{"docx rejected", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "report.docx", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTextPreviewable(tc.contentType, tc.filename); got != tc.want {
				t.Errorf("isTextPreviewable(%q, %q) = %v, want %v", tc.contentType, tc.filename, got, tc.want)
			}
		})
	}
}

// MUL-3192 — buildMarkdownURL must emit a durable, absolute-when-possible
// URL that loads natively in any client (web, desktop, mobile webview).
// `download_url` may be a short-lived signed URL and is unsafe to persist;
// `markdown_url` is the contract for "ok to embed in markdown body".
//
// Matrix:
//
//   - public CDN durable URL ............... reuse a.Url verbatim
//   - LocalStorage with PublicURL set ....... reuse a.Url (already absolute)
//   - CloudFront-signed mode ................ never reuse a.Url (raw S3),
//                                              prefer absolute API endpoint
//   - LocalStorage relative + PublicURL set . prefix to absolute API endpoint
//   - PublicURL unset ....................... fall back to site-relative
//                                              (web's Next rewrite handles it)
//   - signed URL (CloudFront-signed leaked
//     into a.Url somehow) ................... reject as durable, fall through
//                                              to API endpoint to avoid
//                                              re-opening MUL-3130

func TestBuildMarkdownURL_PublicCdnAbsoluteURLReusedVerbatim(t *testing.T) {
	origPublic := testHandler.cfg.PublicURL
	origSigner := testHandler.CFSigner
	origStorage := testHandler.Storage
	t.Cleanup(func() {
		testHandler.cfg.PublicURL = origPublic
		testHandler.CFSigner = origSigner
		testHandler.Storage = origStorage
	})
	testHandler.cfg.PublicURL = "https://api.multica.test"
	testHandler.CFSigner = nil
	// mockStorage.CdnDomain() returns "cdn.example.com" — that's the
	// operator-set signal that the URL host serves content publicly
	// without per-request auth. Without this, the new gate routes
	// through the API endpoint to be safe.
	testHandler.Storage = &mockStorage{}

	id := seedAttachmentURL(t, "https://cdn.multica.test/uploads/abc.png", "abc.png", "image/png", 1)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	if resp.MarkdownURL != "https://cdn.multica.test/uploads/abc.png" {
		t.Fatalf("markdown_url = %q, want raw a.Url passthrough", resp.MarkdownURL)
	}
}

// MUL-3192 review must-fix 1 — `att.url` for a private S3 / R2 / MinIO
// bucket is absolute https + unsigned but is NOT publicly readable. The
// generic "absolute http(s) without signature" check would have wrongly
// persisted it; the gate now also requires `Storage.CdnDomain()` to be
// set so the operator has explicitly opted into "URLs from this storage
// load directly".
func TestBuildMarkdownURL_PrivateBucketWithoutCdnDomainRoutesThroughAPIEndpoint(t *testing.T) {
	origPublic := testHandler.cfg.PublicURL
	origSigner := testHandler.CFSigner
	origStorage := testHandler.Storage
	t.Cleanup(func() {
		testHandler.cfg.PublicURL = origPublic
		testHandler.CFSigner = origSigner
		testHandler.Storage = origStorage
	})
	testHandler.cfg.PublicURL = "https://api.multica.test"
	testHandler.CFSigner = nil
	testHandler.Storage = &mockStorageNoCdn{}

	id := seedAttachmentURL(t, "https://prod.s3.amazonaws.com/key.png", "key.png", "image/png", 1)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	want := "https://api.multica.test/api/attachments/" + id + "/download"
	if resp.MarkdownURL != want {
		t.Fatalf("markdown_url = %q, want absolute API endpoint %q (private bucket without explicit CDN must not persist raw S3 URL)", resp.MarkdownURL, want)
	}
}

func TestBuildMarkdownURL_CloudFrontSignedModeNeverPersistsRawStorageURL(t *testing.T) {
	origPublic := testHandler.cfg.PublicURL
	origSigner := testHandler.CFSigner
	t.Cleanup(func() {
		testHandler.cfg.PublicURL = origPublic
		testHandler.CFSigner = origSigner
	})
	testHandler.cfg.PublicURL = "https://api.multica.test"
	testHandler.CFSigner = testCloudFrontSigner(t)

	// Raw S3 URL — private bucket, not loadable directly by clients.
	id := seedAttachmentURL(t, "https://prod.s3.amazonaws.com/key.png", "key.png", "image/png", 1)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	want := "https://api.multica.test/api/attachments/" + id + "/download"
	if resp.MarkdownURL != want {
		t.Fatalf("markdown_url = %q, want absolute API endpoint %q", resp.MarkdownURL, want)
	}
	// download_url is allowed to carry a TTL (CloudFront-signed); it's NOT
	// what the client persists, but it IS what the renderer uses for this
	// response. The two are intentionally distinct.
	if resp.DownloadURL == resp.MarkdownURL {
		t.Fatalf("download_url and markdown_url must differ in CloudFront-signed mode (got identical %q)", resp.DownloadURL)
	}
}

func TestBuildMarkdownURL_RelativeStorageURLPrefixedWithPublicURL(t *testing.T) {
	origPublic := testHandler.cfg.PublicURL
	origSigner := testHandler.CFSigner
	t.Cleanup(func() {
		testHandler.cfg.PublicURL = origPublic
		testHandler.CFSigner = origSigner
	})
	testHandler.cfg.PublicURL = "https://api.multica.test"
	testHandler.CFSigner = nil

	// LocalStorage without LOCAL_UPLOAD_BASE_URL stores a site-relative URL.
	id := seedAttachmentURL(t, "/uploads/abc.png", "abc.png", "image/png", 1)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	want := "https://api.multica.test/api/attachments/" + id + "/download"
	if resp.MarkdownURL != want {
		t.Fatalf("markdown_url = %q, want absolute API endpoint %q", resp.MarkdownURL, want)
	}
}

func TestBuildMarkdownURL_PublicURLUnsetFallsBackToSiteRelative(t *testing.T) {
	origPublic := testHandler.cfg.PublicURL
	origSigner := testHandler.CFSigner
	t.Cleanup(func() {
		testHandler.cfg.PublicURL = origPublic
		testHandler.CFSigner = origSigner
	})
	testHandler.cfg.PublicURL = ""
	testHandler.CFSigner = nil

	id := seedAttachmentURL(t, "/uploads/abc.png", "abc.png", "image/png", 1)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	want := "/api/attachments/" + id + "/download"
	if resp.MarkdownURL != want {
		t.Fatalf("markdown_url = %q, want site-relative fallback %q", resp.MarkdownURL, want)
	}
}

func TestBuildMarkdownURL_StripsTrailingSlashOnPublicURL(t *testing.T) {
	origPublic := testHandler.cfg.PublicURL
	origSigner := testHandler.CFSigner
	t.Cleanup(func() {
		testHandler.cfg.PublicURL = origPublic
		testHandler.CFSigner = origSigner
	})
	testHandler.cfg.PublicURL = "https://api.multica.test/"
	testHandler.CFSigner = nil

	id := seedAttachmentURL(t, "/uploads/abc.png", "abc.png", "image/png", 1)
	att, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}

	resp := testHandler.attachmentToResponse(att)
	want := "https://api.multica.test/api/attachments/" + id + "/download"
	if resp.MarkdownURL != want {
		t.Fatalf("markdown_url = %q, want exactly one separator %q", resp.MarkdownURL, want)
	}
}

func TestIsDurablePublicURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"absolute https no signature", "https://cdn.multica.test/foo.png", true},
		{"absolute http no signature", "http://cdn.multica.test/foo.png", true},
		{"absolute with port + path", "https://cdn.example.test:8080/a/b/c.png", true},
		{"empty string", "", false},
		{"site-relative", "/uploads/abc.png", false},
		{"protocol-relative", "//cdn.example/foo.png", false},
		{"data URL", "data:image/png;base64,abc", false},
		{"blob URL", "blob:https://app/abc", false},
		{"unsupported scheme", "ftp://server/foo", false},
		{"cloudfront-signed Signature", "https://cdn.example/foo.png?Signature=abc&Key-Pair-Id=K1", false},
		{"cloudfront-signed Key-Pair-Id alone", "https://cdn.example/foo.png?Key-Pair-Id=K1", false},
		{"s3-presigned X-Amz-Signature", "https://bucket.s3/foo.png?X-Amz-Signature=abc", false},
		{"s3-presigned X-Amz-Expires alone", "https://bucket.s3/foo.png?X-Amz-Expires=900", false},
		{"plain Expires query", "https://cdn.example/foo.png?Expires=99", false},
		{"unrelated query", "https://cdn.example/foo.png?cache=1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDurablePublicURL(tc.url); got != tc.want {
				t.Errorf("isDurablePublicURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestServeLocalUpload_RelaxesFrameAncestorsForPreview covers the self-hosted
// local-disk case where document previews (PDF/HTML) are fetched straight from
// the public /uploads/* static route. That route inherits the global
// "frame-ancestors 'none'" CSP from the middleware, which blocks iframe
// previews; ServeLocalUpload must overwrite it with the same relaxed preview
// policy the /api/attachments download endpoint uses. See MUL-3821 / #4477.
func TestServeLocalUpload_RelaxesFrameAncestorsForPreview(t *testing.T) {
	dir := t.TempDir()
	key := "workspaces/ws-1/preview.pdf"
	full := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const body = "%PDF-1.7 local-disk preview"
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	t.Setenv("LOCAL_UPLOAD_DIR", dir)
	t.Setenv("LOCAL_UPLOAD_BASE_URL", "")
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	h := &Handler{
		Storage: local,
		cfg:     Config{AttachmentFrameAncestors: []string{"https://app.example.test"}},
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil)
	w := httptest.NewRecorder()
	// Simulate the global CSP middleware having already stamped the strict
	// policy on the response before the static route runs.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")

	h.ServeLocalUpload(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
	requireAttachmentPreviewCSP(t, w.Header(), "https://app.example.test")
}

// TestServeLocalUpload_NonLocalStorage404 guards the defensive branch: the
// /uploads/* route is only registered under local storage, but the handler
// must not serve anything when the backing store is not local disk.
func TestServeLocalUpload_NonLocalStorage404(t *testing.T) {
	h := &Handler{Storage: &mockStorage{}}
	req := httptest.NewRequest(http.MethodGet, "/uploads/workspaces/ws-1/x.png", nil)
	w := httptest.NewRecorder()
	h.ServeLocalUpload(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
