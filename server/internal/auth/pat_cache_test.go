package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newRedisTestClient mirrors the helper in the handler package: connect to
// REDIS_TEST_URL, flush, and skip when unset so `go test ./...` works on a
// stock laptop without a Redis instance running.
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

func TestPATCache_NilSafe(t *testing.T) {
	var c *PATCache // nil
	ctx := context.Background()

	if v, ok := c.Get(ctx, "any-hash"); ok || v != "" {
		t.Fatalf("nil cache must miss; got (%q, %v)", v, ok)
	}
	c.Set(ctx, "any-hash", "user-1", PATCacheTTL) // no panic
	c.Invalidate(ctx, "any-hash")                 // no panic
}

func TestNewPATCache_NilRedisReturnsNil(t *testing.T) {
	if c := NewPATCache(nil); c != nil {
		t.Fatalf("NewPATCache(nil) must return nil, got %#v", c)
	}
}

func TestPATCache_SetGetInvalidate(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewPATCache(rdb)
	if c == nil {
		t.Fatal("NewPATCache returned nil")
	}
	ctx := context.Background()

	if _, ok := c.Get(ctx, "missing"); ok {
		t.Fatal("expected miss before set")
	}

	c.Set(ctx, "hash-A", "user-A", PATCacheTTL)
	if v, ok := c.Get(ctx, "hash-A"); !ok || v != "user-A" {
		t.Fatalf("expected hit user-A, got (%q, %v)", v, ok)
	}

	c.Invalidate(ctx, "hash-A")
	if v, ok := c.Get(ctx, "hash-A"); ok {
		t.Fatalf("expected miss after invalidate, got (%q, %v)", v, ok)
	}
}

// TestPATCache_TTL pins the contract that entries expire on PATCacheTTL so
// the auth middleware refreshes last_used_at at most once per window.
//
// We don't sleep PATCacheTTL (60s); instead we assert the TTL is what the
// constructor set, which is the property the middleware actually depends
// on.
func TestPATCache_TTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewPATCache(rdb)
	if c == nil {
		t.Fatal("NewPATCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-T", "user-T", PATCacheTTL)
	ttl, err := rdb.TTL(ctx, patCacheKey("hash-T")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	// Redis returns the remaining TTL; allow a small skew for rounding.
	if ttl <= 0 || ttl > PATCacheTTL+time.Second {
		t.Fatalf("unexpected TTL %v (want ~%v)", ttl, PATCacheTTL)
	}
}

func TestTTLForExpiry(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	// No expiry set → full PATCacheTTL.
	if got := TTLForExpiry(now, time.Time{}); got != PATCacheTTL {
		t.Fatalf("zero expires_at: got %v, want %v", got, PATCacheTTL)
	}

	// Far-future expiry → full PATCacheTTL.
	far := now.Add(24 * time.Hour)
	if got := TTLForExpiry(now, far); got != PATCacheTTL {
		t.Fatalf("far-future expires_at: got %v, want %v", got, PATCacheTTL)
	}

	// Sooner-than-TTL expiry → clamped to remaining lifetime.
	soon := now.Add(10 * time.Second)
	if got := TTLForExpiry(now, soon); got != 10*time.Second {
		t.Fatalf("sooner expires_at: got %v, want 10s", got)
	}

	// Already expired (or exactly now) → 0, caller skips caching.
	if got := TTLForExpiry(now, now); got != 0 {
		t.Fatalf("expires_at == now: got %v, want 0", got)
	}
	if got := TTLForExpiry(now, now.Add(-time.Second)); got != 0 {
		t.Fatalf("past expires_at: got %v, want 0", got)
	}
}

// TestPATCache_Set_RespectsClampedTTL is the regression test for the
// review finding: a PAT expiring in <PATCacheTTL must NOT be cached for
// the full PATCacheTTL window, otherwise it would continue passing auth
// on cache hit after expires_at.
func TestPATCache_Set_RespectsClampedTTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewPATCache(rdb)
	if c == nil {
		t.Fatal("NewPATCache returned nil")
	}
	ctx := context.Background()

	// Cache with a 5s TTL — what TTLForExpiry would return for a token
	// expiring 5s from now.
	c.Set(ctx, "hash-short", "user-short", 5*time.Second)
	ttl, err := rdb.TTL(ctx, patCacheKey("hash-short")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 5*time.Second+time.Second {
		t.Fatalf("expected clamped TTL ~5s, got %v", ttl)
	}

	// Zero / negative TTL must skip caching entirely (already-expired
	// token's TOCTOU-safe path).
	c.Set(ctx, "hash-zero", "user-zero", 0)
	if _, ok := c.Get(ctx, "hash-zero"); ok {
		t.Fatal("zero-TTL Set must not cache")
	}
	c.Set(ctx, "hash-neg", "user-neg", -time.Second)
	if _, ok := c.Get(ctx, "hash-neg"); ok {
		t.Fatal("negative-TTL Set must not cache")
	}
}
