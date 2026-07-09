package handler

import (
	"context"
	"testing"
	"time"
)

func TestNoopLivenessStore_AlwaysUnavailable(t *testing.T) {
	s := NewNoopLivenessStore()
	if s.Available() {
		t.Fatal("noop store reported Available()=true")
	}
	if err := s.Touch(context.Background(), "rt-1", time.Second); err != nil {
		t.Fatalf("noop Touch returned error: %v", err)
	}
	alive, ok := s.IsAliveBatch(context.Background(), []string{"rt-1"})
	if ok {
		t.Fatalf("noop IsAliveBatch returned ok=true with alive=%v", alive)
	}
	// Forget on the noop must not panic.
	s.Forget(context.Background(), "rt-1")
}

func TestRedisLivenessStore_TouchAndIsAlive(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	s := NewRedisLivenessStore(rdb)

	if !s.Available() {
		t.Fatal("redis store reported Available()=false with a live client")
	}

	if err := s.Touch(ctx, "rt-1", 10*time.Second); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	alive, ok := s.IsAliveBatch(ctx, []string{"rt-1", "rt-missing"})
	if !ok {
		t.Fatal("IsAliveBatch returned ok=false against a healthy Redis")
	}
	if !alive["rt-1"] {
		t.Fatal("rt-1 was just touched but IsAliveBatch reported dead")
	}
	if alive["rt-missing"] {
		t.Fatal("rt-missing was never touched but IsAliveBatch reported alive")
	}
}

func TestRedisLivenessStore_TTLExpiry(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	s := NewRedisLivenessStore(rdb)

	// Use a real (small) TTL — go-redis SET supports milliseconds via the
	// time.Duration parameter, but most production Redis builds round
	// sub-second TTLs to one second. Use 1 second + sleep slightly longer.
	if err := s.Touch(ctx, "rt-expire", 1*time.Second); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	alive, ok := s.IsAliveBatch(ctx, []string{"rt-expire"})
	if !ok || !alive["rt-expire"] {
		t.Fatalf("expected fresh touch to be alive, got ok=%v alive=%+v", ok, alive)
	}

	time.Sleep(1500 * time.Millisecond)

	alive, ok = s.IsAliveBatch(ctx, []string{"rt-expire"})
	if !ok {
		t.Fatal("IsAliveBatch returned ok=false against a healthy Redis")
	}
	if alive["rt-expire"] {
		t.Fatal("expected key to expire after TTL but IsAliveBatch reported alive")
	}
}

func TestRedisLivenessStore_Forget(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	s := NewRedisLivenessStore(rdb)

	if err := s.Touch(ctx, "rt-forget", 10*time.Second); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	s.Forget(ctx, "rt-forget")

	alive, ok := s.IsAliveBatch(ctx, []string{"rt-forget"})
	if !ok {
		t.Fatal("IsAliveBatch returned ok=false against a healthy Redis")
	}
	if alive["rt-forget"] {
		t.Fatal("Forget did not drop the liveness record")
	}
}

func TestRedisLivenessStore_BatchEmptyInput(t *testing.T) {
	rdb := newRedisTestClient(t)
	s := NewRedisLivenessStore(rdb)

	alive, ok := s.IsAliveBatch(context.Background(), nil)
	if !ok {
		t.Fatal("expected ok=true on empty input against a healthy Redis")
	}
	if len(alive) != 0 {
		t.Fatalf("expected empty result map, got %+v", alive)
	}
}
