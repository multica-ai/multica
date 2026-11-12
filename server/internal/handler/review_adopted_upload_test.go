package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReviewReplayAdoptedAttachmentAfterRevocation(t *testing.T) {
	store := installDirectUploadStorage(t)
	f := newPrivateProjectFixture(t)
	p := presignUpload(t, presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2}, f.insider, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")
	if w := completeUpload(t, p.AttachmentID, f.insider, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("initial complete: %d %s", w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"content": "adopt upload", "attachment_ids": []string{p.AttachmentID}})
	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+f.secretIssue+"/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.insider)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = testutil.WithURLParams(req, "id", f.secretIssue)
	w := httptest.NewRecorder()
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("adopt: %d %s", w.Code, w.Body.String())
	}
	removeProjectMemberAsOwner(t, f, f.insider)
	if w := completeUpload(t, p.AttachmentID, f.insider, testWorkspaceID); w.Code == http.StatusOK {
		t.Fatal("revoked member can replay complete for attachment adopted into a private comment")
	}
}

// Model two legal same-length PUTs: A at HEAD, B during GET, then A again
// before the conditional copy. Copy's ETag check alone cannot bind the probe.
type reviewChangingProbeStore struct{ *directUploadStorage }

func (s *reviewChangingProbeStore) ReadPrefix(ctx context.Context, key string, n int, etag string) ([]byte, error) {
	original, _ := s.object(key)
	replacement := make([]byte, len(original.body))
	copy(replacement, []byte("\x89PNG\r\n\x1a\n"))
	s.putStagingObject(key, replacement, original.contentType)
	probe, err := s.directUploadStorage.ReadPrefix(ctx, key, n, etag)
	s.putStagingObject(key, original.body, original.contentType)
	return probe, err
}

func TestReviewProbeAndCopyMustUseSameObject(t *testing.T) {
	store := installDirectUploadStorage(t)
	testHandler.Storage = &reviewChangingProbeStore{store}
	data := bytes.Repeat([]byte("a"), 64)
	p := presignUpload(t, presignUploadRequest{Filename: "payload.bin", ContentType: "application/octet-stream", SizeBytes: 64}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], data, "application/octet-stream")
	w := completeUpload(t, p.AttachmentID, testUserID, testWorkspaceID)
	if w.Code != http.StatusOK {
		return
	} // Refusing a changed source is valid.
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["content_type"] != http.DetectContentType(data) {
		t.Fatalf("published metadata describes another object: got %v, copied bytes sniff as %s", result["content_type"], http.DetectContentType(data))
	}
}
