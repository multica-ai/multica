package handler

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newRedisTestClient connects to the Redis instance indicated by REDIS_TEST_URL
// and flushes it so each test starts from a clean slate. The helper skips the
// calling test if the env var is unset — matches the DATABASE_URL gating in
// the rest of the suite so `go test ./...` still works on a stock laptop
// without a running Redis.
func newRedisTestClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_TEST_URL: %v", err)
	}
	rdb := redis.NewClient(opts)
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("REDIS_TEST_URL unreachable: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})
	return rdb
}

func TestRedisLocalSkillListStore_CreateGetComplete(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != RuntimeLocalSkillPending {
		t.Fatalf("initial status = %s", req.Status)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != req.ID {
		t.Fatalf("round trip lost id: got=%v", got)
	}

	skills := []RuntimeLocalSkillSummary{
		{
			Key:         "review-helper",
			Name:        "Review Helper",
			Description: "Review PRs",
			SourcePath:  "~/.claude/skills/review-helper",
			Provider:    "claude",
			FileCount:   2,
		},
	}
	if err := store.Complete(ctx, req.ID, skills, true); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err = store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != RuntimeLocalSkillCompleted {
		t.Fatalf("status after complete = %s", got.Status)
	}
	if len(got.Skills) != 1 || got.Skills[0].Key != "review-helper" {
		t.Fatalf("skills not persisted: %+v", got.Skills)
	}
}

// TestRedisLocalSkillListStore_PopPendingAcrossInstances is the regression
// test for the exact bug this change fixes: two distinct *store* instances
// (i.e. two API nodes) share one Redis, one creates a pending request, the
// other PopPending-s it. Before the Redis-backed store this returned nil and
// the request timed out.
func TestRedisLocalSkillListStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisLocalSkillListStore(rdb)
	nodeB := NewRedisLocalSkillListStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross")
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
	if popped.Status != RuntimeLocalSkillRunning {
		t.Fatalf("popped status = %s, want running", popped.Status)
	}
	if popped.RunStartedAt == nil {
		t.Fatal("run_started_at not set after pop")
	}

	// A third pop must see nothing (claim was atomic).
	again, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no more pending, got %+v", again)
	}
}

// TestRedisLocalSkillListStore_PopPendingConcurrent asserts the ZREM-wins race
// guard: N concurrent PopPending calls against a single pending request
// return exactly one winner.
func TestRedisLocalSkillListStore_PopPendingConcurrent(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-race")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const N = 8
	var wg sync.WaitGroup
	results := make(chan *RuntimeLocalSkillListRequest, N)
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

func TestRedisLocalSkillListStore_PendingTimeout(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Rewind CreatedAt so the pending threshold is blown — simulates 31s of
	// daemon silence without actually blocking the test that long.
	req.CreatedAt = time.Now().Add(-runtimeLocalSkillPendingTimeout - time.Second)
	if err := store.persistListRequest(ctx, req); err != nil {
		t.Fatalf("persist rewound: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != RuntimeLocalSkillTimeout {
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
}

func TestRedisLocalSkillImportStore_PreservesCreatorID(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillImportStore(rdb)

	name := "Review Helper"
	desc := "Desc"
	req, err := store.Create(ctx, "runtime-1", "user-42", "review-helper", &name, &desc)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.CreatorID != "user-42" {
		t.Fatalf("creator id lost on create")
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// CreatorID is `json:"-"` on the public struct — verify the Redis envelope
	// restores it, otherwise ReportLocalSkillImportResult can't attribute the
	// created Skill to anyone.
	if got.CreatorID != "user-42" {
		t.Fatalf("creator id lost round trip: %q", got.CreatorID)
	}
	if got.Name == nil || *got.Name != name {
		t.Fatalf("name lost: %v", got.Name)
	}
	if got.Description == nil || *got.Description != desc {
		t.Fatalf("description lost: %v", got.Description)
	}
}

func TestRedisLocalSkillImportStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisLocalSkillImportStore(rdb)
	nodeB := NewRedisLocalSkillImportStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-import", "user-1", "review-helper", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-import")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.ID != req.ID {
		t.Fatalf("cross-node pop failed: got %+v", popped)
	}
	if popped.Status != RuntimeLocalSkillRunning {
		t.Fatalf("popped status = %s", popped.Status)
	}
	if popped.SkillKey != "review-helper" {
		t.Fatalf("skill_key lost: %q", popped.SkillKey)
	}
}

// Smoke test: make sure the runtime-local-skill store keys don't collide
// across runtimes — PopPending for runtime A must not see B's pending.
func TestRedisLocalSkillListStore_PerRuntimeIsolation(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	if _, err := store.Create(ctx, "runtime-A"); err != nil {
		t.Fatalf("create A: %v", err)
	}
	reqB, err := store.Create(ctx, "runtime-B")
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
	ids, err := rdb.ZRange(ctx, localSkillListPendingKey("runtime-A"), 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange A: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending for A after pop(B), got %d: %v", len(ids), ids)
	}
}

// TestRedisLocalSkillListStore_PopPendingAtomicClaim pins the PR-1557 review
// fix: the claim (ZREM pending + persist running record) MUST land as one
// atomic unit. If the old two-step ordering came back ("ZRem first, SET
// second") a transient error between the two would strand the request — not
// in pending, still serialised as "pending" on disk, never re-dispatched.
//
// We verify the happy-path invariant end-to-end: after one PopPending the
// record is in "running" state AND a second PopPending on the same runtime
// returns nothing (i.e. the pending zset no longer references the id).
func TestRedisLocalSkillListStore_PopPendingAtomicClaim(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisLocalSkillListStore(rdb)

	req, err := store.Create(ctx, "runtime-atomic")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	popped, err := store.PopPending(ctx, "runtime-atomic")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.ID != req.ID {
		t.Fatalf("pop returned wrong request: %+v", popped)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after pop: %v", err)
	}
	if got.Status != RuntimeLocalSkillRunning {
		t.Fatalf("record status = %s, want running", got.Status)
	}

	// The pending queue must no longer reference the claimed id — exposed
	// via PopPending rather than poking the zset directly.
	again, err := store.PopPending(ctx, "runtime-atomic")
	if err != nil {
		t.Fatalf("second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("second pop should be empty, got %+v", again)
	}
}

// Compile-time assertions: the Redis stores MUST satisfy the interfaces so
// NewRouter's assignment stays type-safe.
var (
	_ LocalSkillListStore   = (*RedisLocalSkillListStore)(nil)
	_ LocalSkillImportStore = (*RedisLocalSkillImportStore)(nil)
	_ LocalSkillListStore   = (*InMemoryLocalSkillListStore)(nil)
	_ LocalSkillImportStore = (*InMemoryLocalSkillImportStore)(nil)
)
