package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
		AgentID:     "agent-runtask",
		Agent:       &AgentData{ID: "agent-runtask", Name: "test-agent"},
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

func TestRunTask_InjectsPrivateTaskTempDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	workspacesRoot := filepath.Join(t.TempDir(), strings.Repeat("long-workspaces-root-", 3))
	workspaceID := "ws-private-temp"
	taskID := "task-private-temp-with-long-id-that-would-overflow-socket-paths"
	envRoot := execenv.PredictRootDir(workspacesRoot, workspaceID, taskID)

	captureFile := filepath.Join(t.TempDir(), "agent-env.txt")
	fakeBin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
if [ -d "$TMPDIR" ]; then
  tmpdir_exists=yes
else
  tmpdir_exists=no
fi
printf 'TMPDIR=%s\nTMP=%s\nTEMP=%s\nTMPDIR_EXISTS=%s\n' "$TMPDIR" "$TMP" "$TEMP" "$tmpdir_exists" > "$CAPTURE_FILE"
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"sess-private-temp"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-private-temp","result":"done"}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	if err := os.Chmod(fakeBin, 0o755); err != nil {
		t.Fatalf("chmod fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin, Model: ""},
			},
		},
	}

	task := Task{
		ID:          taskID,
		WorkspaceID: workspaceID,
		RuntimeID:   "rt-1",
		IssueID:     "issue-private-temp",
		AgentID:     "agent-private-temp",
		AuthToken:   "mat_private_temp",
		Agent: &AgentData{
			ID:   "agent-private-temp",
			Name: "test-agent",
			CustomEnv: map[string]string{
				"CAPTURE_FILE": captureFile,
				"TMPDIR":       "/shared/tmp",
				"TMP":          "/shared/tmp",
				"TEMP":         "/shared/tmp",
			},
		},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := d.runTask(context.Background(), task, "claude", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask(): %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("runTask status = %q, want completed (comment=%q)", result.Status, result.Comment)
	}

	raw, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read captured agent env: %v", err)
	}
	got := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("malformed captured env line %q", line)
		}
		got[key] = value
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if got[key] == "" {
			t.Fatalf("%s was not captured", key)
		}
		if got[key] != got["TMPDIR"] {
			t.Fatalf("%s = %q, want same private task temp dir %q", key, got[key], got["TMPDIR"])
		}
	}
	if got["TMPDIR_EXISTS"] != "yes" {
		t.Fatalf("fake agent saw TMPDIR_EXISTS=%q, want yes", got["TMPDIR_EXISTS"])
	}
	taskTempDir := got["TMPDIR"]
	if strings.HasPrefix(taskTempDir, envRoot) {
		t.Fatalf("task temp dir %q must not live under long env root %q", taskTempDir, envRoot)
	}
	if len(taskTempDir) > 80 {
		t.Fatalf("task temp dir %q length = %d, want <= 80 for Unix-socket headroom", taskTempDir, len(taskTempDir))
	}
	if _, err := os.Stat(taskTempDir); !os.IsNotExist(err) {
		t.Fatalf("expected task temp dir %q to be cleaned after run, stat err=%v", taskTempDir, err)
	}
}

// TestTaskTempBaseDir covers the MULTICA_AGENT_TEMP_BASE validation contract:
// Windows ignores it, while Unix honors a valid absolute directory and reports
// unusable configured bases from the real task-directory creation instead of
// silently falling back to /tmp.
func TestTaskTempBaseDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("MULTICA_AGENT_TEMP_BASE", `C:\configured-but-ignored`)
		got, configured, err := taskTempBaseDir()
		if err != nil {
			t.Fatalf("taskTempBaseDir(): %v", err)
		}
		if configured {
			t.Fatal("taskTempBaseDir() marked Windows override as configured")
		}
		if got != socketSafeTempBaseDir() {
			t.Fatalf("taskTempBaseDir() = %q, want platform default %q", got, socketSafeTempBaseDir())
		}
		return
	}

	validBase := t.TempDir()
	cases := []struct {
		name           string
		value          string
		set            bool
		want           string
		wantConfigured bool
		wantErr        bool
	}{
		{name: "unset keeps platform default", set: false, want: socketSafeTempBaseDir()},
		{name: "empty keeps platform default", set: true, value: "  ", want: socketSafeTempBaseDir()},
		{name: "valid absolute dir is honored", set: true, value: validBase, want: validBase, wantConfigured: true},
		{name: "relative path rejected", set: true, value: "relative/base", wantConfigured: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Register the restore hook in both branches: t.Setenv remembers
			// whether the variable was originally set and undoes either case.
			t.Setenv("MULTICA_AGENT_TEMP_BASE", tc.value)
			if !tc.set {
				if err := os.Unsetenv("MULTICA_AGENT_TEMP_BASE"); err != nil {
					t.Fatalf("unset MULTICA_AGENT_TEMP_BASE: %v", err)
				}
			}
			got, configured, err := taskTempBaseDir()
			if configured != tc.wantConfigured {
				t.Fatalf("taskTempBaseDir() configured = %v, want %v", configured, tc.wantConfigured)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("taskTempBaseDir() = %q, want error", got)
				}
				// The message must name the variable the operator set, so the
				// failure is actionable rather than a bare mkdir/stat error.
				if !strings.Contains(err.Error(), "MULTICA_AGENT_TEMP_BASE") {
					t.Fatalf("error %q does not mention MULTICA_AGENT_TEMP_BASE", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("taskTempBaseDir(): %v", err)
			}
			if got != tc.want {
				t.Fatalf("taskTempBaseDir() = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("configured base creates private 0700 task dir", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_TEMP_BASE", validBase)
		dir, err := ensureTaskTempDir("root", "ws", "task")
		if err != nil {
			t.Fatalf("ensureTaskTempDir(): %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat task temp dir: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("task temp dir mode = %o, want 0700", info.Mode().Perm())
		}
	})

	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write notDir fixture: %v", err)
	}
	readOnlyBase := filepath.Join(t.TempDir(), "read-only")
	if err := os.Mkdir(readOnlyBase, 0o500); err != nil {
		t.Fatalf("mkdir readOnlyBase fixture: %v", err)
	}
	// t.TempDir cleanup needs to descend into it again.
	t.Cleanup(func() { _ = os.Chmod(readOnlyBase, 0o700) })

	for _, tc := range []struct {
		name string
		base string
	}{
		{name: "missing dir rejected", base: filepath.Join(validBase, "missing")},
		{name: "non-directory rejected", base: notDir},
		{name: "non-writable dir rejected", base: readOnlyBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_AGENT_TEMP_BASE", tc.base)
			dir, err := ensureTaskTempDir("root", "ws", "task")
			if err == nil {
				_ = os.RemoveAll(dir)
				if tc.base == readOnlyBase {
					t.Skip("process can write to the read-only fixture")
				}
				t.Fatalf("ensureTaskTempDir() = %q with unusable MULTICA_AGENT_TEMP_BASE, want error", dir)
			}
			if !strings.Contains(err.Error(), "MULTICA_AGENT_TEMP_BASE") {
				t.Fatalf("error %q does not mention MULTICA_AGENT_TEMP_BASE", err)
			}
		})
	}
}

// TestRunTask_TaskTempBaseOverride is the MULTICA_AGENT_TEMP_BASE counterpart
// of TestRunTask_InjectsPrivateTaskTempDir: with the variable set, all three
// temp vars point at one fresh private dir under the configured base, agent
// custom_env still cannot override them, and the dir is removed on task exit.
func TestRunTask_TaskTempBaseOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	tempBase := t.TempDir()
	t.Setenv("MULTICA_AGENT_TEMP_BASE", tempBase)

	workspacesRoot := t.TempDir()
	workspaceID := "ws-temp-base"
	taskID := "task-temp-base"

	captureFile := filepath.Join(t.TempDir(), "agent-env.txt")
	fakeBin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
printf 'TMPDIR=%s\nTMP=%s\nTEMP=%s\n' "$TMPDIR" "$TMP" "$TEMP" > "$CAPTURE_FILE"
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"sess-temp-base"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-temp-base","result":"done"}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin, Model: ""},
			},
		},
	}

	task := Task{
		ID:          taskID,
		WorkspaceID: workspaceID,
		RuntimeID:   "rt-1",
		IssueID:     "issue-temp-base",
		AgentID:     "agent-temp-base",
		AuthToken:   "mat_temp_base",
		Agent: &AgentData{
			ID:   "agent-temp-base",
			Name: "test-agent",
			CustomEnv: map[string]string{
				"CAPTURE_FILE": captureFile,
				"TMPDIR":       "/shared/tmp",
				"TMP":          "/shared/tmp",
				"TEMP":         "/shared/tmp",
			},
		},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := d.runTask(context.Background(), task, "claude", 0, taskLog)
	if err != nil {
		t.Fatalf("runTask(): %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("runTask status = %q, want completed (comment=%q)", result.Status, result.Comment)
	}

	raw, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read captured agent env: %v", err)
	}
	got := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("malformed captured env line %q", line)
		}
		got[key] = value
	}
	taskTempDir := got["TMPDIR"]
	if taskTempDir == "" {
		t.Fatal("TMPDIR was not captured")
	}
	for _, key := range []string{"TMP", "TEMP"} {
		if got[key] != taskTempDir {
			t.Fatalf("%s = %q, want same private task temp dir %q", key, got[key], taskTempDir)
		}
	}
	if filepath.Dir(taskTempDir) != tempBase {
		t.Fatalf("task temp dir %q is not directly under configured base %q", taskTempDir, tempBase)
	}
	if _, err := os.Stat(taskTempDir); !os.IsNotExist(err) {
		t.Fatalf("expected task temp dir %q to be cleaned after run, stat err=%v", taskTempDir, err)
	}
}

// TestRunTask_TaskTempBaseInvalidFailsStartup pins the "no silent fallback"
// half of the contract at the level operators experience it: an unusable
// MULTICA_AGENT_TEMP_BASE fails the task with a message naming the variable,
// and the agent never starts against a /tmp dir it did not ask for.
func TestRunTask_TaskTempBaseInvalidFailsStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	missingBase := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("MULTICA_AGENT_TEMP_BASE", missingBase)

	workspacesRoot := t.TempDir()
	captureFile := filepath.Join(t.TempDir(), "agent-env.txt")
	fakeBin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
printf 'ran\n' > "$CAPTURE_FILE"
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin, Model: ""},
			},
		},
	}

	task := Task{
		ID:          "task-temp-base-invalid",
		WorkspaceID: "ws-temp-base-invalid",
		RuntimeID:   "rt-1",
		IssueID:     "issue-temp-base-invalid",
		AgentID:     "agent-temp-base-invalid",
		AuthToken:   "mat_temp_base_invalid",
		Agent: &AgentData{
			ID:        "agent-temp-base-invalid",
			Name:      "test-agent",
			CustomEnv: map[string]string{"CAPTURE_FILE": captureFile},
		},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := d.runTask(context.Background(), task, "claude", 0, taskLog)
	if err == nil {
		t.Fatal("runTask() succeeded with an unusable MULTICA_AGENT_TEMP_BASE, want failure")
	}
	if !strings.Contains(err.Error(), "MULTICA_AGENT_TEMP_BASE") {
		t.Fatalf("runTask() error = %v, want it to name MULTICA_AGENT_TEMP_BASE", err)
	}
	if _, statErr := os.Stat(captureFile); !os.IsNotExist(statErr) {
		t.Fatalf("agent ran despite the temp-base failure, stat err=%v", statErr)
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
		AgentID:     "agent-runtask-start-lease",
		Agent:       &AgentData{ID: "agent-runtask-start-lease", Name: "test-agent"},
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

type prepareLeaseCountingTransport struct {
	base  http.RoundTripper
	calls *atomic.Int64
}

func (t *prepareLeaseCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/prepare-lease") {
		t.calls.Add(1)
	}
	return t.base.RoundTrip(req)
}

func TestRunTask_PrepareTimeoutStopsLeaseDuringBlockedStartTask(t *testing.T) {
	oldRefresh := taskPrepareLeaseRefresh
	oldTimeout := taskPrepareLeaseTimeout
	taskPrepareLeaseRefresh = 10 * time.Millisecond
	taskPrepareLeaseTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		taskPrepareLeaseRefresh = oldRefresh
		taskPrepareLeaseTimeout = oldTimeout
	})

	var leaseCalls atomic.Int64
	startEntered := make(chan struct{})
	var closeStartOnce sync.Once
	releaseStart := make(chan struct{})
	var releaseStartOnce sync.Once
	t.Cleanup(func() { releaseStartOnce.Do(func() { close(releaseStart) }) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/prepare-lease"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/start"):
			closeStartOnce.Do(func() { close(startEntered) })
			<-releaseStart
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	// Count requests where the extender starts them. A cancelled RoundTrip can
	// return before httptest schedules its handler, so counting in the handler
	// can make an already-in-flight request look like post-timeout activity.
	client := NewClient(srv.URL)
	client.client.Transport = &prepareLeaseCountingTransport{
		base:  client.client.Transport,
		calls: &leaseCalls,
	}

	workspacesRoot := t.TempDir()
	fakeBin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	d := &Daemon{
		client:             client,
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots:     make(map[string]int),
		taskPrepareTimeout: 150 * time.Millisecond,
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin},
			},
		},
	}

	task := Task{
		ID:          "task-runtask-start-timeout",
		WorkspaceID: "ws-runtask-start-timeout",
		RuntimeID:   "rt-1",
		IssueID:     "issue-runtask-start-timeout",
		AgentID:     "agent-runtask-start-timeout",
		Agent:       &AgentData{ID: "agent-runtask-start-timeout", Name: "test-agent"},
	}
	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	startedAt := time.Now()
	_, err := d.runTask(context.Background(), task, "claude", 0, taskLog)
	if !errors.Is(err, errTaskPrepareTimeout) {
		t.Fatalf("runTask error = %v, want task prepare timeout", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("runTask took %s, want prepare deadline to stop blocked /start", elapsed)
	}
	select {
	case <-startEntered:
	default:
		t.Fatal("runTask did not reach /start")
	}
	releaseStartOnce.Do(func() { close(releaseStart) })
	if got := leaseCalls.Load(); got == 0 {
		t.Fatal("prepare lease request was never started while /start was blocked")
	}
	leaseCallsAtReturn := leaseCalls.Load()
	lastLeaseCalls := leaseCallsAtReturn
	stableReads := 0
	deadline := time.Now().Add(12 * taskPrepareLeaseRefresh)
	for stableReads < 3 && time.Now().Before(deadline) {
		time.Sleep(taskPrepareLeaseRefresh)
		got := leaseCalls.Load()
		if got == lastLeaseCalls {
			stableReads++
			continue
		}
		lastLeaseCalls = got
		stableReads = 0
	}
	if stableReads < 3 {
		t.Fatalf("prepare lease kept extending after timeout: calls %d -> %d", leaseCallsAtReturn, leaseCalls.Load())
	}
	if got := taskRunFailureReason(err); got != "timeout" {
		t.Fatalf("taskRunFailureReason = %q, want retryable platform timeout", got)
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
