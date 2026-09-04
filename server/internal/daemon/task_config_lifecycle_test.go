package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

const taskConfigLifecycleSecret = "unique-task-config-lifecycle-sentinel"

func taskConfigLifecycleRef() taskConfigRef {
	return taskConfigRef{
		Provider:    "aws_secrets_manager",
		ProviderRef: "approved/task-config/lifecycle",
		Version:     "version-1",
		Path:        "deploy/terraform/backend.hcl",
		Mode:        0o600,
		Repo:        "github.com/example/infrastructure",
		Target:      "main",
		Account:     "123456789012",
		Region:      "ap-southeast-2",
	}
}

func taskConfigLifecycleTask(id string, ref taskConfigRef, selectors *TaskConfigSelectors) Task {
	refJSON, err := json.Marshal(ref)
	if err != nil {
		panic(err)
	}
	return Task{
		ID:          id,
		WorkspaceID: "workspace-task-config-lifecycle",
		RuntimeID:   "runtime-task-config-lifecycle",
		IssueID:     "issue-task-config-lifecycle",
		AgentID:     "agent-task-config-lifecycle",
		Agent:       &AgentData{ID: "agent-task-config-lifecycle", Name: "test-agent"},
		ProjectResources: []ProjectResourceData{{
			ID:           "resource-task-config-lifecycle",
			ResourceType: "task_config",
			ResourceRef:  refJSON,
		}},
		TaskConfigSelectors: selectors,
	}
}

func newTaskConfigLifecycleDaemon(t *testing.T, task Task, resolve func(http.ResponseWriter, *http.Request), start func(http.ResponseWriter, *http.Request)) (*Daemon, *atomic.Int32, *atomic.Int32, *lockedBuffer, string) {
	t.Helper()
	workspacesRoot := t.TempDir()
	envRoot := execenv.PredictRootDir(execenv.RootDirParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    task.WorkspaceID,
		TaskID:         task.ID,
	})
	var resolveCalls atomic.Int32
	var startCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/configs/"):
			resolveCalls.Add(1)
			resolve(w, r)
		case strings.HasSuffix(r.URL.Path, "/start"):
			startCalls.Add(1)
			start(w, r)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	logs := &lockedBuffer{}
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{task.RuntimeID: {ID: task.RuntimeID, Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			Agents: map[string]AgentEntry{
				// The launch failure is deliberate: it gets runTask through the
				// StartTask gate without starting a real agent process.
				"claude": {Path: filepath.Join(t.TempDir(), "missing-claude")},
			},
		},
	}
	return d, &resolveCalls, &startCalls, logs, envRoot
}

func assertTaskConfigLifecycleClean(t *testing.T, envRoot, rel string) {
	t.Helper()
	target := filepath.Join(envRoot, "workdir", filepath.FromSlash(rel))
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("task_config target %q remains after run, stat err=%v", target, err)
	}
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(target), "*"))
	if err != nil {
		t.Fatalf("glob task_config parent: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("task_config parent retains files after run: %v", entries)
	}
}

// TestRunTaskTaskConfigMaterializesBeforeStartAndCleansAfterLaunchFailure
// covers the positive lifecycle seam with a real daemon client: the resolve
// response becomes a 0600 file before /start, and both the published file and
// its temporary sibling are gone when the agent launch fails.
func TestRunTaskTaskConfigMaterializesBeforeStartAndCleansAfterLaunchFailure(t *testing.T) {
	ref := taskConfigLifecycleRef()
	selectors := &TaskConfigSelectors{Repo: ref.Repo, Target: ref.Target, Account: ref.Account, Region: ref.Region}
	task := taskConfigLifecycleTask("task-config-lifecycle-positive", ref, selectors)
	var (
		d            *Daemon
		resolveCalls *atomic.Int32
		startCalls   *atomic.Int32
		logs         *lockedBuffer
		envRoot      string
	)
	d, resolveCalls, startCalls, logs, envRoot = newTaskConfigLifecycleDaemon(t, task, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(taskConfigLifecycleSecret))
	}, func(w http.ResponseWriter, _ *http.Request) {
		target := filepath.Join(envRoot, "workdir", filepath.FromSlash(ref.Path))
		data, err := os.ReadFile(target)
		if err != nil {
			t.Errorf("resolve file not present before /start: %v", err)
		}
		if string(data) != taskConfigLifecycleSecret {
			t.Errorf("resolve file content = %q, want provider bytes", string(data))
		}
		info, err := os.Stat(target)
		if err != nil {
			return
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("resolve file mode = %o, want 600", info.Mode().Perm())
		}
		w.WriteHeader(http.StatusOK)
	})

	_, _ = d.runTask(context.Background(), task, "claude", 0, slog.New(slog.NewTextHandler(logs, nil)))
	if resolveCalls.Load() != 1 || startCalls.Load() != 1 {
		t.Fatalf("resolve/start calls = %d/%d, want 1/1", resolveCalls.Load(), startCalls.Load())
	}
	assertTaskConfigLifecycleClean(t, envRoot, ref.Path)
	if strings.Contains(logs.String(), taskConfigLifecycleSecret) {
		t.Fatalf("task log contains task_config provider bytes: %s", logs.String())
	}
}

// TestRunTaskTaskConfigNegativeLifecycleMatrix proves that every failure
// before or during the downstream launch is fail-closed and cleanup-safe. The
// HTTP spy is the command boundary: /start is the only downstream transition
// and must remain untouched for provider, preflight, and cancellation errors.
func TestRunTaskTaskConfigNegativeLifecycleMatrix(t *testing.T) {
	ref := taskConfigLifecycleRef()
	matching := &TaskConfigSelectors{Repo: ref.Repo, Target: ref.Target, Account: ref.Account, Region: ref.Region}
	wrong := &TaskConfigSelectors{Repo: ref.Repo, Target: ref.Target, Account: ref.Account, Region: "wrong-region"}
	tests := []struct {
		name             string
		selectors        *TaskConfigSelectors
		resolveErrStatus int
		startErrStatus   int
		cancel           bool
		wantStart        int32
	}{
		{name: "provider failure", selectors: matching, resolveErrStatus: http.StatusBadGateway},
		{name: "preflight selector mismatch", selectors: wrong},
		{name: "start failure", selectors: matching, startErrStatus: http.StatusServiceUnavailable, wantStart: 1},
		{name: "cancelled before provider", selectors: matching, cancel: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := taskConfigLifecycleTask("task-config-lifecycle-"+strings.ReplaceAll(tc.name, " ", "-"), ref, tc.selectors)
			d, resolveCalls, startCalls, logs, envRoot := newTaskConfigLifecycleDaemon(t, task, func(w http.ResponseWriter, _ *http.Request) {
				if tc.resolveErrStatus != 0 {
					http.Error(w, "stable provider failure", tc.resolveErrStatus)
					return
				}
				_, _ = w.Write([]byte(taskConfigLifecycleSecret))
			}, func(w http.ResponseWriter, _ *http.Request) {
				if tc.startErrStatus != 0 {
					http.Error(w, "stable start failure", tc.startErrStatus)
					return
				}
				w.WriteHeader(http.StatusOK)
			})
			ctx := context.Background()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, _ = d.runTask(ctx, task, "claude", 0, slog.New(slog.NewTextHandler(logs, nil)))
			if resolveCalls.Load() > 1 {
				t.Fatalf("resolve calls = %d, want at most 1", resolveCalls.Load())
			}
			if startCalls.Load() != tc.wantStart {
				t.Fatalf("start calls = %d, want %d", startCalls.Load(), tc.wantStart)
			}
			assertTaskConfigLifecycleClean(t, envRoot, ref.Path)
			if strings.Contains(logs.String(), taskConfigLifecycleSecret) {
				t.Fatalf("task log contains task_config provider bytes: %s", logs.String())
			}
		})
	}
}
