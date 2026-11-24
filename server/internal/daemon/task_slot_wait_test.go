package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type executionCapacityWaitWriter struct {
	signal chan struct{}
	once   sync.Once
}

func (w *executionCapacityWaitWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "local_directory: waiting for execution capacity") {
		w.once.Do(func() { close(w.signal) })
	}
	return len(p), nil
}

func TestRunBatchPollerDirectoryWaitReleasesExecutionCapacity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: "daemon"})
	if err != nil {
		t.Fatal(err)
	}
	resource := ProjectResourceData{ID: "resource", ResourceType: localDirectoryResourceType, ResourceRef: ref}
	holder := Task{ID: "holder", RuntimeID: "local", IssueID: "holder-issue", AgentID: "holder-agent", Agent: &AgentData{Name: "holder"}, ProjectResources: []ProjectResourceData{resource}}
	waiter := Task{ID: "waiter", RuntimeID: "local", IssueID: "waiter-issue", AgentID: "waiter-agent", Agent: &AgentData{Name: "waiter"}, ProjectResources: []ProjectResourceData{resource}}
	unrelated := Task{ID: "unrelated", RuntimeID: "other", IssueID: "other-issue", AgentID: "other-agent", Agent: &AgentData{Name: "other"}}

	holderStarted := make(chan struct{})
	waiting := make(chan struct{})
	unrelatedStarted := make(chan struct{})
	waiterStarted := make(chan struct{})
	finishHolder := make(chan struct{})
	finishUnrelated := make(chan struct{})
	var finishHolderOnce, finishUnrelatedOnce, waitingOnce sync.Once
	var claims atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim"):
			var tasks []Task
			switch claims.Add(1) {
			case 1:
				tasks = []Task{holder}
			case 2:
				select {
				case <-holderStarted:
					tasks = []Task{waiter}
				case <-r.Context().Done():
					return
				}
			case 3:
				tasks = []Task{unrelated}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		case strings.HasSuffix(r.URL.Path, "/tasks/waiter/wait-local-directory"):
			waitingOnce.Do(func() { close(waiting) })
			_, _ = w.Write([]byte("{}"))
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "dispatched"})
		}
	}))
	t.Cleanup(srv.Close)

	d := New(Config{ServerBaseURL: srv.URL, DaemonID: "daemon", WorkspacesRoot: t.TempDir(), PollInterval: 10 * time.Millisecond, MaxConcurrentTasks: 2}, slog.New(slog.NewTextHandler(noopWriter{}, nil)))
	d.workspaces["workspace"] = &workspaceState{workspaceID: "workspace", runtimeIDs: []string{"local", "other"}}
	d.runtimeIndex["local"] = Runtime{ID: "local", Provider: "codex"}
	d.runtimeIndex["other"] = Runtime{ID: "other", Provider: "claude"}
	d.cancelPollInterval = time.Hour
	d.taskSlotWait = 20 * time.Millisecond

	var mu sync.Mutex
	inUse := map[int]string{}
	usedSlots := map[string]int{}
	peak := 0
	d.runner = taskRunnerFunc(func(ctx context.Context, task Task, _ string, slot int, _ *slog.Logger) (TaskResult, error) {
		mu.Lock()
		if owner, used := inUse[slot]; used {
			t.Errorf("task %s reused slot %d while %s was running", task.ID, slot, owner)
		}
		inUse[slot] = task.ID
		usedSlots[task.ID] = slot
		if len(inUse) > peak {
			peak = len(inUse)
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			delete(inUse, slot)
			mu.Unlock()
		}()
		var finish <-chan struct{}
		switch task.ID {
		case "holder":
			close(holderStarted)
			finish = finishHolder
		case "unrelated":
			close(unrelatedStarted)
			finish = finishUnrelated
		case "waiter":
			close(waiterStarted)
			return TaskResult{Status: "completed"}, nil
		}
		select {
		case <-finish:
			return TaskResult{Status: "completed"}, nil
		case <-ctx.Done():
			return TaskResult{Status: "cancelled"}, ctx.Err()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	var taskWG sync.WaitGroup
	sem := newTaskSlotSemaphore(2)
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		d.runBatchPoller(ctx, ctx, sem, make(chan struct{}, 1), &taskWG)
	}()
	t.Cleanup(func() {
		cancel()
		finishHolderOnce.Do(func() { close(finishHolder) })
		finishUnrelatedOnce.Do(func() { close(finishUnrelated) })
		<-pollDone
		taskWG.Wait()
	})
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("second task never parked on the holder's directory")
	}
	select {
	case <-unrelatedStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("parked directory waiter pinned execution capacity for an unrelated runtime")
	}
	if got := d.activeTasks.Load(); got != 3 {
		t.Fatalf("owned tasks = %d, want holder, waiter and unrelated task", got)
	}
	if got := d.resourceWaitTasks.Load(); got != 1 {
		t.Fatalf("resource waiters = %d, want 1", got)
	}
	if got := d.localPathLocks.Holder(realDir); got != "holder" {
		t.Fatalf("directory holder = %q, want holder", got)
	}
	finishHolderOnce.Do(func() { close(finishHolder) })
	select {
	case <-waiterStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("waiter did not resume after the holder released its directory and slot")
	}
	mu.Lock()
	defer mu.Unlock()
	if peak > 2 {
		t.Errorf("peak running tasks = %d, want at most 2", peak)
	}
	if usedSlots["waiter"] == usedSlots["unrelated"] {
		t.Errorf("resumed waiter reused unrelated task's occupied slot: %v", usedSlots)
	}
}

func TestDirectoryWaitCancellationWhileReacquiringSlot(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"server cancellation", "daemon shutdown"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			realDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: "daemon"})
			if err != nil {
				t.Fatal(err)
			}
			waiting := make(chan struct{})
			waitingCapacity := make(chan struct{})
			var waitingOnce sync.Once
			var serverCancelled atomic.Bool
			var prepareLeases atomic.Int64
			failReasons := make(chan string, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/prepare-lease"):
					prepareLeases.Add(1)
				case strings.HasSuffix(r.URL.Path, "/wait-local-directory"):
					waitingOnce.Do(func() { close(waiting) })
				case strings.HasSuffix(r.URL.Path, "/fail"):
					var body struct {
						FailureReason string `json:"failure_reason"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode failure: %v", err)
					}
					failReasons <- body.FailureReason
				}
				status := "dispatched"
				if serverCancelled.Load() {
					status = "cancelled"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
			}))
			t.Cleanup(srv.Close)
			d := New(Config{ServerBaseURL: srv.URL, DaemonID: "daemon"}, slog.New(slog.NewTextHandler(&executionCapacityWaitWriter{signal: waitingCapacity}, &slog.HandlerOptions{Level: slog.LevelDebug})))
			d.cancelPollInterval = 10 * time.Millisecond
			d.prepareLeaseRefresh = 5 * time.Millisecond
			releaseHolder, err := d.localPathLocks.Acquire(context.Background(), realDir, "holder", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(releaseHolder)
			slots := newTaskSlotSemaphore(1)
			lease := newTaskSlotLease(slots, <-slots, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				release func()
				abort   bool
			}
			done := make(chan result, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				release, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, Task{ID: "waiter", RuntimeID: "runtime", ProjectResources: []ProjectResourceData{{ID: "resource", ResourceType: localDirectoryResourceType, ResourceRef: ref}}}, d.logger, lease)
				done <- result{release: release, abort: abort}
			}()
			t.Cleanup(func() {
				cancel()
				releaseHolder()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("directory waiter did not stop during cleanup")
				}
			})
			select {
			case <-waiting:
			case <-time.After(3 * time.Second):
				t.Fatal("task did not enter the directory wait")
			}
			var occupied int
			select {
			case occupied = <-slots:
			case <-time.After(3 * time.Second):
				t.Fatal("directory wait did not release its execution slot")
			}
			releaseHolder()
			select {
			case <-waitingCapacity:
			case <-time.After(3 * time.Second):
				t.Fatal("waiter did not reach execution-capacity admission")
			}
			if holder := d.localPathLocks.Holder(realDir); holder != "" {
				t.Fatalf("waiter kept directory %q while waiting for execution capacity", holder)
			}
			previousLeases := prepareLeases.Load()
			leaseDeadline := time.After(3 * time.Second)
			for prepareLeases.Load() <= previousLeases {
				select {
				case <-leaseDeadline:
					t.Fatal("prepare lease stopped while the task waited for execution capacity")
				case <-time.After(time.Millisecond):
				}
			}
			if mode == "server cancellation" {
				serverCancelled.Store(true)
			} else {
				cancel()
			}
			select {
			case got := <-done:
				if !got.abort || got.release != nil {
					if got.release != nil {
						got.release()
					}
					t.Fatalf("cancelled slot reacquisition returned abort=%v release=%v", got.abort, got.release != nil)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("slot reacquisition ignored cancellation")
			}
			if holder := d.localPathLocks.Holder(realDir); holder != "" {
				t.Errorf("cancelled waiter kept the directory: %q", holder)
			}
			if n := d.resourceWaitTasks.Load(); n != 0 {
				t.Errorf("resource wait count = %d after cancellation", n)
			}
			lease.release()
			if len(slots) != 0 {
				t.Fatal("cancelled waiter returned another task's execution slot")
			}
			slots <- occupied
			if mode == "server cancellation" {
				if len(failReasons) != 0 {
					t.Fatal("server-cancelled task received another failure report")
				}
			} else {
				select {
				case reason := <-failReasons:
					if reason != "runtime_recovery" {
						t.Errorf("shutdown failure reason = %q, want runtime_recovery", reason)
					}
				default:
					t.Fatal("daemon shutdown did not report the owned waiter for recovery")
				}
			}
		})
	}
}

func TestDirectoryFastPathsRetainExecutionSlot(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"direct", "worktree", "chat", "no directory"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ref := localDirectoryRef{LocalPath: dir, DaemonID: "daemon"}
			if mode == "worktree" {
				ref.ExecutionMode = "worktree"
			}
			raw, err := json.Marshal(ref)
			if err != nil {
				t.Fatal(err)
			}
			task := Task{ID: "task", ProjectResources: []ProjectResourceData{{ID: "resource", ResourceType: localDirectoryResourceType, ResourceRef: raw}}}
			if mode == "chat" {
				task.ChatSessionID = "chat"
			}
			if mode == "no directory" {
				task.ProjectResources = nil
			}
			d := &Daemon{cfg: Config{DaemonID: "daemon"}, localPathLocks: NewLocalPathLocker(), logger: slog.New(slog.NewTextHandler(noopWriter{}, nil))}
			slots := newTaskSlotSemaphore(1)
			lease := newTaskSlotLease(slots, <-slots, nil)
			release, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), task, d.logger, lease)
			if release != nil {
				defer release()
			}
			if abort {
				t.Fatal("uncontended task aborted")
			}
			if len(slots) != 0 || lease.index != 0 {
				t.Fatal("uncontended or exempt task returned its execution slot")
			}
			if (release != nil) != (mode == "direct") {
				t.Fatalf("path lock held=%v for %s", release != nil, mode)
			}
			lease.release()
			if len(slots) != 1 {
				t.Fatal("task did not return its slot on completion")
			}
		})
	}
}

func TestDirectDirectoryWaitDoesNotBlockWorktreeSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: "daemon"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{})
	waitingCapacity := make(chan struct{})
	var waitingOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/wait-local-directory") {
			waitingOnce.Do(func() { close(waiting) })
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "dispatched"})
	}))
	t.Cleanup(srv.Close)
	d := New(Config{ServerBaseURL: srv.URL, DaemonID: "daemon"}, slog.New(slog.NewTextHandler(&executionCapacityWaitWriter{signal: waitingCapacity}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	d.cancelPollInterval = time.Hour
	releaseInitial, err := d.localPathLocks.Acquire(context.Background(), realDir, "initial", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	slots := newTaskSlotSemaphore(1)
	lease := newTaskSlotLease(slots, <-slots, nil)
	directDone := make(chan bool, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		release, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, Task{ID: "direct", ProjectResources: []ProjectResourceData{{ID: "resource", ResourceType: localDirectoryResourceType, ResourceRef: ref}}}, d.logger, lease)
		if release != nil {
			release()
		}
		lease.release()
		directDone <- abort
	}()
	t.Cleanup(func() {
		cancel()
		releaseInitial()
		wg.Wait()
	})
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("Direct task did not park behind the initial directory holder")
	}
	var snapshotSlot int
	select {
	case snapshotSlot = <-slots:
	case <-ctx.Done():
		t.Fatal("Direct waiter did not return its execution capacity")
	}
	snapshotDone := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { slots <- snapshotSlot }()
		select {
		case <-waitingCapacity:
		case <-ctx.Done():
			snapshotDone <- ctx.Err()
			return
		}
		// Worktree preparation already owns its execution slot when it takes
		// this same mutex to snapshot a source also used in Direct mode.
		release, err := d.localPathLocks.Acquire(ctx, realDir, "snapshot", nil)
		if release != nil {
			release()
		}
		snapshotDone <- err
	}()
	releaseInitial()
	if err := <-snapshotDone; err != nil || ctx.Err() != nil {
		t.Fatalf("worktree snapshot could not progress while Direct waited for its slot: %v", err)
	}
	if abort := <-directDone; abort || ctx.Err() != nil {
		t.Fatal("Direct task did not reacquire its directory and capacity after the snapshot")
	}
	if got := len(slots); got != 1 {
		t.Fatalf("free slots after mixed-mode preparation = %d, want 1", got)
	}
}
