package handler

import (
	"context"
	"github.com/multica-ai/multica/server/internal/service"
	"net/http"
	"testing"
)

func TestReviewCompleteAfterCleanup(t *testing.T) {
	store := installDirectUploadStorage(t)
	p := presignUpload(t, presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")
	if w := completeUpload(t, p.AttachmentID, testUserID, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("first complete: %d %s", w.Code, w.Body.String())
	}
	worker := &service.AttachmentUploadReconciler{Queries: testHandler.Queries, Storage: store}
	worker.RunOnce(context.Background())
	if w := completeUpload(t, p.AttachmentID, testUserID, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("complete after cleanup: got %d want 200: %s", w.Code, w.Body.String())
	}
}

func TestReviewCompleteExpiredPending(t *testing.T) {
	store := installDirectUploadStorage(t)
	p := presignUpload(t, presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2}, testUserID, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")
	if _, err := testPool.Exec(context.Background(), `UPDATE attachment_upload_session SET expires_at=now()-interval '1 hour' WHERE id=$1`, p.AttachmentID); err != nil {
		t.Fatal(err)
	}
	if w := completeUpload(t, p.AttachmentID, testUserID, testWorkspaceID); w.Code == http.StatusOK {
		t.Fatal("expired pending session was published: got 200, want rejection")
	}
}

func TestReviewCompleteCommentAfterRevocation(t *testing.T) {
	store := installDirectUploadStorage(t)
	f := newPrivateProjectFixture(t)
	p := presignUpload(t, presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2, CommentID: f.secretComment}, f.insider, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")
	removeProjectMemberAsOwner(t, f, f.insider)
	if w := completeUpload(t, p.AttachmentID, f.insider, testWorkspaceID); w.Code == http.StatusOK {
		t.Fatal("revoked project member published attachment to private comment: got 200, want rejection")
	}
}

func TestReviewReplayAfterProjectRevocation(t *testing.T) {
	store := installDirectUploadStorage(t)
	f := newPrivateProjectFixture(t)
	p := presignUpload(t, presignUploadRequest{Filename: "a.txt", ContentType: "text/plain", SizeBytes: 2, IssueID: f.secretIssue}, f.insider, testWorkspaceID)
	store.putStagingObject(store.presignedKeys[0], []byte("hi"), "text/plain")
	if w := completeUpload(t, p.AttachmentID, f.insider, testWorkspaceID); w.Code != http.StatusOK {
		t.Fatalf("first complete: %d", w.Code)
	}
	removeProjectMemberAsOwner(t, f, f.insider)
	if w := completeUpload(t, p.AttachmentID, f.insider, testWorkspaceID); w.Code == http.StatusOK {
		t.Fatal("revoked project member can replay complete and obtain attachment URLs: got 200, want rejection")
	}
}
