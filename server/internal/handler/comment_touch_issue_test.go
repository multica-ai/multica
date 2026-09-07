package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestCreateComment_BumpsIssueActivity pins MUL-5009 and MUL-6343: a new
// comment advances both the legacy updated_at clock and the semantic activity
// clock in the same statement.
func TestCreateComment_BumpsIssueActivity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "comment bumps updated_at", "", "")

	var before, activityBefore time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at, last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&before, &activityBefore); err != nil {
		t.Fatalf("read issue clocks before: %v", err)
	}

	// Guarantee a measurable wall-clock gap so the bump is unambiguous; now() is
	// evaluated per statement and the issue was inserted moments ago.
	time.Sleep(10 * time.Millisecond)

	w := httptest.NewRecorder()
	r := withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "a fresh comment"}),
		"id", issueID,
	)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var after, activityAfter time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at, last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&after, &activityAfter); err != nil {
		t.Fatalf("read issue clocks after: %v", err)
	}

	if !after.After(before) {
		t.Fatalf("issue updated_at was not bumped by a new comment: before=%s after=%s", before, after)
	}
	if !activityAfter.After(activityBefore) {
		t.Fatalf("issue last_activity_at was not bumped by a new comment: before=%s after=%s", activityBefore, activityAfter)
	}
}

// TestCreateComment_WorkspaceMismatchPersistsNothing pins the tenant-integrity
// guarantee of the CreateComment CTE (MUL-5009 nit2): CreateComment is the
// single carrier of "a comment always belongs to an issue in the same
// workspace and always bumps it". If the passed workspace does not match the
// target issue's workspace, the leading UPDATE matches no issue row, the
// dependent INSERT selects nothing, and the :one query returns pgx.ErrNoRows —
// so no mis-attributed comment is written and the issue is not touched.
func TestCreateComment_WorkspaceMismatchPersistsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "comment workspace guard", "", "")

	var before, activityBefore time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at, last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&before, &activityBefore); err != nil {
		t.Fatalf("read issue clocks before: %v", err)
	}

	// A workspace that is NOT the issue's workspace: the issue exists, but the
	// (issue, workspace) pair matches no row.
	wrongWorkspace := parseUUID("11111111-1111-1111-1111-111111111111")
	_, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: wrongWorkspace,
		AuthorType:  "member",
		AuthorID:    parseUUID(testUserID),
		Content:     "should never persist",
		Type:        "comment",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows on workspace mismatch, got %v", err)
	}

	var commentCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&commentCount); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("workspace mismatch must persist no comment, found %d", commentCount)
	}

	var after, activityAfter time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at, last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&after, &activityAfter); err != nil {
		t.Fatalf("read issue clocks after: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("workspace mismatch must not bump updated_at: before=%s after=%s", before, after)
	}
	if !activityAfter.Equal(activityBefore) {
		t.Fatalf("workspace mismatch must not bump last_activity_at: before=%s after=%s", activityBefore, activityAfter)
	}
}

func TestUpdateAndDeleteCommentBumpIssueActivity(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := createCommentTriggerPreviewIssue(t, "comment mutations bump activity", "", "")
	comment, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: parseUUID(testWorkspaceID),
		AuthorType:  "member",
		AuthorID:    parseUUID(testUserID),
		Content:     "before",
		Type:        "comment",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	base := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	setBase := func() {
		t.Helper()
		if _, err := testPool.Exec(ctx, `UPDATE issue SET last_activity_at = $2 WHERE id = $1`, issueID, base); err != nil {
			t.Fatalf("reset last_activity_at: %v", err)
		}
	}
	readActivity := func() time.Time {
		t.Helper()
		var got time.Time
		if err := testPool.QueryRow(ctx, `SELECT last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&got); err != nil {
			t.Fatalf("read last_activity_at: %v", err)
		}
		return got
	}

	setBase()
	updated, err := testHandler.Queries.UpdateComment(ctx, db.UpdateCommentParams{
		ID:      comment.ID,
		Content: "after",
	})
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if updated.IssueRevision == 0 {
		t.Fatal("comment edit omitted the advanced issue revision")
	}
	if got := readActivity(); !got.After(base) {
		t.Fatalf("comment edit did not advance activity: base=%s got=%s", base, got)
	}

	setBase()
	noOp, err := testHandler.Queries.UpdateComment(ctx, db.UpdateCommentParams{
		ID:      comment.ID,
		Content: "after",
	})
	if err != nil {
		t.Fatalf("no-op UpdateComment: %v", err)
	}
	if noOp.IssueRevision != 0 {
		t.Fatalf("no-op comment edit issue revision = %d, want 0", noOp.IssueRevision)
	}
	if got := readActivity(); !got.Equal(base) {
		t.Fatalf("no-op comment edit changed activity: base=%s got=%s", base, got)
	}

	setBase()
	deleted, err := testHandler.Queries.DeleteComment(ctx, db.DeleteCommentParams{
		ID:          comment.ID,
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil || !deleted.Changed {
		t.Fatalf("DeleteComment = (%+v, %v), want changed result", deleted, err)
	}
	if deleted.IssueRevision == 0 {
		t.Fatal("comment delete omitted the advanced issue revision")
	}
	if got := readActivity(); !got.After(base) {
		t.Fatalf("comment delete did not advance activity: base=%s got=%s", base, got)
	}
}

func TestCommentMutationEventsCarryIssueRevision(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "comment mutation event revision")
	commentID := dbfx.Comment(t, issueID, "before")

	h := *testHandler
	h.Bus = events.New()
	var updatedEvent, deletedEvent events.Event
	h.Bus.Subscribe(protocol.EventCommentUpdated, func(event events.Event) {
		updatedEvent = event
	})
	h.Bus.Subscribe(protocol.EventCommentDeleted, func(event events.Event) {
		deletedEvent = event
	})

	update := httptest.NewRecorder()
	h.UpdateComment(update, withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
		"content": "after",
	}), "commentId", commentID))
	if update.Code != http.StatusOK {
		t.Fatalf("UpdateComment = %d: %s", update.Code, update.Body.String())
	}
	var response CommentResponse
	if err := json.NewDecoder(update.Body).Decode(&response); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if response.IssueRevision != 2 {
		t.Fatalf("update response issue_revision = %d, want 2", response.IssueRevision)
	}
	updatedPayload, ok := updatedEvent.Payload.(map[string]any)
	if !ok || updatedPayload["issue_revision"] != int64(2) {
		t.Fatalf("update event issue_revision = %#v, want 2", updatedEvent.Payload)
	}

	deleted := httptest.NewRecorder()
	h.DeleteComment(deleted, withURLParam(newRequest(http.MethodDelete, "/api/comments/"+commentID, nil), "commentId", commentID))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("DeleteComment = %d: %s", deleted.Code, deleted.Body.String())
	}
	deletedPayload, ok := deletedEvent.Payload.(map[string]any)
	if !ok || deletedPayload["issue_revision"] != int64(3) {
		t.Fatalf("delete event issue_revision = %#v, want 3", deletedEvent.Payload)
	}
}

// A conditional update that loses after waiting for the comment row must not
// advance the parent issue. The old sibling CTEs could run the issue UPDATE
// from a stale target snapshot even when the comment UPDATE later matched no
// rows, producing phantom activity and a phantom owner revision.
func TestUpdateCommentLosingRaceDoesNotTouchIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := dbfx.Issue(t, "losing comment update", testutil.Cols{
		"revision":         7,
		"last_activity_at": testutil.Raw("'2020-01-01T00:00:00Z'::timestamptz"),
	})
	commentID := dbfx.Comment(t, issueID, "base")

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT id FROM comment WHERE id = $1 FOR UPDATE`, commentID); err != nil {
		t.Fatalf("lock comment: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, updateErr := testHandler.Queries.UpdateComment(context.Background(), db.UpdateCommentParams{
			ID:               parseUUID(commentID),
			Content:          "loser",
			ExpectedRevision: pgtype.Int8{Int64: 1, Valid: true},
			ContentBase:      pgtype.Text{String: "base", Valid: true},
		})
		done <- updateErr
	}()
	waitForCommentMutationLock(t, "UpdateComment", done)

	if _, err := holder.Exec(ctx, `UPDATE comment SET content = 'winner', revision = revision + 1 WHERE id = $1`, commentID); err != nil {
		t.Fatalf("commit winning edit: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("losing update error = %v, want pgx.ErrNoRows", err)
	}

	assertIssueActivityState(t, issueID, 7, time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC))
}

// A delete that loses after waiting for the same comment row likewise must not
// mutate the issue. The issue touch now depends on DELETE ... RETURNING, so an
// empty delete result is an empty activity source.
func TestDeleteCommentLosingRaceDoesNotTouchIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := dbfx.Issue(t, "losing comment delete", testutil.Cols{
		"revision":         11,
		"last_activity_at": testutil.Raw("'2020-01-02T00:00:00Z'::timestamptz"),
	})
	commentID := dbfx.Comment(t, issueID, "delete me")

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT id FROM comment WHERE id = $1 FOR UPDATE`, commentID); err != nil {
		t.Fatalf("lock comment: %v", err)
	}

	type deleteResult struct {
		row db.DeleteCommentRow
		err error
	}
	done := make(chan deleteResult, 1)
	go func() {
		row, deleteErr := testHandler.Queries.DeleteComment(context.Background(), db.DeleteCommentParams{
			ID:          parseUUID(commentID),
			WorkspaceID: parseUUID(testWorkspaceID),
		})
		done <- deleteResult{row: row, err: deleteErr}
	}()
	lockDone := make(chan error, 1)
	go func() {
		result := <-done
		if result.err == nil && result.row.Changed {
			lockDone <- errors.New("losing delete unexpectedly changed a row")
			return
		}
		lockDone <- result.err
	}()
	waitForCommentMutationLock(t, "DeleteComment", lockDone)

	if _, err := holder.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID); err != nil {
		t.Fatalf("commit winning delete: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	if err := <-lockDone; err != nil {
		t.Fatalf("losing delete: %v", err)
	}

	assertIssueActivityState(t, issueID, 11, time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC))
}

// Issue teardown locks the owner and then cascades into comments. A comment
// mutation must wait for that owner lock before taking the child lock; taking
// them in the opposite order would deadlock when teardown reaches the cascade.
func TestCommentMutationsFollowIssueTeardownLockOrder(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name      string
		queryName string
		mutate    func(context.Context, string) error
	}{
		{
			name:      "update",
			queryName: "UpdateComment",
			mutate: func(ctx context.Context, commentID string) error {
				_, err := testHandler.Queries.UpdateComment(ctx, db.UpdateCommentParams{
					ID:      parseUUID(commentID),
					Content: "concurrent edit",
				})
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				return errors.New("comment update succeeded after its issue was deleted")
			},
		},
		{
			name:      "delete",
			queryName: "DeleteComment",
			mutate: func(ctx context.Context, commentID string) error {
				result, err := testHandler.Queries.DeleteComment(ctx, db.DeleteCommentParams{
					ID:          parseUUID(commentID),
					WorkspaceID: parseUUID(testWorkspaceID),
				})
				if err != nil {
					return err
				}
				if result.Changed {
					return errors.New("comment delete changed a row after its issue was deleted")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			issueID := dbfx.Issue(t, "comment mutation vs issue teardown "+tt.name)
			commentID := dbfx.Comment(t, issueID, "before")

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

			mutationDone := make(chan error, 1)
			go func() {
				mutationDone <- tt.mutate(ctx, commentID)
			}()
			waitForCommentMutationLock(t, tt.queryName, mutationDone)

			// This is the delete phase of deleteIssueAndCollectAttachmentURLs.
			// If the mutation took the comment before waiting for the issue, the
			// ON DELETE CASCADE below closes a real issue/comment deadlock cycle.
			deleteErr := qtx.DeleteIssue(ctx, db.DeleteIssueParams{
				ID:          parseUUID(issueID),
				WorkspaceID: parseUUID(testWorkspaceID),
			})
			if deleteErr == nil {
				deleteErr = teardown.Commit(ctx)
			} else {
				_ = teardown.Rollback(context.Background())
			}
			mutationErr := <-mutationDone
			if deleteErr != nil {
				t.Fatalf("issue teardown deadlocked or failed: %v", deleteErr)
			}
			if mutationErr != nil {
				t.Fatalf("comment mutation deadlocked or returned an unexpected result: %v", mutationErr)
			}
		})
	}
}

func waitForCommentMutationLock(t *testing.T, queryName string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("%s completed before the competing row lock was released: %v", queryName, err)
		default:
		}
		var waiting bool
		if err := testPool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%' || $1 || '%'
			)
		`, "-- name: "+queryName+" :one").Scan(&waiting); err != nil {
			t.Fatalf("observe blocked %s: %v", queryName, err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not block on the competing comment lock", queryName)
}

func assertIssueActivityState(t *testing.T, issueID string, wantRevision int64, wantActivity time.Time) {
	t.Helper()
	var revision int64
	var activity time.Time
	dbfx.QueryRow(t, `SELECT revision, last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&revision, &activity)
	if revision != wantRevision || !activity.Equal(wantActivity) {
		t.Fatalf("issue activity = (revision %d, %s), want (%d, %s)", revision, activity, wantRevision, wantActivity)
	}
}
