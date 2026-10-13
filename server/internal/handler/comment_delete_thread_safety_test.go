package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Deleting a thread must not leave an active task whose input was deleted,
// and must stay possible after the root alone was deleted.

// admitTaskBeforeDeleteTxStarter runs admit once, committed, right before the
// delete transaction begins: after any cancellation the handler does up
// front, before the delete takes its locks.
type admitTaskBeforeDeleteTxStarter struct {
	delegate *pgxpool.Pool
	admit    func()
	once     sync.Once
}

func (s *admitTaskBeforeDeleteTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	s.once.Do(s.admit)
	return s.delegate.Begin(ctx)
}

// failCancellationTxStarter begins real transactions whose task cancellation
// statements fail.
type failCancellationTxStarter struct{ delegate *pgxpool.Pool }

type failCancellationTx struct{ pgx.Tx }

func (s failCancellationTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.delegate.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failCancellationTx{Tx: tx}, nil
}

func (t failCancellationTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.Contains(query, "-- name: CancelAgentTasks") {
		return nil, errors.New("injected task cancellation failure")
	}
	return t.Tx.Query(ctx, query, args...)
}

type threadTaskFixture struct {
	runtimeID, agentID, issueID, root, reply string
}

func newThreadTaskFixture(t *testing.T, label string) threadTaskFixture {
	t.Helper()
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, label+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, label+" agent")
	root := dbfx.Comment(t, issueID, "root")
	reply := dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": root})
	return threadTaskFixture{runtimeID: runtimeID, agentID: agentID, issueID: issueID, root: root, reply: reply}
}

func (f threadTaskFixture) queueTask(t *testing.T, triggerCommentID string) string {
	t.Helper()
	var taskID string
	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
		VALUES ($1, $2, $3, 'queued', 0, $4)
		RETURNING id
	`, f.agentID, f.runtimeID, f.issueID, triggerCommentID).Scan(&taskID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

func (f threadTaskFixture) deleteThreadRequest() *http.Request {
	return withURLParam(newRequest(http.MethodDelete, "/api/comments/"+f.root+"/thread", nil), "commentId", f.root)
}

func TestDeleteCommentThread_CancelsTaskAdmittedAfterCancellationPass(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newThreadTaskFixture(t, "Thread delete admission race")

	// The reply is already committed; its agent task is admitted only after
	// the handler's up-front cancellation, which therefore cannot see it.
	var taskID string
	h := *testHandler
	h.TxStarter = &admitTaskBeforeDeleteTxStarter{delegate: testPool, admit: func() { taskID = f.queueTask(t, f.reply) }}
	testutil.Call(t, h.DeleteCommentThread, f.deleteThreadRequest()).Want(http.StatusNoContent)

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("task triggered by a deleted reply is %s, want cancelled", status)
	}
}

func TestDeleteCommentThread_CancellationFailureKeepsThread(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newThreadTaskFixture(t, "Thread delete cancellation failure")
	taskID := f.queueTask(t, f.reply)

	starter := failCancellationTxStarter{delegate: testPool}
	h := *testHandler
	h.TxStarter = starter
	h.TaskService = &service.TaskService{Queries: testHandler.Queries, TxStarter: starter}
	w := httptest.NewRecorder()
	h.DeleteCommentThread(w, f.deleteThreadRequest())
	if w.Code == http.StatusNoContent {
		t.Fatalf("thread deleted although its task could not be cancelled")
	}

	var comments int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE id = ANY($1::uuid[])`, []string{f.root, f.reply}).Scan(&comments)
	if comments != 2 {
		t.Errorf("thread has %d comments after a failed delete, want 2", comments)
	}
	var status string
	var trigger *string
	dbfx.QueryRow(t, `SELECT status, trigger_comment_id::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &trigger)
	if status != "queued" || trigger == nil || *trigger != f.reply {
		t.Errorf("task = %s triggered by %v, want queued and triggered by the reply", status, trigger)
	}
}

func TestDeleteCommentThread_AfterRootWasDeleted(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issue := dbfx.Issue(t, "Thread delete after root delete")
	// An ordinary delete tombstoned the root and kept its reply.
	root := dbfx.Comment(t, issue, "", testutil.Cols{"deleted_at": time.Now()})
	dbfx.Comment(t, issue, "reply", testutil.Cols{"parent_id": root})

	req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+root+"/thread", nil), "commentId", root)
	testutil.Call(t, testHandler.DeleteCommentThread, req).Want(http.StatusNoContent)

	entries, status := fetchTimeline(t, issue)
	if status != http.StatusOK {
		t.Fatalf("timeline status = %d, want 200", status)
	}
	if len(entries) != 0 {
		t.Errorf("timeline has %d entries after deleting the thread, want 0", len(entries))
	}
}
