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
		ID:              "upd-1",
		RuntimeID:       "rt-1",
		InitiatorUserID: "user-1",
		Status:          UpdateRunning,
		TargetVersion:   "v1.2.3",
		CreatedAt:       now.Add(-time.Second),
		UpdatedAt:       now,
		RunStartedAt:    &now,
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
	if got.InitiatorUserID != "user-1" {
		t.Fatalf("initiator user ID lost: %+v", got)
	}
}

func TestRedisUpdateStore_CreateGetComplete(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-1", "runtime:"+"runtime-1", "v1.2.3", "user-1", true)
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

	if _, err := store.Create(ctx, "runtime-1", "runtime:"+"runtime-1", "v1.2.4", "user-1", true); err != nil {
		t.Fatalf("create after complete should be allowed: %v", err)
	}
}

func TestRedisUpdateStore_CreateWithoutMultiPermission(t *testing.T) {
	rdb := newRedisTestClientWithoutMulti(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	req, err := store.Create(ctx, "runtime-no-multi", "runtime:"+"runtime-no-multi", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create without MULTI permission: %v", err)
	}
	if req.Status != UpdatePending {
		t.Fatalf("initial status = %s, want %s", req.Status, UpdatePending)
	}

	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("get created request: %v", err)
	}
	if got == nil || got.ID != req.ID {
		t.Fatalf("created request was not persisted: %+v", got)
	}
	pending, err := store.HasPending(ctx, "runtime-no-multi")
	if err != nil {
		t.Fatalf("check pending request: %v", err)
	}
	if !pending {
		t.Fatal("created request was not queued")
	}
}

func TestRedisUpdateStore_PopPendingAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross", "runtime:"+"runtime-cross", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}

	popped, err := nodeB.PopPending(ctx, "runtime-cross", true)
	if err != nil {
		t.Fatalf("node B pop: %v", err)
	}
	if popped == nil {
		t.Fatal("node B did not see node A's pending update")
	}
	// Delivery does not start the update; the daemon's "running" report does.
	if popped.ID != req.ID || popped.Status != UpdatePending {
		t.Fatalf("popped mismatch: %+v", popped)
	}

	// Node A must not deliver it again before the re-offer interval.
	again, err := nodeA.PopPending(ctx, "runtime-cross", true)
	if err != nil {
		t.Fatalf("node A second pop: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no due request, got %+v", again)
	}
}

func TestRedisUpdateStore_ReportAndPollAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	nodeA := NewRedisUpdateStore(rdb)
	nodeB := NewRedisUpdateStore(rdb)
	nodeC := NewRedisUpdateStore(rdb)
	nodeD := NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-report", "runtime:"+"runtime-report", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}
	if _, err := nodeB.PopPending(ctx, "runtime-report", true); err != nil {
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

	req, err := nodeA.Create(ctx, "runtime-fail", "runtime:"+"runtime-fail", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := nodeB.PopPending(ctx, "runtime-fail", true); err != nil {
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

	req, err := store.Create(ctx, "runtime-active", "runtime:"+"runtime-active", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(ctx, "runtime-active", "runtime:"+"runtime-active", "v1.2.4", "user-1", true); err != errUpdateInProgress {
		t.Fatalf("second create error = %v, want errUpdateInProgress", err)
	}
	if err := store.Complete(ctx, req.ID, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := store.Create(ctx, "runtime-active", "runtime:"+"runtime-active", "v1.2.4", "user-1", true); err != nil {
		t.Fatalf("create after terminal should succeed: %v", err)
	}
}

func TestRedisUpdateStore_PopPendingConcurrent(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-race", "runtime:"+"runtime-race", "v1.2.3", "user-1", true)
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
			popped, err := store.PopPending(ctx, "runtime-race", true)
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

func TestRedisUpdateStore_ReadsEnvelopeFromPreviousRelease(t *testing.T) {
	// The previous release kept the run start beside the payload only; a
	// request it wrote must keep it after the rollout.
	raw := []byte(`{"r":{"id":"upd-old","runtime_id":"rt-1","status":"running","target_version":"v1.2.3","created_at":"2026-11-19T10:00:00Z","updated_at":"2026-11-19T10:00:01Z"},"s":"2026-11-19T10:00:01Z","u":"user-1"}`)
	got, err := (&RedisUpdateStore{}).unmarshalRequest(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.InitiatorUserID != "user-1" || got.RunStartedAt == nil {
		t.Fatalf("legacy envelope lost fields: %+v", got)
	}
}

func TestRedisUpdateStore_StaleActiveSlotDoesNotBlockCreate(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-stale", "runtime:"+"runtime-stale", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Complete(ctx, req.ID, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// A cleanup write that failed leaves the slot pointing at a finished request.
	if err := rdb.Set(ctx, updateMachineSlotKey("runtime:"+"runtime-stale"), req.ID, time.Hour).Err(); err != nil {
		t.Fatalf("restore stale slot: %v", err)
	}

	if _, err := store.Create(ctx, "runtime-stale", "runtime:"+"runtime-stale", "v1.2.4", "user-1", true); err != nil {
		t.Fatalf("create behind a stale slot: %v", err)
	}
}

func TestRedisUpdateStore_ConcurrentCreatesKeepOneRequest(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	const n = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Create(ctx, "runtime-concurrent", "runtime:"+"runtime-concurrent", "v1.2.3", "user-1", true)
			switch {
			case err == nil:
				mu.Lock()
				created++
				mu.Unlock()
			case err != errUpdateInProgress:
				t.Errorf("create: %v", err)
			}
			// Readers polling the slot meanwhile must not clear it.
			_, _ = store.GetActive(ctx, "runtime:"+"runtime-concurrent")
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("created %d requests for one runtime, want 1", created)
	}
	queued, err := rdb.ZCard(ctx, updateOffersKey("runtime-concurrent")).Result()
	if err != nil || queued != 1 {
		t.Fatalf("offer entries = %d, %v; want 1", queued, err)
	}
}

func TestRedisUpdateStore_RestoresDroppedOfferEntry(t *testing.T) {
	withUpdateReofferInterval(t, 0)
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-rolling", "runtime:"+"runtime-rolling", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Schedule(ctx, req.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	// Lose the offer entry, as a failed index write would.
	if err := rdb.ZRem(ctx, updateOffersKey("runtime-rolling"), req.ID).Err(); err != nil {
		t.Fatalf("drop offer entry: %v", err)
	}
	mustHasPending(t, store, "runtime-rolling", false)

	// The next read of the request restores it.
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateScheduled {
		t.Fatalf("status = %s, want scheduled", got.Status)
	}
	mustHasPending(t, store, "runtime-rolling", true)
}

func TestRedisUpdateStore_ScheduledRequestOutlivesRetention(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)

	req, err := store.Create(ctx, "runtime-ttl", "runtime:"+"runtime-ttl", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Schedule(ctx, req.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	for _, key := range []string{updateKey(req.ID), updateMachineSlotKey("runtime:" + "runtime-ttl"), updateOffersKey("runtime-ttl")} {
		ttl, err := rdb.TTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("ttl %s: %v", key, err)
		}
		if ttl < updateScheduledTimeout {
			t.Fatalf("%s expires in %s; a queued request must last its whole %s wait", key, ttl, updateScheduledTimeout)
		}
	}
}

var (
	_ UpdateStore = (*InMemoryUpdateStore)(nil)
	_ UpdateStore = (*RedisUpdateStore)(nil)
)
