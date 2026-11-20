package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// forEachUpdateStore runs fn against the in-memory store and, when
// REDIS_TEST_URL is set, the Redis store.
func forEachUpdateStore(t *testing.T, fn func(t *testing.T, store UpdateStore, age func(id string, by time.Duration))) {
	t.Run("memory", func(t *testing.T) {
		store := NewInMemoryUpdateStore()
		fn(t, store, func(id string, by time.Duration) {
			store.mu.Lock()
			defer store.mu.Unlock()
			store.requests[id].CreatedAt = store.requests[id].CreatedAt.Add(-by)
		})
	})
	t.Run("redis", func(t *testing.T) {
		rdb := newRedisTestClient(t)
		store := NewRedisUpdateStore(rdb)
		fn(t, store, func(id string, by time.Duration) {
			req, err := store.Get(context.Background(), id)
			if err != nil || req == nil {
				t.Fatalf("get %s: %v", id, err)
			}
			req.CreatedAt = req.CreatedAt.Add(-by)
			persistUpdateForTest(t, rdb, store, req)
		})
	})
}

func mustCreateUpdate(t *testing.T, store UpdateStore, runtimeID, machineKey string, queue bool) *UpdateRequest {
	t.Helper()
	req, err := store.Create(context.Background(), runtimeID, machineKey, "v0.12.0", "user-1", queue)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return req
}

func TestUpdateQueue_OfferedUntilStarted(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, _ func(string, time.Duration)) {
		ctx := context.Background()
		req := mustCreateUpdate(t, store, "rt-1", "machine-1", true)

		for i := 0; i < 2; i++ {
			offered, err := store.PopPending(ctx, "rt-1", true)
			if err != nil || offered == nil || offered.ID != req.ID || offered.Status != UpdatePending {
				t.Fatalf("offer %d = %+v, %v; want the pending request again", i, offered, err)
			}
		}
		tasks := 3
		got, err := store.Transition(ctx, req.ID, scheduleUpdate(&tasks))
		if err != nil || got.Status != UpdateScheduled || *got.ActiveTasks != 3 {
			t.Fatalf("schedule = %+v, %v", got, err)
		}
		if offered, _ := store.PopPending(ctx, "rt-1", true); offered == nil || offered.ID != req.ID {
			t.Fatalf("a scheduled request must still be offered (daemon restart), got %+v", offered)
		}
		if got, err := store.Transition(ctx, req.ID, startUpdate); err != nil || got.Status != UpdateRunning {
			t.Fatalf("start = %+v, %v", got, err)
		}
		if offered, _ := store.PopPending(ctx, "rt-1", true); offered != nil {
			t.Fatalf("a running request must not be offered again, got %+v", offered)
		}
	})
}

func TestUpdateQueue_DaemonWithoutQueueSupportRunsFromDelivery(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, _ func(string, time.Duration)) {
		ctx := context.Background()
		req := mustCreateUpdate(t, store, "rt-1", "machine-1", true)
		offered, err := store.PopPending(ctx, "rt-1", false)
		if err != nil || offered == nil || offered.Status != UpdateRunning {
			t.Fatalf("offer = %+v, %v; want running", offered, err)
		}
		if _, err := store.Transition(ctx, req.ID, cancelUpdate); !errors.Is(err, errUpdateNotCancellable) {
			t.Fatalf("cancel after delivery: %v, want errUpdateNotCancellable", err)
		}
	})
}

func TestUpdateQueue_CancelAndStartExclusive(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, _ func(string, time.Duration)) {
		ctx := context.Background()

		cancelled := mustCreateUpdate(t, store, "rt-1", "machine-1", true)
		if _, err := store.Transition(ctx, cancelled.ID, cancelUpdate); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if _, err := store.Transition(ctx, cancelled.ID, startUpdate); !errors.Is(err, errUpdateWithdrawn) {
			t.Fatalf("start after cancel: %v, want errUpdateWithdrawn", err)
		}
		if _, err := store.Transition(ctx, cancelled.ID, scheduleUpdate(nil)); !errors.Is(err, errUpdateWithdrawn) {
			t.Fatalf("schedule after cancel: %v, want errUpdateWithdrawn", err)
		}

		for round := 0; round < 20; round++ {
			req := mustCreateUpdate(t, store, "rt-1", "machine-1", true)
			var wg sync.WaitGroup
			var cancelErr, startErr error
			wg.Add(2)
			go func() { defer wg.Done(); _, cancelErr = store.Transition(ctx, req.ID, cancelUpdate) }()
			go func() { defer wg.Done(); _, startErr = store.Transition(ctx, req.ID, startUpdate) }()
			wg.Wait()
			if (cancelErr == nil) == (startErr == nil) {
				t.Fatalf("round %d: cancel=%v start=%v, want exactly one to win", round, cancelErr, startErr)
			}
			final, _ := store.Get(ctx, req.ID)
			if want := map[bool]UpdateStatus{true: UpdateCancelled, false: UpdateRunning}[cancelErr == nil]; final.Status != want {
				t.Fatalf("round %d: stored %s, want %s", round, final.Status, want)
			}
			if final.Status == UpdateRunning {
				_, _ = store.Transition(ctx, req.ID, finishUpdate(UpdateCompleted, "", ""))
			}
		}
	})
}

func TestUpdateQueue_OneUnfinishedUpdatePerMachine(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, _ func(string, time.Duration)) {
		ctx := context.Background()
		first := mustCreateUpdate(t, store, "rt-claude", "machine-1", true)

		if _, err := store.Create(ctx, "rt-codex", "machine-1", "v0.12.0", "user-1", true); !errors.Is(err, errUpdateInProgress) {
			t.Fatalf("second runtime of the machine: %v, want errUpdateInProgress", err)
		}
		active, err := store.GetActive(ctx, "machine-1")
		if err != nil || active == nil || active.ID != first.ID {
			t.Fatalf("active = %+v, %v; want the first request", active, err)
		}
		mustCreateUpdate(t, store, "rt-other", "machine-2", true)

		if _, err := store.Transition(ctx, first.ID, cancelUpdate); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if active, _ := store.GetActive(ctx, "machine-1"); active != nil {
			t.Fatalf("active after cancel = %+v, want none", active)
		}
		mustCreateUpdate(t, store, "rt-codex", "machine-1", true)
	})
}

func TestUpdateQueue_OnlyQueuedRequestsAreCancellable(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, _ func(string, time.Duration)) {
		ctx := context.Background()
		// A CLI request does not opt into queueing; its --wait does not know "cancelled".
		cli := mustCreateUpdate(t, store, "rt-1", "machine-1", false)
		if _, err := store.Transition(ctx, cli.ID, cancelUpdate); !errors.Is(err, errUpdateNotCancellable) {
			t.Fatalf("cancel CLI request: %v, want errUpdateNotCancellable", err)
		}
		if got, _ := store.Get(ctx, cli.ID); got.Status != UpdatePending || updateCancellable(got) {
			t.Fatalf("CLI request = %+v, want pending and not cancellable", got)
		}
	})
}

func TestUpdateQueue_ConcurrentCreatesOnOneMachine(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, _ func(string, time.Duration)) {
		const n = 16
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := store.Create(context.Background(), fmt.Sprintf("rt-%d", i), "machine-1", "v0.12.0", "user-1", true)
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)
		created := 0
		for err := range errs {
			switch {
			case err == nil:
				created++
			case !errors.Is(err, errUpdateInProgress):
				t.Fatalf("create: %v", err)
			}
		}
		if created != 1 {
			t.Fatalf("created %d requests on one machine, want 1", created)
		}
	})
}

func TestUpdateQueue_ExpiresAfter24Hours(t *testing.T) {
	forEachUpdateStore(t, func(t *testing.T, store UpdateStore, age func(string, time.Duration)) {
		ctx := context.Background()
		req := mustCreateUpdate(t, store, "rt-1", "machine-1", true)
		if _, err := store.Transition(ctx, req.ID, scheduleUpdate(nil)); err != nil {
			t.Fatalf("schedule: %v", err)
		}

		age(req.ID, updateQueueTimeout-time.Minute)
		if got, _ := store.Get(ctx, req.ID); got.Status != UpdateScheduled {
			t.Fatalf("status before 24h = %s", got.Status)
		}
		age(req.ID, 2*time.Minute)
		if got, _ := store.Get(ctx, req.ID); got.Status != UpdateExpired {
			t.Fatalf("status after 24h = %s, want expired", got.Status)
		}
		if _, err := store.Transition(ctx, req.ID, startUpdate); !errors.Is(err, errUpdateWithdrawn) {
			t.Fatalf("start after expiry: %v, want errUpdateWithdrawn", err)
		}
		if offered, _ := store.PopPending(ctx, "rt-1", true); offered != nil {
			t.Fatalf("an expired request must not be offered, got %+v", offered)
		}
		mustCreateUpdate(t, store, "rt-1", "machine-1", true)
	})
}

// TestQueuedUpdateThroughHandlers walks the HTTP flow on one machine with two
// runtimes: queue, see it from the sibling runtime, refuse a second update,
// cancel, and refuse the daemon's start afterwards.
func TestQueuedUpdateThroughHandlers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	original := testHandler.UpdateStore
	testHandler.UpdateStore = NewInMemoryUpdateStore()
	t.Cleanup(func() { testHandler.UpdateStore = original })

	runtimes := make([]string, 2)
	for i, provider := range []string{"queue_test_claude", "queue_test_codex"} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
			VALUES ($1, 'queue-test-daemon', $2, 'local', $2, 'online', 'queue test', '{}'::jsonb, $3, now())
			RETURNING id
		`, testWorkspaceID, provider, testUserID).Scan(&runtimes[i]); err != nil {
			t.Fatalf("create runtime: %v", err)
		}
		id := runtimes[i]
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, id) })
	}
	rtA, rtB := runtimes[0], runtimes[1]

	call := func(fn http.HandlerFunc, method, path string, body any, params ...string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		fn(w, withURLParams(newRequest(method, path, body), params...))
		return w
	}

	w := call(testHandler.InitiateUpdate, http.MethodPost, "/api/runtimes/"+rtA+"/update",
		map[string]any{"target_version": "v0.12.0", "queue_when_busy": true}, "runtimeId", rtA)
	var created UpdateRequest
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &created) != nil || !created.Cancellable {
		t.Fatalf("initiate: %d %s", w.Code, w.Body.String())
	}

	// The heartbeat offers it as deferrable to a daemon that can hold it.
	ack, _, err := testHandler.processHeartbeat(ctx, rtA, protocol.DaemonHeartbeatRequestPayload{RuntimeID: rtA, SupportsQueuedUpdate: true})
	if err != nil || ack.PendingUpdate == nil || ack.PendingUpdate.ID != created.ID || !ack.PendingUpdate.Deferrable {
		t.Fatalf("heartbeat offer = %+v, %v", ack, err)
	}
	report := func(status string) int {
		return call(testHandler.ReportUpdateResult, http.MethodPost, "/api/daemon/runtimes/"+rtA+"/update/"+created.ID+"/result",
			map[string]any{"status": status, "active_tasks": 2}, "runtimeId", rtA, "updateId", created.ID).Code
	}
	if code := report("scheduled"); code != http.StatusOK {
		t.Fatalf("scheduled report: %d", code)
	}

	// The sibling runtime of the same daemon sees the queued update.
	w = call(testHandler.GetActiveUpdate, http.MethodGet, "/api/runtimes/"+rtB+"/update", nil, "runtimeId", rtB)
	var active struct {
		Update *UpdateRequest `json:"update"`
	}
	if json.Unmarshal(w.Body.Bytes(), &active) != nil || active.Update == nil || active.Update.ID != created.ID ||
		active.Update.Status != UpdateScheduled || *active.Update.ActiveTasks != 2 {
		t.Fatalf("active from sibling runtime: %d %s", w.Code, w.Body.String())
	}
	w = call(testHandler.InitiateUpdate, http.MethodPost, "/api/runtimes/"+rtB+"/update",
		map[string]any{"target_version": "v0.12.0", "queue_when_busy": true}, "runtimeId", rtB)
	if w.Code != http.StatusConflict {
		t.Fatalf("second update on the machine: %d %s", w.Code, w.Body.String())
	}

	w = call(testHandler.CancelUpdate, http.MethodPost, "/api/runtimes/"+rtB+"/update/"+created.ID+"/cancel", nil,
		"runtimeId", rtB, "updateId", created.ID)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	if code := report("running"); code != http.StatusConflict {
		t.Fatalf("start after cancel: %d, want 409", code)
	}
	if code := report("completed"); code != http.StatusOK {
		t.Fatalf("late terminal report: %d, want 200 (ignored)", code)
	}
	w = call(testHandler.CancelUpdate, http.MethodPost, "/api/runtimes/"+rtA+"/update/"+created.ID+"/cancel", nil,
		"runtimeId", rtA, "updateId", created.ID)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("second cancel (another tab): %d %s, want 200 cancelled", w.Code, w.Body.String())
	}
}
