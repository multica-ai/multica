package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// intPtr returns a pointer to v for test fixtures that exercise the *int
// queued_tasks field in heartbeat acks.
func intPtr(v int) *int { return &v }

// newDrainTestDaemon returns a Daemon stripped to the pieces the drain logic
// touches, with a real temp workspaces root so drain.json persistence is
// exercised end-to-end.
func newDrainTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	return &Daemon{
		cfg:        Config{WorkspacesRoot: t.TempDir()},
		logger:     slog.Default(),
		workspaces: map[string]*workspaceState{},
	}
}

func TestBeginDrain_StateClaimPauseAndPersistence(t *testing.T) {
	d := newDrainTestDaemon(t)

	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}
	if got := d.daemonState.Load(); got != daemonStateDraining {
		t.Fatalf("daemonState = %d, want draining (%d)", got, daemonStateDraining)
	}
	if !d.claimPaused() {
		t.Fatal("claims must be pause-signaled after beginDrain (defers auto-update/demotion)")
	}
	// AC-2 corrected contract (CEO 2026-08-10): the batch poller must KEEP
	// claiming while draining so pre-boundary queued work is claimed and
	// completed. The drain ref defers auto-update/demotion (decision two) but
	// does not block claim entry — only the server-update / below-minimum
	// holders do.
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must succeed while draining (draining keeps claiming queued work)")
	}
	d.exitClaim()
	// AC-13: the marker must be persisted on disk.
	data, err := os.ReadFile(d.drainFilePath())
	if err != nil {
		t.Fatalf("read drain marker: %v", err)
	}
	var payload struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode drain marker: %v", err)
	}
	if payload.State != "draining" {
		t.Fatalf("drain marker state = %q, want draining", payload.State)
	}

	// beginDrain is idempotent while already draining.
	if err := d.beginDrain(); err != nil {
		t.Fatalf("repeated beginDrain: %v", err)
	}
}

func TestAbortDrain_ResumesClaimsAndClearsPersistence(t *testing.T) {
	d := newDrainTestDaemon(t)
	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}

	if err := d.abortDrain(); err != nil {
		t.Fatalf("abortDrain: %v", err)
	}
	if got := d.daemonState.Load(); got != daemonStateRunning {
		t.Fatalf("daemonState = %d, want running (%d)", got, daemonStateRunning)
	}
	if d.claimPaused() {
		t.Fatal("claims must resume after abortDrain")
	}
	// AC-3: claims are immediately accepted again after abort.
	if !d.tryEnterClaim() {
		t.Fatal("tryEnterClaim must succeed after abortDrain")
	}
	d.exitClaim()
	if _, err := os.Stat(d.drainFilePath()); !os.IsNotExist(err) {
		t.Fatalf("drain marker must be removed after abortDrain, stat err = %v", err)
	}

	// aborting when not draining is an error, not a silent no-op.
	if err := d.abortDrain(); err == nil {
		t.Fatal("abortDrain while running must return an error")
	}
}

func TestFinishDrainThenStop_ArmsAndAbortClearsFlag(t *testing.T) {
	d := newDrainTestDaemon(t)
	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}
	if err := d.finishDrainThenStop(); err != nil {
		t.Fatalf("finishDrainThenStop: %v", err)
	}
	if !d.finishThenStop.Load() {
		t.Fatal("finishThenStop must be armed after finishDrainThenStop")
	}

	if err := d.abortDrain(); err != nil {
		t.Fatalf("abortDrain: %v", err)
	}
	if d.finishThenStop.Load() {
		t.Fatal("abortDrain must clear the finishThenStop flag")
	}

	// finish_then_stop without draining is an error.
	if err := d.finishDrainThenStop(); err == nil {
		t.Fatal("finishDrainThenStop while running must return an error")
	}
}

func TestRestoreDrainState_ResumesDrainingOnRestart(t *testing.T) {
	t.Run("marker present", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		if err := d.beginDrain(); err != nil {
			t.Fatalf("beginDrain: %v", err)
		}

		// Simulate a restart: a fresh Daemon over the same workspaces root.
		restarted := &Daemon{
			cfg:        Config{WorkspacesRoot: d.cfg.WorkspacesRoot},
			logger:     slog.Default(),
			workspaces: map[string]*workspaceState{},
		}
		restarted.restoreDrainState()

		if got := restarted.daemonState.Load(); got != daemonStateDraining {
			t.Fatalf("restored daemonState = %d, want draining (%d)", got, daemonStateDraining)
		}
		if !restarted.claimPaused() {
			t.Fatal("restored daemon must hold the drain claim-pause ref (defers auto-update/demotion)")
		}
		// Corrected contract: a restored draining daemon keeps claiming queued
		// work — the drain ref does not block claim entry.
		if !restarted.tryEnterClaim() {
			t.Fatal("restored daemon must accept claims (draining keeps claiming queued work)")
		}
		restarted.exitClaim()
		if got := restarted.registrationStatus(); got != "draining" {
			t.Fatalf("registrationStatus = %q, want draining", got)
		}
	})

	t.Run("no marker", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		d.restoreDrainState()
		if got := d.daemonState.Load(); got != daemonStateRunning {
			t.Fatalf("daemonState = %d, want running (%d)", got, daemonStateRunning)
		}
		if d.claimPaused() {
			t.Fatal("no marker must leave claims unpaused")
		}
	})

	t.Run("malformed marker ignored", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		if err := os.MkdirAll(d.cfg.WorkspacesRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d.drainFilePath(), []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		d.restoreDrainState()
		if got := d.daemonState.Load(); got != daemonStateRunning {
			t.Fatalf("malformed marker must be ignored; daemonState = %d", got)
		}
	})
}

func TestRegistrationStatus_ReportsDraining(t *testing.T) {
	d := newDrainTestDaemon(t)
	if got := d.registrationStatus(); got != "online" {
		t.Fatalf("registrationStatus before drain = %q, want online", got)
	}
	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}
	if got := d.registrationStatus(); got != "draining" {
		t.Fatalf("registrationStatus while draining = %q, want draining", got)
	}
}

func TestDaemonStateString(t *testing.T) {
	d := newDrainTestDaemon(t)
	if got := d.daemonStateString(); got != "running" {
		t.Fatalf("state string = %q, want running", got)
	}
	if err := d.beginDrain(); err != nil {
		t.Fatal(err)
	}
	if got := d.daemonStateString(); got != "draining" {
		t.Fatalf("state string = %q, want draining", got)
	}
	d.daemonState.Store(daemonStateShuttingDown)
	if got := d.daemonStateString(); got != "stopped" {
		t.Fatalf("state string = %q, want stopped", got)
	}
}

// TestDrainMonitor_StopsWhenIdleAndArmed covers AC-4: with finish-then-stop
// armed and no in-flight tasks the daemon deregisters and cancels its root
// context. AC-5 (normal stop) is untouched 鈥?the monitor never fires unless
// draining.
func TestDrainMonitor_StopsWhenIdleAndArmed(t *testing.T) {
	d := newDrainTestDaemon(t)
	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}
	if err := d.finishDrainThenStop(); err != nil {
		t.Fatalf("finishDrainThenStop: %v", err)
	}
	var cancelCalls atomic.Int32
	d.cancelFunc = func() { cancelCalls.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.drainMonitorLoop(ctx)
	}()

	// While a task is still in flight the monitor must NOT stop the daemon.
	// Give it more than one tick to prove it keeps waiting.
	d.activeTasks.Store(1)
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cancelCalls.Load() != 0 {
			t.Fatal("monitor cancelled the daemon while a task was still in flight")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Corrected contract (CEO 2026-08-10): even with no in-flight task, the
	// monitor must keep waiting while the server still holds queued work for
	// the draining runtimes — pre-boundary queued tasks must be claimed and
	// completed before shutdown (AC-4 extended).
	d.activeTasks.Store(0)
	d.recordQueuedTasks("rt-1", intPtr(2))
	deadline = time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cancelCalls.Load() != 0 {
			t.Fatal("monitor cancelled the daemon while queued tasks remained")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Both in-flight and queued counts reach zero -> the monitor should
	// deregister + cancel on the next tick.
	d.recordQueuedTasks("rt-1", intPtr(0))
	waitUntil(t, 3*time.Second, func() bool { return cancelCalls.Load() == 1 })

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drainMonitorLoop did not return after cancelling the daemon")
	}
	if got := d.daemonState.Load(); got != daemonStateShuttingDown {
		t.Fatalf("daemonState after stop = %d, want shutting_down (%d)", got, daemonStateShuttingDown)
	}
}

// TestDrainMonitor_DoesNotFireWhenNotArmed pins the drain-resident mode: a
// drained daemon without finish-then-stop stays up (waits for abort or a later
// finish_then_stop) even when idle.
func TestDrainMonitor_DoesNotFireWhenNotArmed(t *testing.T) {
	d := newDrainTestDaemon(t)
	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}
	var cancelCalls atomic.Int32
	d.cancelFunc = func() { cancelCalls.Add(1) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.drainMonitorLoop(ctx)

	time.Sleep(2500 * time.Millisecond)
	if cancelCalls.Load() != 0 {
		t.Fatal("monitor cancelled the daemon although finish-then-stop was not armed")
	}
}

func TestShutdownHandler_ReportsStoppingState(t *testing.T) {
	d := newDrainTestDaemon(t)
	var cancelled atomic.Bool
	d.cancelFunc = func() { cancelled.Store(true) }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	d.shutdownHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := d.daemonState.Load(); got != daemonStateShuttingDown {
		t.Fatalf("daemonState after /shutdown = %d, want shutting_down (%d)", got, daemonStateShuttingDown)
	}
	waitUntil(t, time.Second, func() bool { return cancelled.Load() })
}

func TestDrainHandler_Actions(t *testing.T) {
	newHandler := func(t *testing.T, d *Daemon) http.HandlerFunc {
		t.Helper()
		return d.drainHandler()
	}
	post := func(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/drain", strings.NewReader(body)))
		return rec
	}

	t.Run("drain then abort", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		h := newHandler(t, d)

		rec := post(t, h, `{"action":"drain"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("drain action: got %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if got := d.daemonState.Load(); got != daemonStateDraining {
			t.Fatalf("daemonState after /drain = %d, want draining", got)
		}
		if !d.claimPaused() {
			t.Fatal("claims must be paused after /drain")
		}

		rec = post(t, h, `{"action":"abort"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("abort action: got %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if got := d.daemonState.Load(); got != daemonStateRunning {
			t.Fatalf("daemonState after abort = %d, want running", got)
		}
		if d.claimPaused() {
			t.Fatal("claims must resume after abort")
		}
	})

	t.Run("finish_then_stop", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		h := newHandler(t, d)
		post(t, h, `{"action":"drain"}`)

		rec := post(t, h, `{"action":"finish_then_stop"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("finish_then_stop: got %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		if !d.finishThenStop.Load() {
			t.Fatal("finish_then_stop must arm the auto-close flag")
		}
	})

	t.Run("rejects unknown action", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		rec := post(t, d.drainHandler(), `{"action":"frobnicate"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("unknown action: got %d, want 400", rec.Code)
		}
	})

	t.Run("rejects non-post", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		rec := httptest.NewRecorder()
		d.drainHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/drain", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET /drain: got %d, want 405", rec.Code)
		}
	})

	t.Run("abort without draining is a 400", func(t *testing.T) {
		d := newDrainTestDaemon(t)
		rec := post(t, d.drainHandler(), `{"action":"abort"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("abort while running: got %d, want 400", rec.Code)
		}
	})
}

func TestHealthReportsDrainStateAndCounts(t *testing.T) {
	d := newDrainTestDaemon(t)
	d.ready.Store(true)
	d.activeTasks.Store(3)
	d.recordQueuedTasks("rt-1", intPtr(4))
	d.recordQueuedTasks("rt-2", intPtr(1))
	if err := d.beginDrain(); err != nil {
		t.Fatalf("beginDrain: %v", err)
	}

	rec := httptest.NewRecorder()
	d.healthHandler(time.Now()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.State != "draining" {
		t.Fatalf("state = %q, want draining", resp.State)
	}
	if resp.Status != "running" {
		t.Fatalf("status = %q, want running (drain is orthogonal to readiness)", resp.Status)
	}
	if resp.DrainingInflightTasks != 3 {
		t.Fatalf("draining_inflight_tasks = %d, want 3", resp.DrainingInflightTasks)
	}
	if resp.DrainingQueuedTasks != 5 {
		t.Fatalf("draining_queued_tasks = %d, want 5", resp.DrainingQueuedTasks)
	}
}

func TestHeartbeatAckCachesQueuedTasks(t *testing.T) {
	d := newDrainTestDaemon(t)
	d.handleHeartbeatActions(context.Background(), "rt-1", &HeartbeatResponse{
		RuntimeID:  "rt-1",
		QueuedTasks: intPtr(7),
	})
	d.handleHeartbeatActions(context.Background(), "rt-2", &HeartbeatResponse{
		RuntimeID:  "rt-2",
		QueuedTasks: intPtr(3),
	})
	if got := d.queuedTaskCount(); got != 10 {
		t.Fatalf("queuedTaskCount = %d, want 10", got)
	}
	// Updating one runtime leaves the other's count intact.
	d.handleHeartbeatActions(context.Background(), "rt-1", &HeartbeatResponse{
		RuntimeID:  "rt-1",
		QueuedTasks: intPtr(1),
	})
	if got := d.queuedTaskCount(); got != 4 {
		t.Fatalf("queuedTaskCount after update = %d, want 4", got)
	}
	// An absent (nil) ack keeps the previous value — the server only fills
	// QueuedTasks while draining, so a non-draining ack must not zero the cache.
	d.handleHeartbeatActions(context.Background(), "rt-1", &HeartbeatResponse{RuntimeID: "rt-1"})
	if got := d.queuedTaskCount(); got != 4 {
		t.Fatalf("queuedTaskCount after nil ack = %d, want 4", got)
	}
}

// waitUntil polls cond until it returns true or the deadline elapses.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
