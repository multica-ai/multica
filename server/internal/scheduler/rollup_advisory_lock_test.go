package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestRollupTaskUsageHourlyDoesNotLeakAdvisoryLockOnCancel pins the failure
// mode behind MUL-5983: a cancelled rollup tick used to keep advisory lock
// 4246 forever.
//
// Migration 102 took the lock with the SESSION-scoped pg_try_advisory_lock and
// released it from an `EXCEPTION WHEN OTHERS` handler. plpgsql's OTHERS does
// not match query_canceled, and a session-level advisory lock survives the
// rollback that follows — so a tick killed by a statement timeout, an operator
// cancel, or pgx cancelling the query when the Go context is done handed its
// pooled connection back to pgxpool still holding 4246. Every later tick then
// lost pg_try_advisory_lock and reported "no work", and DeleteWorkspace —
// which waits on pg_advisory_xact_lock(4246) — blocked forever.
//
// The cancel is forced deterministically: another session holds the
// rollup-state row the function updates first, so the tick blocks there long
// enough for its own statement_timeout to fire.
func TestRollupTaskUsageHourlyDoesNotLeakAdvisoryLockOnCancel(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	// The tick mutates the shared rollup singleton; serialise with every
	// other test that touches it (MUL-3980).
	lockRollupSingleton(t, pool)

	// Blocker: hold task_usage_hourly_rollup_state row 1 so the tick's first
	// UPDATE waits instead of completing.
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire blocker connection: %v", err)
	}
	defer blocker.Release()
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker tx: %v", err)
	}
	blockerDone := false
	defer func() {
		if !blockerDone {
			_ = blockerTx.Rollback(context.Background())
		}
	}()
	if _, err := blockerTx.Exec(ctx, `
		SELECT 1 FROM task_usage_hourly_rollup_state WHERE id = 1 FOR UPDATE
	`); err != nil {
		t.Fatalf("lock rollup state row: %v", err)
	}

	// Runner: a pinned connection so we can inspect the advisory locks it
	// still holds after its statement is cancelled. This is the pgxpool
	// connection whose session state outlives the aborted transaction.
	runner, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire runner connection: %v", err)
	}
	defer func() {
		// Leave nothing behind for the next test even if the assertion below
		// fails, and reset the session settings this test changed.
		_, _ = runner.Exec(context.Background(), `SELECT pg_advisory_unlock_all()`)
		_, _ = runner.Exec(context.Background(), `SET statement_timeout = DEFAULT`)
		runner.Release()
	}()

	if _, err := runner.Exec(ctx, `SET statement_timeout = 500`); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	// The singleton guard serialises the rollup tests, but 4246 is also taken
	// by every workspace teardown, so internal/handler's delete tests can hold
	// it for a moment in the binary running alongside this one. A tick that
	// loses that race returns 0 immediately instead of blocking on the row
	// lock, which is a lost attempt rather than a result — retry it.
	var rows int64
	for attempt := 0; attempt < 20; attempt++ {
		err = runner.QueryRow(ctx, `SELECT rollup_task_usage_hourly()`).Scan(&rows)
		if err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err == nil {
		t.Fatalf("expected the rollup tick to be cancelled, got rows=%d", rows)
	}

	_ = blockerTx.Rollback(ctx)
	blockerDone = true
	if _, err := runner.Exec(ctx, `SET statement_timeout = DEFAULT`); err != nil {
		t.Fatalf("reset statement_timeout: %v", err)
	}

	// The cancelled tick must not still own 4246.
	var held bool
	if err := runner.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM pg_locks
			 WHERE locktype = 'advisory'
			   AND pid = pg_backend_pid()
			   AND ((classid::bigint << 32) | objid::bigint) = 4246
		)
	`).Scan(&held); err != nil {
		t.Fatalf("inspect advisory locks: %v", err)
	}
	if held {
		t.Fatal("cancelled rollup tick leaked advisory lock 4246 — every later tick reports no work and DeleteWorkspace blocks forever (MUL-5983)")
	}

	// And the lock must become available to the next waiter, which is what the
	// workspace teardown actually needs. Bounded retry for the same reason as
	// above: a delete test in the parallel binary may hold 4246 right now.
	var acquired bool
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := pool.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(4246)`).Scan(&acquired); err != nil {
			t.Fatalf("probe advisory lock 4246: %v", err)
		}
		if acquired || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !acquired {
		t.Fatal("advisory lock 4246 is still held after a cancelled rollup tick")
	}
}
