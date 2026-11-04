package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// cancelAfterRequeueRow cancels the caller's context once the requeue CAS has
// been scanned. That is the state batch recovery reaches when its shared budget
// runs out during the SQL, cache and status work after the requeue commits.
type cancelAfterRequeueRow struct {
	pgx.Row
	cancel context.CancelFunc
}

func (r cancelAfterRequeueRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.cancel()
	return err
}

type cancelAfterRequeueDB struct {
	db.DBTX
	cancel context.CancelFunc
}

func (d cancelAfterRequeueDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	row := d.DBTX.QueryRow(ctx, query, args...)
	if strings.Contains(query, "-- name: RequeueAgentTaskAfterClaimFailure :one") {
		return cancelAfterRequeueRow{Row: row, cancel: d.cancel}
	}
	return row
}

// A requeue that committed must publish task:queued even when the caller's
// context is already spent by the time the event scope is resolved. Before the
// fix the scope lookup ran on the expired context, resolved to no workspace, and
// the event was dropped without a log line.
func TestRequeueTaskAfterClaimFailurePublishesQueuedEventAfterBudgetExpires(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)

	taskID, _, workspaceID := dispatchedCommentTaskFixture(t, ctx, pool)
	task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}

	var mu sync.Mutex
	var queued []events.Event
	bus := events.New()
	bus.Subscribe(protocol.EventTaskQueued, func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		queued = append(queued, e)
	})

	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	svc := NewTaskService(db.New(cancelAfterRequeueDB{DBTX: pool, cancel: cancel}), pool, nil, bus)

	if _, err := svc.RequeueTaskAfterClaimFailure(requestCtx, task); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if requestCtx.Err() == nil {
		t.Fatal("test seam did not expire the caller context; regression was not exercised")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(queued) != 1 {
		t.Fatalf("task:queued events = %d, want 1 after the requeue committed", len(queued))
	}
	if queued[0].TaskID != taskID || queued[0].WorkspaceID != workspaceID {
		t.Fatalf("task:queued scope = task %q workspace %q, want task %q workspace %q",
			queued[0].TaskID, queued[0].WorkspaceID, taskID, workspaceID)
	}
}
