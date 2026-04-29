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
	c.Set(ctx, "any-hash", "user-1")     // no panic
	c.Invalidate(ctx, "any-hash")        // no panic
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

	c.Set(ctx, "hash-A", "user-A")
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

	c.Set(ctx, "hash-T", "user-T")
	ttl, err := rdb.TTL(ctx, patCacheKey("hash-T")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	// Redis returns the remaining TTL; allow a small skew for rounding.
	if ttl <= 0 || ttl > PATCacheTTL+time.Second {
		t.Fatalf("unexpected TTL %v (want ~%v)", ttl, PATCacheTTL)
	}
}
