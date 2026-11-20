package handler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
			if err := rs.rdb.Set(ctx, updateKey(id), data, updateRetentionFor(req, time.Now())).Err(); err != nil {
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
	req, err := store.Create(context.Background(), runtimeID, "runtime:"+runtimeID, "v1.2.3", "user-1", true)
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

// A queued request offered to a daemon that cannot hold it (released before
// queued updates) goes out exactly once and is running from delivery: that
// daemon installs without waiting for the server's answer, so nothing may
// cancel it from then on.
func TestUpdateStore_QueuedRequestToAFailFastDaemonRunsFromDelivery(t *testing.T) {
	withUpdateReofferInterval(t, 0)
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-legacy")
			if !updateCancellable(req) {
				t.Fatal("a queued request is cancellable before any daemon has it")
			}
			popped, err := h.store.PopPending(ctx, "rt-legacy", false)
			if err != nil || popped == nil || popped.Status != UpdateRunning {
				t.Fatalf("pop = %+v, %v; want it running from delivery", popped, err)
			}
			for i := 0; i < 3; i++ {
				if got, err := h.store.PopPending(ctx, "rt-legacy", true); err != nil || got != nil {
					t.Fatalf("second delivery = %+v, %v; want none", got, err)
				}
			}
			if _, err := h.store.Cancel(ctx, req.ID); !errors.Is(err, errUpdateNotCancellable) {
				t.Fatalf("cancel after a fail-fast delivery = %v, want errUpdateNotCancellable", err)
			}
		})
	}
}

// Cancel and the daemon's "running" report race for the same queued request.
// Exactly one may win, and the stored outcome must match the winner: a
// cancelled update the daemon was told to install (or the reverse) is the bug
// this state machine exists to prevent.
func TestUpdateStore_CancelAndStartNeverBothWin(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			for i := 0; i < 50; i++ {
				rt := fmt.Sprintf("rt-race-%d-%s", i, randomID())
				req := mustCreateUpdate(t, h.store, rt)
				if _, err := h.store.Schedule(ctx, req.ID, nil); err != nil {
					t.Fatalf("schedule: %v", err)
				}
				var wg sync.WaitGroup
				var cancelErr, startErr error
				wg.Add(2)
				go func() { defer wg.Done(); _, cancelErr = h.store.Cancel(ctx, req.ID) }()
				go func() { defer wg.Done(); _, startErr = h.store.MarkRunning(ctx, req.ID) }()
				wg.Wait()
				if (cancelErr == nil) == (startErr == nil) {
					t.Fatalf("round %d: cancel=%v start=%v; exactly one must win", i, cancelErr, startErr)
				}
				got := mustGetUpdate(t, h.store, req.ID)
				if cancelErr == nil && got.Status != UpdateCancelled {
					t.Fatalf("round %d: cancel won but status = %s", i, got.Status)
				}
				if startErr == nil && got.Status != UpdateRunning {
					t.Fatalf("round %d: start won but status = %s", i, got.Status)
				}
			}
		})
	}
}

// Two runtimes of one machine (different workspaces) ask at the same time;
// one request may exist.
func TestUpdateStore_ConcurrentCreatesOnOneMachineKeepOne(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			machine := "daemon:owner:" + randomID()
			const n = 16
			var wg sync.WaitGroup
			var mu sync.Mutex
			created := 0
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, err := h.store.Create(ctx, fmt.Sprintf("rt-%s-%d", machine, i), machine, "v1.2.3", "user-1", true)
					switch {
					case err == nil:
						mu.Lock()
						created++
						mu.Unlock()
					case !errors.Is(err, errUpdateInProgress):
						t.Errorf("create %d: %v", i, err)
					}
				}(i)
			}
			wg.Wait()
			if created != 1 {
				t.Fatalf("created %d requests for one machine, want 1", created)
			}
		})
	}
}

func TestUpdateStore_OlderClientsRequestIsNeverCancelled(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			req, err := h.store.Create(context.Background(), "rt-old-client", "runtime:rt-old-client", "v1.2.3", "user-1", false)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if updateCancellable(req) {
				t.Fatal("an older client's request reported as cancellable")
			}
			if _, err := h.store.Cancel(context.Background(), req.ID); !errors.Is(err, errUpdateNotCancellable) {
				t.Fatalf("cancel = %v, want errUpdateNotCancellable; that client cannot show a cancelled update", err)
			}
		})
	}
}

func TestUpdateStore_OneUpdatePerMachine(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			if _, err := h.store.Create(ctx, "rt-machine-a", "daemon:machine", "v1.2.3", "user-1", true); err != nil {
				t.Fatalf("create through runtime A: %v", err)
			}
			if _, err := h.store.Create(ctx, "rt-machine-b", "daemon:machine", "v1.2.3", "user-2", true); !errors.Is(err, errUpdateInProgress) {
				t.Fatalf("create through runtime B = %v, want errUpdateInProgress", err)
			}
			active, err := h.store.GetActive(ctx, "daemon:machine")
			if err != nil || active == nil || active.RuntimeID != "rt-machine-a" {
				t.Fatalf("active = %+v, %v; want runtime A's request", active, err)
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

			active, err := h.store.GetActive(ctx, "runtime:"+"rt-queue")
			if err != nil || active == nil || active.ID != req.ID {
				t.Fatalf("active = %+v, %v; want the queued request", active, err)
			}
			if _, err := h.store.Create(ctx, "rt-queue", "runtime:"+"rt-queue", "v1.2.4", "user-1", true); !errors.Is(err, errUpdateInProgress) {
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
			if active, err := h.store.GetActive(ctx, "runtime:"+"rt-queue"); err != nil || active != nil {
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
			// Counted from the click: a day after it the queue gives up, however
			// recently the daemon deferred it.
			h.edit(t, req.ID, func(r *UpdateRequest) {
				r.CreatedAt = time.Now().Add(-(updateScheduledTimeout + time.Second))
			})

			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateExpired || got.Error == "" {
				t.Fatalf("expired = %+v", got)
			}
			if _, err := h.store.MarkRunning(ctx, req.ID); !errors.Is(err, errUpdateNotRunnable) {
				t.Fatalf("start after expiry = %v, want errUpdateNotRunnable", err)
			}
			if active, err := h.store.GetActive(ctx, "runtime:"+"rt-expire"); err != nil || active != nil {
				t.Fatalf("active after expiry = %+v, %v; want none", active, err)
			}
			mustCreateUpdate(t, h.store, "rt-expire")
		})
	}
}

// An install that does not report back in time shows as a timeout, but it may
// still be running (the installer has no time limit), so the machine takes no
// other update until the daemon reports how it ended or the install lease
// passes. Otherwise a second update would be installed right after it.
func TestUpdateStore_TimedOutInstallKeepsTheMachineUntilItReports(t *testing.T) {
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
			if _, err := h.store.Create(ctx, "rt-timeout-other", req.MachineKey, "v1.2.4", "user-1", true); !errors.Is(err, errUpdateInProgress) {
				t.Fatalf("create while the timed-out install may still run = %v, want errUpdateInProgress", err)
			}
			if active, err := h.store.GetActive(ctx, req.MachineKey); err != nil || active == nil || active.ID != req.ID {
				t.Fatalf("active = %+v, %v; want the timed-out install", active, err)
			}
			for _, deferrable := range []bool{true, false} {
				if got, err := h.store.PopPending(ctx, "rt-timeout", deferrable); err != nil || got != nil {
					t.Fatalf("offer of a timed-out install = %+v, %v; want none", got, err)
				}
			}

			// The late report settles it and frees the machine.
			if err := h.store.Complete(ctx, req.ID, "Updated to v1.2.3"); err != nil {
				t.Fatalf("late complete: %v", err)
			}
			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateCompleted {
				t.Fatalf("status after the late report = %s, want completed", got.Status)
			}
			if _, err := h.store.Create(ctx, "rt-timeout-other", req.MachineKey, "v1.2.4", "user-1", true); err != nil {
				t.Fatalf("create after the install reported: %v", err)
			}
		})
	}
}

// Without any report, the machine is freed once the install lease passes.
func TestUpdateStore_TimedOutInstallFreesTheMachineAfterTheLease(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req := mustCreateUpdate(t, h.store, "rt-lease")
			if _, err := h.store.MarkRunning(ctx, req.ID); err != nil {
				t.Fatalf("mark running: %v", err)
			}
			h.edit(t, req.ID, func(r *UpdateRequest) {
				aged := time.Now().Add(-(updateRunningTimeout + time.Second))
				r.RunStartedAt = &aged
			})
			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateTimeout {
				t.Fatalf("status = %s, want timeout", got.Status)
			}
			h.edit(t, req.ID, func(r *UpdateRequest) {
				aged := time.Now().Add(-(updateInstallLease + time.Second))
				r.RunStartedAt = &aged
			})
			if active, err := h.store.GetActive(ctx, req.MachineKey); err != nil || active != nil {
				t.Fatalf("active after the lease = %+v, %v; want none", active, err)
			}
			if _, err := h.store.Create(ctx, "rt-lease-other", req.MachineKey, "v1.2.4", "user-1", true); err != nil {
				t.Fatalf("create after the install lease: %v", err)
			}
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
	if _, err := store.Create(ctx, "rt-unrelated", "runtime:"+"rt-unrelated", "v1.2.3", "user-1", true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := mustGetUpdate(t, store, req.ID); got.Status != UpdateScheduled {
		t.Fatalf("status = %s, want scheduled", got.Status)
	}
}

func TestUpdateStore_UnpickedRequestStillTimesOutAfterTwoMinutes(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			req := mustCreateUpdate(t, h.store, "rt-offline")
			h.edit(t, req.ID, func(r *UpdateRequest) {
				r.CreatedAt = time.Now().Add(-(updatePendingTimeout + time.Second))
			})
			if got := mustGetUpdate(t, h.store, req.ID); got.Status != UpdateTimeout {
				t.Fatalf("status = %s; an update no daemon picked up must not wait for a day", got.Status)
			}
		})
	}
}

func TestUpdateStore_FailFastRequestKeepsThePreviousContract(t *testing.T) {
	for _, h := range updateStoreHarnesses(t) {
		t.Run(h.name, func(t *testing.T) {
			ctx := context.Background()
			req, err := h.store.Create(ctx, "rt-fail-fast", "", "v1.2.3", "user-1", false)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := h.store.Create(ctx, "rt-fail-fast", "", "v1.2.4", "user-1", false); !errors.Is(err, errUpdateInProgress) {
				t.Fatalf("second create on the runtime = %v, want errUpdateInProgress", err)
			}
			// Delivered once, running from delivery, never offered again.
			popped, err := h.store.PopPending(ctx, "rt-fail-fast", true)
			if err != nil || popped == nil || popped.Status != UpdateRunning || popped.RunStartedAt == nil {
				t.Fatalf("pop = %+v, %v; want it running from delivery", popped, err)
			}
			if again, err := h.store.PopPending(ctx, "rt-fail-fast", true); err != nil || again != nil {
				t.Fatalf("second pop = %+v, %v; want none", again, err)
			}
			if err := h.store.Fail(ctx, req.ID, "busy"); err != nil {
				t.Fatalf("fail: %v", err)
			}
			if _, err := h.store.Create(ctx, "rt-fail-fast", "", "v1.2.4", "user-1", false); err != nil {
				t.Fatalf("create after it ended: %v", err)
			}
		})
	}
}

func TestUpdateMachineKeyIsScopedByOwner(t *testing.T) {
	daemon := pgtype.Text{String: "shared-daemon-id", Valid: true}
	a := db.AgentRuntime{ID: parseUUID("00000000-0000-0000-0000-00000000000a"), DaemonID: daemon, OwnerID: parseUUID("00000000-0000-0000-0000-0000000000a1")}
	b := db.AgentRuntime{ID: parseUUID("00000000-0000-0000-0000-00000000000b"), DaemonID: daemon, OwnerID: parseUUID("00000000-0000-0000-0000-0000000000b1")}
	sameMachine := db.AgentRuntime{ID: parseUUID("00000000-0000-0000-0000-00000000000c"), DaemonID: daemon, OwnerID: a.OwnerID}
	if updateMachineKey(a) == updateMachineKey(b) {
		t.Fatal("two accounts reporting the same daemon ID share a machine slot")
	}
	if updateMachineKey(a) != updateMachineKey(sameMachine) {
		t.Fatal("two runtimes of one owner's daemon got different machine slots")
	}
}
