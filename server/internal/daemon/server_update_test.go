package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleUpdateReportsWhyItWasDeferred(t *testing.T) {
	tests := []struct {
		name      string
		arrange   func(*Daemon)
		wantError string
	}{
		{
			name: "another update owns the daemon",
			arrange: func(d *Daemon) {
				d.updating.Store(true)
			},
			wantError: "another runtime update is already in progress on this machine",
		},
		{
			name: "agent task is active",
			arrange: func(d *Daemon) {
				d.activeTasks.Store(1)
			},
			wantError: "runtime update deferred because agent work is starting or still active; retry when the machine is idle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload map[string]any
			d, reportCalls := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode update report: %v", err)
				}
				w.WriteHeader(http.StatusOK)
			})
			d.runUpdateFn = func(string) (string, error) {
				t.Fatal("runUpdateFn must not run while the update is deferred")
				return "", nil
			}
			tt.arrange(d)

			d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{
				ID:            "update-1",
				TargetVersion: "v0.4.14",
			})

			if got := atomic.LoadInt32(reportCalls); got != 1 {
				t.Fatalf("update reports = %d, want 1", got)
			}
			if got := payload["status"]; got != "failed" {
				t.Fatalf("status = %v, want failed", got)
			}
			if got := payload["error"]; got != tt.wantError {
				t.Fatalf("error = %v, want %q", got, tt.wantError)
			}
		})
	}
}

func TestHandleUpdateWaitsForEmptyClaimInsteadOfStarving(t *testing.T) {
	originalResolveSelfExecutable := resolveSelfExecutable
	originalIsBrewInstall := isBrewInstall
	resolveSelfExecutable = func() (string, error) {
		return "/tmp/multica-next", nil
	}
	isBrewInstall = func() bool { return false }
	t.Cleanup(func() {
		resolveSelfExecutable = originalResolveSelfExecutable
		isBrewInstall = originalIsBrewInstall
	})

	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	d.claimsInFlight = 1
	updateStarted := make(chan struct{})
	d.runUpdateFn = func(string) (string, error) {
		close(updateStarted)
		return "upgraded", nil
	}
	done := make(chan struct{})
	go func() {
		d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{
			ID:            "update-1",
			TargetVersion: "v0.4.14",
		})
		close(done)
	}()

	waitForServerUpdateBarrier(t, d)
	select {
	case <-updateStarted:
		t.Fatal("update started before the in-flight claim finished")
	default:
	}

	d.exitClaim()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("update did not continue after the empty claim finished")
	}
	select {
	case <-updateStarted:
	default:
		t.Fatal("update never started after the empty claim finished")
	}
}

func TestHandleUpdateReportsBusyWhenClaimBecomesTask(t *testing.T) {
	var payload map[string]any
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode update report: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	d.claimsInFlight = 1
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("runUpdateFn must not run after the claim hands off an active task")
		return "", nil
	}
	done := make(chan struct{})
	go func() {
		d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{
			ID:            "update-1",
			TargetVersion: "v0.4.14",
		})
		close(done)
	}()

	waitForServerUpdateBarrier(t, d)
	d.activeTasks.Add(1)
	d.exitClaim()
	defer d.activeTasks.Add(-1)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("update did not return after the claim handed off a task")
	}
	if got := payload["status"]; got != "failed" {
		t.Fatalf("status = %v, want failed", got)
	}
	if got := payload["error"]; got != "runtime update deferred because agent work is starting or still active; retry when the machine is idle" {
		t.Fatalf("error = %v", got)
	}
}

func waitForServerUpdateBarrier(t *testing.T, d *Daemon) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.claimMu.Lock()
		paused := d.pauseClaims
		d.claimMu.Unlock()
		if paused {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("server update did not pause new claims")
}

func TestHandleUpdateKeepsBarrierOnlyWhenRestartWasScheduled(t *testing.T) {
	tests := []struct {
		name              string
		resolveExecutable func() (string, error)
		wantUpdating      bool
		wantClaimsPaused  bool
		wantRestartCalls  int32
	}{
		{
			name: "restart scheduled",
			resolveExecutable: func() (string, error) {
				return "/tmp/multica-next", nil
			},
			wantUpdating:     true,
			wantClaimsPaused: true,
			wantRestartCalls: 1,
		},
		{
			name: "restart could not be scheduled",
			resolveExecutable: func() (string, error) {
				return "", errors.New("executable unavailable")
			},
			wantUpdating:     false,
			wantClaimsPaused: false,
			wantRestartCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalResolveSelfExecutable := resolveSelfExecutable
			originalIsBrewInstall := isBrewInstall
			resolveSelfExecutable = tt.resolveExecutable
			isBrewInstall = func() bool { return false }
			t.Cleanup(func() {
				resolveSelfExecutable = originalResolveSelfExecutable
				isBrewInstall = originalIsBrewInstall
			})

			d, _ := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			d.runUpdateFn = func(string) (string, error) {
				return "upgraded", nil
			}
			var restartCalls atomic.Int32
			d.cancelFunc = func() {
				restartCalls.Add(1)
			}

			d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{
				ID:            "update-1",
				TargetVersion: "v0.4.14",
			})

			if got := d.updating.Load(); got != tt.wantUpdating {
				t.Fatalf("updating = %v, want %v", got, tt.wantUpdating)
			}
			if got := d.pauseClaims; got != tt.wantClaimsPaused {
				t.Fatalf("pauseClaims = %v, want %v", got, tt.wantClaimsPaused)
			}
			if got := restartCalls.Load(); got != tt.wantRestartCalls {
				t.Fatalf("restart calls = %d, want %d", got, tt.wantRestartCalls)
			}
		})
	}
}
