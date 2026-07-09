package auth

import (
	"context"
	"testing"
	"time"
)

func TestDaemonTokenCache_NilSafe(t *testing.T) {
	var c *DaemonTokenCache // nil
	ctx := context.Background()

	if id, ok := c.Get(ctx, "any-hash"); ok || id != (DaemonTokenIdentity{}) {
		t.Fatalf("nil cache must miss; got (%+v, %v)", id, ok)
	}
	c.Set(ctx, "any-hash", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, AuthCacheTTL)
	c.Invalidate(ctx, "any-hash")
}

func TestNewDaemonTokenCache_NilRedisReturnsNil(t *testing.T) {
	if c := NewDaemonTokenCache(nil); c != nil {
		t.Fatalf("NewDaemonTokenCache(nil) must return nil, got %#v", c)
	}
}

func TestDaemonTokenCache_SetGetInvalidate(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewDaemonTokenCache(rdb)
	if c == nil {
		t.Fatal("NewDaemonTokenCache returned nil")
	}
	ctx := context.Background()

	if _, ok := c.Get(ctx, "missing"); ok {
		t.Fatal("expected miss before set")
	}

	want := DaemonTokenIdentity{WorkspaceID: "ws-uuid", DaemonID: "daemon-1"}
	c.Set(ctx, "hash-D", want, AuthCacheTTL)
	if got, ok := c.Get(ctx, "hash-D"); !ok || got != want {
		t.Fatalf("expected hit %+v, got (%+v, %v)", want, got, ok)
	}

	c.Invalidate(ctx, "hash-D")
	if _, ok := c.Get(ctx, "hash-D"); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestDaemonTokenCache_TTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewDaemonTokenCache(rdb)
	if c == nil {
		t.Fatal("NewDaemonTokenCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-T", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, AuthCacheTTL)
	ttl, err := rdb.TTL(ctx, daemonTokenCacheKey("hash-T")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > AuthCacheTTL+time.Second {
		t.Fatalf("unexpected TTL %v (want ~%v)", ttl, AuthCacheTTL)
	}
}

func TestDaemonTokenCache_Set_RespectsClampedTTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewDaemonTokenCache(rdb)
	if c == nil {
		t.Fatal("NewDaemonTokenCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-short", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, 5*time.Second)
	ttl, err := rdb.TTL(ctx, daemonTokenCacheKey("hash-short")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 5*time.Second+time.Second {
		t.Fatalf("expected clamped TTL ~5s, got %v", ttl)
	}

	c.Set(ctx, "hash-zero", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, 0)
	if _, ok := c.Get(ctx, "hash-zero"); ok {
		t.Fatal("zero-TTL Set must not cache")
	}
	c.Set(ctx, "hash-neg", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, -time.Second)
	if _, ok := c.Get(ctx, "hash-neg"); ok {
		t.Fatal("negative-TTL Set must not cache")
	}
}
