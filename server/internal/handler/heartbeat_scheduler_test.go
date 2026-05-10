package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestBatchedHeartbeatScheduler_CoalescesAndFlushes confirms the core P1 win:
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

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)

	// Hammer Schedule with the same id from many goroutines.
	const callers = 50
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			if err := sched.Schedule(context.Background(), rt); err != nil {
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

// TestBatchedHeartbeatScheduler_OfflineFallsBackSync confirms that the sync
// path is preserved: an offline-status row goes through MarkAgentRuntimeOnline
// immediately, not through the queue.
func TestBatchedHeartbeatScheduler_OfflineFallsBackSync(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeStatus(t, runtimeID, "offline")
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "offline" {
		t.Fatalf("setup: status=%q want offline", rt.Status)
	}

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("offline row should not have been queued, pending=%d", got)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected status=online after sync flip, got %q", status)
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
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Run(ctx)

	if err := sched.Schedule(context.Background(), rt); err != nil {
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

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour)

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
	if err := sched.Schedule(context.Background(), rt); err != nil {
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

// TestBatchedHeartbeatScheduler_FlushIgnoresEmpty exercises the empty-pending
// fast path: a tick with nothing queued must not issue a DB call.
func TestBatchedHeartbeatScheduler_FlushIgnoresEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)
	// Just calling FlushNow with nothing queued should not panic or error.
	sched.FlushNow(context.Background())
	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("pending should remain 0, got %d", got)
	}
}

// TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals confirms the
// next-beat-recovery contract: if the sweeper flips a row to offline between
// Schedule and FlushNow, the bulk UPDATE leaves it offline (no rows
// affected), and the runtime's *next* beat takes the sync path through
// recordHeartbeat → MarkAgentRuntimeOnline to recover.
func TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	// Sweeper races us to offline before the flush.
	setRuntimeStatus(t, runtimeID, "offline")

	sched.FlushNow(context.Background())

	// Bulk UPDATE's status='online' predicate means the row stays offline.
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "offline" {
		t.Fatalf("expected status=offline after raced flush, got %q", status)
	}

	// Reload and re-Schedule: rt.Status is now offline, so the scheduler
	// takes the sync MarkAgentRuntimeOnline path and the row recovers.
	rt2 := loadRuntime(t, runtimeID)
	if err := sched.Schedule(context.Background(), rt2); err != nil {
		t.Fatalf("recovery Schedule: %v", err)
	}
	status2, _, _ := readRuntimeRow(t, runtimeID)
	if status2 != "online" {
		t.Fatalf("expected sync recovery to flip back to online, got %q", status2)
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

	sched := NewPassthroughHeartbeatScheduler(testHandler.Queries)

	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Minute)) {
		t.Fatalf("passthrough did not bump last_seen_at: stale=%s after=%s", stale, lastSeen)
	}

	// Race: snapshot still says online but DB is now offline.
	rt2 := loadRuntime(t, runtimeID)
	setRuntimeStatus(t, runtimeID, "offline")
	if err := sched.Schedule(context.Background(), rt2); err != nil {
		t.Fatalf("Schedule under race: %v", err)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected race recovery via MarkAgentRuntimeOnline, got %q", status)
	}
}

// silenceUnusedPgUUID ensures the package compiles even if no other test
// happens to reference pgtype after future edits trim imports.
var _ = pgtype.UUID{}
