package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dbAttachmentForLocalSign builds a synthetic db.Attachment row pointing
// at a /uploads/ URL — used by the LocalStorage signed-URL test to drive
// attachmentToResponse without going through a full upload round-trip.
func dbAttachmentForLocalSign(t *testing.T, key string) db.Attachment {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	wsUUID, err := uuid.Parse(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse testWorkspaceID: %v", err)
	}
	uploaderUUID, err := uuid.Parse(testUserID)
	if err != nil {
		t.Fatalf("parse testUserID: %v", err)
	}
	return db.Attachment{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:  pgtype.UUID{Bytes: wsUUID, Valid: true},
		UploaderType: "member",
		UploaderID:   pgtype.UUID{Bytes: uploaderUUID, Valid: true},
		Filename:     "x.png",
		Url:          "/uploads/" + key,
		ContentType:  "image/png",
		SizeBytes:    4,
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

// TestIsUploadDenied is a pure-function check on the denylist used by
// UploadFile. No DB / handler fixture required — runs in any environment.
func TestIsUploadDenied(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		contentType string
		want        bool
	}{
		// Allowed shapes — these are the everyday legitimate uploads.
		{"png is allowed", "logo.png", "image/png", false},
		{"pdf is allowed", "report.pdf", "application/pdf", false},
		{"plain text is allowed", "notes.txt", "text/plain", false},
		// SVG is allowed at upload time — the SVG-XSS chain is broken
		// at the serve path (Content-Disposition: attachment) and SVG
		// logos / diagrams are a common legitimate upload.
		{"svg is allowed", "logo.svg", "image/svg+xml", false},
		// JS is allowed because source-code attachments preview as
		// text/plain via /api/attachments/{id}/content. Blocking it
		// here would break the preview feature without adding security
		// on top of the disposition fix.
		{"js source upload is allowed", "snippet.js", "application/javascript", false},

		// Denied: HTML family by extension.
		{".html denied", "evil.html", "text/plain", true},
		{".htm denied", "evil.htm", "text/plain", true},
		{".xhtml denied", "evil.xhtml", "text/plain", true},
		{".shtml denied", "evil.shtml", "text/plain", true},
		{".xht denied", "evil.xht", "text/plain", true},
		{".phtml denied", "evil.phtml", "text/plain", true},

		// Denied: HTML by sniffed content type even if extension is benign.
		// This is the renamed-payload case — logo.png that is actually
		// HTML must still be refused.
		{"text/html under image extension", "logo.png", "text/html", true},
		{"text/html with charset param", "logo.png", "text/html; charset=utf-8", true},
		{"application/xhtml+xml", "diagram.svg", "application/xhtml+xml", true},

		// Case-insensitive on extension and content type.
		{"upper-case extension", "evil.HTML", "text/plain", true},
		{"upper-case content-type", "logo.png", "TEXT/HTML", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUploadDenied(tc.filename, tc.contentType); got != tc.want {
				t.Errorf("isUploadDenied(%q, %q) = %v, want %v",
					tc.filename, tc.contentType, got, tc.want)
			}
		})
	}
}

// TestUploadFile_RejectsHTMLByExtension verifies the upload-edge gate fires
// when a caller tries to upload a .html file. Defense-in-depth on top of
// the Content-Disposition: attachment fix from PR #3023.
func TestUploadFile_RejectsHTMLByExtension(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "evil.html")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("<script>alert(1)</script>"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for .html upload, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUploadFile_RejectsHTMLByContentType verifies the sniffer-side gate.
// A caller renames an HTML payload to logo.png — the extension check
// passes, but http.DetectContentType returns "text/html" so the
// content-type denylist refuses the upload.
func TestUploadFile_RejectsHTMLByContentType(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Disguise as PNG — extension passes, content sniffs as text/html.
	part, err := writer.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	// Leading "<!DOCTYPE html" is the strongest text/html sniff signal.
	part.Write([]byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for renamed HTML payload, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUploadFile_AllowsLegitimateImage is a regression guard: the new
// denylist must not start refusing routine image uploads.
func TestUploadFile_AllowsLegitimateImage(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	// Real PNG signature — DetectContentType returns image/png.
	part.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.UploadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for legitimate PNG, got %d: %s", w.Code, w.Body.String())
	}

	// Clean up the attachment row so this test is rerunnable.
	if _, err := testPool.Exec(
		context.Background(),
		`DELETE FROM attachment WHERE workspace_id = $1 AND filename = $2`,
		testWorkspaceID,
		"logo.png",
	); err != nil {
		t.Fatalf("cleanup attachment: %v", err)
	}
}

// TestServeLocalUpload_RequiresAuth verifies the handler refuses a request
// where the upstream Auth middleware did not stamp X-User-ID. Auth is the
// outer gate; this assertion confirms the inner handler does not have a
// "default open" mode if ever reached without it.
func TestServeLocalUpload_RequiresAuth(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/workspaces/"+testWorkspaceID+"/anything.png", nil)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without X-User-ID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestServeLocalUpload_MemberCanRead is the happy path: a workspace member
// hitting their own workspace's upload bytes gets 200 + the file body.
func TestServeLocalUpload_MemberCanRead(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	key := "workspaces/" + testWorkspaceID + "/abc.png"
	if _, err := local.Upload(context.Background(), key, []byte("body-bytes"), "image/png", "logo.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "body-bytes") {
		t.Errorf("body did not match: %q", rec.Body.String())
	}
}

// TestServeLocalUpload_NonMemberDenied verifies that an authenticated user
// hitting a workspace they do NOT belong to gets 404 (not 403, to avoid an
// IDOR oracle that would let them probe for workspace IDs they have no
// business knowing).
func TestServeLocalUpload_NonMemberDenied(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	// Foreign workspace ID — testUserID is not a member.
	foreignWorkspaceID := "00000000-0000-0000-0000-000000000099"
	key := "workspaces/" + foreignWorkspaceID + "/abc.png"
	if _, err := local.Upload(context.Background(), key, []byte("foreign-body"), "image/png", "logo.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member, got %d: %s", rec.Code, rec.Body.String())
	}
	// 404 must not leak the bytes.
	if strings.Contains(rec.Body.String(), "foreign-body") {
		t.Errorf("response body leaked file contents: %q", rec.Body.String())
	}
}

// TestServeLocalUpload_RejectsDirectoryInPath verifies the handler refuses
// requests whose path resolves to a directory or workspace root, even for
// legitimately-authenticated members. This is the disclosure's
// "directory listing" vector applied at the route layer.
func TestServeLocalUpload_RejectsDirectoryInPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}
	// Seed two files so a listing would have something to leak.
	if _, err := local.Upload(context.Background(), "workspaces/"+testWorkspaceID+"/a.png", []byte("a"), "image/png", "a.png"); err != nil {
		t.Fatalf("Upload a: %v", err)
	}
	if _, err := local.Upload(context.Background(), "workspaces/"+testWorkspaceID+"/b.png", []byte("b"), "image/png", "b.png"); err != nil {
		t.Fatalf("Upload b: %v", err)
	}

	cases := []string{
		"/uploads/workspaces/" + testWorkspaceID + "/",
		"/uploads/workspaces/" + testWorkspaceID,
		"/uploads/",
		"/uploads/workspaces/",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-User-ID", testUserID)
			rec := httptest.NewRecorder()
			testHandler.ServeLocalUpload(local)(rec, req)

			if rec.Code == http.StatusOK {
				t.Errorf("status = 200, want 404 (directory request must not return 200)")
			}
			if strings.Contains(rec.Body.String(), "a.png") || strings.Contains(rec.Body.String(), "b.png") {
				t.Errorf("body leaked filenames: %q", rec.Body.String())
			}
		})
	}
}

// TestServeLocalUpload_UnknownPrefixDenied verifies the explicit-allowlist
// behavior: a key prefix the handler doesn't know about must 404 instead
// of falling through to the storage layer with no auth.
func TestServeLocalUpload_UnknownPrefixDenied(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}
	if _, err := local.Upload(context.Background(), "secrets/admin.png", []byte("secret"), "image/png", "x.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/secrets/admin.png", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown prefix, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("body leaked file contents: %q", rec.Body.String())
	}
}

// TestServeLocalUpload_UserPrefixAllowsAnyAuthedUser confirms that the
// /uploads/users/{userID}/* path is reachable by any authenticated user,
// matching the avatar-display use case (member lists / inbox items
// reference avatars across workspace boundaries).
func TestServeLocalUpload_UserPrefixAllowsAnyAuthedUser(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	// Owner-by-someone-else avatar — the testUserID is reading
	// somebody else's avatar bytes.
	otherUserID := "00000000-0000-0000-0000-000000000088"
	key := "users/" + otherUserID + "/avatar.png"
	if _, err := local.Upload(context.Background(), key, []byte("avatar-body"), "image/png", "avatar.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /uploads/users/*, got %d: %s", rec.Code, rec.Body.String())
	}
}


// TestServeLocalUpload_SignedQueryBypassesAuth verifies the new auth path:
// a request that carries valid ?exp=&sig= query params is served WITHOUT
// any X-User-ID header. This is what unblocks token-auth clients (Desktop,
// legacy-token Web, mobile) on inline <img>/<video> resource loads.
func TestServeLocalUpload_SignedQueryBypassesAuth(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	key := "workspaces/" + testWorkspaceID + "/signed.png"
	if _, err := local.Upload(context.Background(), key, []byte("signed-body"), "image/png", "x.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	signed := storage.SignLocalUploadURL("/uploads/"+key, key, auth.JWTSecret(), time.Now().Add(5*time.Minute))
	req := httptest.NewRequest(http.MethodGet, signed, nil)
	// Deliberately NO X-User-ID — proves the signed query is the only
	// authority.
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("signed URL: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "signed-body") {
		t.Errorf("body did not match: %q", rec.Body.String())
	}
}

// TestServeLocalUpload_SignedQueryRejectsExpired verifies that a signed URL
// past its expiry is refused even on the legitimate route. Otherwise leaked
// URLs would last forever.
func TestServeLocalUpload_SignedQueryRejectsExpired(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	key := "workspaces/" + testWorkspaceID + "/expired.png"
	if _, err := local.Upload(context.Background(), key, []byte("body"), "image/png", "x.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	expired := storage.SignLocalUploadURL("/uploads/"+key, key, auth.JWTSecret(), time.Now().Add(-1*time.Minute))
	req := httptest.NewRequest(http.MethodGet, expired, nil)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired signed URL: expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestServeLocalUpload_SignedQueryRejectsTampered confirms that flipping
// any byte in the signature breaks verification.
func TestServeLocalUpload_SignedQueryRejectsTampered(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	key := "workspaces/" + testWorkspaceID + "/tampered.png"
	if _, err := local.Upload(context.Background(), key, []byte("body"), "image/png", "x.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	signed := storage.SignLocalUploadURL("/uploads/"+key, key, auth.JWTSecret(), time.Now().Add(5*time.Minute))
	// Flip a byte in the sig parameter without renormalizing.
	tampered := signed[:len(signed)-1] + "X"

	req := httptest.NewRequest(http.MethodGet, tampered, nil)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered signed URL: expected 401, got %d", rec.Code)
	}
}

// TestServeLocalUpload_SignedQueryBoundToOneKey is the IDOR check: a sig
// minted for key A must not authorize a request for key B even when both
// belong to the same workspace.
func TestServeLocalUpload_SignedQueryBoundToOneKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	keyA := "workspaces/" + testWorkspaceID + "/a.png"
	keyB := "workspaces/" + testWorkspaceID + "/b.png"
	if _, err := local.Upload(context.Background(), keyA, []byte("body-a"), "image/png", "a.png"); err != nil {
		t.Fatalf("Upload a: %v", err)
	}
	if _, err := local.Upload(context.Background(), keyB, []byte("body-b"), "image/png", "b.png"); err != nil {
		t.Fatalf("Upload b: %v", err)
	}

	// Sign for A, request B with A's signature.
	signed := storage.SignLocalUploadURL("/uploads/"+keyA, keyA, auth.JWTSecret(), time.Now().Add(5*time.Minute))
	parts := strings.SplitN(signed, "?", 2)
	if len(parts) != 2 {
		t.Fatalf("signed URL has no query: %s", signed)
	}
	bWithASig := "/uploads/" + keyB + "?" + parts[1]

	req := httptest.NewRequest(http.MethodGet, bWithASig, nil)
	rec := httptest.NewRecorder()
	testHandler.ServeLocalUpload(local)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cross-key sig: expected 401, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "body-b") {
		t.Errorf("body leaked: %q", rec.Body.String())
	}
}

// TestServeLocalUpload_PartialSignedQueryFailsClosed: if exactly one of
// exp or sig is present, the handler must reject rather than fall back to
// "no signed query attempted."
func TestServeLocalUpload_PartialSignedQueryFailsClosed(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	local := storage.NewLocalStorageFromEnv()
	if local == nil {
		t.Fatal("NewLocalStorageFromEnv returned nil")
	}

	key := "workspaces/" + testWorkspaceID + "/partial.png"
	if _, err := local.Upload(context.Background(), key, []byte("body"), "image/png", "x.png"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	cases := []string{
		"/uploads/" + key + "?exp=1700000000",
		"/uploads/" + key + "?sig=AAAA",
	}
	for _, urlStr := range cases {
		t.Run(urlStr, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, urlStr, nil)
			// Note: NOT setting X-User-ID. The handler reads
			// signed-query first; partial sig must fail-closed
			// here, not fall through to "user is unauthenticated"
			// which would also be 401 but for a different reason.
			rec := httptest.NewRecorder()
			testHandler.ServeLocalUpload(local)(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("partial signed query: expected 401, got %d", rec.Code)
			}
		})
	}
}

// TestAttachmentToResponse_LocalStorageMintsSignedURL is the integration
// check: when the storage backend is LocalStorage, the URL surfaced in
// the JSON response carries valid exp/sig query params. Without this
// every <img src=attachment.url> in token-auth clients would 401.
func TestAttachmentToResponse_LocalStorageMintsSignedURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	tmpDir := t.TempDir()
	t.Setenv("LOCAL_UPLOAD_DIR", tmpDir)
	origStorage := testHandler.Storage
	testHandler.Storage = storage.NewLocalStorageFromEnv()
	defer func() { testHandler.Storage = origStorage }()

	// Build a synthetic Attachment row with a /uploads/ URL.
	att := dbAttachmentForLocalSign(t, "workspaces/"+testWorkspaceID+"/abc.png")
	resp := testHandler.attachmentToResponse(att)

	if !strings.Contains(resp.URL, "exp=") || !strings.Contains(resp.URL, "sig=") {
		t.Fatalf("LocalStorage URL did not carry signed-query params: %s", resp.URL)
	}
	u, err := url.Parse(resp.URL)
	if err != nil {
		t.Fatalf("parse resp.URL: %v", err)
	}
	exp, sig := storage.LocalUploadSignatureFromQuery(u.Query())
	if !storage.VerifyLocalUploadSignature("workspaces/"+testWorkspaceID+"/abc.png", exp, sig, auth.JWTSecret(), time.Now()) {
		t.Errorf("minted URL did not verify: %s", resp.URL)
	}
}
