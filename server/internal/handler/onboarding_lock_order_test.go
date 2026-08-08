package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueguard"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
)

// MUL-4809 review — unified issue-create lock order.
//
// Every issue create (IssueService.Create and the two onboarding-shim paths) must
// take the workspace status-write lock BEFORE the duplicate-guard advisory lock:
//
//	workspace/status → duplicate → issue
//
// Onboarding used to take them in the opposite order (duplicate → status). Since
// the status advisory key is per-workspace and the duplicate advisory key is per
// (workspace, project, parent, title), an onboarding create and a normal create
// racing on the SAME workspace + title contend on BOTH locks — in opposite orders
// they form an ABBA cycle and Postgres kills one with a deadlock (40P01). These
// tests pin the unified order and the same-key concurrency it makes safe.

// resetNoRuntimeOnboarding clears the state the no-runtime bootstrap create branch
// depends on (no active guide issue, user not yet onboarded) so the handler runs
// its create path, and leaves the shared workspace clean for the next test.
func resetNoRuntimeOnboarding(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, noRuntimeIssueTitle)
	testPool.Exec(ctx, `UPDATE "user" SET onboarded_at = NULL, starter_content_state = NULL, language = 'en' WHERE id = $1`, testUserID)
}

// TestOnboardingTakesStatusLockBeforeDuplicateGuard is the deterministic
// lock-order regression at the real onboarding entrypoint. An external tx holds
// the DUPLICATE lock for the onboarding title, standing in for a concurrent create
// already inside the duplicate guard, then the handler is fired.
//
// Under the fixed order the handler acquires the STATUS lock first and only then
// blocks at the duplicate guard — so a probe for the status lock must ALSO block
// (the handler is holding it). Under the old order the handler would block on the
// duplicate lock without ever taking the status lock, and the probe would acquire
// it immediately, failing this test.
func TestOnboardingTakesStatusLockBeforeDuplicateGuard(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureTestWorkspaceStatuses(t)
	wsUUID := parseUUID(testWorkspaceID)
	ctx := context.Background()

	resetNoRuntimeOnboarding(t)
	t.Cleanup(func() { resetNoRuntimeOnboarding(t) })

	// txDup holds the duplicate advisory lock for the onboarding title.
	txDup, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txDup: %v", err)
	}
	defer txDup.Rollback(ctx)
	var emptyUUID pgtype.UUID
	if _, _, err := issueguard.LockAndFindActiveDuplicate(
		ctx, testHandler.Queries.WithTx(txDup), wsUUID, emptyUUID, emptyUUID, noRuntimeIssueTitle, false,
	); err != nil {
		t.Fatalf("txDup acquire duplicate lock: %v", err)
	}

	// Fire the real onboarding handler; it must take the status lock, then block at
	// the duplicate guard on txDup.
	onboardingDone := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		testHandler.BootstrapOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", map[string]string{
			"workspace_id": testWorkspaceID,
		}))
		onboardingDone <- w.Code
	}()

	// Let the handler reach its blocking point, then confirm it is still running.
	time.Sleep(300 * time.Millisecond)
	select {
	case code := <-onboardingDone:
		t.Fatalf("onboarding completed (code %d) while the duplicate lock was held; it should block at the duplicate guard", code)
	default:
	}

	// Probe the status lock. A cancelable context lets us release the probe's
	// workspace-row FOR KEY SHARE before unblocking the handler, so the handler's
	// later IncrementIssueCounter (FOR UPDATE) is not blocked by a lingering probe.
	probeCtx, cancelProbe := context.WithCancel(context.Background())
	statusProbe := make(chan error, 1)
	go func() {
		txProbe, err := testPool.Begin(probeCtx)
		if err != nil {
			statusProbe <- err
			return
		}
		defer txProbe.Rollback(context.Background())
		statusProbe <- issuestatus.LockWorkspaceForStatusWrite(probeCtx, txProbe, wsUUID)
	}()
	select {
	case err := <-statusProbe:
		cancelProbe()
		t.Fatalf("status lock was free while onboarding was blocked at the duplicate guard (err=%v): onboarding did not take the status lock first (lock-order regression)", err)
	case <-time.After(300 * time.Millisecond):
		// Correct: the handler holds the status lock, so the probe blocks.
	}
	cancelProbe()
	<-statusProbe // wait for the probe goroutine to unwind and release its row lock.

	// Release the duplicate lock; the handler proceeds and completes cleanly.
	if err := txDup.Rollback(ctx); err != nil {
		t.Fatalf("release txDup: %v", err)
	}
	select {
	case code := <-onboardingDone:
		if code != http.StatusOK {
			t.Fatalf("onboarding after lock release: expected 200, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onboarding never completed after the duplicate lock was released")
	}
}

// TestNormalCreateAndOnboardingSameKeyNoDeadlock races the two real entrypoints —
// IssueService.Create and the onboarding shim — on the SAME workspace + title.
// Both are held behind an external status lock so they start together, then are
// released to contend for real. With the unified `workspace/status → duplicate →
// issue` order they serialize instead of deadlocking, and the duplicate guard
// leaves exactly one active issue with that title. Under the old inverted
// onboarding order this pairing is the ABBA deadlock the fix removes.
func TestNormalCreateAndOnboardingSameKeyNoDeadlock(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ensureTestWorkspaceStatuses(t)
	wsUUID := parseUUID(testWorkspaceID)
	ctx := context.Background()

	resetNoRuntimeOnboarding(t)
	t.Cleanup(func() { resetNoRuntimeOnboarding(t) })

	// txExt holds the status lock so both creates start blocked on their FIRST lock
	// (the fixed order takes status first) — neither can be holding the duplicate
	// lock yet, so releasing txExt lets them serialize rather than form a cycle.
	txExt, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txExt: %v", err)
	}
	if err := issuestatus.LockWorkspaceForStatusWrite(ctx, txExt, wsUUID); err != nil {
		txExt.Rollback(ctx)
		t.Fatalf("txExt acquire status lock: %v", err)
	}

	createErr := make(chan error, 1)
	go func() {
		_, err := testHandler.IssueService.Create(context.Background(), service.IssueCreateParams{
			WorkspaceID: wsUUID,
			Title:       noRuntimeIssueTitle,
			Status:      "todo",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   parseUUID(testUserID),
		}, service.IssueCreateOpts{})
		createErr <- err
	}()
	onboardingCode := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		testHandler.BootstrapOnboardingNoRuntime(w, newRequest(http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap", map[string]string{
			"workspace_id": testWorkspaceID,
		}))
		onboardingCode <- w.Code
	}()

	// Both goroutines block on the status lock; hold it briefly so they are both
	// queued before either can touch the duplicate lock, then release.
	time.Sleep(200 * time.Millisecond)
	if err := txExt.Rollback(ctx); err != nil {
		t.Fatalf("release txExt: %v", err)
	}

	// Neither entrypoint may deadlock; the create either wins or sees the duplicate.
	select {
	case err := <-createErr:
		if err != nil && !errors.Is(err, service.ErrActiveDuplicate) {
			t.Fatalf("IssueService.Create errored (want success or ErrActiveDuplicate, never a deadlock): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("IssueService.Create never returned — likely deadlocked against onboarding")
	}
	select {
	case code := <-onboardingCode:
		if code != http.StatusOK {
			t.Fatalf("onboarding returned %d (want 200; a 500 here is a deadlock 40P01)", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("onboarding never returned — likely deadlocked against the normal create")
	}

	// The duplicate guard must leave exactly one active issue with the shared title.
	var active int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2 AND status NOT IN ('done','cancelled')`,
		testWorkspaceID, noRuntimeIssueTitle,
	).Scan(&active); err != nil {
		t.Fatalf("count active issues: %v", err)
	}
	if active != 1 {
		t.Fatalf("same-key race left %d active issues with the shared title; want exactly 1 (dedup held)", active)
	}
}
