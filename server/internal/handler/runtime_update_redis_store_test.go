package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRedisUpdateStore_RecordKeepsHiddenFields(t *testing.T) {
	store := &RedisUpdateStore{}
	now := time.Now().UTC().Truncate(time.Microsecond)
	req := &UpdateRequest{
		ID:              "upd-1",
		RuntimeID:       "rt-1",
		InitiatorUserID: "user-1",
		MachineKey:      "daemon:owner:d1",
		QueueWhenBusy:   true,
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
	if got.RunStartedAt == nil || !got.RunStartedAt.Equal(now) {
		t.Fatalf("RunStartedAt = %v, want %s", got.RunStartedAt, now)
	}
	if got.InitiatorUserID != "user-1" || got.MachineKey != "daemon:owner:d1" || !got.QueueWhenBusy {
		t.Fatalf("hidden fields lost: %+v", got)
	}
}

func TestRedisUpdateStore_CreateWithoutMultiPermission(t *testing.T) {
	rdb := newRedisTestClientWithoutMulti(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	req, err := store.Create(ctx, "runtime-no-multi", "", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create without MULTI permission: %v", err)
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdatePending {
		t.Fatalf("created request = %+v", got)
	}
	mustHasPending(t, store, "runtime-no-multi", true)
	if _, err := store.Schedule(ctx, req.ID, nil); err != nil {
		t.Fatalf("schedule without MULTI permission: %v", err)
	}
}

func TestRedisUpdateStore_LifecycleAcrossInstances(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	nodeA, nodeB, nodeC, nodeD := NewRedisUpdateStore(rdb), NewRedisUpdateStore(rdb), NewRedisUpdateStore(rdb), NewRedisUpdateStore(rdb)

	req, err := nodeA.Create(ctx, "runtime-cross", "", "v1.2.3", "user-1", false)
	if err != nil {
		t.Fatalf("node A create: %v", err)
	}
	popped, err := nodeB.PopPending(ctx, "runtime-cross", false)
	if err != nil || popped == nil || popped.ID != req.ID || popped.Status != UpdateRunning {
		t.Fatalf("node B pop = %+v, %v", popped, err)
	}
	if again, err := nodeB.PopPending(ctx, "runtime-cross", false); err != nil || again != nil {
		t.Fatalf("second pop = %+v, %v; want none", again, err)
	}
	if err := nodeC.Fail(ctx, req.ID, "download failed"); err != nil {
		t.Fatalf("node C fail: %v", err)
	}
	if got := mustGetUpdate(t, nodeD, req.ID); got.Status != UpdateFailed || got.Error != "download failed" {
		t.Fatalf("node D sees %+v", got)
	}
}

func TestRedisUpdateStore_FailFastPopIsExclusive(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	req, err := store.Create(ctx, "runtime-race", "", "v1.2.3", "user-1", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			popped, err := NewRedisUpdateStore(rdb).PopPending(ctx, "runtime-race", false)
			if err != nil {
				t.Errorf("pop: %v", err)
				return
			}
			if popped != nil {
				if popped.ID != req.ID {
					t.Errorf("popped %s", popped.ID)
				}
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("a fail-fast request was delivered %d times, want once", winners)
	}
}

// The machine slot lives exactly as long as the unfinished request: every way
// a request ends frees it, and while it waits the slot does not lapse before
// the request could.
func TestRedisUpdateStore_MachineSlotFollowsTheRequest(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	machine := "daemon:owner:" + randomID()
	slot := updateMachineSlotKey(machine)

	ends := map[string]func(*UpdateRequest){
		"complete": func(r *UpdateRequest) { _ = store.Complete(ctx, r.ID, "ok") },
		"fail":     func(r *UpdateRequest) { _ = store.Fail(ctx, r.ID, "boom") },
		"cancel":   func(r *UpdateRequest) { _, _ = store.Cancel(ctx, r.ID) },
	}
	for name, end := range ends {
		req, err := store.Create(ctx, "rt-slot-a", machine, "v1.2.3", "user-1", true)
		if err != nil {
			t.Fatalf("%s: create: %v", name, err)
		}
		if _, err := store.Schedule(ctx, req.ID, nil); err != nil {
			t.Fatalf("%s: schedule: %v", name, err)
		}
		if ttl := rdb.PTTL(ctx, slot).Val(); ttl < updateScheduledTimeout {
			t.Fatalf("%s: slot TTL %s is shorter than a queued request can wait", name, ttl)
		}
		end(req)
		if n := rdb.Exists(ctx, slot).Val(); n != 0 {
			t.Fatalf("%s: slot still held after the request ended", name)
		}
		if n := rdb.ZCard(ctx, updateOffersKey("rt-slot-a")).Val(); n != 0 {
			t.Fatalf("%s: ended request still in the offer queue", name)
		}
		if ttl := rdb.PTTL(ctx, updateKey(req.ID)).Val(); ttl > updateStoreRetention {
			t.Fatalf("%s: finished record TTL %s, want at most the retention", name, ttl)
		}
	}
}

// A request nobody read after it should have timed out still holds the slot;
// the next Create reads it, ends it, and goes ahead.
func TestRedisUpdateStore_TimedOutRequestDoesNotBlockTheNextOne(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	machine := "daemon:owner:" + randomID()

	stale, err := store.Create(ctx, "rt-stale-a", machine, "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	editRedisUpdate(t, store, stale.ID, func(r *UpdateRequest) {
		r.CreatedAt = time.Now().Add(-(updatePendingTimeout + time.Second))
	})
	next, err := store.Create(ctx, "rt-stale-b", machine, "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create after the old request timed out: %v", err)
	}
	if got := mustGetUpdate(t, store, stale.ID); got.Status != UpdateTimeout {
		t.Fatalf("stale request = %s, want timeout", got.Status)
	}
	if active, err := store.GetActive(ctx, machine); err != nil || active == nil || active.ID != next.ID {
		t.Fatalf("active = %+v, %v; want the new request", active, err)
	}
}

// A write computed from a record that changed meanwhile must not land.
func TestRedisUpdateStore_StaleWriteIsRejected(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisUpdateStore(rdb)
	req, err := store.Create(ctx, "rt-cas", "", "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := rdb.Get(ctx, updateKey(req.ID)).Val()
	if _, err := store.MarkRunning(ctx, req.ID); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	stale := *req
	stale.Status = UpdateCancelled
	data, err := store.marshalRequest(&stale)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := updateCASScript.Run(ctx, rdb,
		[]string{updateKey(req.ID), updateMachineSlotKey(req.MachineKey), updateOffersKey(req.RuntimeID)},
		before, data, updateTTLMillis(updateStoreRetention), req.ID, "1", "keep",
	).Int64()
	if err != nil || ok != 0 {
		t.Fatalf("stale write = %d, %v; want it refused", ok, err)
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
	if _, err := store.Create(ctx, "rt-cas-2", req.MachineKey, "v1.2.3", "user-1", true); !errors.Is(err, errUpdateInProgress) {
		t.Fatalf("create while installing = %v; the refused write must not have freed the slot", err)
	}
}

func editRedisUpdate(t *testing.T, store *RedisUpdateStore, id string, fn func(*UpdateRequest)) {
	t.Helper()
	ctx := context.Background()
	raw, err := store.rdb.Get(ctx, updateKey(id)).Bytes()
	if err != nil {
		t.Fatalf("edit: load %s: %v", id, err)
	}
	req, err := store.unmarshalRequest(raw)
	if err != nil {
		t.Fatalf("edit: decode %s: %v", id, err)
	}
	fn(req)
	data, err := store.marshalRequest(req)
	if err != nil {
		t.Fatalf("edit: encode %s: %v", id, err)
	}
	if err := store.rdb.Set(ctx, updateKey(id), data, updateRetentionFor(req, time.Now())).Err(); err != nil {
		t.Fatalf("edit: store %s: %v", id, err)
	}
}

var (
	_ UpdateStore = (*InMemoryUpdateStore)(nil)
	_ UpdateStore = (*RedisUpdateStore)(nil)
)
