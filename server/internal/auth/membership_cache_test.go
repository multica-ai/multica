package auth

import (
	"context"
	"testing"
	"time"
)

func TestMembershipCache_NilSafe(t *testing.T) {
	var c *MembershipCache // nil
	ctx := context.Background()

	if c.Get(ctx, "any-user", "any-workspace") {
		t.Fatal("nil cache must miss")
	}
	c.Set(ctx, "any-user", "any-workspace") // no panic
	c.Invalidate(ctx, "any-user", "any-workspace") // no panic
}

func TestNewMembershipCache_NilRedisReturnsNil(t *testing.T) {
	if c := NewMembershipCache(nil); c != nil {
		t.Fatalf("NewMembershipCache(nil) must return nil, got %#v", c)
	}
}

func TestMembershipCache_SetGetInvalidate(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewMembershipCache(rdb)
	if c == nil {
		t.Fatal("NewMembershipCache returned nil")
	}
	ctx := context.Background()

	if c.Get(ctx, "user-1", "ws-1") {
		t.Fatal("expected miss before set")
	}

	c.Set(ctx, "user-1", "ws-1")
	if !c.Get(ctx, "user-1", "ws-1") {
		t.Fatal("expected hit after set")
	}

	c.Invalidate(ctx, "user-1", "ws-1")
	if c.Get(ctx, "user-1", "ws-1") {
		t.Fatal("expected miss after invalidate")
	}
}

func TestMembershipCache_TTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewMembershipCache(rdb)
	if c == nil {
		t.Fatal("NewMembershipCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "user-T", "ws-T")
	ttl, err := rdb.TTL(ctx, membershipKey("user-T", "ws-T")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > MembershipCacheTTL+time.Second {
		t.Fatalf("unexpected TTL %v (want ~%v)", ttl, MembershipCacheTTL)
	}
}

func TestMembershipCache_IsolatesKeysByUser(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewMembershipCache(rdb)
	if c == nil {
		t.Fatal("NewMembershipCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "user-A", "ws-1")
	c.Set(ctx, "user-B", "ws-1")

	if !c.Get(ctx, "user-A", "ws-1") {
		t.Fatal("user-A should be cached")
	}
	if !c.Get(ctx, "user-B", "ws-1") {
		t.Fatal("user-B should be cached")
	}

	c.Invalidate(ctx, "user-A", "ws-1")
	if c.Get(ctx, "user-A", "ws-1") {
		t.Fatal("user-A should be invalidated")
	}
	if !c.Get(ctx, "user-B", "ws-1") {
		t.Fatal("user-B should still be cached")
	}
}
