package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeLivenessStore lets tests drive every Available / Touch / IsAliveBatch
// branch of recordHeartbeat without spinning up Redis. It records call counts
// so we can assert the gate behavior without any DB-time dependence.
type fakeLivenessStore struct {
	mu          sync.Mutex
	available   bool
	touchErr    error
	touched     []string
	aliveResult map[string]bool
	aliveOK     bool
	forgotten   []string
}

func (f *fakeLivenessStore) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available
}

func (f *fakeLivenessStore) Touch(_ context.Context, runtimeID string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched = append(f.touched, runtimeID)
	return f.touchErr
}

func (f *fakeLivenessStore) IsAliveBatch(_ context.Context, ids []string) (map[string]bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.aliveOK {
		return nil, false
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = f.aliveResult[id]
	}
	return out, true
}

func (f *fakeLivenessStore) Forget(_ context.Context, runtimeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgotten = append(f.forgotten, runtimeID)
}

func (f *fakeLivenessStore) touchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.touched)
}

// readRuntimeRow returns the fresh agent_runtime row for assertions.
func readRuntimeRow(t *testing.T, runtimeID string) (status string, lastSeen time.Time, updatedAt time.Time) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, last_seen_at, updated_at FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&status, &lastSeen, &updatedAt); err != nil {
		t.Fatalf("read runtime row: %v", err)
	}
	return
}

func setRuntimeLastSeenAt(t *testing.T, runtimeID string, when time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_runtime SET last_seen_at = $1 WHERE id = $2`, when, runtimeID,
	); err != nil {
		t.Fatalf("force last_seen_at: %v", err)
	}
}

func setRuntimeStatus(t *testing.T, runtimeID, status string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_runtime SET status = $1 WHERE id = $2`, status, runtimeID,
	); err != nil {
		t.Fatalf("force status: %v", err)
	}
}

// loadRuntime is a thin wrapper around the sqlc query to keep the test bodies
// short.
func loadRuntime(t *testing.T, runtimeID string) db.AgentRuntime {
	t.Helper()
	uuid, err := pgUUID(runtimeID)
	if err != nil {
		t.Fatalf("parse runtime id: %v", err)
	}
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), uuid)
	if err != nil {
		t.Fatalf("GetAgentRuntime: %v", err)
	}
	return rt
}

func pgUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, err
	}
	return u, nil
}

// TestRecordHeartbeat_NoopStoreAlwaysWritesDB confirms that without a Redis
// LivenessStore the heartbeat path keeps the legacy behavior: every call
// bumps last_seen_at on the DB row.
func TestRecordHeartbeat_NoopStoreAlwaysWritesDB(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	orig := testHandler.LivenessStore
	testHandler.LivenessStore = NewNoopLivenessStore()
	t.Cleanup(func() { testHandler.LivenessStore = orig })

	// Pin last_seen_at to "just now" to ensure the DB-flush condition is not
	// what's driving the write.
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	before := rt.LastSeenAt.Time

	time.Sleep(50 * time.Millisecond)

	if err := testHandler.recordHeartbeat(context.Background(), rt); err != nil {
		t.Fatalf("recordHeartbeat: %v", err)
	}

	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(before) {
		t.Fatalf("noop-store heartbeat did not bump last_seen_at: before=%s after=%s", before, lastSeen)
	}
}

// TestRecordHeartbeat_RedisAvailableSkipsDBWithinFlushWindow confirms the hot
// path: when Redis is the source of truth and the row is fresh, the heartbeat
// touches Redis but does NOT rewrite the DB row.
func TestRecordHeartbeat_RedisAvailableSkipsDBWithinFlushWindow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	fake := &fakeLivenessStore{available: true, aliveOK: true}
	orig := testHandler.LivenessStore
	testHandler.LivenessStore = fake
	t.Cleanup(func() { testHandler.LivenessStore = orig })

	// Pin last_seen_at to "just now" so we are inside the flush window.
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	before := rt.LastSeenAt.Time

	if err := testHandler.recordHeartbeat(context.Background(), rt); err != nil {
		t.Fatalf("recordHeartbeat: %v", err)
	}

	if fake.touchCount() != 1 {
		t.Fatalf("expected exactly one Touch, got %d", fake.touchCount())
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.Equal(before) {
		t.Fatalf("DB last_seen_at should not have been rewritten within flush window: before=%s after=%s", before, lastSeen)
	}
}

// TestRecordHeartbeat_DBFlushOnStaleRow confirms the DB summary flush:
// even with Redis healthy, a row whose last_seen_at exceeds the flush
// interval gets a write so the UI's display value stays bounded.
func TestRecordHeartbeat_DBFlushOnStaleRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	fake := &fakeLivenessStore{available: true, aliveOK: true}
	orig := testHandler.LivenessStore
	testHandler.LivenessStore = fake
	t.Cleanup(func() { testHandler.LivenessStore = orig })

	// Push last_seen_at past the flush threshold.
	stale := time.Now().Add(-2 * runtimeHeartbeatDBFlushInterval)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	if err := testHandler.recordHeartbeat(context.Background(), rt); err != nil {
		t.Fatalf("recordHeartbeat: %v", err)
	}

	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Minute)) {
		t.Fatalf("DB last_seen_at should have been flushed: stale=%s after=%s", stale, lastSeen)
	}
}

// TestRecordHeartbeat_OfflineToOnlineForcesDBWrite confirms that an offline
// row's first heartbeat always rewrites the DB to flip status, even with
// Redis healthy.
func TestRecordHeartbeat_OfflineToOnlineForcesDBWrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	fake := &fakeLivenessStore{available: true, aliveOK: true}
	orig := testHandler.LivenessStore
	testHandler.LivenessStore = fake
	t.Cleanup(func() { testHandler.LivenessStore = orig })

	setRuntimeStatus(t, runtimeID, "offline")
	// Keep last_seen_at fresh so the DB-flush condition is not what's
	// driving the write — only the offline→online transition is.
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "offline" {
		t.Fatalf("setup: status = %q, want offline", rt.Status)
	}

	if err := testHandler.recordHeartbeat(context.Background(), rt); err != nil {
		t.Fatalf("recordHeartbeat: %v", err)
	}

	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected status=online after offline→online heartbeat, got %q", status)
	}
}

// TestRecordHeartbeat_TouchErrorFallsBackToDB confirms graceful degradation:
// if Redis Touch errors, the heartbeat still writes the DB so the sweeper's
// DB-only fallback path observes a fresh last_seen_at.
func TestRecordHeartbeat_TouchErrorFallsBackToDB(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	fake := &fakeLivenessStore{
		available: true,
		touchErr:  errors.New("simulated redis outage"),
	}
	orig := testHandler.LivenessStore
	testHandler.LivenessStore = fake
	t.Cleanup(func() { testHandler.LivenessStore = orig })

	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	before := rt.LastSeenAt.Time

	time.Sleep(50 * time.Millisecond)

	if err := testHandler.recordHeartbeat(context.Background(), rt); err != nil {
		t.Fatalf("recordHeartbeat: %v", err)
	}

	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(before) {
		t.Fatalf("Touch failure should have fallen back to a DB write: before=%s after=%s", before, lastSeen)
	}
}

// TestRecordHeartbeat_SweeperRaceRecoversOnline pins the regression for the
// status-snapshot race: rt.Status was read from a prior SELECT, but the
// sweeper can flip the row to offline between that SELECT and the heartbeat's
// write. Without the affected-rows fallback in recordHeartbeat, the heartbeat
// would only bump last_seen_at and leave the row stuck offline. The legacy
// UpdateAgentRuntimeHeartbeat always re-asserted status='online', so this
// regression test guards the new SELECT/Touch/MarkOnline path against the
// same scenario.
func TestRecordHeartbeat_SweeperRaceRecoversOnline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	// Force the noop store so recordHeartbeat takes the DB-write path
	// without any Redis interference. The race is independent of the
	// liveness store — it lives entirely between the rt.Status snapshot
	// and the DB UPDATE.
	orig := testHandler.LivenessStore
	testHandler.LivenessStore = NewNoopLivenessStore()
	t.Cleanup(func() { testHandler.LivenessStore = orig })

	// Snapshot the runtime while it is still online.
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "online" {
		t.Fatalf("setup: runtime should be online, got %q", rt.Status)
	}

	// Simulate the sweeper flipping the row to offline between the
	// snapshot and the heartbeat's UPDATE.
	setRuntimeStatus(t, runtimeID, "offline")

	if err := testHandler.recordHeartbeat(context.Background(), rt); err != nil {
		t.Fatalf("recordHeartbeat: %v", err)
	}

	status, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected sweeper-raced runtime to recover online, got %q", status)
	}
	if time.Since(lastSeen) > 30*time.Second {
		t.Fatalf("last_seen_at not refreshed: %s ago", time.Since(lastSeen))
	}
}
