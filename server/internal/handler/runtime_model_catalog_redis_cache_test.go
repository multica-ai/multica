package handler

import (
	"context"
	"testing"
	"time"
)

// Reuses the newRedisTestClient helper from
// runtime_local_skills_redis_store_test.go: same Redis instance, same gating on
// REDIS_TEST_URL, same FlushDB-per-test isolation. The in-memory twin of these
// assertions lives in runtime_model_catalog_test.go — both backends must agree,
// because a deployment silently swaps between them based on REDIS_URL.

func TestRedisModelCatalogCache_RoundTrip(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	cache := NewRedisModelCatalogCache(rdb)

	if got, err := cache.Get(ctx, "runtime-1"); err != nil || got != nil {
		t.Fatalf("cold cache should miss: got=%+v err=%v", got, err)
	}

	if err := cache.Put(ctx, "runtime-1", sampleCatalog(), true); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := cache.Get(ctx, "runtime-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected a cached snapshot")
	}
	if got.RuntimeID != "runtime-1" || len(got.Models) != 2 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if !got.Models[0].Default {
		t.Error("the default badge must survive the Redis round trip")
	}
	if got.StoredAt.IsZero() {
		t.Error("StoredAt must be persisted — the serve/revalidate windows depend on it")
	}
	if age := got.Age(time.Now()); age < 0 || age > time.Minute {
		t.Errorf("implausible snapshot age %s", age)
	}

	// A sibling API node reading the same key must see the same snapshot; that
	// is the whole reason this backend exists.
	sibling := NewRedisModelCatalogCache(rdb)
	if shared, err := sibling.Get(ctx, "runtime-1"); err != nil || shared == nil {
		t.Fatalf("snapshot not visible to a sibling node: got=%+v err=%v", shared, err)
	}
}

func TestRedisModelCatalogCache_SkipsUncacheableResults(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	cache := NewRedisModelCatalogCache(rdb)

	if err := cache.Put(ctx, "runtime-empty", nil, true); err != nil {
		t.Fatalf("put empty: %v", err)
	}
	if got, _ := cache.Get(ctx, "runtime-empty"); got != nil {
		t.Fatalf("empty catalog must not be cached: %+v", got)
	}

	if err := cache.Put(ctx, "runtime-unsupported", sampleCatalog(), false); err != nil {
		t.Fatalf("put unsupported: %v", err)
	}
	if got, _ := cache.Get(ctx, "runtime-unsupported"); got != nil {
		t.Fatalf("unsupported runtime must not be cached: %+v", got)
	}
}

func TestRedisModelCatalogCache_InvalidateAndExpiry(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	cache := NewRedisModelCatalogCache(rdb)

	if err := cache.Put(ctx, "runtime-1", sampleCatalog(), true); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := cache.Invalidate(ctx, "runtime-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if got, _ := cache.Get(ctx, "runtime-1"); got != nil {
		t.Fatalf("expected a miss after Invalidate: %+v", got)
	}

	// The key must carry a TTL so a runtime that never comes back cannot pin a
	// snapshot in Redis forever.
	if err := cache.Put(ctx, "runtime-1", sampleCatalog(), true); err != nil {
		t.Fatalf("re-put: %v", err)
	}
	ttl, err := rdb.TTL(ctx, modelCatalogKey("runtime-1")).Result()
	if err != nil {
		t.Fatalf("ttl: %v", err)
	}
	if ttl <= 0 || ttl > modelCatalogServeWindow {
		t.Fatalf("ttl = %s, want (0, %s]", ttl, modelCatalogServeWindow)
	}

	// A snapshot older than the serve window is a miss even if the key is still
	// present (the age check is the contract, not the TTL).
	stale := NewRedisModelCatalogCache(rdb)
	stale.ttl = time.Nanosecond
	if got, err := stale.Get(ctx, "runtime-1"); err != nil || got != nil {
		t.Fatalf("expected a miss past the serve window: got=%+v err=%v", got, err)
	}
}

func TestRedisModelCatalogCache_DropsUndecodableSnapshot(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	cache := NewRedisModelCatalogCache(rdb)

	if err := rdb.Set(ctx, modelCatalogKey("runtime-1"), "not-json", time.Minute).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got, err := cache.Get(ctx, "runtime-1"); err == nil || got != nil {
		t.Fatalf("expected a decode error and no snapshot: got=%+v err=%v", got, err)
	}
	// The poisoned value must be gone so it cannot fail every later read.
	if n, err := rdb.Exists(ctx, modelCatalogKey("runtime-1")).Result(); err != nil || n != 0 {
		t.Fatalf("undecodable snapshot was not dropped: exists=%d err=%v", n, err)
	}
}
