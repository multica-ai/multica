package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Give a manually signalled context the same Err contract as an expired deadline.
type shutdownWaitDeadlineContext struct{ context.Context }

func (c shutdownWaitDeadlineContext) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func TestHandleTask_LocalDirectoryWaitCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, serverStatus, wantReason string
		shutdown, deadline             bool
	}{
		{name: "shutdown", serverStatus: "running", wantReason: "runtime_recovery", shutdown: true},
		{name: "user cancellation", serverStatus: "cancelled"},
		{name: "server cancellation during shutdown", serverStatus: "cancelled", shutdown: true},
		{name: "server completion during shutdown", serverStatus: "completed", shutdown: true},
		{name: "server failure during shutdown", serverStatus: "failed", shutdown: true},
		{name: "server deletion during shutdown", serverStatus: "deleted", shutdown: true},
		{name: "parent deadline", serverStatus: "running", wantReason: "cancelled", deadline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			realDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			locker := NewLocalPathLocker()
			release, err := locker.Acquire(context.Background(), realDir, "holder", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var parent context.Context = ctx
			if tc.deadline {
				parent = shutdownWaitDeadlineContext{ctx}
			}
			parked := make(chan struct{}, 1)
			leaseStarted := make(chan struct{}, 1)
			leaseStopped := make(chan struct{}, 1)
			reports := make(chan map[string]any, 2)
			var stopped atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/wait-local-directory"):
					parked <- struct{}{}
				case strings.HasSuffix(r.URL.Path, "/prepare-lease"):
					_, _ = io.Copy(io.Discard, r.Body)
					select {
					case leaseStarted <- struct{}{}:
					default:
					}
					<-r.Context().Done()
					select {
					case leaseStopped <- struct{}{}:
					default:
					}
				case strings.HasSuffix(r.URL.Path, "/status"):
					status := "running"
					if stopped.Load() {
						status = tc.serverStatus
					}
					if status == "deleted" {
						http.Error(w, "task not found", http.StatusNotFound)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
				case strings.HasSuffix(r.URL.Path, "/fail"):
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					reports <- body
				default:
					t.Errorf("unexpected callback: %s", r.URL.Path)
				}
			}))
			defer func() { cancel(); srv.CloseClientConnections(); srv.Close() }()
			poll := time.Hour
			if !tc.shutdown && !tc.deadline {
				poll = time.Millisecond
			}
			d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				cfg: Config{DaemonID: "daemon"}, localPathLocks: locker,
				runtimeIndex:       map[string]Runtime{"rt": {ID: "rt", Provider: "codex"}},
				cancelPollInterval: poll, prepareLeaseRefresh: time.Millisecond}
			d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
				t.Error("runner started while lock held")
				return TaskResult{}, nil
			})
			ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: "daemon"})
			if err != nil {
				t.Fatal(err)
			}
			task := Task{ID: "waiter", RuntimeID: "rt", ProjectResources: []ProjectResourceData{{ID: "resource", ResourceType: localDirectoryResourceType, ResourceRef: ref}}}
			done := make(chan struct{})
			go func() { defer close(done); d.handleTask(parent, task, 0) }()
			wait := func(ch <-chan struct{}, what string) {
				t.Helper()
				select {
				case <-ch:
				case <-time.After(3 * time.Second):
					t.Fatalf("timed out waiting for %s", what)
				}
			}
			wait(parked, "directory wait")
			if got := d.resourceWaitTasks.Load(); got != 1 {
				t.Fatalf("wait count=%d", got)
			}
			wait(leaseStarted, "prepare lease request")
			stopped.Store(true)
			if tc.shutdown || tc.deadline {
				cancel()
			}
			wait(done, "handleTask cleanup")
			wait(leaseStopped, "lease cancellation")
			if got := d.resourceWaitTasks.Load(); got != 0 {
				t.Errorf("wait count after return=%d", got)
			}
			if got := locker.Holder(realDir); got != "holder" {
				t.Errorf("cancelled waiter disturbed holder: %q", got)
			}
			if tc.wantReason == "" {
				select {
				case report := <-reports:
					t.Errorf("terminal server task received fail: %v", report)
				default:
				}
			} else {
				select {
				case report := <-reports:
					if report["failure_reason"] != tc.wantReason {
						t.Errorf("failure_reason=%v, want %s", report["failure_reason"], tc.wantReason)
					}
					if tc.wantReason == "runtime_recovery" && report["error"] != "daemon shut down while task was waiting for its local directory lock" {
						t.Errorf("shutdown diagnostic=%v", report["error"])
					}
				default:
					t.Error("missing fail callback")
				}
			}
			select {
			case report := <-reports:
				t.Errorf("duplicate callback: %v", report)
			default:
			}
			release()
			nextCtx, nextCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer nextCancel()
			nextRelease, err := locker.Acquire(nextCtx, realDir, "next", nil)
			if err != nil {
				t.Fatalf("cancelled waiter leaked lock: %v", err)
			}
			nextRelease()
		})
	}
}
