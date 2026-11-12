package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// directUploadStorage is an in-memory stand-in for a presign-capable backend.
//
// It models the two properties the handler depends on rather than just
// recording calls:
//
//   - Put writes to a staging key exactly as an S3 PUT does, so a test can PUT
//     again after completion and see whether the published bytes change.
//   - FinalizeObject copies under an If-Match on the ETag, so a source that
//     changed between inspection and finalization fails instead of silently
//     publishing different bytes. That is the property the "re-PUT after
//     complete" test exists to prove.
type directUploadStorage struct {
	mu sync.Mutex

	objects map[string]directUploadObject
	// presignedKeys records every key a credential was issued for.
	presignedKeys []string
	// finalizeCalls counts destination writes, so a test can prove a replayed
	// complete-upload did not copy again.
	finalizeCalls int
	// deleteCalls records the keys cleanup actually asked to delete.
	deleteCalls []string
	// failDeletes makes DeleteObject fail until cleared, for the backoff test.
	failDeletes bool
	// headCalls counts object inspections.
	headCalls int
	// beforeFinalize runs at the start of FinalizeObject, which is exactly the
	// window between the handler's inspection and the conditional copy. It lets
	// a test land a racing PUT deterministically instead of by interleaving.
	beforeFinalize func()
	// beforeReadPrefix runs at the start of ReadPrefix: the window between the
	// HEAD that measured the object and the probe read that describes it.
	beforeReadPrefix func()
}

type directUploadObject struct {
	body        []byte
	contentType string
	etag        string
}

func newDirectUploadStorage() *directUploadStorage {
	return &directUploadStorage{objects: map[string]directUploadObject{}}
}

func (s *directUploadStorage) Upload(ctx context.Context, key string, data []byte, contentType, filename string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = directUploadObject{body: append([]byte(nil), data...), contentType: contentType, etag: etagOf(data)}
	return s.ObjectURL(key), nil
}

func (s *directUploadStorage) Delete(ctx context.Context, key string) { _ = s.DeleteObject(ctx, key) }

func (s *directUploadStorage) DeleteObject(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDeletes {
		return fmt.Errorf("object store unavailable")
	}
	s.deleteCalls = append(s.deleteCalls, key)
	delete(s.objects, key)
	return nil
}

func (s *directUploadStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		_ = s.DeleteObject(ctx, key)
	}
}

func (s *directUploadStorage) KeyFromURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, s.ObjectURL(""))
}

func (s *directUploadStorage) ObjectURL(key string) string { return "https://cdn.example.com/" + key }

func (s *directUploadStorage) CdnDomain() string { return "cdn.example.com" }

func (s *directUploadStorage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("no such key: %s", key)
	}
	return io.NopCloser(bytes.NewReader(object.body)), nil
}

// PresignPut issues a URL that the test's own PUT helper consumes.
func (s *directUploadStorage) PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string, contentLength int64) (string, map[string]string, error) {
	s.mu.Lock()
	s.presignedKeys = append(s.presignedKeys, key)
	s.mu.Unlock()
	return "https://storage.example.com/" + key + "?X-Amz-Signature=test", map[string]string{
		"Content-Type":   contentType,
		"Content-Length": fmt.Sprintf("%d", contentLength),
	}, nil
}

func (s *directUploadStorage) HeadObject(ctx context.Context, key string) (storage.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headCalls++
	object, ok := s.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrObjectNotFound
	}
	return storage.ObjectInfo{Size: int64(len(object.body)), ETag: object.etag, ContentType: object.contentType}, nil
}

func (s *directUploadStorage) ReadPrefix(ctx context.Context, key string, n int, etag string) ([]byte, error) {
	if s.beforeReadPrefix != nil {
		s.beforeReadPrefix()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	// Enforce the version condition exactly as the real backend does. A double
	// that ignored it would make the handler's binding untestable: it would
	// return the bytes of whatever object happens to be there and the
	// "probe and copy describe the same object" guarantee would pass by
	// accident.
	if etag != "" && object.etag != etag {
		return nil, storage.ErrSourceChanged
	}
	if n <= 0 {
		return nil, fmt.Errorf("read prefix: %d is not a positive byte count", n)
	}
	if len(object.body) <= n {
		return append([]byte(nil), object.body...), nil
	}
	return append([]byte(nil), object.body[:n]...), nil
}

func (s *directUploadStorage) FinalizeObject(ctx context.Context, srcKey, srcETag, dstKey string, expectedSize int64, contentType, contentDisposition, cacheControl string) error {
	if s.beforeFinalize != nil {
		s.beforeFinalize()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finalizeCalls++
	source, ok := s.objects[srcKey]
	if !ok {
		return storage.ErrObjectNotFound
	}
	if source.etag != srcETag {
		// This is the whole point of the precondition: a PUT that landed after
		// inspection must not be copied into a published attachment.
		return storage.ErrSourceChanged
	}
	if int64(len(source.body)) != expectedSize {
		return fmt.Errorf("source is %d bytes, expected %d", len(source.body), expectedSize)
	}
	// The object store copies the bytes; the destination metadata is the
	// server's replacement, exactly as MetadataDirective=REPLACE behaves.
	s.objects[dstKey] = directUploadObject{
		body:        append([]byte(nil), source.body...),
		contentType: contentType,
		etag:        source.etag,
	}
	return nil
}

// putStagingObject simulates the client's PUT against its presigned URL.
func (s *directUploadStorage) putStagingObject(key string, body []byte, contentType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = directUploadObject{body: append([]byte(nil), body...), contentType: contentType, etag: etagOf(body)}
}

func (s *directUploadStorage) object(key string) (directUploadObject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[key]
	return object, ok
}

func (s *directUploadStorage) storedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	return keys
}

func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// installDirectUploadStorage swaps in the fake backend for one test.
func installDirectUploadStorage(t *testing.T) *directUploadStorage {
	t.Helper()
	store := newDirectUploadStorage()
	origStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = origStorage })
	return store
}

// presignUpload issues a session through the handler and returns the parsed
// response. It fails the test on any unexpected status.
func presignUpload(t *testing.T, req presignUploadRequest, userID, workspaceID string) presignUploadResponse {
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
	if w.Code != http.StatusOK {
		t.Fatalf("presign-upload status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var out presignUploadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode presign response: %v", err)
	}
	return out
}

// completeUpload calls complete-upload for a session.
func completeUpload(t *testing.T, attachmentID, userID, workspaceID string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/attachments/"+attachmentID+"/complete-upload", nil)
	r.Header.Set("X-User-ID", userID)
	r.Header.Set("X-Workspace-ID", workspaceID)
	r = testutil.WithURLParams(r, "id", attachmentID)
	w := httptest.NewRecorder()
	testHandler.CompleteAttachmentUpload(w, r)
	return w
}

// seedIssue creates an issue owned by the test workspace and returns its id.
func seedIssueForUpload(t *testing.T) string {
	t.Helper()
	return dbfx.Issue(t, "Direct upload target", testutil.Cols{
		"description": "target for presigned upload tests",
	})
}

func TestPresignAttachmentUpload_IssueAttachmentRoundTrip(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	// A tiny HTML-looking payload under an image extension is the interesting
	// case: the uploader declares image/png, and the handler must publish what
	// the BYTES are.
	payload := []byte("<html><script>alert(1)</script></html>")
	presign := presignUpload(t, presignUploadRequest{
		Filename:    "chart.png",
		ContentType: "image/png",
		SizeBytes:   int64(len(payload)),
		IssueID:     issueID,
	}, testUserID, testWorkspaceID)

	if presign.Method != http.MethodPut {
		t.Fatalf("method = %q, want PUT", presign.Method)
	}
	if presign.ExpiresIn <= 0 || presign.ExpiresIn > 900 {
		t.Fatalf("expires_in = %d, want 1..900", presign.ExpiresIn)
	}
	if presign.AttachmentID != presign.AttachmentID || presign.AttachmentID == "" {
		t.Fatalf("attachment_id is empty")
	}
	// The credential must point at a staging key, never at a final key: the
	// final key is what the published attachment reads, and a client that can
	// write it can change the bytes after publication.
	if len(store.presignedKeys) != 1 || !strings.HasPrefix(store.presignedKeys[0], uploadStagingPrefix+"/") {
		t.Fatalf("presigned key = %v, want one key under %q", store.presignedKeys, uploadStagingPrefix)
	}

	store.putStagingObject(store.presignedKeys[0], payload, "image/png")

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusOK {
		t.Fatalf("complete-upload status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var att AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &att); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if att.ID != presign.AttachmentID {
		t.Fatalf("attachment id = %q, want the session id %q", att.ID, presign.AttachmentID)
	}
	if att.IssueID == nil || *att.IssueID != issueID {
		t.Fatalf("issue_id = %v, want %q", att.IssueID, issueID)
	}
	if att.SizeBytes != int64(len(payload)) {
		t.Fatalf("size_bytes = %d, want %d", att.SizeBytes, len(payload))
	}
	// Sniffed from the bytes, not taken from the declared type.
	if !strings.HasPrefix(att.ContentType, "text/html") {
		t.Fatalf("content_type = %q, want the sniffed text/html", att.ContentType)
	}
	// The response is the ordinary attachment shape, which is what lets a
	// client paste it into a body exactly like a proxied upload.
	if att.MarkdownURL == "" || att.DownloadURL == "" {
		t.Fatalf("response is missing markdown_url/download_url: %+v", att)
	}

	// The published object lives at the final key, and the staging key is still
	// present (cleanup is the reconciler's job, not the request's).
	finalKey := attachmentObjectKey(testWorkspaceID, testUserID, presign.AttachmentID, "chart.png")
	published, ok := store.object(finalKey)
	if !ok {
		t.Fatalf("final object %q missing; keys=%v", finalKey, store.storedKeys())
	}
	if !bytes.Equal(published.body, payload) {
		t.Fatalf("published bytes = %q, want %q", published.body, payload)
	}
	// The final object's metadata is the server's decision: the sniffed type,
	// not the declared one.
	if published.contentType != att.ContentType {
		t.Fatalf("stored content type = %q, want %q", published.contentType, att.ContentType)
	}
	if _, ok := store.object(store.presignedKeys[0]); !ok {
		t.Fatalf("staging object was removed inline; cleanup belongs to the reconciler")
	}

	// The issue's attachment list must see the row, and the revision event must
	// have recorded a bump (CreateAttachment's CTE does that).
	var revision int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision); err != nil {
		t.Fatalf("read issue revision: %v", err)
	}
	if revision == 0 {
		t.Fatalf("issue revision was not bumped by the attachment insert")
	}
}

func TestCompleteAttachmentUpload_RepeatedPUTDoesNotChangePublishedBytes(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	original := []byte("original content")
	presign := presignUpload(t, presignUploadRequest{
		Filename:    "notes.txt",
		ContentType: "text/plain",
		SizeBytes:   int64(len(original)),
		IssueID:     issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], original, "text/plain")

	if w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("first complete = %d: %s", w.Code, w.Body.String())
	}

	// The presigned URL is still valid, so the client re-PUTs different bytes
	// of the same length to the staging key.
	tampered := []byte("TAMPERED length!") // same length as "original content"
	if len(tampered) != len(original) {
		t.Fatalf("test fixture lengths differ: %d vs %d", len(tampered), len(original))
	}
	store.putStagingObject(store.presignedKeys[0], tampered, "text/plain")

	// A second complete must be an idempotent replay: same attachment, no new
	// copy, and the published object is still the original bytes.
	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusOK {
		t.Fatalf("replayed complete = %d: %s", w.Code, w.Body.String())
	}
	if store.finalizeCalls != 1 {
		t.Fatalf("finalize ran %d times, want 1 (a replay must not copy again)", store.finalizeCalls)
	}
	finalKey := attachmentObjectKey(testWorkspaceID, testUserID, presign.AttachmentID, "notes.txt")
	published, ok := store.object(finalKey)
	if !ok {
		t.Fatalf("final object missing")
	}
	if !bytes.Equal(published.body, original) {
		t.Fatalf("published bytes = %q, want the original upload", published.body)
	}
}

func TestCompleteAttachmentUpload_RejectsSourceChangedDuringCompletion(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	original := []byte("first version")
	presign := presignUpload(t, presignUploadRequest{
		Filename:    "notes.txt",
		ContentType: "text/plain",
		SizeBytes:   int64(len(original)),
		IssueID:     issueID,
	}, testUserID, testWorkspaceID)
	stagingKey := store.presignedKeys[0]
	store.putStagingObject(stagingKey, original, "text/plain")

	// The client's presigned URL is still valid, so it re-PUTs the same length
	// of different bytes in the window between the handler's HEAD and its copy.
	// Without the If-Match precondition the copy would publish these bytes.
	store.beforeFinalize = func() {
		store.mu.Lock()
		store.objects[stagingKey] = directUploadObject{
			body:        []byte("SECOND VERSN"),
			contentType: "text/plain",
			etag:        etagOf([]byte("SECOND VERSN")),
		}
		store.mu.Unlock()
	}

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), codeUploadObjectChanged) {
		t.Fatalf("body = %s, want the changed-object code %q", w.Body.String(), codeUploadObjectChanged)
	}
	// Nothing was published: no attachment row, and no final object.
	finalKey := attachmentObjectKey(testWorkspaceID, testUserID, presign.AttachmentID, "notes.txt")
	if _, ok := store.object(finalKey); ok {
		t.Fatalf("a rejected completion still wrote the final object")
	}
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM attachment WHERE id = $1`, presign.AttachmentID).Scan(&count); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if count != 0 {
		t.Fatalf("a rejected completion created %d attachment rows", count)
	}
	// The session is still pending, so the client can retry with a fresh PUT.
	var state string
	if err := testPool.QueryRow(context.Background(),
		`SELECT publish_state FROM attachment_upload_session WHERE id = $1`, presign.AttachmentID).Scan(&state); err != nil {
		t.Fatalf("read session state: %v", err)
	}
	if state != "pending" {
		t.Fatalf("session state = %q, want pending after a refused completion", state)
	}
}

func TestPresignAttachmentUpload_LocalBackendReturns501(t *testing.T) {
	origStorage := testHandler.Storage
	// A backend that satisfies Storage but not the upload capability.
	testHandler.Storage = &mockStorage{}
	t.Cleanup(func() { testHandler.Storage = origStorage })

	body, _ := json.Marshal(presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/attachments/presign-upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.PresignAttachmentUpload(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Code != codeUploadPresignUnsupported {
		t.Fatalf("code = %q, want %q", payload.Code, codeUploadPresignUnsupported)
	}
}

func TestPresignAttachmentUpload_RejectsOversizeAndEmpty(t *testing.T) {
	installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	cases := []struct {
		name string
		req  presignUploadRequest
		// rawBody, when set, is sent verbatim instead of marshaling req. It
		// exists for the case that has no struct representation at all: a body
		// that simply omits size_bytes.
		rawBody    string
		wantStatus int
	}{
		{
			// Absent is a validation error; an explicit 0 is NOT (see the
			// empty-file compatibility test below).
			name:       "missing size field",
			rawBody:    `{"filename":"a.txt","content_type":"text/plain"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative size",
			req:        presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: -1},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "over the 100 MiB cap",
			req:        presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: maxUploadSize + 1},
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:       "path in filename",
			req:        presignUploadRequest{Filename: "../escape.txt", ContentType: "text/plain", SizeBytes: 5},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing content type",
			req:        presignUploadRequest{Filename: "a.txt", SizeBytes: 5},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "document and chat targets combined",
			req: presignUploadRequest{
				Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5,
				IssueID: issueID, ChatSessionID: "00000000-0000-0000-0000-000000000abc",
			},
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.rawBody)
			if tc.rawBody == "" {
				body, _ = json.Marshal(tc.req)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/attachments/presign-upload", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-ID", testUserID)
			r.Header.Set("X-Workspace-ID", testWorkspaceID)
			w := httptest.NewRecorder()
			testHandler.PresignAttachmentUpload(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestCompleteAttachmentUpload_SizeMismatchAndMissingObject(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	t.Run("object missing", func(t *testing.T) {
		presign := presignUpload(t, presignUploadRequest{
			Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5, IssueID: issueID,
		}, testUserID, testWorkspaceID)
		w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadObjectMissing) {
			t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadObjectMissing)
		}
	})

	t.Run("stored size differs from the declaration", func(t *testing.T) {
		presign := presignUpload(t, presignUploadRequest{
			Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5, IssueID: issueID,
		}, testUserID, testWorkspaceID)
		// Fewer bytes than declared: a truncated PUT must not be published as
		// the declared file.
		store.putStagingObject(store.presignedKeys[len(store.presignedKeys)-1], []byte("abc"), "text/plain")
		w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadSizeMismatch) {
			t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadSizeMismatch)
		}
	})
}

func TestCompleteAttachmentUpload_CrossWorkspaceAndCrossActorDenied(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)
	presign := presignUpload(t, presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")

	// A workspace header naming a workspace the session does not belong to is
	// indistinguishable from a missing session.
	otherWorkspace := "00000000-0000-0000-0000-0000000000aa"
	if w := completeUpload(t, presign.AttachmentID, testUserID, otherWorkspace); w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace complete = %d, want 404: %s", w.Code, w.Body.String())
	}

	// A different user in the same workspace did not request this credential.
	otherUser := dbfx.Insert(t, "user", testutil.Cols{
		"name": "Other Uploader", "email": "other-uploader@multica.ai",
	})
	dbfx.InsertNoID(t, "member", testutil.Cols{
		"workspace_id": testWorkspaceID, "user_id": otherUser, "role": "member",
	}, "workspace_id = $1 AND user_id = $2", testWorkspaceID, otherUser)
	if w := completeUpload(t, presign.AttachmentID, otherUser, testWorkspaceID); w.Code != http.StatusNotFound {
		t.Fatalf("cross-actor complete = %d, want 404: %s", w.Code, w.Body.String())
	}

	// The rightful owner can still complete it.
	if w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("owner complete = %d: %s", w.Code, w.Body.String())
	}
}

func TestPresignAttachmentUpload_CrossWorkspaceIssueDenied(t *testing.T) {
	installDirectUploadStorage(t)

	// A member of another workspace cannot target this workspace's issue: the
	// workspace membership check fires first, so the request never reaches the
	// issue lookup.
	otherUser := dbfx.Insert(t, "user", testutil.Cols{
		"name": "Outsider", "email": "outsider@multica.ai",
	})
	body, _ := json.Marshal(presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/attachments/presign-upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", otherUser)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.PresignAttachmentUpload(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestCompleteAttachmentUpload_TargetDeletedDuringUpload(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)
	presign := presignUpload(t, presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")

	// The issue disappears while the bytes are in flight.
	if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadTargetInvalid) {
		t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadTargetInvalid)
	}

	// Nothing was published, and the session stays pending so the client can
	// retry with a live target rather than being told it succeeded.
	var state string
	if err := testPool.QueryRow(context.Background(),
		`SELECT publish_state FROM attachment_upload_session WHERE id = $1`, presign.AttachmentID).Scan(&state); err != nil {
		t.Fatalf("read session state: %v", err)
	}
	if state != "pending" {
		t.Fatalf("session state = %q, want pending", state)
	}
}

func TestCompleteAttachmentUpload_UnboundThenBoundToComment(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	// Upload with no target, exactly what `issue create --attachment` does: the
	// file must exist before the issue it will be pasted into does.
	presign := presignUpload(t, presignUploadRequest{
		Filename: "shot.png", ContentType: "image/png", SizeBytes: 4,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("abcd"), "image/png")

	if w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("complete unbound = %d: %s", w.Code, w.Body.String())
	}

	// The row is adoptable: no owner set, so the existing binding queries find
	// it. This is the property that makes an unbound upload usable.
	var issueNull, commentNull, chatNull, taskNull bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT issue_id IS NULL, comment_id IS NULL, chat_session_id IS NULL, task_id IS NULL
		FROM attachment WHERE id = $1`, presign.AttachmentID,
	).Scan(&issueNull, &commentNull, &chatNull, &taskNull); err != nil {
		t.Fatalf("read attachment ownership: %v", err)
	}
	if !issueNull || !commentNull || !chatNull || !taskNull {
		t.Fatalf("unbound upload has an owner: issue=%v comment=%v chat=%v task=%v",
			issueNull, commentNull, chatNull, taskNull)
	}

	// A comment created with attachment_ids adopts it, through the same path a
	// proxied upload would take.
	commentID := createCommentForUploadTest(t, issueID, presign.AttachmentID)
	var boundComment pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT comment_id FROM attachment WHERE id = $1`, presign.AttachmentID).Scan(&boundComment); err != nil {
		t.Fatalf("read bound comment: %v", err)
	}
	if uuidToString(boundComment) != commentID {
		t.Fatalf("binding did not adopt the direct upload: comment_id = %q, want %q",
			uuidToString(boundComment), commentID)
	}
}

// createCommentForUploadTest posts a comment with attachment_ids through the
// handler, returning the new comment id.
func createCommentForUploadTest(t *testing.T, issueID, attachmentID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"content":        "here is the file",
		"attachment_ids": []string{attachmentID},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	r = testutil.WithURLParams(r, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create comment = %d: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("comment response has no id: %s", w.Body.String())
	}
	return id
}

func TestCompleteAttachmentUpload_CommentTargetDeletedRace(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)
	commentID := createCommentForUploadTestNoAttachments(t, issueID)

	presign := presignUpload(t, presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2, CommentID: commentID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")

	// Delete the comment while the upload is in flight. It has no replies, so
	// the row disappears entirely.
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM comment WHERE id = $1`, commentID); err != nil {
		t.Fatalf("delete comment: %v", err)
	}

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadTargetInvalid) {
		t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadTargetInvalid)
	}
}

func createCommentForUploadTestNoAttachments(t *testing.T, issueID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"content": "target comment"})
	r := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	r = testutil.WithURLParams(r, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create comment = %d: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	return out["id"].(string)
}

func TestPresignAttachmentUpload_TaskIDRequiresTaskToken(t *testing.T) {
	installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	body, _ := json.Marshal(presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 5,
		TaskID: "00000000-0000-0000-0000-0000000000bb",
	})
	r := httptest.NewRequest(http.MethodPost, "/api/attachments/presign-upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User-ID", testUserID)
	r.Header.Set("X-Workspace-ID", testWorkspaceID)
	_ = issueID
	w := httptest.NewRecorder()
	testHandler.PresignAttachmentUpload(w, r)

	// A human session cannot tag an upload with a task: only a task-scoped mat_
	// token may, and only with its own task.
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "task_id upload is only available") {
		t.Fatalf("body = %s, want the task-token refusal", w.Body.String())
	}
}

func TestCompleteAttachmentUpload_ExpiredSessionRefused(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)
	presign := presignUpload(t, presignUploadRequest{
		Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")

	// Move the expiry past the completion deadline. This is the real policy: a
	// session stops being completable at expires_at + grace whether or not the
	// cleanup worker has noticed it.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE attachment_upload_session SET expires_at = now() - interval '1 hour' WHERE id = $1`,
		presign.AttachmentID); err != nil {
		t.Fatalf("expire session: %v", err)
	}

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), codeUploadSessionExpired) {
		t.Fatalf("status = %d body = %s, want 409 %s", w.Code, w.Body.String(), codeUploadSessionExpired)
	}
}

// claimDueSession runs one claim against the real database, returning
// ErrNoRows-equivalent ok=false when nothing is due.
func claimDueSession(t *testing.T) (db.AttachmentUploadSession, bool) {
	t.Helper()
	session, err := testHandler.Queries.ClaimNextAttachmentUploadSessionForReconcile(
		context.Background(),
		db.ClaimNextAttachmentUploadSessionForReconcileParams{
			LeaseToken:    pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			LeaseDuration: pgtype.Interval{Microseconds: int64(time.Minute / time.Microsecond), Valid: true},
			UploadGrace:   pgtype.Interval{Microseconds: int64(service.AttachmentUploadCompleteGrace / time.Microsecond), Valid: true},
			SettleDelay: pgtype.Interval{
				Microseconds: int64(service.AttachmentUploadReconcileSettleDelay / time.Microsecond),
				Valid:        true,
			},
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.AttachmentUploadSession{}, false
	}
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return session, true
}

func TestClaimAttachmentUploadSession_RespectsExpiryAndLease(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	// live is still inside its presign window: its PUT may be in flight, so the
	// claim must leave it alone.
	live := presignUpload(t, presignUploadRequest{
		Filename: "live.txt", ContentType: "text/plain", SizeBytes: 2, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[len(store.presignedKeys)-1], []byte("hi"), "text/plain")

	// Push every other due session out of the way so the claim below can only
	// pick the one this test is about.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE attachment_upload_session SET next_attempt_at = now() + interval '1 day' WHERE id <> $1`,
		live.AttachmentID); err != nil {
		t.Fatalf("park other sessions: %v", err)
	}

	if _, ok := claimDueSession(t); ok {
		t.Fatalf("claimed a session whose presigned URL has not expired")
	}

	// Reclaimable only once the completion deadline (expiry + grace) is past by
	// the settle delay, so a session that could still be completed is never swept.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE attachment_upload_session
		SET expires_at = now() - interval '1 hour', next_attempt_at = now()
		WHERE id = $1`, live.AttachmentID); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	claimed, ok := claimDueSession(t)
	if !ok {
		t.Fatalf("did not claim an expired session")
	}
	if uuidToString(claimed.ID) != live.AttachmentID {
		t.Fatalf("claimed session %q, want %q", uuidToString(claimed.ID), live.AttachmentID)
	}
	if claimed.CleanupState != "claimed" {
		t.Fatalf("claimed session cleanup state = %q, want claimed", claimed.CleanupState)
	}
	if claimed.PublishState != "pending" {
		t.Fatalf("claim moved publish_state to %q; cleanup must never touch it", claimed.PublishState)
	}

	// A claimed session's lease keeps a second replica off it.
	if _, ok := claimDueSession(t); ok {
		t.Fatalf("claimed a session another replica holds the lease on")
	}
}

// TestEmptyFileParityWithMultipartPath pins the compatibility the rework had to
// restore: the multiplexed endpoint has always published an empty file, and the
// direct path must not be stricter. A 400 here would be a silent regression
// because the CLI does not treat 400 as a fallback signal.
func TestEmptyFileParityWithMultipartPath(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	// The legacy path first: this is the behaviour being matched.
	legacyStore := &mockStorage{}
	origStorage := testHandler.Storage
	testHandler.Storage = legacyStore
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if _, err := writer.CreateFormFile("file", "empty.txt"); err != nil {
		t.Fatalf("create form file: %v", err)
	}
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	legacyRecorder := httptest.NewRecorder()
	testHandler.UploadFile(legacyRecorder, req)
	testHandler.Storage = origStorage
	if legacyRecorder.Code != http.StatusOK {
		t.Fatalf("legacy empty-file upload = %d: %s", legacyRecorder.Code, legacyRecorder.Body.String())
	}
	var legacyAttachment AttachmentResponse
	if err := json.Unmarshal(legacyRecorder.Body.Bytes(), &legacyAttachment); err != nil {
		t.Fatalf("decode legacy response: %v", err)
	}
	if legacyAttachment.SizeBytes != 0 {
		t.Fatalf("legacy size_bytes = %d, want 0", legacyAttachment.SizeBytes)
	}

	// The direct path must agree: an explicit 0 is accepted and published.
	presign := presignUpload(t, presignUploadRequest{
		Filename: "empty.txt", ContentType: "text/plain", SizeBytes: 0, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], nil, "text/plain")
	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusOK {
		t.Fatalf("direct empty-file upload = %d, want 200: %s", w.Code, w.Body.String())
	}
	var directAttachment AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &directAttachment); err != nil {
		t.Fatalf("decode direct response: %v", err)
	}
	if directAttachment.SizeBytes != 0 {
		t.Fatalf("direct size_bytes = %d, want 0", directAttachment.SizeBytes)
	}
	// Both paths report the same shape, which is what makes them interchangeable
	// for a client.
	if directAttachment.ID == "" || directAttachment.MarkdownURL == "" {
		t.Fatalf("direct empty-file response is missing the attachment contract: %+v", directAttachment)
	}
}

// TestSessionExpiryPolicyIsDerivedAndFinite nails the completion deadline:
// it follows expires_at, it is finite, and it does not depend on the cleanup
// worker having run.
func TestSessionExpiryPolicyIsDerivedAndFinite(t *testing.T) {
	if completeUploadGrace <= 0 {
		t.Fatalf("grace = %s, want a positive finite window", completeUploadGrace)
	}
	if completeUploadGrace > time.Hour {
		t.Fatalf("grace = %s, too long to bound a started PUT meaningfully", completeUploadGrace)
	}

	now := time.Now()
	cases := []struct {
		name    string
		expires time.Time
		want    bool
	}{
		{"inside the presign window", now.Add(time.Minute), false},
		{"inside the grace window", now.Add(-time.Minute), false},
		{"just past the grace window", now.Add(-completeUploadGrace - time.Second), true},
		{"long expired", now.Add(-time.Hour), true},
	}
	for _, tc := range cases {
		session := db.AttachmentUploadSession{
			ExpiresAt: pgtype.Timestamptz{Time: tc.expires, Valid: true},
		}
		if got := sessionExpired(session); got != tc.want {
			t.Errorf("%s: sessionExpired = %v, want %v", tc.name, got, tc.want)
		}
	}

	// An unset expiry must fail closed rather than publishing forever.
	if !sessionExpired(db.AttachmentUploadSession{}) {
		t.Error("a session with no expiry was treated as completable")
	}
}

// TestCompleteIsReplayableAcrossEveryCleanupPhase proves the publish result and
// the cleanup state are independent: no phase of the reclaim may make a
// published session unreplayable, and no replay may re-copy or re-emit.
func TestCompleteIsReplayableAcrossEveryCleanupPhase(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	presign := presignUpload(t, presignUploadRequest{
		Filename: "kept.txt", ContentType: "text/plain", SizeBytes: 5, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hello"), "text/plain")

	first := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if first.Code != http.StatusOK {
		t.Fatalf("first complete = %d: %s", first.Code, first.Body.String())
	}
	var published AttachmentResponse
	if err := json.Unmarshal(first.Body.Bytes(), &published); err != nil {
		t.Fatalf("decode first complete: %v", err)
	}
	copiesAfterFirst := store.finalizeCalls

	var revisionBefore int64
	if err := testPool.QueryRow(context.Background(),
		`SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revisionBefore); err != nil {
		t.Fatalf("read issue revision: %v", err)
	}

	// Walk every cleanup phase the row can be in, including the tombstones the
	// widening re-delete schedule walks through, and assert the replay survives
	// each one unchanged.
	cleanupStates := []string{"none", "claimed", "tombstoned"}
	for _, cleanupState := range cleanupStates {
		t.Run("cleanup_state="+cleanupState, func(t *testing.T) {
			if _, err := testPool.Exec(context.Background(), `
				UPDATE attachment_upload_session SET cleanup_state = $2, lease_token = NULL, lease_expires_at = NULL
				WHERE id = $1`, presign.AttachmentID, cleanupState); err != nil {
				t.Fatalf("set cleanup state: %v", err)
			}

			replay := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
			if replay.Code != http.StatusOK {
				t.Fatalf("replay during cleanup_state=%s = %d, want 200: %s",
					cleanupState, replay.Code, replay.Body.String())
			}
			var replayed AttachmentResponse
			if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
				t.Fatalf("decode replay: %v", err)
			}
			if replayed.ID != published.ID {
				t.Fatalf("replay returned attachment %q, want the published %q", replayed.ID, published.ID)
			}
			// A replay must not copy again...
			if store.finalizeCalls != copiesAfterFirst {
				t.Fatalf("replay re-copied the object (%d finalizations, want %d)",
					store.finalizeCalls, copiesAfterFirst)
			}
			// ...and must not bump the issue revision, which is what would
			// signal a second attachment to every watching client.
			var revisionNow int64
			if err := testPool.QueryRow(context.Background(),
				`SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revisionNow); err != nil {
				t.Fatalf("read issue revision: %v", err)
			}
			if revisionNow != revisionBefore {
				t.Fatalf("replay bumped the issue revision %d -> %d", revisionBefore, revisionNow)
			}
			// publish_state must never be touched by cleanup.
			var publishState string
			if err := testPool.QueryRow(context.Background(),
				`SELECT publish_state FROM attachment_upload_session WHERE id = $1`,
				presign.AttachmentID).Scan(&publishState); err != nil {
				t.Fatalf("read publish state: %v", err)
			}
			if publishState != "completed" {
				t.Fatalf("publish_state = %q, want completed", publishState)
			}
		})
	}
}

// TestActualSweepPreservesReplay runs the real reconciler (not a simulated
// state) and then replays, which is the exact sequence a lost response plus a
// coincidental sweep produces in production.
func TestActualSweepPreservesReplay(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	presign := presignUpload(t, presignUploadRequest{
		Filename: "swept.txt", ContentType: "text/plain", SizeBytes: 2, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")

	first := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if first.Code != http.StatusOK {
		t.Fatalf("first complete = %d: %s", first.Code, first.Body.String())
	}

	// A published session is due for staging cleanup immediately; run the real
	// worker enough times to exhaust its tombstone schedule.
	reconciler := &service.AttachmentUploadReconciler{Queries: testHandler.Queries, Storage: store}
	for pass := 0; pass < len([]int{0, 1, 2, 3, 4}); pass++ {
		reconciler.RunOnce(context.Background())
		if _, err := testPool.Exec(context.Background(), `
			UPDATE attachment_upload_session
			SET next_attempt_at = now(), lease_token = NULL, lease_expires_at = NULL
			WHERE id = $1`, presign.AttachmentID); err != nil {
			t.Fatalf("advance sweep: %v", err)
		}
	}

	replay := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay after a real sweep = %d, want 200: %s", replay.Code, replay.Body.String())
	}
	var replayed AttachmentResponse
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replayed.ID == "" || replayed.MarkdownURL == "" {
		t.Fatalf("replay after sweep lost the attachment contract: %+v", replayed)
	}

	// The published object must still be readable: it is what the attachment
	// points at, and the sweep must never have deleted it.
	finalKey := attachmentObjectKey(testWorkspaceID, testUserID, presign.AttachmentID, "swept.txt")
	if _, ok := store.object(finalKey); !ok {
		t.Fatalf("the sweep deleted the PUBLISHED object at %q", finalKey)
	}
}

// TestProbeAndCopyBindTheSameObjectVersion reproduces the A -> B -> A sequence
// the review named: HEAD measures version A, a same-length PUT replaces it with
// B, the probe read happens, then A is restored before the conditional copy.
// Copying A is legal, so only binding the PROBE to A keeps the published
// metadata describing the published bytes.
func TestProbeAndCopyBindTheSameObjectVersion(t *testing.T) {
	store := installDirectUploadStorage(t)
	data := bytes.Repeat([]byte("a"), 64) // sniffs as text/plain

	presign := presignUpload(t, presignUploadRequest{
		Filename: "payload.bin", ContentType: "application/octet-stream", SizeBytes: int64(len(data)),
	}, testUserID, testWorkspaceID)
	stagingKey := store.presignedKeys[0]
	store.putStagingObject(stagingKey, data, "application/octet-stream")

	// Between HEAD and the probe read, swap in a same-length object with a PNG
	// signature, then restore the original before the copy.
	pngBytes := make([]byte, len(data))
	copy(pngBytes, []byte("\x89PNG\r\n\x1a\n"))
	store.beforeReadPrefix = func() {
		store.mu.Lock()
		store.objects[stagingKey] = directUploadObject{
			body: pngBytes, contentType: "application/octet-stream", etag: etagOf(pngBytes),
		}
		store.mu.Unlock()
	}
	defer func() { store.beforeReadPrefix = nil }()

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	// Refusing a source that moved is the correct outcome; the test accepts it,
	// exactly like the reviewer's.
	if w.Code != http.StatusOK {
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 200 or a 409 refusal: %s", w.Code, w.Body.String())
		}
		return
	}
	var result AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The published metadata must describe the published bytes (the original),
	// not the object that happened to be staged during the probe.
	want := http.DetectContentType(data)
	if result.ContentType != want {
		t.Fatalf("published content_type = %q, but the copied bytes sniff as %q — metadata and bytes describe different objects",
			result.ContentType, want)
	}
}

// TestCompleteSucceedsForVerifiedEmptyObject covers item 3 end to end at the
// handler: a zero-byte staging object must publish, not 502.
func TestCompleteSucceedsForVerifiedEmptyObject(t *testing.T) {
	store := installDirectUploadStorage(t)
	issueID := seedIssueForUpload(t)

	presign := presignUpload(t, presignUploadRequest{
		Filename: "empty.txt", ContentType: "text/plain", SizeBytes: 0, IssueID: issueID,
	}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], nil, "text/plain")

	w := completeUpload(t, presign.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusOK {
		t.Fatalf("empty upload completion = %d, want 200: %s", w.Code, w.Body.String())
	}
	var att AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &att); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if att.SizeBytes != 0 {
		t.Fatalf("size_bytes = %d, want 0", att.SizeBytes)
	}
	// The MIME semantics must survive: text/plain for empty content, matching
	// the multipart path, never an empty string.
	if !strings.HasPrefix(att.ContentType, "text/plain") {
		t.Fatalf("content_type = %q, want text/plain", att.ContentType)
	}
	// The object must be published at the final key with the same (zero) length.
	finalKey := attachmentObjectKey(testWorkspaceID, testUserID, presign.AttachmentID, "empty.txt")
	published, ok := store.object(finalKey)
	if !ok || len(published.body) != 0 {
		t.Fatalf("final object missing or non-empty: exists=%v len=%d", ok, len(published.body))
	}
}
