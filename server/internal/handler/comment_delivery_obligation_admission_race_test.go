package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type commentAdmissionBarrierDB struct {
	*pgxpool.Pool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *commentAdmissionBarrierDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: UpsertCommentDeliveryObligation") {
		d.once.Do(func() {
			close(d.entered)
			<-d.release
		})
	}
	return d.Pool.QueryRow(ctx, sql, args...)
}

func TestCommentDeliveryObligationRejectsStaleRoutedSource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, edit := range []string{"note", "retarget"} {
		t.Run(edit, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "stale source runtime")
			oldAgent := dbfx.Agent(t, "stale source old recipient", runtimeID)
			newAgent := dbfx.Agent(t, "stale source new recipient", runtimeID)
			issueID := dbfx.Issue(t, "stale source admission")
			dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			barrier := &commentAdmissionBarrierDB{Pool: testPool, entered: make(chan struct{}), release: make(chan struct{})}
			h := New(db.New(barrier), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
			recorder := httptest.NewRecorder()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				h.CreateComment(recorder, withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
					"content": "[@Old](mention://agent/" + oldAgent + ") Handle this request",
				}), "id", issueID))
			}()
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(barrier.release) })
				select {
				case <-finished:
				case <-time.After(10 * time.Second):
					t.Error("stale admission handler did not exit")
				}
			})
			select {
			case <-barrier.entered:
			case <-finished:
				t.Fatalf("creation exited before admission: %d %s", recorder.Code, recorder.Body.String())
			case <-time.After(5 * time.Second):
				t.Fatal("creation did not reach admission")
			}
			var commentID string
			var revision int64
			dbfx.QueryRow(t, `SELECT id, revision FROM comment WHERE issue_id=$1 AND author_id=$2`, issueID, testUserID).Scan(&commentID, &revision)
			content := "/note Withdraw this request"
			if edit == "retarget" {
				content = "[@New](mention://agent/" + newAgent + ") Handle this request instead"
			}
			testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequest("PATCH", "/api/comments/"+commentID, map[string]any{
				"content": content, "expected_revision": revision,
			}), "commentId", commentID)).Want(200)
			release.Do(func() { close(barrier.release) })
			select {
			case <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("creation did not finish after the edit committed")
			}
			if recorder.Code != 201 {
				t.Fatalf("creation response: %d %s", recorder.Code, recorder.Body.String())
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status IN ('queued','dispatched','running','waiting_local_directory')`, issueID, oldAgent); n != 0 {
				t.Fatalf("stale routing created %d tasks for the old recipient after %s committed", n, edit)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1 AND agent_id=$2`, commentID, oldAgent); n != 0 {
				t.Fatalf("stale routing accepted %d obligations for the old recipient", n)
			}
			if edit == "retarget" && !commentCoveredByAnyActiveTask(t, issueID, newAgent, commentID) {
				t.Fatal("the committed edit lost its new recipient")
			}
		})
	}
}

func TestCommentDeliveryObligationAdmissionWaitingOnEditUsesFreshSource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "waiting admission runtime")
	agentID := dbfx.Agent(t, "waiting admission recipient", runtimeID)
	issueID := dbfx.Issue(t, "admission waiting behind edit", testutil.Cols{"status": "in_progress"})
	seedDupRacePR(t, issueID, 9995993)
	root := dbfx.Comment(t, issueID, "Earlier thread")
	other := dbfx.Comment(t, issueID, "Old-head input", testutil.Cols{"parent_id": root})
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": other, "status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA}})
	dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
	var accepted CommentResponse
	testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "[@Target](mention://agent/" + agentID + ") Accepted before editing", "parent_id": root}), "id", issueID)).Want(201).JSON(&accepted)
	source := commentDeliverySourceForTest(t, parseUUID(accepted.ID))
	issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	barrier := &commentWriteBarrierDB{Pool: testPool, statement: "CancelAgentTasksByTriggerComment", entered: make(chan struct{}), release: make(chan struct{})}
	h := New(db.New(barrier), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
	h.TxStarter = barrier
	h.TaskService.TxStarter = barrier
	editDone := make(chan struct{})
	recorder := httptest.NewRecorder()
	go func() {
		defer close(editDone)
		h.UpdateComment(recorder, withURLParam(newRequest("PATCH", "/api/comments/"+accepted.ID, map[string]any{"content": "/note Withdrawn", "expected_revision": accepted.Revision}).WithContext(context.WithValue(t.Context(), commentWriteRoleKey{}, "writer")), "commentId", accepted.ID))
	}()
	var release sync.Once
	unblock := func() { release.Do(func() { close(barrier.release) }) }
	t.Cleanup(func() {
		unblock()
		select {
		case <-editDone:
		case <-time.After(10 * time.Second):
			t.Error("editing request did not stop")
		}
	})
	select {
	case <-barrier.entered:
	case <-editDone:
		t.Fatalf("edit exited before cancellation: %d", recorder.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("edit did not reach its guarded mutation")
	}
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		h.resolveCommentTriggerEnqueue(context.WithValue(t.Context(), commentWriteRoleKey{}, "cancel"), issue, commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}, source)
	}()
	t.Cleanup(func() {
		unblock()
		select {
		case <-producerDone:
		case <-time.After(10 * time.Second):
			t.Error("waiting admission did not stop")
		}
	})
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
waitForEdit:
	for {
		select {
		case <-producerDone:
			t.Fatal("admission did not wait for the editing transaction")
		case <-ticker.C:
			writer, producer := barrier.writerPID.Load(), barrier.cancelPID.Load()
			if writer != 0 && producer != 0 {
				var blocked bool
				if err := testPool.QueryRow(t.Context(), `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, int32(writer), int32(producer)).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break waitForEdit
				}
			}
		case <-deadline.C:
			t.Fatal("PostgreSQL did not observe admission waiting behind the edit")
		}
	}
	unblock()
	select {
	case <-editDone:
	case <-time.After(10 * time.Second):
		t.Fatal("edit did not finish")
	}
	if recorder.Code != 200 {
		t.Fatalf("edit returned %d: %s", recorder.Code, recorder.Body.String())
	}
	select {
	case <-producerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("admission did not finish after edit committed")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1 AND agent_id=$2`, accepted.ID, agentID); n != 0 {
		t.Fatalf("admission reused a pre-wait snapshot and resurrected %d withdrawn inputs", n)
	}
	if commentCoveredByAnyActiveTask(t, issueID, agentID, accepted.ID) {
		t.Fatal("waiting admission executed withdrawn input")
	}
}
