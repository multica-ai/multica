package tagaccess

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryHTTPAssertionReplayStoreAtomicallyConsumesExactTuple(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	clock := &mutableAssertionClock{now: now}
	store, err := NewMemoryHTTPAssertionReplayStore(clock)
	if err != nil {
		t.Fatal(err)
	}
	claim := HTTPAssertionReplay{
		Issuer: HTTPAssertionIssuer, Audience: HTTPAssertionAudience,
		RequestID: "request-1", Nonce: "nonce-1", ExpiresAt: now.Add(5 * time.Second),
	}
	var consumed atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, consumeErr := store.Consume(context.Background(), claim)
			if consumeErr != nil {
				t.Errorf("Consume: %v", consumeErr)
				return
			}
			if ok {
				consumed.Add(1)
			}
		}()
	}
	wg.Wait()
	if consumed.Load() != 1 {
		t.Fatalf("successful consumes = %d, want 1", consumed.Load())
	}

	differentNonce := claim
	differentNonce.Nonce = "nonce-2"
	if ok, err := store.Consume(context.Background(), differentNonce); err != nil || !ok {
		t.Fatalf("different exact tuple consume = %v, %v", ok, err)
	}
	clock.now = claim.ExpiresAt
	reused := claim
	reused.ExpiresAt = clock.now.Add(5 * time.Second)
	if ok, err := store.Consume(context.Background(), reused); err != nil || !ok {
		t.Fatalf("expired tuple reuse = %v, %v", ok, err)
	}
}

type mutableAssertionClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableAssertionClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
