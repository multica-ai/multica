package handler

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRedisUpdateStore_EnvelopePersistsRunStartedAt(t *testing.T) {
	store := &RedisUpdateStore{}
	now := time.Now().UTC().Truncate(time.Microsecond)
	req := &UpdateRequest{
		ID:            "upd-1",
		RuntimeID:     "rt-1",
		Status:        UpdateRunning,
		TargetVersion: "v1.2.3",
		CreatedAt:     now.Add(-time.Second),
		UpdatedAt:     now,
		RunStartedAt:  &now,
	}

	data, err := store.marshalRequest(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := store.unmarshalRequest(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RunStartedAt == nil {
		t.Fatal("RunStartedAt lost on round trip")
	}
	if !got.RunStartedAt.Equal(now) {
		t.Fatalf("RunStartedAt = %s, want %s", got.RunStartedAt, now)
	}
	if got.TargetVersion != "v1.2.3" {
		t.Fatalf("target version lost: %+v", got)
	}
}

func TestRedisUpdateStore_CreateGetComplete(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-1", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if req.Status != UpdatePending {
		t.Fatalf("initial status = %s", req.Status)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != req.ID || got.TargetVersion != "v1.2.3" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	if err := store.Complete(ctx, req.ID, "updated"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, err = store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get after complete: %v", err)
	}
	if got.Status != UpdateCompleted || got.Output != "updated" {
		t.Fatalf("completed request mismatch: %+v", got)
	}

	if _, err := store.Create(ctx, "runtime-1", "v1.2.4"); err != nil {
		t.Fatalf("create after complete should be allowed: %v", err)
	}
}

func TestRedisUpdateStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross", "v1.2.3")
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B pop: %v", err)
	}
	if popped == nil {
		t.Fatal("node B did not see node A's pending update")
	}
	if popped.ID != req.ID || popped.Status != UpdateRunning {
		t.Fatalf("popped mismatch: %+v", popped)
	}
	if popped.RunStartedAt == nil {
		t.Fatal("RunStartedAt not set after pop")
	}

	again, err := nodeB.PopPending(ctx, "runtime-cross")
	if err != nil {
		t.Fatalf("node B second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no more pending, got %+v", again)
	}
}

func TestRedisUpdateStore_ReportAndPollAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)
	nodeC := NewRedisUpdateStore(rdb)
	nodeD := NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-report", "v1.2.3")
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}
	if _, err := nodeB.PopPending(ctx, "runtime-report"); err != nil {
		t.Fatalf("node B pop: %v", err)
	}
	if err := nodeC.Complete(ctx, req.ID, "updated to v1.2.3"); err != nil {
		t.Fatalf("node C complete: %v", err)
	}

	got, err := nodeD.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("node D get: %v", err)
	}
	if got == nil || got.Status != UpdateCompleted || got.Output != "updated to v1.2.3" {
		t.Fatalf("node D terminal state mismatch: %+v", got)
	}
}

func TestRedisUpdateStore_FailAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)
	nodeC := NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-fail", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := nodeB.PopPending(ctx, "runtime-fail"); err != nil {
		t.Fatalf("pop: %v", err)
	}
	if err := nodeC.Fail(ctx, req.ID, "download failed"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got, err := nodeA.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != UpdateFailed || got.Error != "download failed" {
		t.Fatalf("failed request mismatch: %+v", got)
	}
}

func TestRedisUpdateStore_RejectsConcurrentActive(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-active", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, "runtime-active", "v1.2.4"); err != errUpdateInProgress {
		t.Fatalf("second create error = %v, want errUpdateInProgress", err)
	}
	if err := store.Complete(ctx, req.ID, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := store.Create(ctx, "runtime-active", "v1.2.4"); err != nil {
		t.Fatalf("create after terminal should succeed: %v", err)
	}
}

func TestRedisUpdateStore_RunningTimeoutClearsActive(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-timeout", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	popped, err := store.PopPending(ctx, "runtime-timeout")
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if popped == nil || popped.Status != UpdateRunning {
		t.Fatalf("expected running, got %+v", popped)
	}

	aged := time.Now().Add(-(updateRunningTimeout + time.Second))
	popped.RunStartedAt = &aged
	if err := store.persistRequest(ctx, popped); err != nil {
		t.Fatalf("persist aged request: %v", err)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != UpdateTimeout {
		t.Fatalf("status = %s, want timeout", got.Status)
	}
	if _, err := store.Create(ctx, "runtime-timeout", "v1.2.4"); err != nil {
		t.Fatalf("create after timeout should succeed: %v", err)
	}
}

func TestRedisUpdateStore_PopPendingConcurrent(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-race", "v1.2.3")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	results := make(chan *UpdateRequest, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			popped, err := store.PopPending(ctx, "runtime-race")
			if err != nil {
				errs <- err
				return
			}
			results <- popped
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent pop error: %v", err)
	}

	winners := 0
	for popped := range results {
		if popped != nil {
			winners++
			if popped.ID != req.ID {
				t.Fatalf("winner popped wrong id: %s", popped.ID)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
}

var (
	_ UpdateStore = (*InMemoryUpdateStore)(nil)
	_ UpdateStore = (*RedisUpdateStore)(nil)
)
