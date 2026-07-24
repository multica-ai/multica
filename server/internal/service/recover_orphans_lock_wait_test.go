package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The recover-orphans query pages forward with a keyset cursor that advances
// permanently past whatever a page returned, so it MUST NOT use FOR UPDATE SKIP
// LOCKED (MUL-4332 review round 4, point 1). If an older orphan is briefly locked
// while a page is being selected — a sweep or a stale-dispatch reclaim holding it —
// SKIP LOCKED would silently drop it; the page, filled by the newer rows behind it,
// would advance the cursor past that older row, and no later page could ever select
// it again (the runtime is already back `online`, so the offline sweep won't reap it
// either). Plain FOR UPDATE instead WAITS for the lock and, once it releases,
// re-checks the row against the WHERE and includes it.
//
// This reproduces exactly that race: lock the OLDEST orphan in a concurrent
// transaction, prove the drain BLOCKS (does not skip past) while the lock is held,
// then release it and assert the previously-locked row is failed with its event.
// Under the old SKIP LOCKED query the drain would finish immediately with the oldest
// row still `running` forever, and this test would fail.
func TestRecoverOrphansWaitsForLockedOldestOrphan(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime id: %v", err)
	}

	// Three healthy orphans with distinct, increasing created_at. The OLDEST is the
	// one we lock while the page is selected.
	seed := func(ageInterval string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
			VALUES ($1, $2, $3, 'running', 0, now() - $4::interval)
			RETURNING id`, agentID, runtimeID, issueID, ageInterval).Scan(&id); err != nil {
			t.Fatalf("seed task: %v", err)
		}
		return id
	}
	oldest := seed("3 minutes")
	mid := seed("2 minutes")
	newest := seed("1 minute")
	all := []string{oldest, mid, newest}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, all)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = ANY($1::uuid[])`, all)
	})

	// Hold a FOR UPDATE lock on the oldest orphan in a dedicated connection, exactly
	// as a sweep or a stale-dispatch reclaim would briefly hold it.
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock conn: %v", err)
	}
	lockReleased := false
	releaseLock := func() {
		if lockReleased {
			return
		}
		lockReleased = true
		lockConn.Release()
	}
	defer releaseLock()

	lockTx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT id FROM agent_task_queue WHERE id = $1 FOR UPDATE`, oldest); err != nil {
		t.Fatalf("lock oldest orphan: %v", err)
	}

	// Run one drain page in the background. A page size above the row count means a
	// single page covers all three — so blocking on the oldest row blocks the whole
	// page (nothing is partially failed until the lock releases).
	type drainResult struct {
		failed int
		err    error
	}
	done := make(chan drainResult, 1)
	go func() {
		var candidates []db.AgentTaskQueue
		failed, err := svc.FailBulkTasksWithEvents(ctx,
			func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
				c, e := qtx.SelectOrphanedTasksForRuntime(ctx, db.SelectOrphanedTasksForRuntimeParams{
					RuntimeID:  util.MustParseUUID(runtimeID),
					MaxPerTick: 500,
				})
				candidates = c
				return c, e
			},
			func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
				return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
					Ids:           ids,
					Error:         pgtype.Text{String: "daemon restarted while task was in flight", Valid: true},
					FailureReason: pgtype.Text{String: "runtime_recovery", Valid: true},
				})
			})
		_ = candidates
		done <- drainResult{failed: len(failed), err: err}
	}()

	// While the lock is held the drain must NOT finish: plain FOR UPDATE blocks on
	// the locked oldest row instead of skipping it.
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("drain errored while the oldest orphan was locked: %v", res.err)
		}
		t.Fatalf("drain completed (%d failed) while the oldest orphan was locked — it skipped instead of waiting (SKIP LOCKED regression)", res.failed)
	case <-time.After(750 * time.Millisecond):
		// Expected: blocked on the lock.
	}

	// Nothing may be partially failed while the page is blocked behind the lock.
	for _, id := range all {
		if s := taskStatusForTest(t, pool, id); s != "running" {
			t.Fatalf("row %s = %q before unlock, want running (a blocked page must fail nothing yet)", id, s)
		}
	}

	// Release the lock; the drain must now unblock and fail every orphan.
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	releaseLock()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("drain errored after the lock released: %v", res.err)
		}
		if res.failed != len(all) {
			t.Errorf("failed = %d, want %d (the drain must include the previously-locked oldest row)", res.failed, len(all))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not complete after the lock released — plain FOR UPDATE never unblocked")
	}

	// The previously-locked oldest orphan is now failed with exactly one event.
	if s := taskStatusForTest(t, pool, oldest); s != "failed" {
		t.Errorf("oldest orphan status = %q, want failed (plain FOR UPDATE must wait for the lock, not skip past)", s)
	}
	if n := subjectEventCount(t, pool, oldest); n != 1 {
		t.Errorf("oldest orphan events = %d, want 1 (fact ⇔ event for the recovered row)", n)
	}
	// The other two rows are failed too, so the whole page landed.
	for _, id := range []string{mid, newest} {
		if s := taskStatusForTest(t, pool, id); s != "failed" {
			t.Errorf("row %s status = %q, want failed", id, s)
		}
	}
}
