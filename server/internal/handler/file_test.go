package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type mockStorage struct{}

func (m *mockStorage) Upload(_ context.Context, key string, _ []byte, _ string, _ string) (string, error) {
	return fmt.Sprintf("https://cdn.example.com/%s", key), nil
}

func (m *mockStorage) Delete(_ context.Context, _ string)       {}
func (m *mockStorage) DeleteKeys(_ context.Context, _ []string) {}
func (m *mockStorage) KeyFromURL(rawURL string) string          { return rawURL }
func (m *mockStorage) CdnDomain() string                        { return "cdn.example.com" }

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

func TestPreviewAttachmentMarkdownUsesStoredAttachmentURL(t *testing.T) {
	attachmentURL := "https://cdn.example.com/workspaces/" + testWorkspaceID + "/preview.md"
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'preview.md', $3, 'text/markdown', 14)`,
		testWorkspaceID,
		testUserID,
		attachmentURL,
	); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	defer func() {
		if _, err := testPool.Exec(
			context.Background(),
			`DELETE FROM attachment WHERE workspace_id = $1 AND url = $2`,
			testWorkspaceID,
			attachmentURL,
		); err != nil {
			t.Fatalf("cleanup attachment: %v", err)
		}
	}()

	origFetch := fetchMarkdownPreview
	fetchMarkdownPreview = func(_ context.Context, rawURL string) (*http.Response, error) {
		if rawURL != attachmentURL+"?download=1" {
			t.Fatalf("preview fetch URL = %q, want signed download URL", rawURL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("# Preview\n")),
		}, nil
	}
	defer func() { fetchMarkdownPreview = origFetch }()

	req := httptest.NewRequest("GET", "/api/attachments/preview?url="+attachmentURL+"?download=1", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PreviewAttachmentMarkdown(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewAttachmentMarkdown: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "# Preview\n" {
		t.Fatalf("preview body = %q", got)
	}
}

func TestPreviewAttachmentMarkdownRejectsUnknownURL(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/attachments/preview?url=https://cdn.example.com/missing.md", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PreviewAttachmentMarkdown(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("PreviewAttachmentMarkdown unknown URL: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPreviewAttachmentMarkdownSupportsRelativeAttachmentURL(t *testing.T) {
	attachmentURL := "/uploads/workspaces/" + testWorkspaceID + "/preview.md"
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'relative-preview.md', $3, 'text/markdown', 14)`,
		testWorkspaceID,
		testUserID,
		attachmentURL,
	); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	defer func() {
		if _, err := testPool.Exec(
			context.Background(),
			`DELETE FROM attachment WHERE workspace_id = $1 AND url = $2`,
			testWorkspaceID,
			attachmentURL,
		); err != nil {
			t.Fatalf("cleanup attachment: %v", err)
		}
	}()

	origFetch := fetchMarkdownPreview
	fetchMarkdownPreview = func(_ context.Context, rawURL string) (*http.Response, error) {
		wantURL := "https://preview.example.com" + attachmentURL
		if rawURL != wantURL {
			t.Fatalf("preview fetch URL = %q, want %q", rawURL, wantURL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("# Local Preview\n")),
		}, nil
	}
	defer func() { fetchMarkdownPreview = origFetch }()

	req := httptest.NewRequest("GET", "/api/attachments/preview?url="+url.QueryEscape(attachmentURL), nil)
	req.Host = "preview.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PreviewAttachmentMarkdown(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewAttachmentMarkdown relative URL: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "# Local Preview\n" {
		t.Fatalf("preview body = %q", got)
	}
}

func TestPreviewAttachmentMarkdownIgnoresForwardedHostForRelativeAttachmentURL(t *testing.T) {
	attachmentURL := "/uploads/workspaces/" + testWorkspaceID + "/forwarded-host.md"
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'forwarded-host.md', $3, 'text/markdown', 14)`,
		testWorkspaceID,
		testUserID,
		attachmentURL,
	); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	defer func() {
		if _, err := testPool.Exec(
			context.Background(),
			`DELETE FROM attachment WHERE workspace_id = $1 AND url = $2`,
			testWorkspaceID,
			attachmentURL,
		); err != nil {
			t.Fatalf("cleanup attachment: %v", err)
		}
	}()

	origFetch := fetchMarkdownPreview
	fetchMarkdownPreview = func(_ context.Context, rawURL string) (*http.Response, error) {
		wantURL := "https://api.example.com" + attachmentURL
		if rawURL != wantURL {
			t.Fatalf("preview fetch URL = %q, want %q", rawURL, wantURL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString("# Local Preview\n")),
		}, nil
	}
	defer func() { fetchMarkdownPreview = origFetch }()

	req := httptest.NewRequest("GET", "/api/attachments/preview?url="+url.QueryEscape(attachmentURL), nil)
	req.Host = "api.example.com"
	req.Header.Set("X-Forwarded-Host", "attacker.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PreviewAttachmentMarkdown(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewAttachmentMarkdown forwarded host: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPreviewAttachmentMarkdownRejectsOversizedPreview(t *testing.T) {
	attachmentURL := "https://cdn.example.com/workspaces/" + testWorkspaceID + "/oversized.md"
	if _, err := testPool.Exec(
		context.Background(),
		`INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'oversized.md', $3, 'text/markdown', $4)`,
		testWorkspaceID,
		testUserID,
		attachmentURL,
		maxMarkdownPreviewSize+1,
	); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	defer func() {
		if _, err := testPool.Exec(
			context.Background(),
			`DELETE FROM attachment WHERE workspace_id = $1 AND url = $2`,
			testWorkspaceID,
			attachmentURL,
		); err != nil {
			t.Fatalf("cleanup attachment: %v", err)
		}
	}()

	origFetch := fetchMarkdownPreview
	fetchMarkdownPreview = func(_ context.Context, _ string) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: -1,
			Body:          io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), maxMarkdownPreviewSize+1))),
		}, nil
	}
	defer func() { fetchMarkdownPreview = origFetch }()

	req := httptest.NewRequest("GET", "/api/attachments/preview?url="+attachmentURL, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.PreviewAttachmentMarkdown(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("PreviewAttachmentMarkdown oversized body: expected 413, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), strings.Repeat("a", 100)) {
		t.Fatalf("expected error response, got preview body")
	}
}
