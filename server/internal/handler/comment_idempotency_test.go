package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// GH #8221: a comment POST whose response was lost must be safe to resend.

func idempotentCommentRequest(issueID, key string, body map[string]any) *http.Request {
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body)
	if key != "" {
		req.Header.Set(commentIdempotencyKeyHeader, key)
	}
	return withURLParam(req, "id", issueID)
}

func newCommentIdempotencyKey(t *testing.T) string {
	t.Helper()
	key := "test-" + uuid.NewString()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment_idempotency_receipt WHERE idempotency_key = $1`, key)
	})
	return key
}

func TestCreateCommentIdempotencyKeyReplaysSavedComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment")
	key := newCommentIdempotencyKey(t)
	var revision int64
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision)

	var first CommentResponse
	created := testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "verdict"}))
	created.Want(http.StatusCreated).JSON(&first)
	if got := created.Header().Get(commentIdempotencyReplayedHeader); got != "" {
		t.Fatalf("first request %s = %q, want unset", commentIdempotencyReplayedHeader, got)
	}

	// The retry may carry a different body: the CLI re-uploads attachments and
	// rewrites their links on every attempt.
	var second CommentResponse
	replayed := testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "verdict, re-rendered"}))
	replayed.Want(http.StatusOK).JSON(&second)
	if got := replayed.Header().Get(commentIdempotencyReplayedHeader); got != "true" {
		t.Fatalf("replay %s = %q, want true", commentIdempotencyReplayedHeader, got)
	}
	if second.IssueRevision != first.IssueRevision {
		t.Fatalf("replay issue_revision = %d, want %d", second.IssueRevision, first.IssueRevision)
	}
	if second.ID != first.ID || second.Content != "verdict" {
		t.Fatalf("replay = %s %q, want the saved comment %s %q", second.ID, second.Content, first.ID, first.Content)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 1 {
		t.Fatalf("comments on issue = %d, want 1", n)
	}
	var after int64
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&after)
	if after != revision+1 {
		t.Fatalf("issue revision = %d, want %d (the replay must not touch the issue)", after, revision+1)
	}

	// A different key is a different comment.
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, newCommentIdempotencyKey(t), map[string]any{"content": "verdict"})).Want(http.StatusCreated)
	// No key keeps the old behaviour.
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, "", map[string]any{"content": "verdict"})).Want(http.StatusCreated)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 3 {
		t.Fatalf("comments on issue = %d, want 3", n)
	}
}

func TestCreateCommentIdempotencyKeyRejectsDifferentTarget(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment target")
	otherIssueID := dbfx.Issue(t, "idempotent comment other target")
	key := newCommentIdempotencyKey(t)

	var root CommentResponse
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "root"})).Want(http.StatusCreated).JSON(&root)

	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "root", "parent_id": root.ID})).Want(http.StatusUnprocessableEntity)
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(otherIssueID, key, map[string]any{"content": "root"})).Want(http.StatusUnprocessableEntity)
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "root", "type": "progress_update"})).Want(http.StatusUnprocessableEntity)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id IN ($1, $2)`, issueID, otherIssueID); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}
}

func TestCreateCommentIdempotencyKeyDoesNotRepostDeletedComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment deleted")
	key := newCommentIdempotencyKey(t)
	var first CommentResponse
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "gone"})).Want(http.StatusCreated).JSON(&first)
	dbfx.Exec(t, `DELETE FROM comment WHERE id = $1`, first.ID)

	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "gone"})).Want(http.StatusConflict)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("comments = %d, want 0", n)
	}
}

func TestCreateCommentIdempotencyKeyTooLong(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment long key")
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, strings.Repeat("k", commentIdempotencyMaxKeyLength+1), map[string]any{"content": "x"})).Want(http.StatusBadRequest)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("comments = %d, want 0", n)
	}
}

// Two attempts racing with one key (the first still in flight when the client
// gives up and resends) must leave one comment, and both must be told about it.
func TestCreateCommentIdempotencyKeyConcurrentRequestsPostOnce(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment race")
	key := newCommentIdempotencyKey(t)

	const attempts = 4
	recorders := make([]*httptest.ResponseRecorder, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range recorders {
		recorders[i] = httptest.NewRecorder()
		req := idempotentCommentRequest(issueID, key, map[string]any{"content": "once"})
		wg.Add(1)
		go func(rec *httptest.ResponseRecorder) {
			defer wg.Done()
			<-start
			testHandler.CreateComment(rec, req)
		}(recorders[i])
	}
	close(start)
	wg.Wait()

	created := 0
	ids := map[string]bool{}
	for _, rec := range recorders {
		switch rec.Code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("attempt status = %d: %s", rec.Code, rec.Body.String())
		}
		var resp CommentResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
		ids[resp.ID] = true
	}
	if created != 1 || len(ids) != 1 {
		t.Fatalf("created = %d, distinct ids = %d, want 1 and 1", created, len(ids))
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}
}

// The attachment path runs its own transaction; the receipt must be part of it.
func TestCreateCommentIdempotencyKeyWithAttachments(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment attachment")
	key := newCommentIdempotencyKey(t)
	firstAttachment := unlinkedIssueAttachment(t, "")
	var first CommentResponse
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{
		"content": "see file", "attachment_ids": []string{firstAttachment},
	})).Want(http.StatusCreated).JSON(&first)

	// A rerun uploads the file again; the replay must not bind the new upload.
	secondAttachment := unlinkedIssueAttachment(t, "")
	var second CommentResponse
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{
		"content": "see file", "attachment_ids": []string{secondAttachment},
	})).Want(http.StatusOK).JSON(&second)
	if second.ID != first.ID || len(second.Attachments) != 1 || second.Attachments[0].ID != firstAttachment {
		t.Fatalf("replay = %s %+v, want comment %s with attachment %s", second.ID, second.Attachments, first.ID, firstAttachment)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND comment_id IS NULL`, secondAttachment); n != 1 {
		t.Fatalf("second upload bound = %d unbound rows, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 1 {
		t.Fatalf("comments = %d, want 1", n)
	}
}

func TestSweepCommentIdempotencyReceipts(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	oldKey := newCommentIdempotencyKey(t)
	freshKey := newCommentIdempotencyKey(t)
	now := time.Now()
	for _, row := range []struct {
		key string
		at  time.Time
	}{{oldKey, now.Add(-commentIdempotencyRetention - time.Hour)}, {freshKey, now.Add(-time.Hour)}} {
		dbfx.Exec(t, `INSERT INTO comment_idempotency_receipt
			(workspace_id, actor_type, actor_id, idempotency_key, issue_id, comment_id, request_fingerprint, created_at)
			VALUES ($1, 'member', $2, $3, gen_random_uuid(), gen_random_uuid(), '\x00', $4)`,
			testWorkspaceID, testUserID, row.key, pgtype.Timestamptz{Time: row.at, Valid: true})
	}
	if _, err := SweepCommentIdempotencyReceipts(context.Background(), testHandler.Queries, now); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment_idempotency_receipt WHERE idempotency_key = $1`, oldKey); n != 0 {
		t.Fatalf("expired receipt kept")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment_idempotency_receipt WHERE idempotency_key = $1`, freshKey); n != 1 {
		t.Fatalf("fresh receipt removed")
	}
}

func lookupIdempotentCommentRequest(issueID, key, query string) *http.Request {
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/comments/idempotent"+query, nil)
	if key != "" {
		req.Header.Set(commentIdempotencyKeyHeader, key)
	}
	return withURLParam(req, "id", issueID)
}

// The CLI asks before uploading attachments, so a rerun whose comment already
// landed does not leave fresh uploads bound to nothing.
func TestLookupIdempotentComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "idempotent comment lookup")
	rootID := dbfx.Comment(t, issueID, "root")
	key := newCommentIdempotencyKey(t)
	query := "?parent_id=" + rootID

	testutil.Call(t, testHandler.LookupIdempotentComment, lookupIdempotentCommentRequest(issueID, "", query)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.LookupIdempotentComment, lookupIdempotentCommentRequest(issueID, key, query)).Want(http.StatusNotFound)

	var created CommentResponse
	testutil.Call(t, testHandler.CreateComment, idempotentCommentRequest(issueID, key, map[string]any{"content": "reply", "parent_id": rootID})).Want(http.StatusCreated).JSON(&created)

	var found CommentResponse
	resp := testutil.Call(t, testHandler.LookupIdempotentComment, lookupIdempotentCommentRequest(issueID, key, query))
	resp.Want(http.StatusOK).JSON(&found)
	if resp.Header().Get(commentIdempotencyReplayedHeader) != "true" || found.ID != created.ID || found.IssueRevision != created.IssueRevision {
		t.Fatalf("lookup = %s rev %d (replayed %q), want %s rev %d", found.ID, found.IssueRevision, resp.Header().Get(commentIdempotencyReplayedHeader), created.ID, created.IssueRevision)
	}
	// Same rules as a replayed POST: another thread is a different comment.
	testutil.Call(t, testHandler.LookupIdempotentComment, lookupIdempotentCommentRequest(issueID, key, "")).Want(http.StatusUnprocessableEntity)
	testutil.Call(t, testHandler.LookupIdempotentComment, lookupIdempotentCommentRequest(issueID, key, "?parent_id=nope")).Want(http.StatusBadRequest)
	// The lookup only reads.
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 2 {
		t.Fatalf("comments = %d, want 2", n)
	}
}
