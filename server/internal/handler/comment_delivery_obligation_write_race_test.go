package handler

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type commentWriteRoleKey struct{}

type commentWriteBarrierDB struct {
	*pgxpool.Pool
	statement            string
	entered, release     chan struct{}
	once                 sync.Once
	writerPID, cancelPID atomic.Uint32
	mutatedTaskID        atomic.Value
}

func (d *commentWriteBarrierDB) before(ctx context.Context, sql string) {
	if ctx.Value(commentWriteRoleKey{}) == "writer" && strings.HasPrefix(sql, "-- name: "+d.statement+" ") {
		d.once.Do(func() { close(d.entered); <-d.release })
	}
}

func (d *commentWriteBarrierDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.before(ctx, sql)
	if ctx.Value(commentWriteRoleKey{}) == "cancel" && strings.HasPrefix(sql, "-- name: UpsertCommentDeliveryObligation ") {
		conn, err := d.Pool.Acquire(ctx)
		if err != nil {
			return errRow{err: err}
		}
		d.cancelPID.Store(conn.Conn().PgConn().PID())
		return commentObservedConnectionRow{Row: conn.QueryRow(ctx, sql, args...), conn: conn}
	}
	return d.observe(ctx, sql, d.Pool.QueryRow(ctx, sql, args...))
}

type commentObservedConnectionRow struct {
	pgx.Row
	conn *pgxpool.Conn
}

func (r commentObservedConnectionRow) Scan(dest ...any) error {
	defer r.conn.Release()
	return r.Row.Scan(dest...)
}

func (d *commentWriteBarrierDB) observe(ctx context.Context, sql string, row pgx.Row) pgx.Row {
	if ctx.Value(commentWriteRoleKey{}) == "writer" && strings.HasPrefix(sql, "-- name: "+d.statement+" ") {
		return commentWriteResultRow{Row: row, db: d}
	}
	return row
}

type commentWriteResultRow struct {
	pgx.Row
	db *commentWriteBarrierDB
}

func (r commentWriteResultRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	if len(dest) > 0 {
		if id, ok := dest[0].(*pgtype.UUID); ok {
			r.db.mutatedTaskID.Store(uuidToString(*id))
		}
	}
	return nil
}

// Observe the actual transaction PID as well as pool-based writes. The fixed
// implementation may correctly block withdrawal until its guarded write commits.
func (d *commentWriteBarrierDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	pid := tx.Conn().PgConn().PID()
	switch ctx.Value(commentWriteRoleKey{}) {
	case "writer":
		d.writerPID.Store(pid)
	case "cancel":
		d.cancelPID.Store(pid)
	}
	return commentWriteBarrierTx{Tx: tx, db: d}, nil
}

type commentWriteBarrierTx struct {
	pgx.Tx
	db *commentWriteBarrierDB
}

func (tx commentWriteBarrierTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.db.before(ctx, sql)
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx commentWriteBarrierTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.db.before(ctx, sql)
	return tx.db.observe(ctx, sql, tx.Tx.QueryRow(ctx, sql, args...))
}

func TestCommentDeliveryObligationWithdrawalAtActualWrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, path := range []string{"enqueue", "merge", "planned"} {
		for _, action := range []string{"cancel agent", "cancel issue", "edit note", "legacy edit note", "delete comment"} {
			t.Run(path+"/"+action, func(t *testing.T) {
				runtimeID := dbfx.Runtime(t, "write boundary runtime")
				agentID := dbfx.Agent(t, "write boundary recipient", runtimeID)
				issueID := dbfx.Issue(t, "write boundary issue")
				seedDupRacePR(t, issueID, 9995991)
				root := dbfx.Comment(t, issueID, "Old-head input")
				blocker := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root, "status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA}})
				dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
				var accepted CommentResponse
				testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "[@Target](mention://agent/" + agentID + ") Process this input", "parent_id": root}), "id", issueID)).Want(201).JSON(&accepted)
				if len(accepted.TriggerOutcomes) != 1 || accepted.TriggerOutcomes[0].Status != DispatchDeferred {
					t.Fatalf("input was not accepted: %+v", accepted.TriggerOutcomes)
				}
				statement := "CreateAgentTask"
				switch path {
				case "enqueue":
					dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1`, blocker)
				case "merge":
					statement = "MergeCommentIntoPendingTask"
					dbfx.Exec(t, `UPDATE agent_task_queue SET context=jsonb_build_object('head_sha',$2::text) WHERE id=$1`, blocker, dupRaceHeadB)
				case "planned":
					statement = "RegisterPlannedCommentForActiveTask"
					dbfx.Exec(t, `UPDATE agent_task_queue SET status='dispatched', dispatched_at=now(), context=jsonb_build_object('head_sha',$2::text) WHERE id=$1`, blocker, dupRaceHeadB)
				}
				barrier := &commentWriteBarrierDB{Pool: testPool, statement: statement, entered: make(chan struct{}), release: make(chan struct{})}
				h := New(db.New(barrier), testPool, testHandler.Hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, Config{})
				h.TxStarter = barrier
				h.TaskService.TxStarter = barrier
				issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
				if err != nil {
					t.Fatal(err)
				}
				agent, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(agentID))
				if err != nil {
					t.Fatal(err)
				}
				source := commentDeliverySourceForTest(t, parseUUID(accepted.ID))
				writerDone := make(chan struct{})
				go func() {
					defer close(writerDone)
					h.resolveCommentTriggerEnqueue(context.WithValue(t.Context(), commentWriteRoleKey{}, "writer"), issue, commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent, AlreadyPending: path == "merge"}, source)
				}()
				var release sync.Once
				unblock := func() { release.Do(func() { close(barrier.release) }) }
				t.Cleanup(func() {
					unblock()
					select {
					case <-writerDone:
					case <-time.After(10 * time.Second):
						t.Error("writer did not stop")
					}
				})
				select {
				case <-barrier.entered:
				case <-writerDone:
					t.Fatal("writer exited before the actual mutation")
				case <-time.After(5 * time.Second):
					t.Fatal("writer did not reach actual mutation")
				}
				cancelCtx, stop := context.WithTimeout(context.WithValue(t.Context(), commentWriteRoleKey{}, "cancel"), 8*time.Second)
				defer stop()
				cancelResult := make(chan error, 1)
				go func() {
					var err error
					switch action {
					case "cancel agent":
						_, err = h.TaskService.CancelTasksForAgent(cancelCtx, agent.ID)
					case "cancel issue":
						err = h.TaskService.CancelTasksForIssue(cancelCtx, issue.ID)
					default:
						recorder := httptest.NewRecorder()
						if strings.Contains(action, "edit note") {
							body := map[string]any{"content": "/note Withdrawn"}
							if action == "edit note" {
								body["expected_revision"] = accepted.Revision
							}
							h.UpdateComment(recorder, withURLParam(newRequest("PATCH", "/api/comments/"+accepted.ID, body).WithContext(cancelCtx), "commentId", accepted.ID))
							if recorder.Code != 200 {
								err = fmt.Errorf("edit status=%d: %s", recorder.Code, recorder.Body.String())
							}
						} else {
							h.DeleteComment(recorder, withURLParam(newRequest("DELETE", "/api/comments/"+accepted.ID, nil).WithContext(cancelCtx), "commentId", accepted.ID))
							if recorder.Code != 204 {
								err = fmt.Errorf("delete status=%d: %s", recorder.Code, recorder.Body.String())
							}
						}
					}
					cancelResult <- err
				}()
				// Release only after withdrawal commits or PostgreSQL proves that
				// its transaction waits for this writer. Timing alone is not proof.
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				var cancelErr error
				cancelFinished := false
			waitBoundary:
				for {
					select {
					case cancelErr = <-cancelResult:
						cancelFinished = true
						break waitBoundary
					case <-ticker.C:
						wp, cp := barrier.writerPID.Load(), barrier.cancelPID.Load()
						if wp != 0 && cp != 0 {
							var blocked bool
							if err := testPool.QueryRow(t.Context(), `SELECT $1::integer=ANY(pg_blocking_pids($2::integer))`, int32(wp), int32(cp)).Scan(&blocked); err != nil {
								t.Fatal(err)
							}
							if blocked {
								break waitBoundary
							}
						}
					case <-cancelCtx.Done():
						t.Fatal("withdrawal neither committed nor waited for the guarded writer")
					}
				}
				unblock()
				select {
				case <-writerDone:
				case <-time.After(10 * time.Second):
					t.Fatal("writer did not finish")
				}
				if !cancelFinished {
					select {
					case cancelErr = <-cancelResult:
					case <-time.After(10 * time.Second):
						t.Fatal("withdrawal did not finish")
					}
				}
				if cancelErr != nil {
					t.Fatal(cancelErr)
				}
				if commentCoveredByAnyActiveTask(t, issueID, agentID, accepted.ID) {
					t.Fatal("withdrawn input became active after withdrawal completed")
				}
				if value := barrier.mutatedTaskID.Load(); value != nil {
					if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status IN ('queued','dispatched','running','waiting_local_directory')`, value.(string)); n != 0 {
						t.Fatal("a mutated task survived withdrawal after its comment reference was cleared")
					}
				}
				if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1 AND agent_id=$2`, accepted.ID, agentID); n != 0 {
					t.Fatalf("withdrawn input retained %d obligations", n)
				}
			})
		}
	}
}
