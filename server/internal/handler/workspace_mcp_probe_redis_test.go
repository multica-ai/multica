package handler

import (
	"context"
	"sync"
	"testing"
)

func TestRedisMcpProbeStore_ClaimsOnceAcrossNodes(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	nodeA := NewRedisMcpProbeStore(rdb)
	nodeB := NewRedisMcpProbeStore(rdb)
	created, err := nodeA.Create(ctx, McpProbeCreate{WorkspaceID: "ws", ServerID: "srv", RuntimeID: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	claims := make(chan *McpProbeRequest, 2)
	var wg sync.WaitGroup
	for _, store := range []*RedisMcpProbeStore{nodeA, nodeB} {
		wg.Go(func() {
			req, err := store.PopPending(ctx, "rt")
			if err != nil {
				t.Error(err)
			}
			claims <- req
		})
	}
	wg.Wait()
	close(claims)
	count := 0
	for req := range claims {
		if req != nil {
			count++
			if req.ID != created.ID || req.Status != McpProbeRunning {
				t.Fatalf("claim=%+v", req)
			}
		}
	}
	if count != 1 {
		t.Fatalf("claims=%d, want 1", count)
	}
	if err := nodeB.Complete(ctx, created.ID, []string{"search"}, 42); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.Fail(ctx, created.ID, "handshake", "secret", 999); err != nil {
		t.Fatal(err)
	}
	result, err := nodeA.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != McpProbeCompleted || result.ElapsedMs != 42 {
		t.Fatalf("late report overwrote result: %+v", result)
	}
}

func TestRedisMcpProbeStore_TimeoutRemovesPendingJob(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisMcpProbeStore(rdb)
	created, err := store.Create(ctx, McpProbeCreate{RuntimeID: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	created.CreatedAt = created.CreatedAt.Add(-mcpProbePendingTimeout - 1)
	raw, err := store.marshalRequest(created)
	if err != nil {
		t.Fatal(err)
	}
	if err := rdb.Set(ctx, mcpProbeKey(created.ID), raw, mcpProbeStoreRetention).Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, created.ID, []string{"late"}, 1); err != nil {
		t.Fatal(err)
	}
	result, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != McpProbeTimeout {
		t.Fatalf("late completion replaced timeout: %+v", result)
	}
	pending, err := store.HasPending(ctx, "rt")
	if err != nil || pending {
		t.Fatalf("pending=%v error=%v", pending, err)
	}
}

func TestRedisMcpProbeStore_WithoutMultiPermission(t *testing.T) {
	rdb := newRedisTestClientWithoutMulti(t)
	ctx := context.Background()
	store := NewRedisMcpProbeStore(rdb)
	created, err := store.Create(ctx, McpProbeCreate{RuntimeID: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PopPending(ctx, "rt"); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, created.ID, []string{"search"}, 42); err != nil {
		t.Fatal(err)
	}
	result, err := store.Get(ctx, created.ID)
	if err != nil || result.Status != McpProbeCompleted {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
