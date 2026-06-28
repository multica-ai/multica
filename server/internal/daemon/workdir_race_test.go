package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// TestHandleTask_DoesNotCallStartTaskItself is the regression guard for
// issue #3999 race A. handleTask must not call /tasks/{id}/start before
// runner.run — the runner is now responsible for calling StartTask only
// after execenv.Prepare/Reuse has put env.WorkDir on disk, so consumers
// that read status==running can resolve the workdir path without racing
// the daemon's os.MkdirAll.
//
// Before the fix: handleTask called StartTask before invoking the runner,
// flipping the server-side state to "running" while the per-task workdir
// still didn't exist on disk. Hermes/OpenClaw agents that resolved
// /multica_workspaces/{ws}/{short-id}/workdir from the running signal
// would then hit FileNotFoundError.
func TestHandleTask_DoesNotCallStartTaskItself(t *testing.T) {
	t.Parallel()

	var (
		startCalls   atomic.Int64
		runnerCalled atomic.Bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			startCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots:     make(map[string]int),
		cancelPollInterval: time.Hour, // disable poll-cancel path; we only care about the entry-side ordering
	}

	// Fake runner that does NOT call StartTask — production runTask does
	// the call itself, after Prepare/Reuse confirms env.WorkDir on disk.
	d.runner = taskRunnerFunc(func(_ context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		runnerCalled.Store(true)
		return TaskResult{Status: "completed"}, nil
	})

	task := Task{
		ID:          "task-no-start",
		WorkspaceID: "ws-no-start",
		RuntimeID:   "rt-1",
		IssueID:     "issue-no-start",
		Agent:       &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	if !runnerCalled.Load() {
		t.Fatal("fake runner was never invoked — handleTask aborted before runner.run, can't assert ordering")
	}
	if got := startCalls.Load(); got != 0 {
		t.Fatalf("handleTask called /start %d time(s); StartTask must be runTask's responsibility now (issue #3999 race A)", got)
	}
}

// TestRunTask_StartTaskCalledAfterWorkdirOnDisk is the behavioral regression
// guard for issue #3999 race A. Calls runTask directly with a missing agent
// binary so the run aborts at exec time — but only AFTER reaching the
// post-Prepare StartTask call. The fake server records whether the per-task
// workdir already exists on disk at the moment /start is hit; before the
// fix it did not.
func TestRunTask_StartTaskCalledAfterWorkdirOnDisk(t *testing.T) {
	t.Parallel()

	workspacesRoot := t.TempDir()
	workspaceID := "ws-runtask"
	taskID := "task-runtask-after-mkdir"
	expectedEnvRoot := execenv.PredictRootDir(workspacesRoot, workspaceID, taskID)
	expectedWorkDir := filepath.Join(expectedEnvRoot, "workdir")

	var (
		startCalled   atomic.Bool
		workdirOnDisk atomic.Bool
		envRootOnDisk atomic.Bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			startCalled.Store(true)
			if info, err := os.Stat(expectedWorkDir); err == nil && info.IsDir() {
				workdirOnDisk.Store(true)
			}
			if info, err := os.Stat(expectedEnvRoot); err == nil && info.IsDir() {
				envRootOnDisk.Store(true)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Provider entry intentionally points at a non-existent binary: runTask
	// reaches Prepare → StartTask → ReportProgress before agent.Backend.Run
	// fails at exec time. We don't care about the eventual error; the
	// regression guard is the order of /start vs. os.MkdirAll(envRoot).
	missingBin := filepath.Join(t.TempDir(), "definitely-not-claude")
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			Agents: map[string]AgentEntry{
				"claude": {Path: missingBin, Model: ""},
			},
		},
	}

	task := Task{
		ID:          taskID,
		WorkspaceID: workspaceID,
		RuntimeID:   "rt-1",
		IssueID:     "issue-runtask",
		Agent:       &AgentData{Name: "test-agent"},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The Run() failure is expected; we only assert the pre-Run ordering.
	_, _ = d.runTask(context.Background(), task, "claude", 0, taskLog)

	if !startCalled.Load() {
		t.Fatal("runTask did not call /start — Fix A's StartTask placement is missing")
	}
	if !envRootOnDisk.Load() {
		t.Fatal("envRoot did not exist on disk when /start was called — Prepare must run before StartTask (issue #3999 race A)")
	}
	if !workdirOnDisk.Load() {
		t.Fatal("envRoot/workdir did not exist on disk when /start was called — os.MkdirAll must complete before StartTask (issue #3999 race A)")
	}
}

func TestRunTask_ExtendsPrepareLeaseDuringStartTask(t *testing.T) {
	oldRefresh := taskPrepareLeaseRefresh
	oldTimeout := taskPrepareLeaseTimeout
	taskPrepareLeaseRefresh = 10 * time.Millisecond
	taskPrepareLeaseTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		taskPrepareLeaseRefresh = oldRefresh
		taskPrepareLeaseTimeout = oldTimeout
	})

	workspacesRoot := t.TempDir()
	workspaceID := "ws-runtask-start-lease"
	taskID := "task-runtask-start-lease"
	var (
		startEntered     atomic.Bool
		leaseDuringStart atomic.Bool
		closeLeaseOnce   sync.Once
	)
	leaseSeenDuringStart := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/prepare-lease"):
			if startEntered.Load() {
				leaseDuringStart.Store(true)
				closeLeaseOnce.Do(func() { close(leaseSeenDuringStart) })
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/start"):
			startEntered.Store(true)
			select {
			case <-leaseSeenDuringStart:
			case <-time.After(2 * time.Second):
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	missingBin := filepath.Join(t.TempDir(), "definitely-not-claude")
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			Agents: map[string]AgentEntry{
				"claude": {Path: missingBin, Model: ""},
			},
		},
	}

	task := Task{
		ID:          taskID,
		WorkspaceID: workspaceID,
		RuntimeID:   "rt-1",
		IssueID:     "issue-runtask-start-lease",
		Agent:       &AgentData{Name: "test-agent"},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, _ = d.runTask(context.Background(), task, "claude", 0, taskLog)

	if !startEntered.Load() {
		t.Fatal("runTask did not call /start")
	}
	if !leaseDuringStart.Load() {
		t.Fatal("prepare lease was not extended while /start was still in flight")
	}
}

// TestHandleTask_KeepsEnvRootActiveAcrossCompletion is the regression guard
// for issue #3999 race B. After runner.run returns, the in-process active
// guard installed inside runTask (defer unmarkActiveEnvRoot at the
// goroutine's exit) has already fired by the time handleTask calls
// reportTaskResult and execenv.WriteGCMeta. Without an outer guard at the
// handleTask level, the GC loop sees a window where the directory has
// neither isActiveEnvRoot nor a .gc_meta.json file — falling through to
// orphanByMTime, gated only by the 72h GCOrphanTTL.
//
// This test fakes the inner guard's lifecycle (mark + deferred unmark),
// then asserts that at the moment /complete is hit (i.e. between runner.run
// returning and WriteGCMeta running), isActiveEnvRoot(envRoot) is still
// true thanks to the outer guard handleTask installs.
func TestHandleTask_KeepsEnvRootActiveAcrossCompletion(t *testing.T) {
	t.Parallel()

	workspacesRoot := t.TempDir()
	workspaceID := "ws-active-during-complete"
	taskID := "task-active-during-complete"
	expectedEnvRoot := execenv.PredictRootDir(workspacesRoot, workspaceID, taskID)

	var (
		completeCalled   atomic.Bool
		activeAtComplete atomic.Bool
	)

	d := &Daemon{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots:     make(map[string]int),
		cancelPollInterval: time.Hour,
		cfg:                Config{WorkspacesRoot: workspacesRoot},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/complete") {
			completeCalled.Store(true)
			// This is the exact window race B exposed: the inner deferred
			// unmark has already fired (see fake runner below); only the
			// outer guard installed by handleTask keeps the env root in the
			// active set at this moment.
			if d.isActiveEnvRoot(expectedEnvRoot) {
				activeAtComplete.Store(true)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d.client = NewClient(srv.URL)

	// Fake runner mimics the real runTask's mark/defer-unmark pair. Without
	// the outer guard added in handleTask, the deferred unmark would bring
	// isActiveEnvRoot back to false before reportTaskResult fires.
	d.runner = taskRunnerFunc(func(_ context.Context, tk Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		predicted := execenv.PredictRootDir(d.cfg.WorkspacesRoot, tk.WorkspaceID, tk.ID)
		d.markActiveEnvRoot(predicted)
		defer d.unmarkActiveEnvRoot(predicted)
		return TaskResult{
			Status:  "completed",
			EnvRoot: predicted,
		}, nil
	})

	task := Task{
		ID:          taskID,
		WorkspaceID: workspaceID,
		RuntimeID:   "rt-1",
		IssueID:     "issue-active-during-complete",
		Agent:       &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	if !completeCalled.Load() {
		t.Fatal("/complete was never hit — handleTask did not reach reportTaskResult")
	}
	if !activeAtComplete.Load() {
		t.Fatal("env root was NOT in the active set at /complete time — issue #3999 race B regression: GC could reclaim the directory between runner.run returning and WriteGCMeta landing on disk")
	}
	// And the outer guard must have been released by the time handleTask
	// returned, otherwise we'd be leaking active marks across tasks.
	if d.isActiveEnvRoot(expectedEnvRoot) {
		t.Fatal("env root remained active after handleTask returned — outer guard's deferred unmark did not fire")
	}
}
