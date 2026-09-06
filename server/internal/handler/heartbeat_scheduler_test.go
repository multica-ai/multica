package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestBatchedHeartbeatScheduler_CoalescesAndFlushes confirms the batching win:
// many Schedule calls for the same id within a tick window collapse to a
// single bulk UPDATE, and the DB observes the bump after FlushNow.
func TestBatchedHeartbeatScheduler_CoalescesAndFlushes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	// Push the row's last_seen_at into the past so the post-flush value is
	// distinguishable from the pre-flush one.
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, nil)

	// Hammer Schedule with the same id from many goroutines.
	const callers = 50
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
				t.Errorf("Schedule: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected coalesced pending=1, got %d", got)
	}

	// Pre-flush the DB row should still show the stale value.
	_, lastSeenBefore, _ := readRuntimeRow(t, runtimeID)
	if !lastSeenBefore.Equal(stale) {
		// stale time is rounded by the DB, allow same instant
		if lastSeenBefore.After(stale.Add(time.Second)) {
			t.Fatalf("DB unexpectedly bumped before flush: %s", lastSeenBefore)
		}
	}

	sched.FlushNow(context.Background())

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after flush, got %d", got)
	}

	_, lastSeenAfter, _ := readRuntimeRow(t, runtimeID)
	if !lastSeenAfter.After(stale.Add(time.Hour)) {
		t.Fatalf("flush did not bump last_seen_at: stale=%s after=%s", stale, lastSeenAfter)
	}
}

// TestBatchedHeartbeatScheduler_ExplicitDeregisterReceiptStaysOffline confirms
// that a heartbeat queued before a user-visible deregistration cannot undo the
// offline state or leave an online row with stale offline_reason metadata.
func TestBatchedHeartbeatScheduler_ExplicitDeregisterReceiptStaysOffline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	recoveries := &recordingRecoveryNotifier{}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, nil)
	sched.RecoveryNotifier = recoveries
	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	reason := []byte(`{"code":"provider_removed"}`)
	if err := testHandler.Queries.SetAgentRuntimeOfflineWithReason(context.Background(), db.SetAgentRuntimeOfflineWithReasonParams{
		ID:            rt.ID,
		OfflineReason: reason,
	}); err != nil {
		t.Fatalf("SetAgentRuntimeOfflineWithReason: %v", err)
	}

	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("pre-deregister heartbeat should remain queued, pending=%d", got)
	}
	sched.FlushNow(context.Background())

	var status string
	var reasonPreserved bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, metadata->'offline_reason' = $2::jsonb
		FROM agent_runtime
		WHERE id = $1
	`, runtimeID, reason).Scan(&status, &reasonPreserved); err != nil {
		t.Fatalf("read runtime after flush: %v", err)
	}
	if status != "offline" || !reasonPreserved {
		t.Fatalf("explicit deregistration changed after receipt: status=%q reason_preserved=%t", status, reasonPreserved)
	}
	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("preserved offline runtime remained pending: %d", got)
	}
	// A preserved explicit deregistration is not a recovery: no refresh event.
	if len(recoveries.workspaceIDs) != 0 {
		t.Fatalf("preserved offline runtime published recovery events: %v", recoveries.workspaceIDs)
	}
}

// TestBatchedHeartbeatScheduler_StopDrains confirms the shutdown contract:
// IDs queued before Stop must be flushed to the DB by the time Stop returns,
// otherwise a graceful restart would lose heartbeat state.
func TestBatchedHeartbeatScheduler_StopDrains(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	// Long tick so the natural ticker can't fire during the test — only
	// the Stop drain can flush.
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Run(ctx)

	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected pending=1 before Stop, got %d", got)
	}

	sched.Stop()

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after Stop drain, got %d", got)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Hour)) {
		t.Fatalf("Stop did not drain pending bump: stale=%s after=%s", stale, lastSeen)
	}
}

// TestBatchedHeartbeatScheduler_StopFlushesLateSchedule verifies the
// defense-in-depth flush in Stop(): if Run already returned via ctx.Done()
// and a heartbeat is then Schedule'd before Stop is called, that bump must
// still hit the DB after Stop returns. This guards the production shutdown
// race where in-flight HTTP heartbeats can call Schedule while sweepCtx is
// already cancelled.
func TestBatchedHeartbeatScheduler_StopFlushesLateSchedule(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour, nil)

	runCtx, runCancel := context.WithCancel(context.Background())
	go sched.Run(runCtx)

	// Force Run to exit via ctx.Done() before any Schedule call. Wait for
	// it to fully drain (which closes doneCh by reading it directly is
	// awkward; instead, briefly poll on a separate Stop-less path). The
	// simplest deterministic signal: cancel, then sleep just enough for
	// the goroutine to hit the ctx.Done() branch and close doneCh.
	runCancel()
	time.Sleep(50 * time.Millisecond)

	// Now Schedule a late heartbeat. Run is gone; only Stop's defensive
	// flush can persist this.
	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected pending=1 before Stop, got %d", got)
	}

	sched.Stop()

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after Stop's defensive flush, got %d", got)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Hour)) {
		t.Fatalf("Stop did not flush late Schedule: stale=%s after=%s", stale, lastSeen)
	}
}

// TestBatchedHeartbeatScheduler_StopRecoveryUsesBoundedContextAndKnownWorkspace
// verifies that the Stop drain keeps its deadline and avoids another lookup.
func TestBatchedHeartbeatScheduler_StopRecoveryUsesBoundedContextAndKnownWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeLastSeenAt(t, runtimeID, time.Now().Add(-2*heartbeatReceiptRecoveryThreshold))
	rt := loadRuntime(t, runtimeID)

	recoveries := &recordingRecoveryNotifier{}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour, nil)
	sched.RecoveryNotifier = recoveries
	go sched.Run(context.Background())
	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	rows, err := testHandler.Queries.MarkRuntimesOfflineByIDs(context.Background(), db.MarkRuntimesOfflineByIDsParams{
		Ids:          []pgtype.UUID{rt.ID},
		StaleSeconds: service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		t.Fatalf("MarkRuntimesOfflineByIDs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sweeper rows = %d, want 1", len(rows))
	}

	sched.Stop()

	if len(recoveries.workspaceIDs) != 1 || recoveries.workspaceIDs[0] != testWorkspaceID {
		t.Fatalf("recovery workspaces = %v, want [%s]", recoveries.workspaceIDs, testWorkspaceID)
	}
	deadline, ok := recoveries.contexts[0].Deadline()
	if !ok {
		t.Fatal("Stop recovery context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 10*time.Second {
		t.Fatalf("Stop recovery deadline remaining = %s, want within (0s, 10s]", remaining)
	}
}

// TestBatchedHeartbeatScheduler_FlushIgnoresEmpty exercises the empty-pending
// fast path: a tick with nothing queued must not issue a DB call.
func TestBatchedHeartbeatScheduler_FlushIgnoresEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, nil)
	// Just calling FlushNow with nothing queued should not panic or error.
	sched.FlushNow(context.Background())
	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("pending should remain 0, got %d", got)
	}
}

// TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals confirms that if the
// sweeper flips a row offline between Schedule and FlushNow, receipt
// reconciliation restores it without a new heartbeat lookup.
// recordingRecoveryNotifier captures RuntimeRecoveryNotifier calls so tests
// can assert a lifecycle refresh fires for the already-known workspace.
type recordingRecoveryNotifier struct {
	workspaceIDs []string
	contexts     []context.Context
}

func (r *recordingRecoveryNotifier) NotifyRuntimeRecovered(ctx context.Context, workspaceID string) {
	r.workspaceIDs = append(r.workspaceIDs, workspaceID)
	r.contexts = append(r.contexts, ctx)
}

func TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeLastSeenAt(t, runtimeID, time.Now().Add(-2*heartbeatReceiptRecoveryThreshold))
	rt := loadRuntime(t, runtimeID)

	recoveries := &recordingRecoveryNotifier{}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, nil)
	sched.RecoveryNotifier = recoveries
	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	// The real sweeper query races us to offline before the flush. Its stale
	// predicate is the fact receipt reconciliation is allowed to rely on.
	rows, err := testHandler.Queries.MarkRuntimesOfflineByIDs(context.Background(), db.MarkRuntimesOfflineByIDsParams{
		Ids:          []pgtype.UUID{rt.ID},
		StaleSeconds: service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		t.Fatalf("MarkRuntimesOfflineByIDs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sweeper rows = %d, want 1", len(rows))
	}

	flushCtx := context.WithValue(context.Background(), recoveryContextKey{}, "batch")
	sched.FlushNow(flushCtx)

	// The bulk UPDATE omits the offline row, and receipt reconciliation flips
	// it back online without waiting for another heartbeat.
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected receipt reconciliation to recover online, got %q", status)
	}
	// The reconciled flip is a real offline → online recovery: one lifecycle
	// refresh, no more.
	if len(recoveries.workspaceIDs) != 1 || recoveries.workspaceIDs[0] != testWorkspaceID {
		t.Fatalf("recovery workspaces = %v, want [%s]", recoveries.workspaceIDs, testWorkspaceID)
	}
	if got := recoveries.contexts[0].Value(recoveryContextKey{}); got != "batch" {
		t.Fatalf("recovery context value = %v, want batch", got)
	}
}

func TestBatchedHeartbeatScheduler_DeletedReceiptInvalidatesRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	rt := loadRuntime(t, runtimeID)
	notifier := &recordingRuntimeGoneNotifier{}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, notifier)
	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("delete runtime before flush: %v", err)
	}

	sched.FlushNow(context.Background())

	if len(notifier.runtimeIDs) != 1 || notifier.runtimeIDs[0] != runtimeID {
		t.Fatalf("runtime-gone notifications = %v, want [%s]", notifier.runtimeIDs, runtimeID)
	}
	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("deleted runtime remained pending after reconciliation: %d", got)
	}
}

// TestPassthroughHeartbeatScheduler_TouchAndRaceRecovery confirms the legacy
// behavior is preserved end-to-end: an online row gets bumped via Touch, and
// a row whose status was raced to offline between SELECT and Schedule is
// recovered via MarkAgentRuntimeOnline.
func TestPassthroughHeartbeatScheduler_TouchAndRaceRecovery(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	recoveries := &recordingRecoveryNotifier{}
	sched := NewPassthroughHeartbeatScheduler(testHandler.Queries)
	sched.RecoveryNotifier = recoveries

	if err := sched.Schedule(context.Background(), rt.ID, rt.WorkspaceID); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// Ordinary online bump: no recovery refresh.
	if len(recoveries.workspaceIDs) != 0 {
		t.Fatalf("online heartbeat published recovery events: %v", recoveries.workspaceIDs)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Minute)) {
		t.Fatalf("passthrough did not bump last_seen_at: stale=%s after=%s", stale, lastSeen)
	}

	// Race: snapshot still says online but DB is now offline.
	rt2 := loadRuntime(t, runtimeID)
	setRuntimeStatus(t, runtimeID, "offline")
	scheduleCtx := context.WithValue(context.Background(), recoveryContextKey{}, "passthrough")
	if err := sched.Schedule(scheduleCtx, rt2.ID, rt2.WorkspaceID); err != nil {
		t.Fatalf("Schedule under race: %v", err)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected race recovery via MarkAgentRuntimeOnline, got %q", status)
	}
	// The race flip is a real recovery: exactly one lifecycle refresh.
	if len(recoveries.workspaceIDs) != 1 || recoveries.workspaceIDs[0] != testWorkspaceID {
		t.Fatalf("recovery workspaces = %v, want [%s]", recoveries.workspaceIDs, testWorkspaceID)
	}
	if got := recoveries.contexts[0].Value(recoveryContextKey{}); got != "passthrough" {
		t.Fatalf("recovery context value = %v, want passthrough", got)
	}
}

type recoveryContextKey struct{}

// silenceUnusedPgUUID ensures the package compiles even if no other test
// happens to reference pgtype after future edits trim imports.
var _ = pgtype.UUID{}
