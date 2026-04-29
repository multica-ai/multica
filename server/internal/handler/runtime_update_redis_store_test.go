package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRedisUpdateStore_CreateGetComplete(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-1", "v0.1.13")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != UpdatePending {
		t.Fatalf("initial status = %s", req.Status)
	}
	if req.TargetVersion != "v0.1.13" {
		t.Fatalf("target version = %q", req.TargetVersion)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != req.ID {
		t.Fatalf("round trip lost id: got=%v", got)
	}

	if err := store.Complete(ctx, req.ID, "ok"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err = store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != UpdateCompleted {
		t.Fatalf("status after complete = %s", got.Status)
	}
	if got.Output != "ok" {
		t.Fatalf("output not persisted: %q", got.Output)
	}
}

// TestRedisUpdateStore_PopPendingAcrossInstances is the regression test for
// the bug this change fixes: two distinct *store* instances (i.e. two API
// nodes) share one Redis, one creates a pending request, the other PopPending-s
// it. Before the Redis-backed store this returned nil and the daemon never
// saw the update.
func TestRedisUpdateStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross", "v0.1.13")
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B pop: %v", err)
	}
	if popped == nil {
		t.Fatal("node B did not see node A's pending request")
	}
	if popped.ID != req.ID {
		t.Fatalf("popped id = %s, want %s", popped.ID, req.ID)
	}
	if popped.Status != UpdateRunning {
		t.Fatalf("popped status = %s, want running", popped.Status)
	}
	if popped.TargetVersion != "v0.1.13" {
		t.Fatalf("target_version lost: %q", popped.TargetVersion)
	}

	// A second pop must see nothing (claim was atomic).
	again, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no more pending, got %+v", again)
	}
}

// TestRedisUpdateStore_PopPendingConcurrent asserts the ZREM-wins race guard:
// N concurrent PopPending calls against a single pending request return
// exactly one winner.
func TestRedisUpdateStore_PopPendingConcurrent(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-race", "v0.1.13")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const N = 8
	var wg sync.WaitGroup
	results := make(chan *UpdateRequest, N)
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			popped, err := store.PopPending(ctx, "runtime-race")
			if err != nil {
				errs <- err
				return
			}
			results <- popped
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent pop error: %v", err)
	}

	winners := 0
	for popped := range results {
		if popped != nil {
			winners++
			if popped.ID != req.ID {
				t.Fatalf("winner popped wrong id: %s", popped.ID)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
}

// TestRedisUpdateStore_RejectsConcurrentUpdate verifies the "one update at a
// time per runtime" rule still holds across nodes — node B's Create must fail
// with errUpdateInProgress when node A already has an active request.
func TestRedisUpdateStore_RejectsConcurrentUpdate(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)

	if _, err := nodeA.Create(ctx, "runtime-busy", "v0.1.13"); err != nil {
		t.Fatalf("node A create: %v", err)
	}

	if _, err := nodeB.Create(ctx, "runtime-busy", "v0.1.14"); !errors.Is(err, errUpdateInProgress) {
		t.Fatalf("node B Create err = %v, want errUpdateInProgress", err)
	}

	// Pop and complete the first request — the next Create should now succeed.
	popped, err := nodeA.PopPending(ctx, "runtime-busy")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil {
		t.Fatal("expected to pop the active update")
	}
	if err := nodeA.Complete(ctx, popped.ID, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if _, err := nodeB.Create(ctx, "runtime-busy", "v0.1.14"); err != nil {
		t.Fatalf("node B re-create after completion: %v", err)
	}
}

// TestRedisUpdateStore_Timeout pins the timeout transition: a pending request
// older than 120s must surface as UpdateTimeout, and PopPending must skip it.
func TestRedisUpdateStore_Timeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-timeout", "v0.1.13")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Rewind CreatedAt so the timeout threshold is blown without blocking
	// the test 121 seconds.
	req.CreatedAt = time.Now().Add(-updateTimeout - time.Second)
	if err := store.persistRequest(ctx, req); err != nil {
		t.Fatalf("persist rewound: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != UpdateTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}

	// A subsequent PopPending must NOT return a timed-out request.
	popped, err := store.PopPending(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("pop after timeout: %v", err)
	}
	if popped != nil {
		t.Fatalf("expected no pending after timeout, got %+v", popped)
	}

	// And the active key should have been cleared so the next Create works.
	if _, err := store.Create(ctx, "runtime-timeout", "v0.1.14"); err != nil {
		t.Fatalf("re-create after timeout: %v", err)
	}
}

// TestRedisUpdateStore_PerRuntimeIsolation — keys for runtime A must not bleed
// into a PopPending for runtime B.
func TestRedisUpdateStore_PerRuntimeIsolation(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	if _, err := store.Create(ctx, "runtime-A", "v0.1.13"); err != nil {
		t.Fatalf("create A: %v", err)
	}
	reqB, err := store.Create(ctx, "runtime-B", "v0.1.13")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	popped, err := store.PopPending(ctx, "runtime-B")
	if err != nil {
		t.Fatalf("pop B: %v", err)
	}
	if popped == nil || popped.ID != reqB.ID {
		t.Fatalf("pop returned wrong request: %+v", popped)
	}

	// A's request is still pending.
	ids, err := rdb.ZRange(ctx, updatePendingKey("runtime-A"), 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange A: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending for A after pop(B), got %d: %v", len(ids), ids)
	}
}

// Compile-time assertions: both implementations MUST satisfy the interface so
// NewRouter's assignment stays type-safe.
var (
	_ UpdateStore = (*RedisUpdateStore)(nil)
	_ UpdateStore = (*InMemoryUpdateStore)(nil)
)
