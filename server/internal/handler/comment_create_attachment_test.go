package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A comment and the attachments it was posted with are one database outcome
// (#8296 review). Committing the comment first left a window in which it could
// gain a reply and be deleted into a tombstone, and the link would then bind
// the uploaded objects to that placeholder — invisible in the timeline, and
// missed by the prune's storage cleanup.

// failLinkAttachmentsTxStarter begins real transactions whose
// LinkAttachmentsToComment statement fails, standing in for any failure
// between creating the comment and linking its attachments.
type failLinkAttachmentsTxStarter struct {
	delegate *pgxpool.Pool
}

type failLinkAttachmentsTx struct {
	pgx.Tx
}

func (s *failLinkAttachmentsTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failLinkAttachmentsTx{Tx: tx}, nil
}

func (t failLinkAttachmentsTx) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(query, "-- name: LinkAttachmentsToComment") {
		return pgconn.CommandTag{}, errors.New("injected attachment link failure")
	}
	return t.Tx.Exec(ctx, query, args...)
}

func createCommentWithAttachment(t *testing.T, h *Handler, issueID, attachmentID string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content":        "see the screenshot",
		"attachment_ids": []string{attachmentID},
	})
	return testutil.Call(t, h.CreateComment, withURLParam(req, "id", issueID))
}

func unlinkedIssueAttachment(t *testing.T, issueID string) string {
	t.Helper()
	var owner any
	if issueID != "" {
		owner = issueID
	}
	return dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": owner,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "shot.png", "url": "https://example.test/shot.png",
		"content_type": "image/png", "size_bytes": 1,
	})
}

func TestCreateCommentLinksItsAttachmentsInOneTransaction(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, unbound := range []bool{false, true} {
		t.Run(fmt.Sprintf("unbound=%t", unbound), func(t *testing.T) {
			issueID := dbfx.Issue(t, "comment with attachments")
			owner := issueID
			if unbound {
				owner = ""
			}
			attachmentID := unlinkedIssueAttachment(t, owner)
			var revision int64
			dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision)

			var resp CommentResponse
			createCommentWithAttachment(t, testHandler, issueID, attachmentID).Want(http.StatusCreated).JSON(&resp)
			if len(resp.Attachments) != 1 || resp.Attachments[0].ID != attachmentID {
				t.Fatalf("response attachments = %+v, want the posted one", resp.Attachments)
			}
			att := resp.Attachments[0]
			if att.IssueID == nil || *att.IssueID != issueID || att.CommentID == nil || *att.CommentID != resp.ID {
				t.Fatalf("attachment owners = %+v, want issue %s and comment %s", att, issueID, resp.ID)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND issue_id = $2 AND comment_id = $3`, attachmentID, issueID, resp.ID); n != 1 {
				t.Fatalf("attachment linked rows = %d, want 1", n)
			}
			if resp.IssueRevision != revision+1 {
				t.Fatalf("issue revision = %d, want %d", resp.IssueRevision, revision+1)
			}
		})
	}
}

func TestCreateCommentRejectedLeavesUploadUnbound(t *testing.T) {
	issueID := dbfx.Issue(t, "rejected comment with upload")
	attachmentID := unlinkedIssueAttachment(t, "")
	runtimeID := dbfx.Runtime(t, "comment admission runtime")
	agentID := dbfx.Agent(t, "comment admission agent", runtimeID)
	parentID := dbfx.Comment(t, issueID, "reply here")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "trigger_comment_id": parentID, "runtime_id": runtimeID,
	})
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "missing required reply parent", "attachment_ids": []string{attachmentID},
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testutil.Call(t, testHandler.CreateComment, withURLParam(req, "id", issueID)).Want(http.StatusConflict)
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND issue_id IS NULL AND comment_id IS NULL`, attachmentID); n != 1 {
		t.Fatal("rejected comment bound its upload")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1 AND id <> $2`, issueID, parentID); n != 0 {
		t.Fatalf("rejected comment left %d comments", n)
	}
}

func TestCreateCommentRejectsUnavailableAttachmentsAtomically(t *testing.T) {
	for _, kind := range []string{"other workspace", "other issue", "existing comment", "source context", "chat session", "chat message", "task", "missing"} {
		t.Run(kind, func(t *testing.T) {
			issueID := dbfx.Issue(t, "attachment isolation")
			validID := unlinkedIssueAttachment(t, "")
			invalidID := unlinkedIssueAttachment(t, "")
			attachmentWorkspaceID := testWorkspaceID
			wantComments := 0
			switch kind {
			case "other workspace":
				ws := dbfx.Workspace(t, "other attachment workspace", "comment-att-"+invalidID)
				dbfx.Exec(t, `UPDATE attachment SET workspace_id = $2 WHERE id = $1`, invalidID, ws)
				attachmentWorkspaceID = ws
			case "other issue":
				other := dbfx.Issue(t, "other attachment issue")
				dbfx.Exec(t, `UPDATE attachment SET issue_id = $2 WHERE id = $1`, invalidID, other)
			case "existing comment":
				commentID := dbfx.Comment(t, issueID, "already owns upload")
				dbfx.Exec(t, `UPDATE attachment SET comment_id = $2 WHERE id = $1`, invalidID, commentID)
				wantComments = 1
			case "source context":
				dbfx.Exec(t, `UPDATE attachment SET source_context_id = $2 WHERE id = $1`, invalidID, issueID)
			case "chat session", "chat message":
				runtimeID := dbfx.Runtime(t, "private chat attachment runtime")
				agentID := dbfx.Agent(t, "private chat attachment agent", runtimeID)
				sessionID := dbfx.ChatSession(t, agentID)
				if kind == "chat session" {
					dbfx.Exec(t, `UPDATE attachment SET chat_session_id = $2 WHERE id = $1`, invalidID, sessionID)
				} else {
					messageID := dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "role": "user", "content": "private file"})
					dbfx.Exec(t, `UPDATE attachment SET chat_message_id = $2 WHERE id = $1`, invalidID, messageID)
				}
			case "task":
				dbfx.Exec(t, `UPDATE attachment SET task_id = $2 WHERE id = $1`, invalidID, issueID)
			case "missing":
				dbfx.Exec(t, `DELETE FROM attachment WHERE id = $1`, invalidID)
			}
			var before db.Attachment
			if kind != "missing" {
				var err error
				before, err = testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
					ID: parseUUID(invalidID), WorkspaceID: parseUUID(attachmentWorkspaceID),
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			var revision int64
			dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&revision)
			req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
				"content": "all or nothing", "attachment_ids": []string{validID, invalidID},
			})
			testutil.Call(t, testHandler.CreateComment, withURLParam(req, "id", issueID)).Want(http.StatusConflict)
			if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != wantComments {
				t.Fatalf("refused attachment left %d comments, want %d", n, wantComments)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND issue_id IS NULL AND comment_id IS NULL`, validID); n != 1 {
				t.Fatal("refused batch bound its valid upload")
			}
			if kind != "missing" {
				after, err := testHandler.Queries.GetAttachment(context.Background(), db.GetAttachmentParams{
					ID: parseUUID(invalidID), WorkspaceID: parseUUID(attachmentWorkspaceID),
				})
				if err != nil || after != before {
					t.Fatalf("refused attachment changed: before=%+v after=%+v err=%v", before, after, err)
				}
			}
			var afterRevision int64
			dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id = $1`, issueID).Scan(&afterRevision)
			if afterRevision != revision {
				t.Fatalf("refused attachment bumped issue revision from %d to %d", revision, afterRevision)
			}
		})
	}
}

// A link that fails takes the comment with it: no comment may exist whose
// attachments were never bound.
func TestCreateCommentRollsBackWhenAttachmentLinkFails(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, unbound := range []bool{false, true} {
		t.Run(fmt.Sprintf("unbound=%t", unbound), func(t *testing.T) {
			issueID := dbfx.Issue(t, "comment attachment link failure")
			owner := issueID
			if unbound {
				owner = ""
			}
			attachmentID := unlinkedIssueAttachment(t, owner)

			h := *testHandler
			h.TxStarter = &failLinkAttachmentsTxStarter{delegate: testPool}
			createCommentWithAttachment(t, &h, issueID, attachmentID).Want(http.StatusInternalServerError)

			if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 0 {
				t.Fatalf("issue has %d comments after a failed link, want none", n)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND comment_id IS NULL AND COALESCE(issue_id::text, '') = $2`, attachmentID, owner); n != 1 {
				t.Fatal("failed link changed attachment ownership")
			}
		})
	}
}

func TestCreateCommentUnboundAttachmentDoesNotDeadlockWithIssueLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	issueID := dbfx.Issue(t, "unbound attachment vs issue link")
	attachmentID := unlinkedIssueAttachment(t, "")
	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	qtx := testHandler.Queries.WithTx(holder)
	if _, err := qtx.LockAttachmentsForIssueLink(ctx, db.LockAttachmentsForIssueLinkParams{
		WorkspaceID: parseUUID(testWorkspaceID), AttachmentIds: []pgtype.UUID{parseUUID(attachmentID)},
	}); err != nil {
		t.Fatal(err)
	}

	var got *testutil.Response
	done := make(chan error, 1)
	go func() {
		got = createCommentWithAttachment(t, testHandler, issueID, attachmentID)
		done <- nil
	}()
	waitForBlockedQuery(t, done, "LockUnboundAttachmentsForCommentLink", "LockAttachmentsForCommentLink")

	// The issue update already holds the ownerless upload. It must be able to
	// take the issue next while comment creation waits without holding it.
	_, linkErr := qtx.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
		AttachmentIds: []pgtype.UUID{parseUUID(attachmentID)}, BumpRevision: true,
	})
	if linkErr == nil {
		linkErr = holder.Commit(ctx)
	}
	<-done
	if linkErr != nil {
		t.Fatalf("issue link deadlocked or failed: %v", linkErr)
	}
	got.Want(http.StatusCreated)
}

func TestCreateCommentUnboundAttachmentCanOnlyBeClaimedOnce(t *testing.T) {
	attachmentID := unlinkedIssueAttachment(t, "")
	issues := []string{dbfx.Issue(t, "first claimant"), dbfx.Issue(t, "second claimant")}
	responses := make(chan *testutil.Response, len(issues))
	for _, issueID := range issues {
		go func() {
			responses <- createCommentWithAttachment(t, testHandler, issueID, attachmentID)
		}()
	}
	var winner CommentResponse
	codes := map[int]int{}
	for range issues {
		resp := <-responses
		codes[resp.Code]++
		if resp.Code == http.StatusCreated {
			resp.JSON(&winner)
		} else {
			resp.Want(http.StatusConflict)
		}
	}
	if codes[http.StatusCreated] != 1 || codes[http.StatusConflict] != 1 {
		t.Fatalf("competing comment statuses = %v, want one 201 and one 409", codes)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = ANY($1::uuid[])`, issues); n != 1 {
		t.Fatalf("competing claims left %d comments, want one", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND comment_id = $2 AND issue_id = $3`, attachmentID, winner.ID, winner.IssueID); n != 1 {
		t.Fatal("attachment does not belong to winning comment")
	}
}

// Every mutation here takes its owners first — the issue, then the comment,
// then the attachment. A comment created with an attachment takes the issue in
// its CreateComment statement and only then locks the attachment rows, so it
// cannot close a cycle with the two writers that reach an attachment through
// its issue: DeleteAttachment (issue, then the row) and issue teardown (issue,
// then the rows its cascade removes). The holders below play each of those one
// statement at a time.
// waitForBlockedQuery waits until the request under test is blocked on a row
// lock and returns which of names it is waiting in. Unlike waiting for one
// specific statement, this lets the caller carry on and prove what the wrong
// lock order actually costs: the holder's next statement deadlocking.
func waitForBlockedQuery(t *testing.T, done <-chan error, names ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("request completed before it blocked on any row lock: %v", err)
		default:
		}
		for _, name := range names {
			var waiting bool
			if err := testPool.QueryRow(context.Background(), `
				SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity
					WHERE datname = current_database()
					  AND wait_event_type = 'Lock'
					  AND query LIKE '%' || $1 || '%'
				)
			`, "-- name: "+name+" :").Scan(&waiting); err != nil {
				t.Fatalf("observe blocked %s: %v", name, err)
			}
			if waiting {
				return name
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request blocked on none of %v", names)
	return ""
}

func TestCreateCommentWithAttachmentFollowsOwnerFirstLockOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name         string
		holdSQL      string
		wantCode     int
		wantComments int
		wantLinked   int
	}{
		{
			// The attachment is gone by the time the create proceeds: refuse
			// before any comment exists rather than commit one without it.
			name:         "attachment deleted while the create waits",
			holdSQL:      `DELETE FROM attachment WHERE id = $1`,
			wantCode:     http.StatusConflict,
			wantComments: 0,
			wantLinked:   0,
		},
		{
			// The holder took the issue and released it without touching the
			// attachment: the create proceeds and links it.
			name:         "attachment kept",
			holdSQL:      `SELECT id FROM attachment WHERE id = $1`,
			wantCode:     http.StatusCreated,
			wantComments: 1,
			wantLinked:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			issueID := dbfx.Issue(t, "comment attachment vs attachment delete")
			attachmentID := unlinkedIssueAttachment(t, issueID)

			holder, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin holder: %v", err)
			}
			defer holder.Rollback(context.Background())
			// DeleteAttachment's own order, through its own query: the owner
			// issue, then the row. Taking the lock here by hand would let a
			// production mode too weak to serialize issue writers pass.
			qtx := testHandler.Queries.WithTx(holder)
			if _, err := qtx.LockIssueForAttachmentWrite(ctx, db.LockIssueForAttachmentWriteParams{
				ID:          parseUUID(issueID),
				WorkspaceID: parseUUID(testWorkspaceID),
			}); err != nil {
				t.Fatalf("hold issue: %v", err)
			}
			if _, err := holder.Exec(ctx, tt.holdSQL, attachmentID); err != nil {
				t.Fatalf("hold attachment: %v", err)
			}

			var got *testutil.Response
			done := make(chan error, 1)
			go func() {
				got = createCommentWithAttachment(t, testHandler, issueID, attachmentID)
				done <- nil
			}()
			// The create must wait for the issue while holding no attachment
			// lock. Under a weaker owner lock it gets past the issue and waits
			// on the attachment instead, which the bump below then deadlocks
			// against.
			blockedIn := waitForBlockedQuery(t, done, "CreateComment", "LockAttachmentsForCommentLink")

			// The issue revision bump the delete statement performs last. The
			// create is already waiting for the issue, so this cannot wait for
			// the attachment the holder still owns.
			_, holdErr := holder.Exec(ctx, `UPDATE issue SET revision = revision + 1 WHERE id = $1`, issueID)
			if holdErr == nil {
				holdErr = holder.Commit(ctx)
			}
			<-done
			if holdErr != nil {
				t.Fatalf("attachment delete deadlocked or failed while the create waited in %s: %v", blockedIn, holdErr)
			}
			if blockedIn != "CreateComment" {
				t.Fatalf("create blocked in %s, want the owner issue in CreateComment", blockedIn)
			}
			got.Want(tt.wantCode)
			if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != tt.wantComments {
				t.Fatalf("issue has %d comments, want %d", n, tt.wantComments)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1 AND comment_id IS NOT NULL`, attachmentID); n != tt.wantLinked {
				t.Fatalf("attachment linked rows = %d, want %d", n, tt.wantLinked)
			}
		})
	}
}

// The symmetric side: issue teardown holds the issue and then reaches the
// attachment through the issue_id cascade. A create that took the attachment
// first would deadlock with it; taking the issue first means the create simply
// waits, and then finds the issue gone.
func TestCreateCommentWithAttachmentDoesNotDeadlockWithIssueTeardown(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	issueID := dbfx.Issue(t, "comment attachment vs issue teardown")
	attachmentID := unlinkedIssueAttachment(t, issueID)

	teardown, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin issue teardown: %v", err)
	}
	defer teardown.Rollback(context.Background())
	qtx := testHandler.Queries.WithTx(teardown)
	if _, err := qtx.LockIssueForDelete(ctx, db.LockIssueForDeleteParams{
		ID:          parseUUID(issueID),
		WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("lock issue for teardown: %v", err)
	}

	var got *testutil.Response
	done := make(chan error, 1)
	go func() {
		got = createCommentWithAttachment(t, testHandler, issueID, attachmentID)
		done <- nil
	}()
	waitForCommentMutationLock(t, "CreateComment", done)

	// The cascade reaches the attachment the blocked create asked for.
	deleteErr := qtx.DeleteIssue(ctx, db.DeleteIssueParams{
		ID:          parseUUID(issueID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if deleteErr == nil {
		deleteErr = teardown.Commit(ctx)
	}
	<-done
	if deleteErr != nil {
		t.Fatalf("issue teardown deadlocked or failed: %v", deleteErr)
	}
	got.Want(http.StatusNotFound)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("deleted issue kept %d comments", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1`, attachmentID); n != 0 {
		t.Fatalf("deleted issue kept its attachment")
	}
}

// DeleteAttachment itself must take the issue before the attachment row, so it
// queues behind issue teardown instead of deadlocking with it.
func TestDeleteAttachmentTakesTheIssueBeforeTheRow(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// The owner lock has to hold against both kinds of issue writer: teardown,
	// which takes FOR UPDATE, and an ordinary one such as CreateComment's
	// revision bump, which takes FOR NO KEY UPDATE. A mode that only conflicts
	// with the first leaves the create/delete cycle open.
	for _, holderMode := range []string{"FOR UPDATE", "FOR NO KEY UPDATE"} {
		t.Run(holderMode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			issueID := dbfx.Issue(t, "attachment delete vs issue lock")
			attachmentID := unlinkedIssueAttachment(t, issueID)

			holder, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin holder: %v", err)
			}
			defer holder.Rollback(context.Background())
			if _, err := holder.Exec(ctx, `SELECT id FROM issue WHERE id = $1 `+holderMode, issueID); err != nil {
				t.Fatalf("hold issue: %v", err)
			}

			var got *testutil.Response
			done := make(chan error, 1)
			go func() {
				req := newRequest(http.MethodDelete, "/api/attachments/"+attachmentID, nil)
				got = testutil.Call(t, testHandler.DeleteAttachment, withURLParam(req, "id", attachmentID))
				done <- nil
			}()
			waitForCommentMutationLock(t, "LockIssueForAttachmentWrite", done)

			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("release issue: %v", err)
			}
			<-done
			got.Want(http.StatusNoContent)
			if n := dbfx.Count(t, `SELECT count(*) FROM attachment WHERE id = $1`, attachmentID); n != 0 {
				t.Fatalf("attachment survived its delete")
			}
		})
	}
}
