package handler

import (
	"context"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeCLIUpgradeAllowed(t *testing.T) {
	ownerID := "11111111-1111-4111-8111-111111111111"
	otherID := "22222222-2222-4222-8222-222222222222"

	tests := []struct {
		name   string
		mode   string
		role   string
		userID string
		want   bool
	}{
		{name: "local owner", mode: "local", role: "member", userID: ownerID, want: true},
		{name: "local non-owner member", mode: "local", role: "member", userID: otherID, want: false},
		{name: "local admin non-owner", mode: "local", role: "admin", userID: otherID, want: false},
		{name: "local owner admin", mode: "local", role: "admin", userID: ownerID, want: true},
		{name: "cloud owner member", mode: "cloud", role: "member", userID: ownerID, want: true},
		{name: "cloud admin non-owner", mode: "cloud", role: "admin", userID: otherID, want: true},
		{name: "cloud workspace owner role", mode: "cloud", role: "owner", userID: otherID, want: true},
		{name: "cloud plain member non-owner", mode: "cloud", role: "member", userID: otherID, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			member := db.Member{
				UserID: testUUID(tc.userID),
				Role:   tc.role,
			}
			rt := db.AgentRuntime{
				RuntimeMode: tc.mode,
				OwnerID:     testUUID(ownerID),
			}
			if got := runtimeCLIUpgradeAllowed(member, rt); got != tc.want {
				t.Fatalf("runtimeCLIUpgradeAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInMemoryCLIUpgradeStore_StateMachine(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCLIUpgradeStore()
	runtimeID := "rt-1"

	job, err := store.Create(ctx, runtimeID, "codex", "user-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.Status != CLIUpgradeQueued {
		t.Fatalf("status = %q, want queued", job.Status)
	}

	// Idempotent same runtime+cli.
	again, err := store.Create(ctx, runtimeID, "CODEX", "user-2")
	if err != nil {
		t.Fatalf("Create idempotent: %v", err)
	}
	if again.ID != job.ID {
		t.Fatalf("idempotent id = %q, want %q", again.ID, job.ID)
	}

	has, err := store.HasPending(ctx, runtimeID)
	if err != nil || !has {
		t.Fatalf("HasPending = %v, %v; want true", has, err)
	}

	pending, err := store.PopPending(ctx, runtimeID)
	if err != nil {
		t.Fatalf("PopPending: %v", err)
	}
	if pending == nil || pending.Status != CLIUpgradeDraining {
		t.Fatalf("PopPending = %+v, want draining", pending)
	}
	has, err = store.HasPending(ctx, runtimeID)
	if err != nil || has {
		t.Fatalf("HasPending after pop = %v, %v; want false", has, err)
	}

	if err := store.SetStatus(ctx, job.ID, CLIUpgradeUpgrading, "", ""); err != nil {
		t.Fatalf("SetStatus upgrading: %v", err)
	}
	got, err := store.Get(ctx, job.ID)
	if err != nil || got.Status != CLIUpgradeUpgrading {
		t.Fatalf("Get after upgrading = %+v, %v", got, err)
	}

	if err := store.SetStatus(ctx, job.ID, CLIUpgradeSucceeded, "", "1.2.3"); err != nil {
		t.Fatalf("SetStatus succeeded: %v", err)
	}
	got, err = store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != CLIUpgradeSucceeded || got.Current != "1.2.3" {
		t.Fatalf("terminal job = %+v", got)
	}

	// Terminal is sticky: further SetStatus ignored.
	if err := store.SetStatus(ctx, job.ID, CLIUpgradeFailed, "nope", ""); err != nil {
		t.Fatalf("SetStatus after terminal: %v", err)
	}
	got, err = store.Get(ctx, job.ID)
	if err != nil || got.Status != CLIUpgradeSucceeded {
		t.Fatalf("sticky terminal = %+v, %v", got, err)
	}

	// Different CLI can be queued on same runtime (dispatch stays serial).
	other, err := store.Create(ctx, runtimeID, "claude", "user-1")
	if err != nil {
		t.Fatalf("Create other cli: %v", err)
	}
	if other.ID == job.ID {
		t.Fatal("expected distinct job for different CLI")
	}
}

func TestInMemoryCLIUpgradeStore_QueuedTimeout(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCLIUpgradeStore()
	job, err := store.Create(ctx, "rt-timeout", "claude", "user-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.mu.Lock()
	aged := time.Now().Add(-cliUpgradePendingTimeout - time.Second)
	store.jobs[job.ID].CreatedAt = aged
	store.jobs[job.ID].QueuedAt = aged
	store.mu.Unlock()

	got, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != CLIUpgradeFailed {
		t.Fatalf("status = %q, want failed after pending timeout", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error message")
	}
}

func TestApplyCLIUpgradeTimeout_RunningWindow(t *testing.T) {
	now := time.Now()
	started := now.Add(-cliUpgradeRunningTimeout - time.Second)
	job := &CLIUpgradeJob{
		Status:       CLIUpgradeDraining,
		CreatedAt:    now.Add(-time.Hour),
		DispatchedAt: &started,
	}
	if !applyCLIUpgradeTimeout(job, now, false) {
		t.Fatal("expected running timeout to apply")
	}
	if job.Status != CLIUpgradeFailed {
		t.Fatalf("status = %q, want failed", job.Status)
	}
}

func TestInMemoryCLIUpgradeStore_SerialDispatchPerRuntime(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCLIUpgradeStore()
	runtimeID := "rt-serial"

	codex, err := store.Create(ctx, runtimeID, "codex", "user-1")
	if err != nil {
		t.Fatalf("Create codex: %v", err)
	}
	claude, err := store.Create(ctx, runtimeID, "claude", "user-1")
	if err != nil {
		t.Fatalf("Create claude: %v", err)
	}
	if codex.ID == claude.ID {
		t.Fatal("expected distinct jobs")
	}

	first, err := store.PopPending(ctx, runtimeID)
	if err != nil || first == nil {
		t.Fatalf("first PopPending = %+v, %v", first, err)
	}
	if first.ID != codex.ID || first.Status != CLIUpgradeDraining {
		t.Fatalf("first pop = %+v, want codex draining", first)
	}

	// While codex is in-flight, claude must stay queued — not popped, not failed.
	second, err := store.PopPending(ctx, runtimeID)
	if err != nil {
		t.Fatalf("second PopPending: %v", err)
	}
	if second != nil {
		t.Fatalf("second PopPending = %+v, want nil while another job is in-flight", second)
	}
	got, err := store.Get(ctx, claude.ID)
	if err != nil || got.Status != CLIUpgradeQueued {
		t.Fatalf("claude status = %+v, %v; want queued", got, err)
	}

	if err := store.SetStatus(ctx, codex.ID, CLIUpgradeSucceeded, "", "1.0.0"); err != nil {
		t.Fatalf("SetStatus succeeded: %v", err)
	}

	third, err := store.PopPending(ctx, runtimeID)
	if err != nil || third == nil {
		t.Fatalf("third PopPending = %+v, %v", third, err)
	}
	if third.ID != claude.ID || third.Status != CLIUpgradeDraining {
		t.Fatalf("third pop = %+v, want claude draining", third)
	}
}

func TestInMemoryCLIUpgradeStore_BusyRequeuesToQueued(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCLIUpgradeStore()
	job, err := store.Create(ctx, "rt-busy", "codex", "user-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pending, err := store.PopPending(ctx, "rt-busy")
	if err != nil || pending == nil || pending.Status != CLIUpgradeDraining {
		t.Fatalf("PopPending = %+v, %v", pending, err)
	}

	// Age the wait clock so a refresh on Busy→queued is observable.
	store.mu.Lock()
	aged := time.Now().Add(-cliUpgradePendingTimeout - time.Minute)
	store.jobs[job.ID].QueuedAt = aged
	store.mu.Unlock()

	beforeRequeue := time.Now()
	if err := store.SetStatus(ctx, job.ID, CLIUpgradeQueued, "ignored", ""); err != nil {
		t.Fatalf("SetStatus queued: %v", err)
	}
	got, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != CLIUpgradeQueued {
		t.Fatalf("status = %q, want queued", got.Status)
	}
	if got.Error != "" {
		t.Fatalf("error = %q, want cleared on requeue", got.Error)
	}
	if got.DispatchedAt != nil {
		t.Fatal("DispatchedAt should clear on requeue")
	}
	store.mu.Lock()
	queuedAt := store.jobs[job.ID].QueuedAt
	store.mu.Unlock()
	if !queuedAt.After(aged) || queuedAt.Before(beforeRequeue.Add(-time.Second)) {
		t.Fatalf("QueuedAt not refreshed on Busy requeue: queuedAt=%v aged=%v", queuedAt, aged)
	}

	again, err := store.PopPending(ctx, "rt-busy")
	if err != nil || again == nil || again.ID != job.ID {
		t.Fatalf("re-pop after Busy requeue = %+v, %v", again, err)
	}
}

func TestInMemoryCLIUpgradeStore_QueuedWaitsWhileSiblingDraining(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCLIUpgradeStore()
	runtimeID := "rt-drain-wait"

	first, err := store.Create(ctx, runtimeID, "codex", "user-1")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create(ctx, runtimeID, "claude", "user-1")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	popped, err := store.PopPending(ctx, runtimeID)
	if err != nil || popped == nil || popped.ID != first.ID {
		t.Fatalf("PopPending first = %+v, %v", popped, err)
	}

	// Age the waiting sibling well past the 120s "daemon never claimed" window
	// while the first job is still draining (drain may take up to ~10 minutes).
	store.mu.Lock()
	store.jobs[second.ID].CreatedAt = time.Now().Add(-cliUpgradePendingTimeout - time.Minute)
	store.jobs[second.ID].QueuedAt = time.Now().Add(-cliUpgradePendingTimeout - time.Minute)
	store.mu.Unlock()

	got, err := store.Get(ctx, second.ID)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if got.Status != CLIUpgradeQueued {
		t.Fatalf("second status = %q (%q), want queued while sibling draining >120s", got.Status, got.Error)
	}

	// Still must not Pop the waiter.
	again, err := store.PopPending(ctx, runtimeID)
	if err != nil {
		t.Fatalf("PopPending while draining: %v", err)
	}
	if again != nil {
		t.Fatalf("PopPending = %+v, want nil while sibling draining", again)
	}

	if err := store.SetStatus(ctx, first.ID, CLIUpgradeSucceeded, "", "1.0.0"); err != nil {
		t.Fatalf("SetStatus succeeded: %v", err)
	}

	// After first finishes, second should still be queued (wait clock refreshed)
	// and eligible to Pop — not auto-failed by the old CreatedAt.
	got, err = store.Get(ctx, second.ID)
	if err != nil || got.Status != CLIUpgradeQueued {
		t.Fatalf("second after sibling done = %+v, %v; want queued", got, err)
	}
	third, err := store.PopPending(ctx, runtimeID)
	if err != nil || third == nil || third.ID != second.ID {
		t.Fatalf("PopPending after drain = %+v, %v; want second", third, err)
	}
}

func TestApplyCLIUpgradeTimeout_SkipPendingWhenBlocked(t *testing.T) {
	now := time.Now()
	job := &CLIUpgradeJob{
		Status:    CLIUpgradeQueued,
		CreatedAt: now.Add(-cliUpgradePendingTimeout - time.Minute),
		QueuedAt:  now.Add(-cliUpgradePendingTimeout - time.Minute),
	}
	if applyCLIUpgradeTimeout(job, now, true) {
		t.Fatal("pending timeout must be skipped while sibling in-flight")
	}
	if job.Status != CLIUpgradeQueued {
		t.Fatalf("status = %q, want queued", job.Status)
	}
	if !applyCLIUpgradeTimeout(job, now, false) {
		t.Fatal("expected pending timeout when not blocked")
	}
	if job.Status != CLIUpgradeFailed {
		t.Fatalf("status = %q, want failed", job.Status)
	}
}

func TestInMemoryCLIUpgradeStore_QueuedSurvivesSiblingRunningTimeout(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCLIUpgradeStore()
	runtimeID := "rt-running-timeout"

	first, err := store.Create(ctx, runtimeID, "codex", "user-1")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create(ctx, runtimeID, "claude", "user-1")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	popped, err := store.PopPending(ctx, runtimeID)
	if err != nil || popped == nil || popped.ID != first.ID {
		t.Fatalf("PopPending first = %+v, %v", popped, err)
	}

	// Age second past the 120s pending window and first past the running window.
	store.mu.Lock()
	agedPending := time.Now().Add(-cliUpgradePendingTimeout - time.Minute)
	store.jobs[second.ID].CreatedAt = agedPending
	store.jobs[second.ID].QueuedAt = agedPending
	dispatched := time.Now().Add(-cliUpgradeRunningTimeout - time.Minute)
	store.jobs[first.ID].DispatchedAt = &dispatched
	store.jobs[first.ID].CreatedAt = dispatched
	store.mu.Unlock()

	// applyTimeouts via Get: first should fail running-timeout; second must
	// remain queued with a refreshed wait clock (not false-fail as "daemon
	// did not pick up").
	gotFirst, err := store.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	if gotFirst.Status != CLIUpgradeFailed {
		t.Fatalf("first status = %q (%q), want failed after running timeout", gotFirst.Status, gotFirst.Error)
	}

	gotSecond, err := store.Get(ctx, second.ID)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if gotSecond.Status != CLIUpgradeQueued {
		t.Fatalf("second status = %q (%q), want queued after sibling running-timeout", gotSecond.Status, gotSecond.Error)
	}

	store.mu.Lock()
	queuedAt := store.jobs[second.ID].QueuedAt
	store.mu.Unlock()
	if !queuedAt.After(agedPending) {
		t.Fatalf("second QueuedAt not refreshed after sibling timeout: queuedAt=%v aged=%v", queuedAt, agedPending)
	}

	third, err := store.PopPending(ctx, runtimeID)
	if err != nil || third == nil || third.ID != second.ID {
		t.Fatalf("PopPending after sibling running-timeout = %+v, %v; want second", third, err)
	}
	if third.Status != CLIUpgradeDraining {
		t.Fatalf("second pop status = %q, want draining", third.Status)
	}
}
