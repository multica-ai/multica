package daemon

import (
	"bufio"
	"context"
	"encoding/json"
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

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/daemon/trace"
)

func TestHealthHandlerReportsCLIVersionAndActiveTaskCount(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			CLIVersion:    "v9.9.9",
			DaemonID:      "daemon-test",
			DeviceName:    "dev",
			ServerBaseURL: "http://localhost:8080",
		},
		workspaces: map[string]*workspaceState{},
		logger:     slog.Default(),
	}
	d.activeTasks.Store(3)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	d.healthHandler(time.Now()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Decode into a raw map so the test locks in the exact wire-level JSON
	// keys — the desktop TS client depends on snake_case (cli_version,
	// active_task_count), so a silent struct-tag rename must fail here.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if got, want := raw["cli_version"], "v9.9.9"; got != want {
		t.Errorf("cli_version key: got %v, want %q", got, want)
	}
	// JSON numbers decode to float64 through map[string]any.
	if got, want := raw["active_task_count"], float64(3); got != want {
		t.Errorf("active_task_count key: got %v, want %v", got, want)
	}
	if got, want := raw["status"], "running"; got != want {
		t.Errorf("status key: got %v, want %q", got, want)
	}

	// Also round-trip into the typed struct as a separate check that the
	// field values match, independent of key naming.
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode typed response: %v", err)
	}
	if resp.CLIVersion != "v9.9.9" {
		t.Errorf("CLIVersion: got %q, want %q", resp.CLIVersion, "v9.9.9")
	}
	if resp.ActiveTaskCount != 3 {
		t.Errorf("ActiveTaskCount: got %d, want 3", resp.ActiveTaskCount)
	}
}

func TestHealthHandlerActiveTaskCountTracksCounter(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg:        Config{CLIVersion: "v1.0.0"},
		workspaces: map[string]*workspaceState{},
		logger:     slog.Default(),
	}
	handler := d.healthHandler(time.Now())

	// Simulate the pollLoop increment/decrement protocol.
	d.activeTasks.Add(1)
	d.activeTasks.Add(1)
	assertActiveTaskCount(t, handler, 2)

	d.activeTasks.Add(-1)
	assertActiveTaskCount(t, handler, 1)

	d.activeTasks.Add(-1)
	assertActiveTaskCount(t, handler, 0)
}

func TestShutdownHandlerPostCancelsDaemonContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Daemon{cancelFunc: cancel}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	d.shutdownHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("daemon context was not cancelled after POST /shutdown")
	}
}

func TestShutdownHandlerRejectsNonPost(t *testing.T) {
	t.Parallel()

	cancelled := false
	d := &Daemon{cancelFunc: func() { cancelled = true }}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shutdown", nil)
	d.shutdownHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	// Give the handler's deferred cancel goroutine a moment to fire
	// in case a bug causes it to run anyway.
	time.Sleep(10 * time.Millisecond)
	if cancelled {
		t.Fatal("GET request should not trigger cancellation")
	}
}

func TestTraceHandlerReturnsLatestRun(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "http://localhost:3000")

	store := newTraceStoreForTest(t)
	defer store.Close()
	d := &Daemon{traceStore: store, logger: slog.Default()}
	ctx := context.Background()

	if _, err := store.Append(ctx, trace.TraceLine{TaskID: "task-trace-http", RunID: "run-old", Channel: trace.ChannelNormalized, Content: "old"}); err != nil {
		t.Fatalf("append old run: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := store.Append(ctx, trace.TraceLine{TaskID: "task-trace-http", RunID: "run-new", Channel: trace.ChannelCommandStdout, Content: "new"}); err != nil {
		t.Fatalf("append new run: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/traces/tasks/task-trace-http?tail=20", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	d.traceHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("expected scoped CORS header, got %q", got)
	}
	var resp traceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RunID != "run-new" {
		t.Fatalf("expected latest run-new, got %q", resp.RunID)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Content != "new" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

func TestTraceHandlerBlocksUnknownOriginCORSHeader(t *testing.T) {
	t.Parallel()

	store := newTraceStoreForTest(t)
	defer store.Close()
	d := &Daemon{traceStore: store, logger: slog.Default()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/traces/tasks/task-trace-http?tail=20", nil)
	req.Header.Set("Origin", "https://evil.example")
	d.traceHandler().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS header for unknown origin: %q", got)
	}
}

func TestTraceHandlerAllowsConfiguredAppOrigin(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte("{\n  \"app_url\": \"http://127.0.0.1:13003\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := newTraceStoreForTest(t)
	defer store.Close()
	d := &Daemon{
		cfg:        Config{ConfigPath: configPath},
		traceStore: store,
		logger:     slog.Default(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/traces/tasks/task-options", nil)
	req.Header.Set("Origin", "http://127.0.0.1:13003")
	d.traceHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:13003" {
		t.Fatalf("expected configured app origin, got %q", got)
	}
}

func TestTraceHandlerListSince(t *testing.T) {
	t.Parallel()

	store := newTraceStoreForTest(t)
	defer store.Close()
	d := &Daemon{traceStore: store, logger: slog.Default()}
	ctx := context.Background()

	if _, err := store.Append(ctx, trace.TraceLine{TaskID: "task-since", RunID: "run-1", Channel: trace.ChannelNormalized, Content: "first"}); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, err := store.Append(ctx, trace.TraceLine{TaskID: "task-since", RunID: "run-1", Channel: trace.ChannelNormalized, Content: "second"}); err != nil {
		t.Fatalf("append second: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/traces/tasks/task-since?run_id=run-1&after_seq=1", nil)
	d.traceHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp traceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Content != "second" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

func TestTraceHandlerOptions(t *testing.T) {
	t.Parallel()

	d := &Daemon{traceStore: newTraceStoreForTest(t), logger: slog.Default()}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/traces/tasks/task-options", nil)
	d.traceHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestTraceHandlerStreamSendsInitialLines(t *testing.T) {
	t.Parallel()

	store := newTraceStoreForTest(t)
	defer store.Close()
	d := &Daemon{traceStore: store, logger: slog.Default()}
	ctx := context.Background()

	if _, err := store.Append(ctx, trace.TraceLine{
		TaskID:  "task-stream",
		RunID:   "run-stream",
		Channel: trace.ChannelNormalized,
		Content: "stream-line",
	}); err != nil {
		t.Fatalf("append stream line: %v", err)
	}

	srv := httptest.NewServer(d.traceHandler())
	defer srv.Close()

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, srv.URL+"/task-stream/stream?tail=20", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("expected event-stream content type, got %q", got)
	}

	reader := bufio.NewReader(resp.Body)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for trace event")
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "stream-line") {
			cancel()
			return
		}
	}
}

func TestHealthHandlerRespondsWhileTaskRepoLookupWaits(t *testing.T) {
	const workspaceID = "ws-health"
	const repoURL = "https://github.com/org/repo.git"
	cache := newBlockingLookupRepoCache("/cache/org/repo.git")
	d := &Daemon{
		cfg: Config{CLIVersion: "v1.0.0"},
		workspaces: map[string]*workspaceState{
			workspaceID: {
				workspaceID:     workspaceID,
				runtimeIDs:      []string{"rt-1"},
				allowedRepoURLs: map[string]struct{}{repoURL: {}},
				taskRepoURLs:    map[string]struct{}{},
			},
		},
		repoCache: cache,
		logger:    slog.Default(),
	}
	defer cache.release()

	registerDone := make(chan struct{})
	go func() {
		d.registerTaskRepos(workspaceID, []RepoData{{URL: repoURL}})
		close(registerDone)
	}()
	cache.waitForLookup(t)

	rec := httptest.NewRecorder()
	healthDone := make(chan struct{})
	go func() {
		d.healthHandler(time.Now()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		close(healthDone)
	}()

	select {
	case <-healthDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("/health blocked behind task repo cache lookup")
	}

	cache.release()
	select {
	case <-registerDone:
	case <-time.After(time.Second):
		t.Fatal("registerTaskRepos did not unblock after repo lookup finished")
	}
}

type blockingLookupRepoCache struct {
	path          string
	lookupSeen    chan struct{}
	releaseLookup chan struct{}
	releaseOnce   sync.Once
}

func newBlockingLookupRepoCache(path string) *blockingLookupRepoCache {
	return &blockingLookupRepoCache{
		path:          path,
		lookupSeen:    make(chan struct{}),
		releaseLookup: make(chan struct{}),
	}
}

func (c *blockingLookupRepoCache) Lookup(_, _ string) string {
	select {
	case <-c.lookupSeen:
	default:
		close(c.lookupSeen)
	}
	<-c.releaseLookup
	return c.path
}

func (c *blockingLookupRepoCache) Sync(string, []repocache.RepoInfo) error {
	return nil
}

func (c *blockingLookupRepoCache) CreateWorktree(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

func (c *blockingLookupRepoCache) waitForLookup(t *testing.T) {
	t.Helper()
	select {
	case <-c.lookupSeen:
	case <-time.After(time.Second):
		t.Fatal("registerTaskRepos did not call repo lookup")
	}
}

func (c *blockingLookupRepoCache) release() {
	c.releaseOnce.Do(func() {
		close(c.releaseLookup)
	})
}

func assertActiveTaskCount(t *testing.T, h http.HandlerFunc, want int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ActiveTaskCount != want {
		t.Errorf("active_task_count: got %d, want %d", resp.ActiveTaskCount, want)
	}
}
