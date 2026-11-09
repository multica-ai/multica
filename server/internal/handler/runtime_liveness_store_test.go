package handler

import (
	"context"
	"errors"
	"github.com/multica-ai/multica/server/internal/service"
	"testing"
	"time"
)

func TestRedisOwnerGateRejectsLateHeartbeatAndPendingClaim(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	h := &Handler{LivenessStore: NewRedisLivenessStore(rdb)}
	key := runtimeOwnerGateKey("workspace", "daemon", "codex", "")
	store := NewRedisUpdateStore(rdb)
	request, err := store.Create(ctx, "runtime-owner-test", "v1", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.advanceRuntimeOwner(ctx, key, fenceOwnerA); err != nil {
		t.Fatal(err)
	}
	if err := h.touchRuntimeOwner(ctx, "runtime-owner-test", key, fenceOwnerA); err != nil {
		t.Fatal(err)
	}
	if err := h.advanceRuntimeOwner(ctx, key, fenceOwnerB); err != nil {
		t.Fatal(err)
	}
	if err := h.touchRuntimeOwner(ctx, "runtime-owner-test", key, fenceOwnerA); !errors.Is(err, service.ErrStaleRuntimeOwner) {
		t.Fatalf("late touch: %v", err)
	}
	if _, err := store.PopPending(withPendingOwner(ctx, nil, key, fenceOwnerA), "runtime-owner-test"); !errors.Is(err, service.ErrStaleRuntimeOwner) {
		t.Fatalf("late claim: %v", err)
	}
	if current, err := store.Get(ctx, request.ID); err != nil || current.Status != UpdatePending {
		t.Fatalf("late claim changed request: %+v, %v", current, err)
	}
	claimed, err := store.PopPending(withPendingOwner(ctx, nil, key, fenceOwnerB), "runtime-owner-test")
	if err != nil || claimed == nil || claimed.ID != request.ID {
		t.Fatalf("new owner claim: %+v, %v", claimed, err)
	}
	alive, ok := h.LivenessStore.(*RedisLivenessStore).IsAliveOwnerBatch(ctx, map[string]string{"runtime-owner-test": fenceOwnerB})
	if !ok || alive["runtime-owner-test"] {
		t.Fatalf("old generation liveness survived takeover: %v, %v", alive, ok)
	}
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
