package handler

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCommentDeliveryObligationOldReceiptDoesNotAcknowledgeWaitingEdit(t *testing.T) {
	testCommentDeliveryWaitingEdit(t, false)
}

func TestCommentDeliveryObligationOldCancellationDoesNotAcknowledgeWaitingEdit(t *testing.T) {
	testCommentDeliveryWaitingEdit(t, true)
}

func testCommentDeliveryWaitingEdit(t *testing.T, cancelOldTask bool) {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Receipt before waiting edit")
	commentID := fixture.commentID[2]
	dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, fixture.issueID)
	barrier := &commentWriteBarrierDB{Pool: testPool, statement: "UpdateComment", entered: make(chan struct{}), release: make(chan struct{})}
	h := New(db.New(barrier), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
	h.TxStarter = barrier
	h.TaskService.TxStarter = barrier
	before, err := h.Queries.GetComment(t.Context(), parseUUID(commentID))
	if err != nil {
		t.Fatal(err)
	}
	newContent := "[@Target](mention://agent/" + fixture.agentID + ") New instruction after the claimed body"
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.UpdateComment(recorder, withURLParam(newRequest("PATCH", "/api/comments/"+commentID, map[string]any{"content": newContent, "expected_revision": before.Revision}).WithContext(context.WithValue(t.Context(), commentWriteRoleKey{}, "writer")), "commentId", commentID))
	}()
	var release sync.Once
	unblock := func() { release.Do(func() { close(barrier.release) }) }
	t.Cleanup(func() {
		unblock()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("waiting edit did not stop")
		}
	})
	select {
	case <-barrier.entered:
	case <-done:
		t.Fatalf("edit exited before its mutation: %d", recorder.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("edit did not reach the mutation")
	}
	claimed := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
	if claimed.TriggerCommentContent != before.Content {
		t.Fatalf("claim rendered %q, want original body %q", claimed.TriggerCommentContent, before.Content)
	}
	if revisions := deliveredCommentRevisionsForTask(t, fixture.taskID); revisions[commentID] != before.Revision {
		t.Fatalf("claim did not record the rendered source revision: %v", revisions)
	}
	started, err := testHandler.TaskService.StartTask(t.Context(), parseUUID(fixture.taskID))
	if err != nil {
		t.Fatal(err)
	}
	if cancelOldTask {
		if _, err := testHandler.TaskService.CancelTaskByUser(t.Context(), parseUUID(fixture.taskID), service.TaskCancellationActor{Type: "member", ID: parseUUID(testUserID)}); err != nil {
			t.Fatal(err)
		}
		var revision int64
		dbfx.QueryRow(t, `SELECT (context->'cancelled_comment_revisions'->>$2)::bigint FROM agent_task_queue WHERE id=$1`, fixture.taskID, commentID).Scan(&revision)
		if revision != before.Revision {
			t.Fatalf("cancellation acknowledged revision %d, want old source %d", revision, before.Revision)
		}
	} else {
		// Complete the synthetic daemon run without calling completion replay
		// while the real editing request is paused. Its receipt stays unchanged.
		dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, fixture.taskID)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("edit did not finish")
	}
	if recorder.Code != 200 {
		t.Fatalf("edit returned %d: %s", recorder.Code, recorder.Body.String())
	}
	after, err := h.Queries.GetComment(t.Context(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !started.StartedAt.Valid || !started.StartedAt.Time.After(after.UpdatedAt.Time) {
		t.Fatal("fixture did not place the old-body start after the editing transaction timestamp")
	}
	if !commentCoveredByAnyActiveTask(t, fixture.issueID, fixture.agentID, commentID) {
		t.Fatalf("old-body receipt acknowledged the new revision; response: %s", recorder.Body.String())
	}
	next := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
	if next.TriggerCommentContent != newContent {
		t.Fatalf("follow-up rendered %q, want new instruction %q", next.TriggerCommentContent, newContent)
	}
	if revisions := deliveredCommentRevisionsForTask(t, next.ID); revisions[commentID] != after.Revision {
		t.Fatalf("follow-up receipt did not record the new revision: %v", revisions)
	}
}

func TestCommentDeliveryObligationBoundSupplementRejectsNewRevision(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		name := "pending"
		if delivered {
			name = "delivered"
		}
		t.Run(name, func(t *testing.T) {
			f := newSupplementFixture(t, "codex", "running", true)
			var created CommentResponse
			content := agentMention("Target", f.agentID) + " Guidance for this turn"
			postSteeredComment(t, f.issueID, f.triggerID, content, f.taskID).Want(201).JSON(&created)
			if len(created.Supplements) != 1 || created.Supplements[0].TaskID != f.taskID {
				t.Fatalf("comment was not bound to the running turn: %+v", created.Supplements)
			}
			if delivered {
				if _, err := testHandler.Queries.ClaimNextTaskSupplement(t.Context(), db.ClaimNextTaskSupplementParams{TaskID: parseUUID(f.taskID), IncludeAgentAuthored: true}); err != nil {
					t.Fatal(err)
				}
				if _, err := testHandler.Queries.AckTaskSupplementDelivered(t.Context(), db.AckTaskSupplementDeliveredParams{TaskID: parseUUID(f.taskID), CommentID: parseUUID(created.ID)}); err != nil {
					t.Fatal(err)
				}
			}
			testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest("PATCH", "/api/comments/"+created.ID, map[string]any{"content": content + " Edited", "expected_revision": created.Revision}), "commentId", created.ID)).Want(409)
			comment, err := testHandler.Queries.GetComment(t.Context(), parseUUID(created.ID))
			if err != nil {
				t.Fatal(err)
			}
			if comment.Revision != created.Revision || comment.Content != content {
				t.Fatal("bound supplement accepted a new body under an old receipt")
			}
		})
	}
}
