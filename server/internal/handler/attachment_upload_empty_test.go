package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Zero-byte attachments and the presigned path.
//
// A SigV4 presigned PUT binds Content-Length only when the value is non-zero:
// against a real bucket, a credential issued for size 0 carried
// `X-Amz-SignedHeaders=host` alone and stored a 120 MiB object. The length check
// in complete-upload cannot repair that, because it runs after the bytes are
// already in the bucket — and a staging object stays writable by whoever holds
// the URL until it expires.
//
// So the capability is withdrawn for that one size, at the API boundary and in
// the storage backend, while an empty file stays uploadable: the client falls
// back to the multipart path, exactly as it already does for a backend that
// cannot presign at all.

// presignUploadStatus issues a presign request for an arbitrary body and
// returns the recorder, so a test can assert on a refusal as well as a success.
func presignUploadStatus(t *testing.T, req presignUploadRequest, userID, workspaceID string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal presign request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/attachments/presign-upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", userID)
	r.Header.Set("X-Workspace-ID", workspaceID)
	w := httptest.NewRecorder()
	testHandler.PresignAttachmentUpload(w, r)
	return w
}

// uploadSideEffects is everything one presign request could create or ask a
// storage backend to do. A capability refusal must leave every field at zero.
type uploadSideEffects struct {
	presignedKeys  []string
	sessionRows    int
	attachmentRows int
}

func captureUploadSideEffects(t *testing.T, store *directUploadStorage, issueID string) uploadSideEffects {
	t.Helper()
	var effects uploadSideEffects
	store.mu.Lock()
	effects.presignedKeys = append([]string(nil), store.presignedKeys...)
	store.mu.Unlock()
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment_upload_session WHERE issue_id = $1`, issueID).Scan(&effects.sessionRows); err != nil {
		t.Fatalf("count upload sessions: %v", err)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment WHERE issue_id = $1`, issueID).Scan(&effects.attachmentRows); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	return effects
}

func (e uploadSideEffects) assertNone(t *testing.T) {
	t.Helper()
	if len(e.presignedKeys) != 0 {
		t.Fatalf("a credential was requested for %v; an unsupported upload must not reach the presigner", e.presignedKeys)
	}
	if e.sessionRows != 0 {
		t.Fatalf("an upload session row was created (%d); the refusal must be side-effect free", e.sessionRows)
	}
	if e.attachmentRows != 0 {
		t.Fatalf("an attachment row was created (%d); the refusal must be side-effect free", e.attachmentRows)
	}
}

// TestPresignAttachmentUpload_ZeroByteRefusedWithoutSideEffects is the defect
// this stage exists for: an explicit size_bytes=0 must not produce a credential,
// and must be refused before anything is created.
func TestPresignAttachmentUpload_ZeroByteRefusedWithoutSideEffects(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	w := presignUploadStatus(t, presignUploadRequest{
		Filename: "empty.txt", ContentType: "text/plain", SizeBytes: 0, IssueID: issueID,
	}, testUserID, testWorkspaceID)

	// 501 with the capability code is the only signal a client treats as "this
	// path cannot take this file, use the other one". A 400 would strand the
	// upload: no client falls back on a bad-request, and empty uploads would
	// simply stop working.
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Code      string `json:"code"`
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	if payload.Code != codeUploadPresignUnsupported {
		t.Fatalf("code = %q, want %q", payload.Code, codeUploadPresignUnsupported)
	}
	// No credential of any kind may appear in a refusal: the point is that none
	// exists.
	if payload.UploadURL != "" || strings.Contains(w.Body.String(), "X-Amz-Signature") {
		t.Fatalf("the refusal carried an upload credential: %s", w.Body.String())
	}

	captureUploadSideEffects(t, store, issueID).assertNone(t)
}

// TestPresignAttachmentUpload_ZeroBytePermissionChecksStillRun guards the order
// of the checks. Withdrawing a capability is not a reason to skip
// authorization: a caller who may not upload here at all must be told so, and
// the refusal must be the authorization one, or the endpoint becomes an oracle
// for which targets exist.
func TestPresignAttachmentUpload_ZeroBytePermissionChecksStillRun(t *testing.T) {
	store := installDirectUploadStorage(t)
	f := newPrivateProjectFixture(t)

	t.Run("member outside a private project", func(t *testing.T) {
		w := presignUploadStatus(t, presignUploadRequest{
			Filename: "empty.txt", ContentType: "text/plain", SizeBytes: 0, IssueID: f.secretIssue,
		}, f.outsider, testWorkspaceID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 before any capability refusal: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), codeUploadPresignUnsupported) {
			t.Fatalf("an unauthorized caller was answered with the capability code: %s", w.Body.String())
		}
	})

	t.Run("user outside the workspace", func(t *testing.T) {
		stranger := dbfx.Insert(t, "user", testutil.Cols{
			"name": "Stranger", "email": "stranger-zero@multica.ai",
		})
		w := presignUploadStatus(t, presignUploadRequest{
			Filename: "empty.txt", ContentType: "text/plain", SizeBytes: 0, IssueID: f.secretIssue,
		}, stranger, testWorkspaceID)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
		}
	})

	if len(store.presignedKeys) != 0 {
		t.Fatalf("a refused caller still asked for a credential: %v", store.presignedKeys)
	}
}

// TestPresignAttachmentUpload_ZeroByteInputValidationStillRuns pins the other
// half of the ordering: a malformed request is refused as malformed, not as an
// unsupported size. Otherwise a client sending a bad filename alongside an empty
// file would fall back to a multipart request the server also rejects, and the
// real error would never surface.
func TestPresignAttachmentUpload_ZeroByteInputValidationStillRuns(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	cases := []struct {
		name string
		req  presignUploadRequest
	}{
		{"path in filename", presignUploadRequest{Filename: "../escape.txt", ContentType: "text/plain", SizeBytes: 0, IssueID: issueID}},
		{"missing content type", presignUploadRequest{Filename: "empty.txt", SizeBytes: 0, IssueID: issueID}},
		{"malformed content type", presignUploadRequest{Filename: "empty.txt", ContentType: "not a media type", SizeBytes: 0, IssueID: issueID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := presignUploadStatus(t, tc.req, testUserID, testWorkspaceID)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
	if len(store.presignedKeys) != 0 {
		t.Fatalf("a malformed request asked for a credential: %v", store.presignedKeys)
	}
}

// TestPresignAttachmentUpload_SmallestNonEmptyFileKeepsItsBinding is the
// counterweight: the refusal is about a length that cannot be signed, not about
// small files. One byte still yields a length- and type-bound credential.
func TestPresignAttachmentUpload_SmallestNonEmptyFileKeepsItsBinding(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	presign := presignUpload(t, presignUploadRequest{
		Filename: "tiny.txt", ContentType: "text/plain", SizeBytes: 1, IssueID: issueID,
	}, testUserID, testWorkspaceID)

	if presign.UploadURL == "" {
		t.Fatal("a one-byte upload produced no URL")
	}
	if got := presign.Headers["Content-Length"]; got != "1" {
		t.Fatalf("Content-Length header = %q, want 1: the signed length must reach the client", got)
	}
	if got := presign.Headers["Content-Type"]; !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type header = %q, want the declared type", got)
	}
	if len(store.presignedKeys) != 1 {
		t.Fatalf("presigned keys = %v, want exactly one staging key", store.presignedKeys)
	}
}

// TestPresignAttachmentUpload_BackendCapabilityRefusalMapsTo501 covers the
// storage layer's own refusal. Even with the handler's size gate in place, a
// backend that declines must surface as the same machine-readable capability
// code — never as a 502, which reads as a transient server fault and leaves the
// client retrying instead of falling back.
func TestPresignAttachmentUpload_BackendCapabilityRefusalMapsTo501(t *testing.T) {
	store := installDirectUploadStorage(t)
	store.presignErr = fmt.Errorf("s3 PresignPut: %w", storage.ErrUnsupported)
	t.Cleanup(func() { store.presignErr = nil })
	issueID := seedIssueForUpload(t)

	w := presignUploadStatus(t, presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5, IssueID: issueID,
	}, testUserID, testWorkspaceID)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), codeUploadPresignUnsupported) {
		t.Fatalf("body = %s, want code %q", w.Body.String(), codeUploadPresignUnsupported)
	}
	// Nothing was recorded: the session row is written only after a credential
	// exists, so a refusal cannot leave a pending row behind.
	captureUploadSideEffects(t, store, issueID).assertNone(t)
}

// TestPresignAttachmentUpload_RealS3BackendRefusalIsNotAGatewayError drives the
// product backend's own rule through the handler, with a wrapper that narrows
// the declared size to 0 after the handler's gate has run: that is exactly the
// shape of a backend declining a request the handler thought was fine, and it is
// what must reach the client as the capability code rather than a 502. The
// endpoint is a closed port, so a request that got as far as the network would
// surface as a 502.
func TestPresignAttachmentUpload_RealS3BackendRefusalIsNotAGatewayError(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &zeroLengthBackend{Storage: newZeroLengthRefusingStorage(t)}
	t.Cleanup(func() { testHandler.Storage = origStorage })
	issueID := seedIssueForUpload(t)

	w := presignUploadStatus(t, presignUploadRequest{
		Filename: "declined.txt", ContentType: "text/plain", SizeBytes: 1, IssueID: issueID,
	}, testUserID, testWorkspaceID)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (not a 502): %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), codeUploadPresignUnsupported) {
		t.Fatalf("body = %s, want code %q", w.Body.String(), codeUploadPresignUnsupported)
	}
	// A declined credential leaves no session behind, so nothing has to be
	// reconciled later.
	var sessions int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment_upload_session WHERE issue_id = $1`, issueID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("a declined upload left %d session row(s)", sessions)
	}
}

// zeroLengthBackend forwards everything to a real Storage but hands the
// presigner a zero length regardless of what the handler declared, standing in
// for a backend that declines a request the handler accepted.
type zeroLengthBackend struct {
	storage.Storage
}

func (z *zeroLengthBackend) PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string, contentLength int64) (string, map[string]string, error) {
	_ = contentLength
	presigner, ok := z.Storage.(storage.UploadPresigner)
	if !ok {
		return "", nil, storage.ErrUnsupported
	}
	return presigner.PresignPut(ctx, key, ttl, contentType, 0)
}

func (z *zeroLengthBackend) HeadObject(ctx context.Context, key string) (storage.ObjectInfo, error) {
	return z.Storage.(storage.ObjectInspector).HeadObject(ctx, key)
}

func (z *zeroLengthBackend) ReadPrefix(ctx context.Context, key string, n int, etag string) ([]byte, error) {
	return z.Storage.(storage.ObjectInspector).ReadPrefix(ctx, key, n, etag)
}

func (z *zeroLengthBackend) FinalizeObject(ctx context.Context, srcKey, srcETag, dstKey string, expectedSize int64, contentType, contentDisposition, cacheControl string) error {
	return z.Storage.(storage.ObjectFinalizer).FinalizeObject(ctx, srcKey, srcETag, dstKey, expectedSize, contentType, contentDisposition, cacheControl)
}

// insertEmptyUploadSession writes the row an OLD server produced for a
// zero-byte upload, so the completion path can be exercised for the sessions
// already present in deployed databases. New requests cannot create one.
func insertEmptyUploadSession(t *testing.T, issueID, filename string) string {
	t.Helper()
	sessionUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate session id: %v", err)
	}
	sessionID := sessionUUID.String()

	dbfx.Insert(t, "attachment_upload_session", testutil.Cols{
		"id":                    sessionID,
		"workspace_id":          testWorkspaceID,
		"actor_type":            "member",
		"actor_id":              testUserID,
		"actor_user_id":         testUserID,
		"issue_id":              issueID,
		"filename":              filename,
		"declared_content_type": "text/plain",
		"expected_size":         0,
		"staging_key":           uploadStagingPrefix + "/" + testWorkspaceID + "/" + sessionID,
		"final_key":             attachmentObjectKey(testWorkspaceID, testUserID, sessionID, filename),
		"expires_at":            time.Now().Add(time.Hour),
	})
	return sessionID
}

// stagingKeyOf reads the staging key a session carries, so a test can PUT to it
// without reconstructing the naming rule.
func stagingKeyOf(t *testing.T, sessionID string) string {
	t.Helper()
	var key string
	if err := testPool.QueryRow(context.Background(),
		`SELECT staging_key FROM attachment_upload_session WHERE id = $1`, sessionID).Scan(&key); err != nil {
		t.Fatalf("read staging key: %v", err)
	}
	return key
}

// TestCompleteSucceedsForVerifiedEmptyObject covers the compatibility half: a
// zero-byte session created before the capability was withdrawn must still
// complete, and must still publish an empty object with the multipart path's
// MIME semantics. The session is inserted directly, because new requests can no
// longer produce one.
func TestCompleteSucceedsForVerifiedEmptyObject(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	sessionID := insertEmptyUploadSession(t, issueID, "empty.txt")
	store.putStagingObject(stagingKeyOf(t, sessionID), nil, "text/plain")

	w := completeUpload(t, sessionID, testUserID, testWorkspaceID)
	if w.Code != http.StatusOK {
		t.Fatalf("empty upload completion = %d, want 200: %s", w.Code, w.Body.String())
	}
	var att AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &att); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if att.ID != sessionID {
		t.Fatalf("attachment id = %q, want the session id %q", att.ID, sessionID)
	}
	if att.SizeBytes != 0 {
		t.Fatalf("size_bytes = %d, want 0", att.SizeBytes)
	}
	// The MIME semantics must survive: text/plain for empty content, matching
	// the multipart path, never an empty string.
	if !strings.HasPrefix(att.ContentType, "text/plain") {
		t.Fatalf("content_type = %q, want text/plain", att.ContentType)
	}
	// The response keeps the whole attachment contract, so a client treats an
	// empty direct upload exactly like any other.
	if att.MarkdownURL == "" || att.DownloadURL == "" {
		t.Fatalf("response is missing markdown_url/download_url: %+v", att)
	}
	// The object must be published at the final key with the same (zero) length.
	finalKey := attachmentObjectKey(testWorkspaceID, testUserID, sessionID, "empty.txt")
	published, ok := store.object(finalKey)
	if !ok || len(published.body) != 0 {
		t.Fatalf("final object missing or non-empty: exists=%v len=%d", ok, len(published.body))
	}

	// The replay a client makes after a lost response must return the same
	// attachment without copying again.
	copiesBefore := store.finalizeCalls
	replay := completeUpload(t, sessionID, testUserID, testWorkspaceID)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay = %d, want 200: %s", replay.Code, replay.Body.String())
	}
	var replayed AttachmentResponse
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replayed.ID != att.ID || replayed.SizeBytes != 0 {
		t.Fatalf("replay returned %+v, want the same empty attachment %q", replayed, att.ID)
	}
	if store.finalizeCalls != copiesBefore {
		t.Fatalf("replay re-copied the object (%d finalizations, want %d)", store.finalizeCalls, copiesBefore)
	}
}

// TestCompleteAttachmentUpload_EmptySessionStillEnforcesChecks proves the
// compatibility path is not a hole: existence, size and access are still
// verified for a zero-byte session, because those sessions stay reachable with
// a credential issued before the fix.
func TestCompleteAttachmentUpload_EmptySessionStillEnforcesChecks(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	t.Run("object missing", func(t *testing.T) {
		sessionID := insertEmptyUploadSession(t, issueID, "absent.txt")
		w := completeUpload(t, sessionID, testUserID, testWorkspaceID)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadObjectMissing) {
			t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadObjectMissing)
		}
	})

	t.Run("object larger than the declared zero", func(t *testing.T) {
		sessionID := insertEmptyUploadSession(t, issueID, "padded.txt")
		// The very abuse the fix exists for: a credential issued for nothing,
		// used to store bytes. Complete-upload's own check must still catch it
		// for sessions that already exist.
		store.putStagingObject(stagingKeyOf(t, sessionID), []byte("smuggled bytes"), "text/plain")
		w := completeUpload(t, sessionID, testUserID, testWorkspaceID)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadSizeMismatch) {
			t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadSizeMismatch)
		}
	})

	t.Run("other actor", func(t *testing.T) {
		sessionID := insertEmptyUploadSession(t, issueID, "owned.txt")
		store.putStagingObject(stagingKeyOf(t, sessionID), nil, "text/plain")
		otherUser := dbfx.Insert(t, "user", testutil.Cols{
			"name": "Empty Other", "email": "empty-other@multica.ai",
		})
		dbfx.InsertNoID(t, "member", testutil.Cols{
			"workspace_id": testWorkspaceID, "user_id": otherUser, "role": "member",
		}, "workspace_id = $1 AND user_id = $2", testWorkspaceID, otherUser)
		w := completeUpload(t, sessionID, otherUser, testWorkspaceID)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
		}
	})
}

// TestEmptyFileUploadsThroughMultipartFallback keeps the product promise:
// withdrawing the direct path for empty files must not withdraw empty uploads.
// The legacy endpoint is where the client lands, so it has to publish an empty
// attachment carrying the same contract.
func TestEmptyFileUploadsThroughMultipartFallback(t *testing.T) {
	legacyStore := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = legacyStore
	t.Cleanup(func() { testHandler.Storage = origStorage })

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// The empty file part is the case under test: no bytes are written.
	if _, err := writer.CreateFormFile("file", "empty.txt"); err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.UploadFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("legacy empty-file upload = %d: %s", rec.Code, rec.Body.String())
	}
	var att AttachmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &att); err != nil {
		t.Fatalf("decode legacy response: %v", err)
	}
	if att.SizeBytes != 0 {
		t.Fatalf("legacy size_bytes = %d, want 0", att.SizeBytes)
	}
	if att.ID == "" || att.MarkdownURL == "" || att.DownloadURL == "" {
		t.Fatalf("legacy empty-file response is missing the attachment contract: %+v", att)
	}
	if !strings.HasPrefix(att.ContentType, "text/plain") {
		t.Fatalf("legacy content_type = %q, want text/plain", att.ContentType)
	}
}

// TestIsUnsupportedUploadError pins the translation the handler depends on: a
// capability refusal wrapped by the storage backend is recognised, and anything
// else — a cancellation, a transport fault — is not, so a real failure is never
// hidden behind a fallback.
func TestIsUnsupportedUploadError(t *testing.T) {
	if !isUnsupportedUploadError(storage.ErrUnsupported) {
		t.Fatal("ErrUnsupported is not recognised as a capability refusal")
	}
	if !isUnsupportedUploadError(fmt.Errorf("s3 PresignPut: %w", storage.ErrUnsupported)) {
		t.Fatal("a wrapped ErrUnsupported is not recognised as a capability refusal")
	}
	for _, other := range []error{
		context.Canceled,
		storage.ErrObjectNotFound,
		errors.New("SignatureDoesNotMatch"),
	} {
		if isUnsupportedUploadError(other) {
			t.Fatalf("%v was misread as a capability refusal", other)
		}
	}
}

// newZeroLengthRefusingStorage builds the product backend (not a test double)
// against a closed port: a zero length must be refused before the store is
// contacted, which is what makes the storage-level rule real rather than
// incidental. No real credentials are used.
func newZeroLengthRefusingStorage(t *testing.T) storage.Storage {
	t.Helper()
	t.Setenv("S3_BUCKET", "unit-test-bucket")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("AWS_ENDPOINT_URL", "http://127.0.0.1:1")
	t.Setenv("S3_USE_PATH_STYLE", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "unit-test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "unit-test")
	s := storage.NewS3StorageFromEnv()
	if s == nil {
		t.Fatal("S3 storage was not configured")
	}
	return s
}
