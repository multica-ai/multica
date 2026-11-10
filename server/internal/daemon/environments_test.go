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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/localenv"
)

type recordingEnvironmentReporter struct {
	mu    sync.Mutex
	calls []environmentReportCall
	err   error
}

type environmentReportCall struct {
	taskID string
	envs   []TaskEnvironmentReport
}

func (r *recordingEnvironmentReporter) ReportTaskEnvironments(_ context.Context, taskID string, envs []TaskEnvironmentReport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, environmentReportCall{taskID: taskID, envs: envs})
	return r.err
}

func (r *recordingEnvironmentReporter) take() []environmentReportCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := r.calls
	r.calls = nil
	return calls
}

func newTestEnvironmentService(t *testing.T) (*environmentService, *recordingEnvironmentReporter) {
	t.Helper()
	reporter := &recordingEnvironmentReporter{}
	s := newEnvironmentService(Config{WorkspacesRoot: t.TempDir()}, reporter, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s, reporter
}

func detectedEnv(taskID, key string) localenv.Environment {
	return localenv.Environment{
		Key: key, TaskID: taskID, IssueID: "issue-" + taskID, Source: localenv.SourceDetected,
		Name: "node", URL: "http://localhost:5173", Port: 5173, State: localenv.StateRunning,
		StartedAt: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC),
	}
}

func TestEnvironmentReportSendsChangesHeartbeatsAndAFinalEmptySnapshot(t *testing.T) {
	s, reporter := newTestEnvironmentService(t)
	ctx := context.Background()
	now := time.Now()

	s.report(ctx, now)
	if calls := reporter.take(); len(calls) != 0 {
		t.Fatalf("reported %d snapshots with nothing running", len(calls))
	}

	s.detected = []localenv.Environment{detectedEnv("task-a", "detected:aaa")}
	s.report(ctx, now)
	calls := reporter.take()
	if len(calls) != 1 || calls[0].taskID != "task-a" || len(calls[0].envs) != 1 || calls[0].envs[0].URL != "http://localhost:5173" {
		t.Fatalf("first snapshot = %+v", calls)
	}

	s.report(ctx, now.Add(20*time.Second))
	if calls := reporter.take(); len(calls) != 0 {
		t.Fatalf("an unchanged snapshot was re-sent before the heartbeat: %+v", calls)
	}
	s.report(ctx, now.Add(environmentHeartbeat+time.Second))
	if calls := reporter.take(); len(calls) != 1 {
		t.Fatalf("heartbeat sent %d snapshots, want 1", len(calls))
	}

	s.detected = nil
	s.report(ctx, now.Add(2*environmentHeartbeat))
	calls = reporter.take()
	if len(calls) != 1 || calls[0].taskID != "task-a" || len(calls[0].envs) != 0 {
		t.Fatalf("after the server stopped = %+v, want one empty snapshot", calls)
	}
	s.report(ctx, now.Add(3*environmentHeartbeat))
	if calls := reporter.take(); len(calls) != 0 {
		t.Fatalf("an empty snapshot was sent again: %+v", calls)
	}
}

func TestEnvironmentReportRetriesFailuresAndDropsRefusedTasks(t *testing.T) {
	s, reporter := newTestEnvironmentService(t)
	ctx := context.Background()
	now := time.Now()
	s.detected = []localenv.Environment{detectedEnv("task-a", "detected:aaa")}

	reporter.err = errors.New("connection refused")
	s.report(ctx, now)
	reporter.err = nil
	s.report(ctx, now.Add(time.Second))
	if calls := reporter.take(); len(calls) != 2 {
		t.Fatalf("a failed snapshot was not retried: %d calls", len(calls))
	}

	s.detected = []localenv.Environment{detectedEnv("task-b", "detected:bbb")}
	reporter.err = &requestError{StatusCode: http.StatusNotFound}
	s.report(ctx, now.Add(2*time.Second))
	reporter.take()
	if _, ok := s.reported["task-b"]; ok {
		t.Fatal("a task the server refused is still tracked")
	}
}

func TestEnvironmentReportKeepsADetectedServerOnItsFirstTask(t *testing.T) {
	s, reporter := newTestEnvironmentService(t)
	ctx := context.Background()
	now := time.Now()
	s.detected = []localenv.Environment{detectedEnv("task-a", "detected:aaa")}
	s.report(ctx, now)
	reporter.take()

	// A later task reuses the directory; the same server is still task-a's.
	s.detected = []localenv.Environment{detectedEnv("task-b", "detected:aaa")}
	s.report(ctx, now.Add(time.Second))
	if calls := reporter.take(); len(calls) != 0 {
		t.Fatalf("the server moved tasks: %+v", calls)
	}
}

func TestEnvironmentServiceClosesOutTasksReportedBeforeARestart(t *testing.T) {
	s, reporter := newTestEnvironmentService(t)
	s.detected = []localenv.Environment{detectedEnv("task-a", "detected:aaa")}
	s.report(context.Background(), time.Now())
	reporter.take()

	restarted := newEnvironmentService(Config{WorkspacesRoot: filepath.Dir(filepath.Dir(s.stateDir))}, reporter, nil, s.logger)
	restarted.loadReported()
	restarted.report(context.Background(), time.Now())
	calls := reporter.take()
	if len(calls) != 1 || calls[0].taskID != "task-a" || len(calls[0].envs) != 0 {
		t.Fatalf("after restart = %+v, want an empty snapshot closing task-a", calls)
	}
}

func TestEnvironmentResolveAttributesRunningAndFinishedTasks(t *testing.T) {
	s, _ := newTestEnvironmentService(t)
	running := filepath.Join(s.workspacesRoot, "ws-1", "task-running")
	finished := filepath.Join(s.workspacesRoot, "ws-1", "task-finished")
	for _, dir := range []string{filepath.Join(running, "workdir", "app"), filepath.Join(finished, "workdir")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	untrack := s.trackTask(running, localenv.Task{TaskID: "t-run", IssueID: "i-run", WorkspaceID: "ws-1"})
	if err := execenv.WriteGCMeta(finished, execenv.GCMeta{Kind: execenv.GCKindIssue, IssueID: "i-done", TaskID: "t-done", WorkspaceID: "ws-1"}, s.logger); err != nil {
		t.Fatal(err)
	}

	if task, ok := s.resolve(filepath.Join(running, "workdir", "app")); !ok || task.TaskID != "t-run" || task.RootDir != running {
		t.Fatalf("running task resolved to %+v, %v", task, ok)
	}
	if task, ok := s.resolve(filepath.Join(finished, "workdir")); !ok || task.TaskID != "t-done" || task.IssueID != "i-done" {
		t.Fatalf("finished task resolved to %+v, %v", task, ok)
	}
	for _, dir := range []string{"/Users/me/own-project", filepath.Join(s.workspacesRoot, ".repos", "x"), s.workspacesRoot} {
		if _, ok := s.resolve(dir); ok {
			t.Fatalf("%s was attributed to a task", dir)
		}
	}
	untrack()
	if _, ok := s.resolve(filepath.Join(running, "workdir")); ok {
		t.Fatal("an untracked task without metadata still resolves")
	}

	s.detected = []localenv.Environment{{Key: "detected:x", Dir: finished, State: localenv.StateRunning}}
	if !s.servesFrom(finished) || s.servesFrom(running) {
		t.Fatal("servesFrom does not follow the detected server's directory")
	}
}

func doEnvironmentRequest(handler http.HandlerFunc, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestEnvironmentHandlersRequireATaskCredential(t *testing.T) {
	s, _ := newTestEnvironmentService(t)
	d := &Daemon{logger: s.logger, environments: s}
	for path, handler := range map[string]http.HandlerFunc{
		"/environments/start": d.environmentStartHandler(),
		"/environments/stop":  d.environmentStopHandler(),
		"/environments/list":  d.environmentListHandler(),
	} {
		rec := doEnvironmentRequest(handler, path, "", `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a credential = %d", path, rec.Code)
		}
		rec = doEnvironmentRequest(handler, path, "mat_unknown", `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s with an unknown credential = %d", path, rec.Code)
		}
	}
}
