package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type retryInsertGate struct {
	pool   *pgxpool.Pool
	ready  chan uint32
	resume chan struct{}
	once   sync.Once
}

func (g *retryInsertGate) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &retryInsertGateTx{Tx: tx, gate: g}, nil
}

type retryInsertGateTx struct {
	pgx.Tx
	gate *retryInsertGate
}

func (tx *retryInsertGateTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: CreateRetryTask :one") {
		tx.gate.once.Do(func() {
			tx.gate.ready <- tx.Conn().PgConn().PID()
			select {
			case <-tx.gate.resume:
			case <-ctx.Done():
			}
		})
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

// The retry INSERT must already hold the agent binding stable. Otherwise
// UpdateAgent can sweep the queue and commit B before the retry writes A.
func TestRetryInsertSerializesWithRuntimeSwitch(t *testing.T) {
	for _, path := range []string{"FailTask", "MaybeRetryFailedTask"} {
		t.Run(path, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			oldRuntime := dbfx.Runtime(t, "retry-race-A")
			newRuntime := dbfx.Runtime(t, "retry-race-B")
			agentID := dbfx.Agent(t, "retry-race-agent", oldRuntime)
			parentID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": oldRuntime, "issue_id": dbfx.Issue(t, "retry during rebind"),
				"status": "dispatched", "attempt": 1, "max_attempts": 2,
			})
			dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE parent_task_id=$1", parentID)
			if path == "MaybeRetryFailedTask" {
				dbfx.Exec(t, "UPDATE agent_task_queue SET status='failed', failure_reason='timeout' WHERE id=$1", parentID)
			}
			gate := &retryInsertGate{pool: testPool, ready: make(chan uint32, 1), resume: make(chan struct{})}
			svc := service.NewTaskService(testHandler.Queries, gate, nil, events.New())
			retryResult := make(chan error, 1)
			retryDone := make(chan struct{})
			go func() {
				defer close(retryDone)
				if path == "FailTask" {
					_, err := svc.FailTask(ctx, parseUUID(parentID), "prepare timeout", "", "", "", "timeout", false, "", "")
					retryResult <- err
					return
				}
				parent, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(parentID))
				if err == nil {
					_, err = svc.MaybeRetryFailedTask(ctx, parent)
				}
				retryResult <- err
			}()
			defer func() { cancel(); <-retryDone }()
			var retryPID uint32
			select {
			case retryPID = <-gate.ready:
			case err := <-retryResult:
				t.Fatalf("retry stopped before insert: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]string{"runtime_id": newRuntime}).WithContext(ctx), "id", agentID)
			rebindResult := make(chan *httptest.ResponseRecorder, 1)
			rebindDone := make(chan struct{})
			go func() {
				defer close(rebindDone)
				w := httptest.NewRecorder()
				testHandler.UpdateAgent(w, req)
				rebindResult <- w
			}()
			defer func() { cancel(); <-rebindDone }()

			var response *httptest.ResponseRecorder
			// Observe a real PostgreSQL lock wait, or the old broken switch
			// completing before the insert. No scheduler timing assumption.
			for response == nil {
				select {
				case response = <-rebindResult:
				default:
				}
				var blocked bool
				if err := testPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))", retryPID).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked || response != nil {
					break
				}
				select {
				case <-time.After(5 * time.Millisecond):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			close(gate.resume)
			if err := <-retryResult; err != nil {
				t.Fatalf("retry: %v", err)
			}
			if response == nil {
				response = <-rebindResult
			}
			if response.Code != http.StatusOK {
				t.Fatalf("rebind: %d %s", response.Code, response.Body.String())
			}
			var childID string
			dbfx.QueryRow(t, "SELECT id::text FROM agent_task_queue WHERE parent_task_id=$1", parentID).Scan(&childID)
			if got := taskRuntimes(t, childID)[childID]; got != newRuntime {
				t.Fatalf("retry INSERT escaped the runtime switch: runtime=%s, want B=%s", got, newRuntime)
			}
			if old, err := svc.ClaimTaskForRuntime(ctx, parseUUID(oldRuntime)); err != nil || old != nil {
				t.Fatalf("old runtime claim=%+v/%v, want nothing", old, err)
			}
			claimed, err := svc.ClaimTaskForRuntime(ctx, parseUUID(newRuntime))
			if err != nil || claimed == nil || uuidToString(claimed.ID) != childID {
				t.Fatalf("new runtime claim=%+v/%v, want retry %s", claimed, err, childID)
			}
			if again, err := svc.ClaimTaskForRuntime(ctx, parseUUID(newRuntime)); err != nil || again != nil {
				t.Fatalf("duplicate claim=%+v/%v", again, err)
			}
		})
	}
}
