package service

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"strings"
	"testing"
	"time"
)

type cancelTranscriptTrace struct {
	reads   int
	maxRows int64
}
type cancelTranscriptTraceKey struct{}

func (tr *cancelTranscriptTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, cancelTranscriptTraceKey{}, strings.Contains(data.SQL, "FROM task_message"))
}
func (tr *cancelTranscriptTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if tracked, _ := ctx.Value(cancelTranscriptTraceKey{}).(bool); tracked {
		tr.reads++
		tr.maxRows = max(tr.maxRows, data.CommandTag.RowsAffected())
	}
}

// Check the actual rows transferred from PostgreSQL, not a particular SQL spelling.
func TestCancelChatTranscriptReadIsBounded(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		for _, count := range []int{0, 1, 96} {
			t.Run(fmt.Sprintf("deferred=%t/rows=%d", deferred, count), func(t *testing.T) {
				ctx := context.Background()
				tr := &cancelTranscriptTrace{}
				config := sharedTestPool(t).Config()
				config.ConnConfig.Tracer = tr
				pool, err := pgxpool.NewWithConfig(ctx, config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(pool.Close)
				fx := testutil.New(pool, "", "")
				suffix := time.Now().UnixNano()
				fx.UserID = fx.User(t, "cancel reader", fmt.Sprintf("cancel-reader-%d@example.test", suffix))
				fx.WorkspaceID = fx.Workspace(t, "cancel reader", fmt.Sprintf("cancel-reader-%d", suffix))
				fx.Member(t, fx.WorkspaceID, fx.UserID, "owner")
				runtimeID := fx.Runtime(t, "cancel reader")
				agentID := fx.Agent(t, "cancel reader", runtimeID)
				sessionID := fx.ChatSession(t, agentID)
				cols := testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "running", "started_at": testutil.Raw("now()")}
				if deferred {
					cols["status"] = "cancelled"
					cols["chat_finalize_deferred_at"] = testutil.Raw("now()")
				}
				taskID := fx.Task(t, agentID, cols)
				inputID := fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "role": "user", "content": "keep my input", "task_id": taskID})
				for i := 0; i < count; i++ {
					fx.Insert(t, "task_message", testutil.Cols{"task_id": taskID, "seq": i, "type": "text", "content": strings.Repeat("x", 65536)})
				}
				// A transcript on another task must not block restoring this input.
				other := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed"})
				fx.Insert(t, "task_message", testutil.Cols{"task_id": other, "seq": 1, "type": "text", "content": "other task"})
				fx.Cleanup(t, "DELETE FROM chat_message WHERE chat_session_id=$1", sessionID)
				fx.Cleanup(t, "DELETE FROM chat_draft_restore WHERE chat_session_id=$1", sessionID)
				svc := NewTaskService(db.New(pool), pool, nil, events.New())
				tr.reads = 0
				tr.maxRows = 0
				if deferred {
					if !svc.FinalizeDeferredCancelledChat(ctx, util.MustParseUUID(taskID)) {
						t.Fatal("not finalized")
					}
				} else if _, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(taskID), CancelTaskOptions{}); err != nil {
					t.Fatal(err)
				}
				if tr.reads != 1 || tr.maxRows > 1 {
					t.Fatalf("transcript read transferred %d rows across %d reads; want at most one scalar row", tr.maxRows, tr.reads)
				}
				var inputExists bool
				if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM chat_message WHERE id=$1)", inputID).Scan(&inputExists); err != nil {
					t.Fatal(err)
				}
				if inputExists != (count > 0) {
					t.Fatalf("input exists=%t for %d transcript rows", inputExists, count)
				}
				var stopped int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM chat_message WHERE task_id=$1 AND role='assistant' AND content='Stopped.'", taskID).Scan(&stopped); err != nil {
					t.Fatal(err)
				}
				want := 0
				if count > 0 {
					want = 1
				}
				if stopped != want {
					t.Fatalf("stopped messages=%d want %d", stopped, want)
				}
			})
		}
	}
}
