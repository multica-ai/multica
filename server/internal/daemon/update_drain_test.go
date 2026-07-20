package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleUpdateDefersWhileDrainOwnsBarrier(t *testing.T) {
	d, reportCalls := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	d.activeTasks.Store(1)

	var updateCalls atomic.Int32
	var restartCalls atomic.Int32
	d.runUpdateFn = func(string) (string, error) {
		updateCalls.Add(1)
		return "upgraded", nil
	}
	d.cancelFunc = func() {
		restartCalls.Add(1)
	}

	drainCtx, cancelDrain := context.WithCancel(context.Background())
	drainDone := make(chan struct{})
	go func() {
		d.drainShutdownHandler().ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodPost, "/shutdown/drain", nil).WithContext(drainCtx),
		)
		close(drainDone)
	}()
	t.Cleanup(func() {
		cancelDrain()
		select {
		case <-drainDone:
		case <-time.After(time.Second):
			t.Error("drain handler did not stop during cleanup")
		}
	})

	waitForDrainState(t, d, true)
	d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{
		ID:            "update-1",
		TargetVersion: "v0.4.5",
	})

	if got := updateCalls.Load(); got != 0 {
		t.Fatalf("heartbeat update ran %d time(s) while drain owned the barrier", got)
	}
	if got := restartCalls.Load(); got != 0 {
		t.Fatalf("heartbeat update restarted %d time(s) while drain owned the barrier", got)
	}
	if got := atomic.LoadInt32(reportCalls); got != 0 {
		t.Fatalf("deferred heartbeat update reported %d status update(s), want none", got)
	}
	if !d.isDraining() {
		t.Fatal("heartbeat update stole the drain barrier")
	}
}

func TestDrainReturnsConflictWhileHandleUpdateOwnsBarrier(t *testing.T) {
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	d.activeTasks.Store(1)

	updateStarted := make(chan struct{})
	releaseUpdate := make(chan struct{})
	updateDone := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseUpdate) })
	}
	t.Cleanup(release)

	var restartCalls atomic.Int32
	d.cancelFunc = func() {
		restartCalls.Add(1)
	}
	d.runUpdateFn = func(string) (string, error) {
		close(updateStarted)
		<-releaseUpdate
		return "upgraded", nil
	}

	go func() {
		d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{
			ID:            "update-1",
			TargetVersion: "v0.4.5",
		})
		close(updateDone)
	}()

	select {
	case <-updateStarted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat update did not start")
	}

	drainCtx, cancelDrain := context.WithCancel(context.Background())
	rec := httptest.NewRecorder()
	drainDone := make(chan struct{})
	go func() {
		d.drainShutdownHandler().ServeHTTP(
			rec,
			httptest.NewRequest(http.MethodPost, "/shutdown/drain", nil).WithContext(drainCtx),
		)
		close(drainDone)
	}()

	select {
	case <-drainDone:
	case <-time.After(200 * time.Millisecond):
		cancelDrain()
		<-drainDone
		t.Fatal("drain blocked instead of rejecting an in-progress heartbeat update")
	}
	cancelDrain()
	if rec.Code != http.StatusConflict {
		t.Fatalf("drain status = %d, want %d while update owns the barrier", rec.Code, http.StatusConflict)
	}

	release()
	select {
	case <-updateDone:
	case <-time.After(time.Second):
		t.Fatal("heartbeat update did not finish after release")
	}
	if got := restartCalls.Load(); got != 1 {
		t.Fatalf("heartbeat update restarted %d time(s), want 1", got)
	}
}
