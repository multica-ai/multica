package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
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

func (c *blockingLookupRepoCache) Fetch(string) error {
	return nil
}

func (c *blockingLookupRepoCache) CreateWorktree(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

func (c *blockingLookupRepoCache) CreateSharedClone(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
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

type recordingRepoCache struct {
	lookupPath  string
	fetchCalls  []string
	fetchErr    error
}

func (c *recordingRepoCache) Lookup(_, _ string) string                                  { return c.lookupPath }
func (c *recordingRepoCache) Sync(string, []repocache.RepoInfo) error                    { return nil }
func (c *recordingRepoCache) Fetch(barePath string) error {
	c.fetchCalls = append(c.fetchCalls, barePath)
	return c.fetchErr
}
func (c *recordingRepoCache) CreateWorktree(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}
func (c *recordingRepoCache) CreateSharedClone(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

func postRefresh(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/repo/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRepoRefreshHandlerCallsFetch(t *testing.T) {
	t.Parallel()
	cache := &recordingRepoCache{lookupPath: "/cache/ws/foo.git"}
	d := &Daemon{repoCache: cache, logger: slog.Default()}

	rec := postRefresh(t, d.repoRefreshHandler(), `{"url":"https://github.com/o/r.git","workspace_id":"ws-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(cache.fetchCalls) != 1 || cache.fetchCalls[0] != "/cache/ws/foo.git" {
		t.Fatalf("fetch calls: got %v, want [/cache/ws/foo.git]", cache.fetchCalls)
	}
}

func TestRepoRefreshHandlerMissingURLReturns400(t *testing.T) {
	t.Parallel()
	cache := &recordingRepoCache{lookupPath: "/cache/ws/foo.git"}
	d := &Daemon{repoCache: cache, logger: slog.Default()}

	rec := postRefresh(t, d.repoRefreshHandler(), `{"workspace_id":"ws-1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
	if len(cache.fetchCalls) != 0 {
		t.Fatalf("fetch should not be called on bad input, got %v", cache.fetchCalls)
	}
}

func TestRepoRefreshHandlerNotInCacheReturns404(t *testing.T) {
	t.Parallel()
	// Lookup returns "" → repo not in cache.
	cache := &recordingRepoCache{lookupPath: ""}
	d := &Daemon{repoCache: cache, logger: slog.Default()}

	rec := postRefresh(t, d.repoRefreshHandler(), `{"url":"https://github.com/o/r.git","workspace_id":"ws-1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

func TestRepoRefreshHandlerFetchErrorReturns500(t *testing.T) {
	t.Parallel()
	cache := &recordingRepoCache{lookupPath: "/cache/ws/foo.git", fetchErr: fmt.Errorf("boom")}
	d := &Daemon{repoCache: cache, logger: slog.Default()}

	rec := postRefresh(t, d.repoRefreshHandler(), `{"url":"https://github.com/o/r.git","workspace_id":"ws-1"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
}

func TestControllerRepoRefreshHandlerProxiesToAdmin(t *testing.T) {
	// Spin up a fake repocache admin server that records the call.
	var seenWS, seenURL string
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/fetch" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		seenWS = r.URL.Query().Get("workspace_id")
		seenURL = r.URL.Query().Get("url")
		_, _ = w.Write([]byte("fetched\n"))
	}))
	defer admin.Close()

	t.Setenv("MULTICA_REPOCACHE_URL", admin.URL)
	d := &Daemon{logger: slog.Default()}

	rec := postRefresh(t, d.controllerRepoRefreshHandler(), `{"url":"https://github.com/o/r.git","workspace_id":"ws-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if seenWS != "ws-1" {
		t.Errorf("workspace_id query: got %q, want %q", seenWS, "ws-1")
	}
	if seenURL != "https://github.com/o/r.git" {
		t.Errorf("url query: got %q, want %q", seenURL, "https://github.com/o/r.git")
	}
}

func TestControllerRepoRefreshHandlerMissingEnvReturns503(t *testing.T) {
	t.Setenv("MULTICA_REPOCACHE_URL", "")
	d := &Daemon{logger: slog.Default()}

	rec := postRefresh(t, d.controllerRepoRefreshHandler(), `{"url":"https://github.com/o/r.git","workspace_id":"ws-1"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
}

func TestControllerRepoRefreshHandlerForwardsUpstream404(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown", http.StatusNotFound)
	}))
	defer admin.Close()
	t.Setenv("MULTICA_REPOCACHE_URL", admin.URL)
	d := &Daemon{logger: slog.Default()}

	rec := postRefresh(t, d.controllerRepoRefreshHandler(), `{"url":"https://github.com/o/r.git","workspace_id":"ws-1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404 (forwarded), body=%s", rec.Code, rec.Body.String())
	}
}

// Sanity check: the URL-escape path in the proxy handler builds the expected
// query string. Belt-and-suspenders for the workspace_id and url params.
func TestControllerRepoRefreshHandlerEscapesQuery(t *testing.T) {
	var rawQuery string
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("ok"))
	}))
	defer admin.Close()
	t.Setenv("MULTICA_REPOCACHE_URL", admin.URL)
	d := &Daemon{logger: slog.Default()}

	rec := postRefresh(t, d.controllerRepoRefreshHandler(), `{"url":"https://github.com/o/r.git?fancy=1","workspace_id":"ws+special"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	parsed, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse rawQuery %q: %v", rawQuery, err)
	}
	if parsed.Get("workspace_id") != "ws+special" {
		t.Errorf("workspace_id round-trip: got %q, want %q", parsed.Get("workspace_id"), "ws+special")
	}
	if parsed.Get("url") != "https://github.com/o/r.git?fancy=1" {
		t.Errorf("url round-trip: got %q, want %q", parsed.Get("url"), "https://github.com/o/r.git?fancy=1")
	}
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
