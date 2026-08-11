package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// setWorkspaceDeleteLockTimeoutForTest is a package-private hook used only by
// the tests below to keep them fast.
func setWorkspaceDeleteLockTimeoutForTest(t *testing.T, v time.Duration) {
	t.Helper()
	workspaceDeleteLockTimeoutOverride = v
	t.Cleanup(func() { workspaceDeleteLockTimeoutOverride = 0 })
}

// TestDeleteWorkspace_FailsFastWhenRollupLockHeld is the regression test for
// MUL-5983: "clicking delete workspace does nothing".
//
// The teardown transaction takes global advisory lock 4246 so the
// task_usage_hourly rollup cannot write aggregates for a workspace that is
// being torn down. That lock is batch-held — a rollup tick, a backfill run, or
// (before migration 272) a cancelled tick that leaked it — and nothing above
// this handler has a deadline: the desktop client's fetch has no timeout, so
// an unbounded wait leaves the delete dialog spinning on "Deleting…" until the
// app is force quit, and the workspace is never deleted.
//
// With SET LOCAL lock_timeout in place the same contention has to surface as a
// bounded, retryable 503 while the workspace stays intact.
func TestDeleteWorkspace_FailsFastWhenRollupLockHeld(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping live-Postgres workspace delete lock test")
	}
	ctx := context.Background()

	// Taking 4246 by hand is exactly what the rollup family's cross-binary
	// guard exists to serialise: internal/scheduler runs in parallel against
	// the same database, and its own 4246 tests read the lock's state (MUL-3980).
	lockRollupSingleton(t)

	setWorkspaceDeleteLockTimeoutForTest(t, 500*time.Millisecond)

	// Hold 4246 on a pinned connection for the duration of the request, the
	// way a running rollup tick (or a leaked one) holds it in production.
	holder, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire advisory lock holder: %v", err)
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_lock(4246)`); err != nil {
		holder.Release()
		t.Fatalf("hold advisory lock 4246: %v", err)
	}
	// Idempotent: the deadline branch below releases the holder early to
	// unblock the handler, and the defer still runs afterwards. A second
	// Exec/Release on a returned pgxpool connection panics, which would
	// replace the assertion failure with a stack trace.
	var releaseOnce sync.Once
	releaseHolder := func() {
		releaseOnce.Do(func() {
			_, _ = holder.Exec(context.Background(), `SELECT pg_advisory_unlock(4246)`)
			holder.Release()
		})
	}
	defer releaseHolder()

	const slug = "handler-tests-delete-lock"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete Lock", slug, "DeleteWorkspace lock timeout test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	// The handler runs on its own goroutine so a regression shows up as a
	// failed deadline here instead of hanging the whole test binary.
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.DeleteWorkspace(w, req)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		// Unblock the handler so the test binary can still shut down cleanly.
		releaseHolder()
		<-done
		t.Fatal("DeleteWorkspace blocked on advisory lock 4246 instead of timing out — this is the hang users see as 'delete workspace does nothing' (MUL-5983)")
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 while advisory lock 4246 is held, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if !exists {
		t.Fatal("workspace must survive a teardown that never got its locks")
	}
}

// TestDeleteWorkspace_SucceedsWhenRollupLockIsFree is the counterpart: the
// lock timeout must not make an uncontended delete flaky.
func TestDeleteWorkspace_SucceedsWhenRollupLockIsFree(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping live-Postgres workspace delete lock test")
	}
	ctx := context.Background()

	// The teardown itself waits on 4246 under a 500 ms budget, so a rollup
	// test holding that lock in the parallel internal/scheduler binary would
	// turn this "uncontended" delete into a 503. Same guard, same reason.
	lockRollupSingleton(t)

	setWorkspaceDeleteLockTimeoutForTest(t, 500*time.Millisecond)

	const slug = "handler-tests-delete-lock-free"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete Lock Free", slug, "DeleteWorkspace lock timeout test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from an uncontended delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after a successful delete")
	}
}
