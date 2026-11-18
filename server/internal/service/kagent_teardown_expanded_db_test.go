package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestKagentExpandedSharedTeardown(t *testing.T) {
	pool := sharedTestPool(t)
	for _, phase := range []string{"pending", "ready", "blocked", "draining", "deleting", "deleted", "none", "query-error"} {
		t.Run(phase, func(t *testing.T) {
			f := testutil.New(pool, "", "")
			user := f.User(t, "Guard user", uuid.NewString()+"@guard.test")
			ws := f.Workspace(t, "Guard workspace", "guard-"+uuid.NewString())
			f = testutil.New(pool, ws, user)
			rt := f.Runtime(t, "Guard runtime", testutil.Cols{"provider": "claude"})
			a := f.Agent(t, "Guard agent", rt)
			task := f.Task(t, a, testutil.Cols{"runtime_id": rt})
			if phase != "none" && phase != "query-error" {
				desired := "present"
				if phase == "draining" || phase == "deleting" || phase == "deleted" {
					desired = "absent"
				}
				orphan := uuid.NewString()
				// A mismatched workspace must not hide an enrollment from the guard.
				f.InsertNoID(t, "kagent_enrollment", testutil.Cols{"workspace_id": uuid.NewString(), "runtime_id": rt, "agent_id": orphan, "desired_state": desired, "phase": phase, "applied_generation": 1}, "agent_id=$1", orphan)
			}
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			var guarded pgx.Tx = tx
			if phase == "query-error" {
				guarded = kagentGuardQueryErrorTx{Tx: tx}
			}
			rid, err := util.ParseUUID(rt)
			if err != nil {
				t.Fatal(err)
			}
			_, err = TeardownRuntime(ctx, guarded, rid, RuntimeTeardownOptions{CancelNonTerminalTasks: true})
			if phase == "none" {
				if err != nil {
					t.Fatal(err)
				}
			} else if phase == "query-error" {
				if err == nil || !strings.Contains(err.Error(), "injected enrollment query failure") {
					t.Fatalf("error=%v", err)
				}
			} else if !errors.Is(err, ErrRuntimeKagentEnrollment) {
				t.Fatalf("error=%v", err)
			}
			// Commit even a refusal: the guard must precede every mutation, rather
			// than relying on the caller's rollback to undo cancellations/unbinding.
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var bound bool
			var status string
			f.QueryRow(t, `SELECT runtime_id IS NOT NULL FROM agent WHERE id=$1`, a).Scan(&bound)
			f.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, task).Scan(&status)
			wantBound, wantStatus := phase != "none", "queued"
			if phase == "none" {
				wantStatus = "cancelled"
			}
			if bound != wantBound || status != wantStatus {
				t.Fatalf("bound=%v status=%s", bound, status)
			}
		})
	}
}

type kagentGuardQueryErrorTx struct{ pgx.Tx }

func (tx kagentGuardQueryErrorTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM kagent_enrollment") {
		return kagentGuardErrorRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

type kagentGuardErrorRow struct{}

func (kagentGuardErrorRow) Scan(...any) error { return errors.New("injected enrollment query failure") }

func TestKagentExpandedGuardRetainsRuntimeLock(t *testing.T) {
	pool := sharedTestPool(t)
	f := testutil.New(pool, "", "")
	user := f.User(t, "Lock guard", uuid.NewString()+"@guard.test")
	ws := f.Workspace(t, "Lock guard", "guard-"+uuid.NewString())
	f = testutil.New(pool, ws, user)
	rt := f.Runtime(t, "Lock guard")
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	rid, err := util.ParseUUID(rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := GuardRuntimeKagentTeardown(ctx, tx, rid); err != nil {
		t.Fatal(err)
	}
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(ctx)
	if _, err := writer.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
		t.Fatal(err)
	}
	// Enrollment creation takes this lock before it inserts intent.
	_, err = db.New(writer).LockAgentRuntimeForAccessChange(ctx, rid)
	var lockErr *pgconn.PgError
	if !errors.As(err, &lockErr) || lockErr.Code != "55P03" {
		t.Fatalf("enrollment writer lock error=%v, want lock timeout", err)
	}
}
