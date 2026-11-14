package handler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRuntimeQuotaStore(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		testRuntimeQuotaStore(t, &memoryRuntimeQuotaStore{})
	})
	t.Run("redis without MULTI", func(t *testing.T) {
		client := newRedisTestClientWithoutMulti(t)
		testRuntimeQuotaStore(t, &redisRuntimeQuotaStore{rdb: client})
		if ttl, err := client.TTL(context.Background(), runtimeQuotaKey("codex-a")).Result(); err != nil || ttl <= 0 || ttl > runtimeQuotaRetention {
			t.Fatalf("quota key TTL = %s, error = %v", ttl, err)
		}
	})
}

func testRuntimeQuotaStore(t *testing.T, store runtimeQuotaStore) {
	t.Helper()
	ctx := context.Background()
	const id = "codex-a"
	now := time.Now()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := store.update(ctx, id, func(current runtimeQuotaState) (runtimeQuotaState, bool) {
				if current.pending(now) {
					return current, false
				}
				return runtimeQuotaState{
					OwnerID: "owner", RequestID: fmt.Sprint(i), RequestedAt: now,
					Report: protocol.SubscriptionQuotaReport{Status: "pending"},
				}, true
			})
			if err != nil || !state.pending(now) {
				t.Errorf("update = %+v, error = %v", state, err)
			}
		}()
	}
	wg.Wait()
	states, err := store.read(ctx, []string{id, "missing"})
	state := states[id]
	if err != nil || !state.pending(now) || states["missing"].RequestID != "" {
		t.Fatalf("batch read = %+v, error = %v", states, err)
	}
	again, err := store.read(ctx, []string{id})
	if err != nil || again[id].RequestID != state.RequestID {
		t.Fatalf("read must leave the request available: %+v, error = %v", again, err)
	}
	if got := state.reportFor("other-owner", now); got.Status != "idle" {
		t.Fatalf("another owner saw request state: %+v", got)
	}
	if got := state.reportFor("owner", now.Add(protocol.SubscriptionQuotaRequestTTL)); got.Status != "timeout" {
		t.Fatalf("request did not time out: %+v", got)
	}
}
