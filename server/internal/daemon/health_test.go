package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestCreateShutdownCredentialWritesFreshOperatorOnlySecret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first, err := createShutdownCredential("test-profile")
	if err != nil {
		t.Fatalf("create first shutdown credential: %v", err)
	}
	second, err := createShutdownCredential("test-profile")
	if err != nil {
		t.Fatalf("create second shutdown credential: %v", err)
	}
	if first == second {
		t.Fatal("successive daemon starts reused the shutdown credential")
	}
	raw, err := base64.RawURLEncoding.DecodeString(second)
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if len(raw) != shutdownCredentialBytes {
		t.Fatalf("credential entropy bytes = %d, want %d", len(raw), shutdownCredentialBytes)
	}

	path := filepath.Join(home, ".multica", "profiles", "test-profile", ShutdownCredentialFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read credential file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != second {
		t.Fatalf("credential file = %q, want latest credential", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat credential file: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("credential mode = %#o, want 0600", got)
		}
	}
}

func TestServeHealthOwnsShutdownCredentialLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := &Daemon{
		cfg:        Config{Profile: "test-profile"},
		workspaces: map[string]*workspaceState{},
		logger:     slog.Default(),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.serveHealth(ctx, ln, time.Now())
	}()

	credentialPath := filepath.Join(home, ".multica", "profiles", "test-profile", ShutdownCredentialFileName)
	deadline := time.Now().Add(time.Second)
	for {
		if data, readErr := os.ReadFile(credentialPath); readErr == nil && strings.TrimSpace(string(data)) != "" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("serveHealth did not publish a shutdown credential")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("serveHealth did not stop after context cancellation")
	}
	if _, err := os.Stat(credentialPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shutdown credential still exists after server stop: %v", err)
	}
}

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
	d.ready.Store(true) // preflight done -> status should be "running"

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
	// The desktop relies on the `os` key (runtime.GOOS) to detect a daemon it
	// can't manage (e.g. Linux-in-WSL behind a Windows desktop). A rename or
	// drop would silently re-break #3916, so lock both the key and its value.
	if got, want := raw["os"], runtime.GOOS; got != want {
		t.Errorf("os key: got %v, want %q", got, want)
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

// TestHealthHandlerReportsStartingUntilReady pins the liveness/readiness split:
// the health server binds and answers before preflight finishes, but it must
// report "starting" until d.ready is set, and only then "running". Otherwise a
// slow or failing preflight would be misreported to `daemon start` (and the
// desktop) as a fully started daemon.
func TestHealthHandlerReportsStartingUntilReady(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg:        Config{CLIVersion: "v1.0.0"},
		workspaces: map[string]*workspaceState{},
		logger:     slog.Default(),
	}
	handler := d.healthHandler(time.Now())

	readStatus := func() string {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		var resp HealthResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp.Status
	}

	if got := readStatus(); got != "starting" {
		t.Fatalf("status before ready: got %q, want \"starting\"", got)
	}

	d.ready.Store(true)

	if got := readStatus(); got != "running" {
		t.Fatalf("status after ready: got %q, want \"running\"", got)
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

	const credential = "operator-shutdown-credential"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Daemon{cancelFunc: cancel}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	req.Header.Set(ShutdownCredentialHeader, credential)
	d.shutdownHandler(credential).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("daemon context was not cancelled after POST /shutdown")
	}
}

func TestShutdownHandlerRejectsMissingOrInvalidOperatorCredential(t *testing.T) {
	t.Parallel()

	const credential = "operator-shutdown-credential"
	for name, supplied := range map[string]string{
		"missing": "",
		"invalid": "task-controlled-credential",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cancelled := make(chan struct{}, 1)
			d := &Daemon{cancelFunc: func() { cancelled <- struct{}{} }}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
			if supplied != "" {
				req.Header.Set(ShutdownCredentialHeader, supplied)
			}

			d.shutdownHandler(credential).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
			}
			select {
			case <-cancelled:
				t.Fatal("unauthorized shutdown request cancelled daemon context")
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestShutdownHandlerFailsClosedWithoutServerCredential(t *testing.T) {
	t.Parallel()

	cancelled := make(chan struct{}, 1)
	d := &Daemon{cancelFunc: func() { cancelled <- struct{}{} }}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)

	d.shutdownHandler("").ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-cancelled:
		t.Fatal("shutdown without a server credential cancelled daemon context")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestShutdownHandlerRejectsNonPost(t *testing.T) {
	t.Parallel()

	cancelled := false
	d := &Daemon{cancelFunc: func() { cancelled = true }}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/shutdown", nil)
	d.shutdownHandler("operator-shutdown-credential").ServeHTTP(rec, req)

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

func TestRepoCheckoutUsesTaskScopedProjectRefByDefault(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-checkout"
	const repoURL = "https://github.com/org/repo.git"
	cache := &recordingRepoCache{lookupPath: "/cache/org/repo.git"}
	d := newRepoCheckoutTestDaemon(t, workspaceID, repoURL, cache)
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID:         workspaceID,
		TaskID:              "task-1",
		WorkDir:             "/tmp/work",
		AgentName:           "implementer",
		Repos:               []RepoData{{URL: repoURL, Ref: "release/v2"}},
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"url":"` + repoURL + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/repo/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(repoCheckoutCapabilityHeader, token)
	d.repoCheckoutHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := cache.lastCreateParams().Ref; got != "release/v2" {
		t.Fatalf("CreateWorktree Ref = %q, want release/v2", got)
	}
}

func TestRepoCheckoutRejectsRefOutsideTaskBinding(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-checkout"
	const repoURL = "https://github.com/org/repo.git"
	cache := &recordingRepoCache{lookupPath: "/cache/org/repo.git"}
	d := newRepoCheckoutTestDaemon(t, workspaceID, repoURL, cache)
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID:         workspaceID,
		TaskID:              "task-1",
		WorkDir:             "/tmp/work",
		AgentName:           "implementer",
		Repos:               []RepoData{{URL: repoURL, Ref: "release/v2"}},
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"url":"` + repoURL + `","ref":"hotfix"}`)
	req := httptest.NewRequest(http.MethodPost, "/repo/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(repoCheckoutCapabilityHeader, token)
	d.repoCheckoutHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := cache.lastCreateParams(); got != (repocache.WorktreeParams{}) {
		t.Fatalf("CreateWorktree called for denied ref: %#v", got)
	}
}

func TestRepoCheckoutCapabilityIsTaskBoundAndRevocable(t *testing.T) {
	t.Parallel()

	const (
		workspaceID = "ws-checkout"
		repoA       = "https://github.com/org/repo-a.git"
		repoB       = "https://github.com/org/repo-b.git"
	)
	cache := &recordingRepoCache{lookupPath: "/cache/org/repo.git"}
	d := newRepoCheckoutTestDaemon(t, workspaceID, repoA, cache)
	d.workspaces[workspaceID].allowedRepoURLs[repoB] = struct{}{}

	tokenA, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID:         workspaceID,
		TaskID:              "task-a",
		WorkDir:             "/tmp/task-a",
		AgentName:           "agent-a",
		Repos:               []RepoData{{URL: repoA, Ref: "release/a"}},
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("register task A capability: %v", err)
	}
	tokenB, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID:         workspaceID,
		TaskID:              "task-b",
		WorkDir:             "/tmp/task-b",
		AgentName:           "agent-b",
		Repos:               []RepoData{{URL: repoB, Ref: "release/b"}},
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("register task B capability: %v", err)
	}
	if tokenA == tokenB {
		t.Fatal("concurrent task capabilities must be unique")
	}

	request := func(token, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set(repoCheckoutCapabilityHeader, token)
		}
		d.repoCheckoutHandler().ServeHTTP(rec, req)
		return rec
	}

	for name, tc := range map[string]struct {
		token string
		body  string
		code  int
	}{
		"missing capability":       {body: `{"url":"` + repoA + `"}`, code: http.StatusUnauthorized},
		"unknown capability":       {token: "unknown", body: `{"url":"` + repoA + `"}`, code: http.StatusUnauthorized},
		"task A using task B repo": {token: tokenA, body: `{"url":"` + repoB + `"}`, code: http.StatusForbidden},
		"legacy caller identity":   {token: tokenA, body: `{"url":"` + repoA + `","workspace_id":"forged","workdir":"/tmp/escape","task_id":"task-b"}`, code: http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			rec := request(tc.token, tc.body)
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
		})
	}

	rec := request(tokenA, `{"url":"`+repoA+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid task A checkout status = %d: %s", rec.Code, rec.Body.String())
	}
	params := cache.lastCreateParams()
	if params.WorkspaceID != workspaceID || params.TaskID != "task-a" || params.WorkDir != "/tmp/task-a" || params.AgentName != "agent-a" || params.Ref != "release/a" {
		t.Fatalf("checkout used caller-controlled or wrong binding: %#v", params)
	}

	d.revokeRepoCheckoutCapability(tokenA)
	rec = request(tokenA, `{"url":"`+repoA+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked capability status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	rec = request(tokenB, `{"url":"`+repoB+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoking task A affected task B: status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRepoCheckoutCapabilityRejectsSequentialRepoReplay(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/org/repo.git"
	cache := &recordingRepoCache{lookupPath: "/cache/org/repo.git"}
	d := newRepoCheckoutTestDaemon(t, "ws-replay", repoURL, cache)
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-replay",
		TaskID:      "task-replay",
		WorkDir:     "/tmp/task-replay",
		AgentName:   "agent-replay",
		Repos:       []RepoData{{URL: repoURL}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	request := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(repoCheckoutCapabilityHeader, token)
		d.repoCheckoutHandler().ServeHTTP(rec, req)
		return rec
	}

	if rec := request(); rec.Code != http.StatusOK {
		t.Fatalf("initial checkout status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := request(); rec.Code != http.StatusConflict {
		t.Fatalf("replayed checkout status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if got := cache.createCount(); got != 1 {
		t.Fatalf("CreateWorktree calls = %d, want 1 after replay", got)
	}
}

func TestRepoCheckoutCapabilityRejectsConcurrentRepoReplay(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/org/repo.git"
	cache := newBlockingCreateRepoCache("/cache/org/repo.git")
	d := &Daemon{repoCache: cache, logger: slog.Default()}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-concurrent-replay",
		TaskID:      "task-concurrent-replay",
		WorkDir:     "/tmp/task-concurrent-replay",
		AgentName:   "agent-concurrent-replay",
		Repos:       []RepoData{{URL: repoURL}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	request := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(repoCheckoutCapabilityHeader, token)
		d.repoCheckoutHandler().ServeHTTP(rec, req)
		return rec
	}

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- request() }()
	cache.waitForCreate(t)

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { secondDone <- request() }()
	var second *httptest.ResponseRecorder
	select {
	case second = <-secondDone:
	case <-time.After(100 * time.Millisecond):
		cache.release()
		<-firstDone
		<-secondDone
		t.Fatal("concurrent replay blocked instead of returning 409")
	}
	if second.Code != http.StatusConflict {
		cache.release()
		<-firstDone
		t.Fatalf("concurrent replay status = %d, want 409: %s", second.Code, second.Body.String())
	}
	if got := cache.createCount(); got != 1 {
		cache.release()
		<-firstDone
		t.Fatalf("CreateWorktree calls = %d, want 1 during concurrent replay", got)
	}

	cache.release()
	select {
	case rec := <-firstDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("initial checkout status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("initial checkout did not finish")
	}
}

func TestRepoCheckoutPropagatesRequestCancellationToWorktreeCreation(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/org/repo.git"
	cache := newBlockingCreateRepoCache("/cache/org/repo.git")
	d := &Daemon{repoCache: cache, logger: slog.Default()}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-cancel",
		TaskID:      "task-cancel",
		WorkDir:     "/tmp/task-cancel",
		AgentName:   "agent-cancel",
		Repos:       []RepoData{{URL: repoURL}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(repoCheckoutCapabilityHeader, token)
		d.repoCheckoutHandler().ServeHTTP(rec, req)
		done <- rec
	}()

	cache.waitForCreate(t)
	cancel()
	select {
	case rec := <-done:
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("cancelled checkout status = %d, want 500: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), context.Canceled.Error()) {
			t.Fatalf("cancelled checkout body = %q, want context cancellation", rec.Body.String())
		}
	case <-time.After(time.Second):
		cache.release()
		t.Fatal("repo checkout ignored request cancellation")
	}
}

func TestRepoCheckoutCapabilityRevokeWaitsForActiveLease(t *testing.T) {
	t.Parallel()

	d := &Daemon{}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-lease",
		TaskID:      "task-lease",
		WorkDir:     "/tmp/task-lease",
		AgentName:   "agent-lease",
		Repos:       []RepoData{{URL: "https://github.com/org/repo.git"}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	_, release, ok := d.acquireRepoCheckoutCapability(token)
	if !ok {
		t.Fatal("initial acquire rejected")
	}

	revokeDone := make(chan struct{})
	go func() {
		d.revokeRepoCheckoutCapability(token)
		close(revokeDone)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		_, extraRelease, acquired := d.acquireRepoCheckoutCapability(token)
		if !acquired {
			break
		}
		extraRelease()
		if time.Now().After(deadline) {
			t.Fatal("revoke did not prevent new capability acquisition")
		}
		runtime.Gosched()
	}

	select {
	case <-revokeDone:
		t.Fatal("revoke returned while an acquired lease was still active")
	case <-time.After(20 * time.Millisecond):
	}

	release()
	release() // Release must be idempotent for defensive handler cleanup.
	select {
	case <-revokeDone:
	case <-time.After(time.Second):
		t.Fatal("revoke did not return after the active lease was released")
	}
}

func TestRepoCheckoutHandlerRevokeWaitsForInFlightRequest(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/org/repo.git"
	cache := newBlockingLookupRepoCache("/cache/org/repo.git")
	d := &Daemon{repoCache: cache, logger: slog.Default()}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-handler",
		TaskID:      "task-handler",
		WorkDir:     "/tmp/task-handler",
		AgentName:   "agent-handler",
		Repos:       []RepoData{{URL: repoURL}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	handlerDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(repoCheckoutCapabilityHeader, token)
		d.repoCheckoutHandler().ServeHTTP(rec, req)
		handlerDone <- rec
	}()
	cache.waitForLookup(t)

	revokeDone := make(chan struct{})
	go func() {
		d.revokeRepoCheckoutCapability(token)
		close(revokeDone)
	}()

	select {
	case <-revokeDone:
		t.Fatal("revoke returned before the in-flight checkout released its lease")
	case <-time.After(20 * time.Millisecond):
	}

	cache.release()
	select {
	case rec := <-handlerDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("in-flight checkout status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight checkout did not finish")
	}
	select {
	case <-revokeDone:
	case <-time.After(time.Second):
		t.Fatal("revoke did not finish after the checkout handler returned")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(repoCheckoutCapabilityHeader, token)
	d.repoCheckoutHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-revoke checkout status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestRepoCheckoutUsesFrozenPolicyWithoutManagementRead(t *testing.T) {
	t.Parallel()

	const (
		workspaceID = "ws-frozen-policy"
		repoURL     = "https://github.com/org/project-only.git"
	)
	var managementReads int
	client := NewClient("http://multica.invalid")
	client.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		managementReads++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"workspace_id":"ws-frozen-policy","repos":[]}`)),
		}, nil
	})
	cache := &recordingRepoCache{lookupPath: "/cache/project-only.git"}
	d := &Daemon{
		client:    client,
		repoCache: cache,
		workspaces: map[string]*workspaceState{
			workspaceID: newWorkspaceState(workspaceID, nil, "", nil, json.RawMessage(`{"github_enabled":true,"co_authored_by_enabled":true}`)),
		},
		logger: slog.Default(),
	}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID:         workspaceID,
		TaskID:              "task-frozen",
		WorkDir:             "/tmp/task-frozen",
		AgentName:           "implementer",
		Repos:               []RepoData{{URL: repoURL}},
		CoAuthoredByEnabled: false,
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(repoCheckoutCapabilityHeader, token)
	d.repoCheckoutHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if managementReads != 0 {
		t.Fatalf("task checkout performed %d management API reads, want 0", managementReads)
	}
	if got := cache.lastCreateParams().CoAuthoredByEnabled; got {
		t.Fatal("checkout used live workspace policy instead of capability snapshot")
	}
}

func TestRepoCheckoutFailsClosedWhenAssignedRepoCacheIsNotReady(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/org/not-ready.git"
	d := &Daemon{repoCache: &recordingRepoCache{}, logger: slog.Default()}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-1", TaskID: "task-1", WorkDir: "/tmp/task-1", AgentName: "agent",
		Repos: []RepoData{{URL: repoURL}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(`{"url":"`+repoURL+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(repoCheckoutCapabilityHeader, token)
	d.repoCheckoutHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestRepoCheckoutRequiresJSONAndLimitsBody(t *testing.T) {
	t.Parallel()

	const repoURL = "https://github.com/org/repo.git"
	d := &Daemon{repoCache: &recordingRepoCache{lookupPath: "/cache/repo.git"}, logger: slog.Default()}
	token, err := d.registerRepoCheckoutCapability(repoCheckoutCapabilityParams{
		WorkspaceID: "ws-1", TaskID: "task-1", WorkDir: "/tmp/task-1", AgentName: "agent",
		Repos: []RepoData{{URL: repoURL}},
	})
	if err != nil {
		t.Fatalf("register capability: %v", err)
	}

	for name, tc := range map[string]struct {
		contentType string
		body        string
		want        int
	}{
		"missing content type": {body: `{"url":"` + repoURL + `"}`, want: http.StatusUnsupportedMediaType},
		"wrong content type":   {contentType: "text/plain", body: `{"url":"` + repoURL + `"}`, want: http.StatusUnsupportedMediaType},
		"oversized body":       {contentType: "application/json", body: `{"url":"` + repoURL + `","padding":"` + strings.Repeat("x", 20<<10) + `"}`, want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/repo/checkout", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			req.Header.Set(repoCheckoutCapabilityHeader, token)
			d.repoCheckoutHandler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newRepoCheckoutTestDaemon(t *testing.T, workspaceID, repoURL string, cache *recordingRepoCache) *Daemon {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/"+workspaceID+"/repos" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  workspaceID,
			Repos:        []RepoData{{URL: repoURL}},
			ReposVersion: "v1",
		})
	}))
	t.Cleanup(srv.Close)
	return &Daemon{
		cfg:       Config{CLIVersion: "v1.0.0"},
		client:    NewClient(srv.URL),
		repoCache: cache,
		workspaces: map[string]*workspaceState{
			workspaceID: newWorkspaceState(workspaceID, nil, "", []RepoData{{URL: repoURL}}, nil),
		},
		logger: slog.Default(),
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

func (c *blockingLookupRepoCache) LookupContext(ctx context.Context, _, _ string) string {
	select {
	case <-c.lookupSeen:
	default:
		close(c.lookupSeen)
	}
	select {
	case <-c.releaseLookup:
		return c.path
	case <-ctx.Done():
		return ""
	}
}

func (c *blockingLookupRepoCache) ResolveContext(ctx context.Context, _ string, url string) (repocache.ResolvedRepo, error) {
	path := c.LookupContext(ctx, "", url)
	if path == "" {
		return repocache.ResolvedRepo{}, errors.New("repo unavailable")
	}
	return repocache.ResolvedRepo{URL: url, BarePath: path}, nil
}

func (c *blockingLookupRepoCache) SyncContext(context.Context, string, []repocache.RepoInfo) error {
	return nil
}

func (c *blockingLookupRepoCache) WithRepoLock(_ string, fn func() error) error {
	return fn()
}

func (c *blockingLookupRepoCache) CreateWorktreeContext(context.Context, repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

type recordingRepoCache struct {
	lookupPath string
	mu         sync.Mutex
	params     []repocache.WorktreeParams
}

type blockingCreateRepoCache struct {
	path          string
	createSeen    chan struct{}
	releaseCreate chan struct{}
	releaseOnce   sync.Once
	mu            sync.Mutex
	creates       int
}

func newBlockingCreateRepoCache(path string) *blockingCreateRepoCache {
	return &blockingCreateRepoCache{
		path:          path,
		createSeen:    make(chan struct{}),
		releaseCreate: make(chan struct{}),
	}
}

func (c *blockingCreateRepoCache) LookupContext(context.Context, string, string) string {
	return c.path
}

func (c *blockingCreateRepoCache) ResolveContext(_ context.Context, _ string, url string) (repocache.ResolvedRepo, error) {
	return repocache.ResolvedRepo{URL: url, BarePath: c.path}, nil
}

func (c *blockingCreateRepoCache) SyncContext(context.Context, string, []repocache.RepoInfo) error {
	return nil
}

func (c *blockingCreateRepoCache) WithRepoLock(_ string, fn func() error) error { return fn() }

func (c *blockingCreateRepoCache) CreateWorktreeContext(ctx context.Context, params repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	c.mu.Lock()
	c.creates++
	c.mu.Unlock()
	select {
	case <-c.createSeen:
	default:
		close(c.createSeen)
	}
	select {
	case <-c.releaseCreate:
		return &repocache.WorktreeResult{Path: params.WorkDir, BranchName: "agent/test"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *blockingCreateRepoCache) waitForCreate(t *testing.T) {
	t.Helper()
	select {
	case <-c.createSeen:
	case <-time.After(time.Second):
		t.Fatal("repo checkout did not call CreateWorktree")
	}
}

func (c *blockingCreateRepoCache) release() {
	c.releaseOnce.Do(func() { close(c.releaseCreate) })
}

func (c *blockingCreateRepoCache) createCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.creates
}

func (c *recordingRepoCache) LookupContext(context.Context, string, string) string {
	return c.lookupPath
}

func (c *recordingRepoCache) ResolveContext(_ context.Context, _ string, url string) (repocache.ResolvedRepo, error) {
	if c.lookupPath == "" {
		return repocache.ResolvedRepo{}, errors.New("repo unavailable")
	}
	return repocache.ResolvedRepo{URL: url, BarePath: c.lookupPath}, nil
}

func (c *recordingRepoCache) SyncContext(context.Context, string, []repocache.RepoInfo) error {
	return nil
}

func (c *recordingRepoCache) WithRepoLock(_ string, fn func() error) error {
	return fn()
}

func (c *recordingRepoCache) CreateWorktreeContext(_ context.Context, params repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.params = append(c.params, params)
	return &repocache.WorktreeResult{Path: params.WorkDir, BranchName: "agent/test"}, nil
}

func (c *recordingRepoCache) lastCreateParams() repocache.WorktreeParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.params) == 0 {
		return repocache.WorktreeParams{}
	}
	return c.params[len(c.params)-1]
}

func (c *recordingRepoCache) createCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.params)
}

func (c *blockingLookupRepoCache) waitForLookup(t *testing.T) {
	t.Helper()
	select {
	case <-c.lookupSeen:
	case <-time.After(time.Second):
		t.Fatal("repo checkout did not call cache lookup")
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
