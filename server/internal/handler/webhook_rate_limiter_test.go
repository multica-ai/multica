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

func TestMemoryWebhookRateLimiter_ZeroLimitDisabled(t *testing.T) {
	l := NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 0, Window: time.Minute})
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if !l.Allow(ctx, "tok") {
			t.Fatalf("Limit=0 should be unbounded, rejected at %d", i)
		}
	}
}
