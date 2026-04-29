package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newRedisTestClient mirrors the helper in internal/auth: connect to
// REDIS_TEST_URL, flush, and skip when unset so `go test ./...` works
// on a stock laptop without a Redis instance running.
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

func TestEmptyClaimCache_NilSafe(t *testing.T) {
	var c *EmptyClaimCache // nil
	ctx := context.Background()

	if c.IsEmpty(ctx, "any-runtime") {
		t.Fatal("nil cache must report not-empty (cache miss)")
	}
	if v := c.CurrentVersion(ctx, "any-runtime"); v != 0 {
		t.Fatalf("nil cache CurrentVersion must be 0, got %d", v)
	}
	c.MarkEmpty(ctx, "any-runtime", 0)
	c.Bump(ctx, "any-runtime")
}

func TestNewEmptyClaimCache_NilRedisReturnsNil(t *testing.T) {
	if c := NewEmptyClaimCache(nil); c != nil {
		t.Fatalf("NewEmptyClaimCache(nil) must return nil, got %#v", c)
	}
}

func TestEmptyClaimCache_EmptyRuntimeIDIsNoOp(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewEmptyClaimCache(rdb)
	ctx := context.Background()

	c.MarkEmpty(ctx, "", 0)
	if c.IsEmpty(ctx, "") {
		t.Fatal("empty runtime ID must not hit cache")
	}
	c.Bump(ctx, "")
}

func TestEmptyClaimCache_MarkAndIsEmptyVersionMatched(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewEmptyClaimCache(rdb)
	ctx := context.Background()

	if c.IsEmpty(ctx, "rt-1") {
		t.Fatal("expected miss before mark")
	}
	v0 := c.CurrentVersion(ctx, "rt-1")
	c.MarkEmpty(ctx, "rt-1", v0)
	if !c.IsEmpty(ctx, "rt-1") {
		t.Fatal("expected hit when MarkEmpty version matches current")
	}
}

// TestEmptyClaimCache_BumpInvalidatesPriorMark is the core race-fix
// pin: an empty verdict written under v0 must be rejected once Bump
// advances the version to v1, even though the empty key itself still
// has TTL remaining.
func TestEmptyClaimCache_BumpInvalidatesPriorMark(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewEmptyClaimCache(rdb)
	ctx := context.Background()

	v0 := c.CurrentVersion(ctx, "rt-bump")
	c.MarkEmpty(ctx, "rt-bump", v0)
	if !c.IsEmpty(ctx, "rt-bump") {
		t.Fatal("precondition: empty verdict tagged with current version should hit")
	}

	c.Bump(ctx, "rt-bump")
	if c.IsEmpty(ctx, "rt-bump") {
		t.Fatal("Bump must invalidate the prior empty verdict")
	}
}

// TestEmptyClaimCache_StaleMarkRejected pins the GPT-Boy race: a slow
// claim reads version v0, the SELECT sees no rows, an enqueue Bumps
// to v1, then the slow claim writes MarkEmpty(v0). The next reader
// must NOT trust this verdict.
func TestEmptyClaimCache_StaleMarkRejected(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewEmptyClaimCache(rdb)
	ctx := context.Background()

	// Slow claim samples version BEFORE select.
	v0 := c.CurrentVersion(ctx, "rt-race")

	// Concurrent enqueue happens.
	c.Bump(ctx, "rt-race")

	// Slow claim writes its empty verdict tagged with the stale v0.
	c.MarkEmpty(ctx, "rt-race", v0)

	if c.IsEmpty(ctx, "rt-race") {
		t.Fatal("MarkEmpty written under a pre-Bump version must be rejected on read")
	}
}

func TestEmptyClaimCache_TTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewEmptyClaimCache(rdb)
	ctx := context.Background()

	c.MarkEmpty(ctx, "rt-ttl", 0)
	ttl, err := rdb.TTL(ctx, emptyClaimKey("rt-ttl")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > EmptyClaimCacheTTL+time.Second {
		t.Fatalf("unexpected empty-key TTL %v (want ~%v)", ttl, EmptyClaimCacheTTL)
	}
}

func TestEmptyClaimCache_RuntimeIsolation(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewEmptyClaimCache(rdb)
	ctx := context.Background()

	vA := c.CurrentVersion(ctx, "rt-A")
	c.MarkEmpty(ctx, "rt-A", vA)
	if c.IsEmpty(ctx, "rt-B") {
		t.Fatal("marking rt-A must not affect rt-B")
	}
	c.Bump(ctx, "rt-A")
	vB := c.CurrentVersion(ctx, "rt-B")
	c.MarkEmpty(ctx, "rt-B", vB)
	if c.IsEmpty(ctx, "rt-A") {
		t.Fatal("marking rt-B must not affect rt-A")
	}
}
