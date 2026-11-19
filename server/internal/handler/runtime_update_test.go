package handler

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// updateStoreHarness runs the same lifecycle checks against every UpdateStore.
type updateStoreHarness struct {
	name  string
	store UpdateStore
	// edit rewrites a stored request in place, standing in for the passage of
	// time the store measures against.
	edit func(t *testing.T, id string, fn func(*UpdateRequest))
}

func updateStoreHarnesses(t *testing.T) []updateStoreHarness {
	t.Helper()
	mem := NewInMemoryUpdateStore()
	harnesses := []updateStoreHarness{{
		name:  "memory",
		store: mem,
		edit: func(t *testing.T, id string, fn func(*UpdateRequest)) {
			mem.mu.Lock()
			defer mem.mu.Unlock()
			req, ok := mem.requests[id]
			if !ok {
				t.Fatalf("edit: request %s not stored", id)
			}
			fn(req)
		},
	}}
	if os.Getenv("REDIS_TEST_URL") == "" {
		return harnesses
	}
	rs := NewRedisUpdateStore(newRedisTestClient(t))
	harnesses = append(harnesses, updateStoreHarness{
		name:  "redis",
		store: rs,
		edit: func(t *testing.T, id string, fn func(*UpdateRequest)) {
			ctx := context.Background()
			raw, err := rs.rdb.Get(ctx, updateKey(id)).Bytes()
			if err != nil {
				t.Fatalf("edit: load %s: %v", id, err)
			}
			req, err := rs.unmarshalRequest(raw)
			if err != nil {
				t.Fatalf("edit: decode %s: %v", id, err)
			}
			fn(req)
			data, err := rs.marshalRequest(req)
			if err != nil {
				t.Fatalf("edit: encode %s: %v", id, err)
			}
			if err := rs.rdb.Set(ctx, updateKey(id), data, updateRequestTTL(req.Status)).Err(); err != nil {
				t.Fatalf("edit: store %s: %v", id, err)
			}
		},
	})
	return harnesses
}

func withUpdateReofferInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := updateReofferInterval
	updateReofferInterval = d
	t.Cleanup(func() { updateReofferInterval = prev })
}

func mustHasPending(t *testing.T, store UpdateStore, runtimeID string, want bool) {
	t.Helper()
	got, err := store.HasPending(context.Background(), runtimeID)
	if err != nil {
		t.Fatalf("has pending: %v", err)
	}
	if got != want {
		t.Fatalf("has pending = %v, want %v", got, want)
	}
}

func mustCreateUpdate(t *testing.T, store UpdateStore, runtimeID string) *UpdateRequest {
	t.Helper()
	req, err := store.Create(context.Background(), runtimeID, "v1.2.3", "user-1", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return req
}

func mustGetUpdate(t *testing.T, store UpdateStore, id string) *UpdateRequest {
	t.Helper()
	req, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if req == nil {
		t.Fatalf("get: request %s not found", id)
	}
	return req
}

func TestUpdateStore_OfferedAgainUntilTheDaemonStartsIt(t *testing.T) {
	withUpdateReofferInterval(t, 40*time.Millisecond)
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			mustHasPending(t, h.store, "rt-offer", false)
			req := mustCreateUpdate(t, h.store, "rt-offer")
			mustHasPending(t, h.store, "rt-offer", true)
			mustHasPending(t, h.store, "rt-other", false)

			popped, err := h.store.PopPending(ctx, "rt-offer", true)
			if err != nil {
				t.Fatalf("pop: %v", err)
			}
			if popped == nil || popped.ID != req.ID || popped.Status != UpdatePending {
				t.Fatalf("popped %+v, want the pending request unchanged", popped)
			}
			// Delivered: not offered again until the re-offer interval passes.
			mustHasPending(t, h.store, "rt-offer", false)
			time.Sleep(60 * time.Millisecond)
			mustHasPending(t, h.store, "rt-offer", true)
			again, err := h.store.PopPending(ctx, "rt-offer", true)
			if err != nil || again == nil || again.ID != req.ID {
				t.Fatalf("re-offer = %+v, %v; want the same request", again, err)
			}

			running, err := h.store.MarkRunning(ctx, req.ID)
			if err != nil {
				t.Fatalf("mark running: %v", err)
			}
			if running.Status != UpdateRunning || running.RunStartedAt == nil {
				t.Fatalf("running = %+v", running)
			}
			// Installing. A deferrable daemon may have missed the answer to its
			// "running" report, so it is offered the request again; a
			// fail-fast daemon (which never waits for that answer) is not.
			time.Sleep(60 * time.Millisecond)
			if got, err := h.store.PopPending(ctx, "rt-offer", false); err != nil || got != nil {
				t.Fatalf("offer of a running update to a fail-fast daemon = %+v, %v; want none", got, err)
			}
			time.Sleep(60 * time.Millisecond)
			reoffered, err := h.store.PopPending(ctx, "rt-offer", true)
			if err != nil || reoffered == nil || reoffered.Status != UpdateRunning {
				t.Fatalf("re-offer of a running update = %+v, %v; want it", reoffered, err)
			}
			// The repeated "running" report is accepted and restarts the clock.
			again2, err := h.store.MarkRunning(ctx, req.ID)
			if err != nil {
				t.Fatalf("repeated mark running: %v", err)
			}
			if !again2.RunStartedAt.After(*running.RunStartedAt) {
				t.Fatalf("run start %s not refreshed past %s", again2.RunStartedAt, running.RunStartedAt)
			}
			if err := h.store.Complete(ctx, req.ID, "done"); err != nil {
				t.Fatalf("complete: %v", err)
			}
			time.Sleep(60 * time.Millisecond)
			mustHasPending(t, h.store, "rt-offer", false)
		})
	}
}

func TestUpdateStore_FailFastOfferBlocksCancelUntilTheDaemonAnswers(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-lease")
			if got, err := h.store.PopPending(ctx, "rt-lease", false); err != nil || got == nil {
				t.Fatalf("pop = %+v, %v", got, err)
			}
			if _, err := h.store.Cancel(ctx, req.ID); !errors.Is(err, errUpdateBeingDelivered) {
				t.Fatalf("cancel while the offer is out = %v, want errUpdateBeingDelivered", err)
			}
			// Even a queue past its 24 hours does not expire under that daemon.
			h.edit(t, req.ID, func(r *UpdateRequest) {
				aged := time.Now().Add(-(updateScheduledTimeout + time.Second))
				r.Status = UpdateScheduled
				r.ScheduledAt = &aged
			})
			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateScheduled {
				t.Fatalf("status = %s; must not expire while a daemon may be starting it", got.Status)
			}
			// The busy answer releases the lease.
			if _, err := h.store.Schedule(ctx, req.ID, nil); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			h.edit(t, req.ID, func(r *UpdateRequest) {
				recent := time.Now()
				r.ScheduledAt = &recent
			})
			if got, err := h.store.Cancel(ctx, req.ID); err != nil || got.Status != UpdateCancelled {
				t.Fatalf("cancel after the busy answer = %+v, %v", got, err)
			}
		})
	}
}

func TestUpdateStore_LeaseRunsOut(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-lease-out")
			if got, err := h.store.PopPending(ctx, "rt-lease-out", false); err != nil || got == nil {
				t.Fatalf("pop = %+v, %v", got, err)
			}
			h.edit(t, req.ID, func(r *UpdateRequest) {
				past := time.Now().Add(-time.Second)
				r.OfferLeaseUntil = &past
			})
			if _, err := h.store.Cancel(ctx, req.ID); err != nil {
				t.Fatalf("cancel after an unanswered offer's lease ran out: %v", err)
			}
		})
	}
}

func TestUpdateStore_ScheduledUpdateStaysQueuedUntilCancelled(t *testing.T) {
	withUpdateReofferInterval(t, 40*time.Millisecond)
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-queue")
			if _, err := h.store.PopPending(ctx, "rt-queue", true); err != nil {
				t.Fatalf("pop: %v", err)
			}
			two := 2
			scheduled, err := h.store.Schedule(ctx, req.ID, &two)
			if err != nil {
				t.Fatalf("schedule: %v", err)
			}
			if scheduled.Status != UpdateScheduled || scheduled.ScheduledAt == nil {
				t.Fatalf("scheduled = %+v", scheduled)
			}
			if scheduled.ActiveTasks == nil || *scheduled.ActiveTasks != 2 {
				t.Fatalf("active tasks = %v, want 2", scheduled.ActiveTasks)
			}
			firstScheduledAt := *scheduled.ScheduledAt

			// Still offered so an idle daemon (or one that restarted) picks it up.
			time.Sleep(60 * time.Millisecond)
			mustHasPending(t, h.store, "rt-queue", true)

			// Re-queueing keeps when the wait started.
			if again, err := h.store.Schedule(ctx, req.ID, nil); err != nil || !again.ScheduledAt.Equal(firstScheduledAt) {
				t.Fatalf("reschedule = %+v, %v; want scheduled_at kept", again, err)
			}

			active, err := h.store.GetActive(ctx, "rt-queue")
			if err != nil || active == nil || active.ID != req.ID {
				t.Fatalf("active = %+v, %v; want the queued request", active, err)
			}
			if _, err := h.store.Create(ctx, "rt-queue", "v1.2.4", "user-1", true); !errors.Is(err, errUpdateInProgress) {
				t.Fatalf("second create = %v, want errUpdateInProgress", err)
			}

			cancelled, err := h.store.Cancel(ctx, req.ID)
			if err != nil {
				t.Fatalf("cancel: %v", err)
			}
			if cancelled.Status != UpdateCancelled {
				t.Fatalf("cancelled = %+v", cancelled)
			}
			if _, err := h.store.MarkRunning(ctx, req.ID); !errors.Is(err, errUpdateNotRunnable) {
				t.Fatalf("start after cancel = %v, want errUpdateNotRunnable", err)
			}
			if _, err := h.store.Schedule(ctx, req.ID, nil); !errors.Is(err, errUpdateNotRunnable) {
				t.Fatalf("schedule after cancel = %v, want errUpdateNotRunnable", err)
			}
			if active, err := h.store.GetActive(ctx, "rt-queue"); err != nil || active != nil {
				t.Fatalf("active after cancel = %+v, %v; want none", active, err)
			}
			time.Sleep(60 * time.Millisecond)
			mustHasPending(t, h.store, "rt-queue", false)
			mustCreateUpdate(t, h.store, "rt-queue")
		})
	}
}

func TestUpdateStore_CancelRejectedOnceInstalling(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-cancel-late")
			if _, err := h.store.MarkRunning(ctx, req.ID); err != nil {
				t.Fatalf("mark running: %v", err)
			}
			if _, err := h.store.Cancel(ctx, req.ID); !errors.Is(err, errUpdateNotCancellable) {
				t.Fatalf("cancel while running = %v, want errUpdateNotCancellable", err)
			}
			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateRunning {
				t.Fatalf("status = %s, want running", got.Status)
			}
		})
	}
}

func TestUpdateStore_ScheduledUpdateExpiresAfter24Hours(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-expire")
			if _, err := h.store.Schedule(ctx, req.ID, nil); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			h.edit(t, req.ID, func(r *UpdateRequest) {
				aged := time.Now().Add(-(updateScheduledTimeout + time.Second))
				r.ScheduledAt = &aged
			})

			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateExpired || got.Error == "" {
				t.Fatalf("expired = %+v", got)
			}
			if _, err := h.store.MarkRunning(ctx, req.ID); !errors.Is(err, errUpdateNotRunnable) {
				t.Fatalf("start after expiry = %v, want errUpdateNotRunnable", err)
			}
			if active, err := h.store.GetActive(ctx, "rt-expire"); err != nil || active != nil {
				t.Fatalf("active after expiry = %+v, %v; want none", active, err)
			}
			mustCreateUpdate(t, h.store, "rt-expire")
		})
	}
}

func TestUpdateStore_RunningRequestTimesOut(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-timeout")
			if _, err := h.store.MarkRunning(ctx, req.ID); err != nil {
				t.Fatalf("mark running: %v", err)
			}
			h.edit(t, req.ID, func(r *UpdateRequest) {
				aged := time.Now().Add(-(updateRunningTimeout + time.Second))
				r.RunStartedAt = &aged
			})

			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateTimeout || got.Error == "" {
				t.Fatalf("timed out = %+v", got)
			}
			mustCreateUpdate(t, h.store, "rt-timeout")
		})
	}
}

func TestUpdateStore_FinishedRequestKeepsItsOutcome(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-final")
			if err := h.store.Complete(ctx, req.ID, "Updated to v1.2.3"); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if err := h.store.Fail(ctx, req.ID, "late failure"); err != nil {
				t.Fatalf("late fail: %v", err)
			}
			got := mustGetUpdate(t, h.store, req.ID)
			if got.Status != UpdateCompleted || got.Output != "Updated to v1.2.3" || got.Error != "" {
				t.Fatalf("final = %+v, want the completed outcome", got)
			}
		})
	}
}

func TestUpdateStore_PopPendingIgnoresFinishedHistory(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			first := mustCreateUpdate(t, h.store, "rt-history")
			if err := h.store.Fail(ctx, first.ID, "allow next request"); err != nil {
				t.Fatalf("fail first: %v", err)
			}
			second := mustCreateUpdate(t, h.store, "rt-history")

			got, err := h.store.PopPending(ctx, "rt-history", true)
			if err != nil {
				t.Fatalf("pop: %v", err)
			}
			if got == nil || got.ID != second.ID {
				t.Fatalf("popped %+v, want the second request", got)
			}
		})
	}
}

func TestInMemoryUpdateStore_KeepsQueuedRequestsPastRetention(t *testing.T) {
	store := NewInMemoryUpdateStore()
	ctx := context.Background()
	req := mustCreateUpdate(t, store, "rt-old-queue")
	if _, err := store.Schedule(ctx, req.ID, nil); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	store.mu.Lock()
	store.requests[req.ID].CreatedAt = time.Now().Add(-2 * updateStoreRetention)
	store.mu.Unlock()

	// Create sweeps old requests; a queued one must survive the sweep.
	if _, err := store.Create(ctx, "rt-unrelated", "v1.2.3", "user-1", true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateScheduled {
		t.Fatalf("status = %s, want scheduled", got.Status)
	}
}
