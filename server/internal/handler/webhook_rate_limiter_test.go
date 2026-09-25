package handler

import (
	"context"
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

func TestMemorySlidingWindowRateLimiter_PrunesExpiredKeys(t *testing.T) {
	limiter := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: 10 * time.Millisecond})
	memoryLimiter, ok := limiter.(*memoryWebhookRateLimiter)
	if !ok {
		t.Fatalf("limiter type = %T, want *memoryWebhookRateLimiter", limiter)
	}
	ctx := context.Background()
	if !limiter.Allow(ctx, "expired") {
		t.Fatal("first key should be allowed")
	}
	time.Sleep(20 * time.Millisecond)
	if !limiter.Allow(ctx, "current") {
		t.Fatal("current key should be allowed")
	}

	memoryLimiter.mu.Lock()
	defer memoryLimiter.mu.Unlock()
	if _, ok := memoryLimiter.hit["expired"]; ok {
		t.Fatal("expired key was not removed by the opportunistic sweep")
	}
	if _, ok := memoryLimiter.hit["current"]; !ok {
		t.Fatal("current key was removed by the opportunistic sweep")
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

func TestMemoryWebhookRateLimiter_CheckDoesNotConsumeBudget(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1, Window: time.Minute})
	ctx := context.Background()
	if !slidingWindowLimiterCheck(ctx, l, "shared-ip") || !slidingWindowLimiterCheck(ctx, l, "shared-ip") {
		t.Fatal("non-consuming checks should remain allowed before bad debt is charged")
	}
	if !l.Allow(ctx, "shared-ip") {
		t.Fatal("check unexpectedly consumed the only budget slot")
	}
	if slidingWindowLimiterCheck(ctx, l, "shared-ip") {
		t.Fatal("check should reject after bad debt reaches the limit")
	}
	if retry := slidingWindowLimiterRetryAfter(ctx, l, "shared-ip"); retry <= 0 || retry > time.Minute {
		t.Fatalf("unexpected retry interval: %v", retry)
	}
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

func TestRedisSlidingWindowRateLimiter_SeparatesInvitationNamespaces(t *testing.T) {
	rdb := newRedisTestClient(t)
	limits := InvitationRateLimits{
		Actor:     SlidingWindowRateLimit{Limit: 1, Window: time.Minute},
		Workspace: SlidingWindowRateLimit{Limit: 1, Window: time.Minute},
		Recipient: SlidingWindowRateLimit{Limit: 1, Window: time.Minute},
	}
	limiters := NewRedisInvitationRateLimiters(rdb, limits)
	ctx := context.Background()

	if !limiters.Actor.Allow(ctx, "same-key") || limiters.Actor.Allow(ctx, "same-key") {
		t.Fatal("actor limiter did not enforce its independent one-request budget")
	}
	if !limiters.Workspace.Allow(ctx, "same-key") {
		t.Fatal("workspace limiter shared the actor Redis namespace")
	}
	if !limiters.Recipient.Allow(ctx, "same-key") {
		t.Fatal("recipient limiter shared another invitation Redis namespace")
	}
}

func TestRedisSlidingWindowRateLimiter_PreservesWebhookFailOpenAndExposesErrors(t *testing.T) {
	rdb := newRedisTestClient(t)
	limiter := NewRedisWebhookRateLimiter(rdb, WebhookRateLimit{Limit: 1, Window: time.Minute})
	if err := rdb.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}

	ctx := context.Background()
	if !limiter.Allow(ctx, "webhook") {
		t.Fatal("legacy webhook Allow must fail open when Redis is unavailable")
	}
	if !slidingWindowLimiterCheck(ctx, limiter, "webhook") {
		t.Fatal("legacy webhook check must fail open when Redis is unavailable")
	}
	allowed, err := slidingWindowLimiterAllow(ctx, limiter, "invitation")
	if err == nil {
		t.Fatal("explicit sliding-window admission must expose the Redis error")
	}
	if allowed {
		t.Fatal("explicit sliding-window admission must not report allowed on Redis error")
	}
	allowed, err = slidingWindowLimiterCheckWithError(ctx, limiter, "invitation")
	if err == nil {
		t.Fatal("explicit sliding-window check must expose the Redis error")
	}
	if allowed {
		t.Fatal("explicit sliding-window check must not report allowed on Redis error")
	}
}
