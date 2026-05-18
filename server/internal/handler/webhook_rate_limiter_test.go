package handler

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryWebhookRateLimiter_AllowsBelowLimit(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 3, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestMemoryWebhookRateLimiter_RejectsAboveLimit(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 2, Window: time.Minute})
	ctx := context.Background()
	if !l.Allow(ctx, "tok") {
		t.Fatal("first should pass")
	}
	if !l.Allow(ctx, "tok") {
		t.Fatal("second should pass")
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("third should be rejected")
	}
	// Different key must have its own budget.
	if !l.Allow(ctx, "other") {
		t.Fatal("different key should pass")
	}
}

func TestMemoryWebhookRateLimiter_WindowExpiry(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: 10 * time.Millisecond})
	ctx := context.Background()
	if !l.Allow(ctx, "tok") {
		t.Fatal("first should pass")
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("second should fail (within window)")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow(ctx, "tok") {
		t.Fatal("third should pass after window")
	}
}

func TestMemoryWebhookRateLimiter_ZeroLimitDisabled(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 0, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("Limit=0 should be unbounded, rejected at %d", i)
		}
	}
}

// TestWebhookLimiterLuaScript_StructureGuard pins the Lua script so any future
// edit that reorders trim/count/insert (or drops EXPIRE) is caught even when
// no live Redis is available to the test process. The per-pod race the script
// guards against ("two pods both see count = limit-1 and both insert") only
// triggers if the three calls run atomically; this regression guard is the
// cheap-but-effective complement to the live-Redis test below.
func TestWebhookLimiterLuaScript_StructureGuard(t *testing.T) {
	// redis.NewScript stores the body, so we just sanity-check ordering by
	// matching the source string. The exact whitespace is brittle but the
	// alternative (Lua AST parsing) is overkill for a 10-line script.
	src := webhookLimiterAllowSource()
	mustBefore := func(a, b string) {
		t.Helper()
		ia, ib := strings.Index(src, a), strings.Index(src, b)
		if ia < 0 || ib < 0 {
			t.Fatalf("script must contain %q and %q, src=%s", a, b, src)
		}
		if ia >= ib {
			t.Fatalf("expected %q to appear before %q, src=%s", a, b, src)
		}
	}
	mustBefore("ZREMRANGEBYSCORE", "ZCARD")
	mustBefore("ZCARD", "ZADD")
	mustBefore("ZADD", "EXPIRE")
}

func TestRedisWebhookRateLimiter_RejectsAboveLimit(t *testing.T) {
	rdb := newRedisTestClient(t)
	defer rdb.Close()

	l := NewRedisWebhookRateLimiter(rdb, WebhookRateLimit{Limit: 3, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.Allow(ctx, "tok") {
		t.Fatal("fourth request should be rejected")
	}
	// Different key has its own budget.
	if !l.Allow(ctx, "other") {
		t.Fatal("different key should pass")
	}
}

func TestRedisWebhookIPRateLimiter_HasSeparateBudgetFromTokenLimiter(t *testing.T) {
	// Per-IP and per-token use the SAME Lua script but DIFFERENT key
	// prefixes. Exhausting one budget must not affect the other —
	// regression-protect by exhausting per-token then proving per-IP is
	// untouched against the same Redis instance.
	rdb := newRedisTestClient(t)
	defer rdb.Close()

	tok := NewRedisWebhookRateLimiter(rdb, WebhookRateLimit{Limit: 1, Window: time.Minute})
	ip := NewRedisWebhookIPRateLimiter(rdb, WebhookRateLimit{Limit: 2, Window: time.Minute})
	ctx := context.Background()
	if !tok.Allow(ctx, "alice") {
		t.Fatal("token limiter first request should pass")
	}
	if tok.Allow(ctx, "alice") {
		t.Fatal("token limiter second request should be rejected")
	}
	// IP limiter must still have its full budget.
	if !ip.Allow(ctx, "alice") {
		t.Fatal("IP limiter first request should pass")
	}
	if !ip.Allow(ctx, "alice") {
		t.Fatal("IP limiter second request should pass")
	}
	if ip.Allow(ctx, "alice") {
		t.Fatal("IP limiter third request should be rejected")
	}
}
