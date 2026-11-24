package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// workspaceCoAuthoredByEnabled gates the prepare-commit-msg hook installed in
// agent worktrees. RFC MUL-2414 adds the `github_enabled` master switch:
// when it is explicitly false the hook must NOT be installed even if
// `co_authored_by_enabled` is true. The function also defaults to true
// whenever settings are absent or malformed so existing workspaces keep
// their historical behavior.
func TestWorkspaceCoAuthoredByEnabled(t *testing.T) {
	cases := []struct {
		name     string
		register bool
		settings string
		want     bool
	}{
		{"unknown workspace defaults on", false, "", true},
		{"registered workspace, nil settings defaults on", true, "", true},
		{"empty object defaults on", true, "{}", true},
		{"co_authored_by absent defaults on", true, `{"github_enabled":true}`, true},
		{"co_authored_by true", true, `{"co_authored_by_enabled":true}`, true},
		{"co_authored_by false", true, `{"co_authored_by_enabled":false}`, false},
		{
			"master off forces hook off even when co_authored_by true",
			true,
			`{"github_enabled":false,"co_authored_by_enabled":true}`,
			false,
		},
		{
			"master on lets co_authored_by decide",
			true,
			`{"github_enabled":true,"co_authored_by_enabled":false}`,
			false,
		},
		{"malformed settings defaults on", true, `not json`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Daemon{workspaces: make(map[string]*workspaceState)}
			if tc.register {
				var raw json.RawMessage
				if tc.settings != "" {
					raw = json.RawMessage(tc.settings)
				}
				d.workspaces["ws"] = newWorkspaceState("ws", nil, "", nil, raw)
			}
			if got := d.workspaceCoAuthoredByEnabled("ws"); got != tc.want {
				t.Fatalf("workspaceCoAuthoredByEnabled(%q) = %v, want %v",
					tc.settings, got, tc.want)
			}
		})
	}
}

// syncWorkspacesFromAPI must not refresh repos or settings for an already-
// tracked workspace. They are only consumed by repo checkout, whose
// ensureRepoReady path refreshes them immediately before use. Keeping this
// periodic sync to workspace/runtime duties prevents idle daemons from
// repeatedly hitting the workspace repos endpoint.
func TestSyncWorkspacesSkipsReposRefreshOnExistingWorkspace(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"

	var repoCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/workspaces":
			json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/repos":
			repoCalls.Add(1)
			json.NewEncoder(w).Encode(WorkspaceReposResponse{
				WorkspaceID:  workspaceID,
				Repos:        []RepoData{},
				ReposVersion: "v1",
				Settings:     json.RawMessage(`{"github_enabled":false,"co_authored_by_enabled":true}`),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:       NewClient(srv.URL),
		logger:       slog.Default(),
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
		runtimeSet:   newRuntimeSetWatcher(),
	}
	// Pretend the workspace was already registered with co-author ON. A live
	// runtime ID keeps workspaceNeedsRuntimeRecovery from short-circuiting the
	// sync into a re-register.
	d.workspaces[workspaceID] = newWorkspaceState(
		workspaceID,
		[]string{"rt-1"},
		"v1",
		nil,
		json.RawMessage(`{"github_enabled":true,"co_authored_by_enabled":true}`),
	)

	if !d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatalf("precondition: expected co-author hook to start enabled")
	}

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if got := repoCalls.Load(); got != 0 {
		t.Fatalf("workspace sync called repos endpoint %d times, want 0", got)
	}

	if !d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatal("workspace sync unexpectedly replaced cached settings")
	}
}

// coAuthoredByStateCache is a repoCacheBackend that records what the daemon
// publishes for installed prepare-commit-msg hooks to read.
type coAuthoredByStateCache struct {
	mu         sync.Mutex
	writes     []bool
	reconciles []bool
	checkouts  []reconciledCheckout
}

// reconciledCheckout records one isolated checkout the daemon asked to bring in
// line with the workspace setting.
type reconciledCheckout struct {
	path    string
	enabled bool
}

func (c *coAuthoredByStateCache) Lookup(string, string) string   { return "" }
func (c *coAuthoredByStateCache) BarePath(string, string) string { return "" }
func (c *coAuthoredByStateCache) Sync(string, []repocache.RepoInfo) error {
	return nil
}
func (c *coAuthoredByStateCache) WithRepoLock(_ string, fn func() error) error { return fn() }
func (c *coAuthoredByStateCache) CreateWorktree(repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return nil, nil
}

func (c *coAuthoredByStateCache) WriteCoAuthoredByState(_ string, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, enabled)
	return nil
}

func (c *coAuthoredByStateCache) ReconcileCoAuthoredByHooks(_ string, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconciles = append(c.reconciles, enabled)
	return nil
}

func (c *coAuthoredByStateCache) ReconcileCoAuthoredByHookInCheckout(checkoutPath, _ string, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkouts = append(c.checkouts, reconciledCheckout{path: checkoutPath, enabled: enabled})
	return nil
}

func (c *coAuthoredByStateCache) reconciledCheckouts() []reconciledCheckout {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]reconciledCheckout(nil), c.checkouts...)
}

func (c *coAuthoredByStateCache) lastReconcile(t *testing.T) bool {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reconciles) == 0 {
		t.Fatal("hooks were never reconciled")
	}
	return c.reconciles[len(c.reconciles)-1]
}

func (c *coAuthoredByStateCache) lastWrite(t *testing.T) bool {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.writes) == 0 {
		t.Fatal("no Co-authored-by state was published")
	}
	return c.writes[len(c.writes)-1]
}

// newCoAuthoredByStateDaemon returns a daemon tracking one workspace whose
// settings the fake server serves from settings, plus the cache recording
// published state.
func newCoAuthoredByStateDaemon(t *testing.T, workspaceID string, settings *string) (*Daemon, *coAuthoredByStateCache) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/workspaces/"+workspaceID+"/repos" {
			http.NotFound(w, r)
			return
		}
		resp := WorkspaceReposResponse{WorkspaceID: workspaceID, ReposVersion: "v1"}
		if settings != nil {
			resp.Settings = json.RawMessage(*settings)
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	cache := &coAuthoredByStateCache{}
	d := &Daemon{
		cfg:        Config{CLIVersion: "v1.0.0"},
		client:     NewClient(srv.URL),
		repoCache:  cache,
		workspaces: map[string]*workspaceState{workspaceID: newWorkspaceState(workspaceID, nil, "", nil, nil)},
		logger:     slog.Default(),
	}
	return d, cache
}

// A settings refresh must publish the current verdict to the repo cache, not
// just to the daemon's in-memory copy: prepare-commit-msg hooks installed by
// earlier checkouts read that file on every commit, and it is the only way a
// toggle-off reaches a checkout that already exists (MUL-6921).
func TestRefreshWorkspaceReposPublishesCoAuthoredByState(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	settings := `{"co_authored_by_enabled":false}`
	d, cache := newCoAuthoredByStateDaemon(t, workspaceID, &settings)

	if _, err := d.refreshWorkspaceRepos(context.Background(), workspaceID); err != nil {
		t.Fatalf("refreshWorkspaceRepos failed: %v", err)
	}
	if cache.lastWrite(t) {
		t.Error("published state = enabled, want disabled after the toggle was turned off")
	}

	settings = `{"co_authored_by_enabled":true}`
	if _, err := d.refreshWorkspaceRepos(context.Background(), workspaceID); err != nil {
		t.Fatalf("refreshWorkspaceRepos failed: %v", err)
	}
	if !cache.lastWrite(t) {
		t.Error("published state = disabled, want enabled after the toggle was turned back on")
	}
}

// The server's workspaces-changed hint is the only signal a running daemon
// gets for a settings edit — the periodic sync makes no repos/settings request
// for a workspace it already tracks. refreshTrackedWorkspaceSettings is what
// turns that hint into a new verdict without waiting for the next checkout.
func TestRefreshTrackedWorkspaceSettingsAppliesToggle(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	settings := `{"co_authored_by_enabled":false}`
	d, cache := newCoAuthoredByStateDaemon(t, workspaceID, &settings)

	if !d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatal("precondition: workspace should start with the default (enabled) verdict")
	}

	d.refreshTrackedWorkspaceSettings(context.Background())

	if d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Error("daemon still reports the trailer as enabled after the workspace disabled it")
	}
	if cache.lastWrite(t) {
		t.Error("published state = enabled, want disabled")
	}
}

// A daemon that starts (or restarts) after the toggle was flipped learns the
// new value from its register response, and nothing else would ever carry it
// to the hooks already on disk. The workspace sync republishes it locally —
// without touching the repos endpoint, which the sibling test above pins.
func TestSyncWorkspacesPublishesCoAuthoredByState(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/workspaces" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
	}))
	t.Cleanup(srv.Close)

	cache := &coAuthoredByStateCache{}
	d := &Daemon{
		client:       NewClient(srv.URL),
		logger:       slog.Default(),
		repoCache:    cache,
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
		runtimeSet:   newRuntimeSetWatcher(),
	}
	d.workspaces[workspaceID] = newWorkspaceState(
		workspaceID,
		[]string{"rt-1"},
		"v1",
		nil,
		json.RawMessage(`{"co_authored_by_enabled":false}`),
	)

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	if cache.lastWrite(t) {
		t.Error("published state = enabled, want disabled for a workspace whose settings say so")
	}
}

// Publishing has to carry hooks written by earlier daemon releases too: they
// read no state file, so a new value alone never reaches them.
func TestPersistCoAuthoredByStateReconcilesHooks(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	settings := `{"co_authored_by_enabled":false}`
	d, cache := newCoAuthoredByStateDaemon(t, workspaceID, &settings)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, nil, "", nil, json.RawMessage(settings))

	d.persistCoAuthoredByState(workspaceID)

	if cache.lastWrite(t) {
		t.Error("published state = enabled, want disabled")
	}
	if cache.lastReconcile(t) {
		t.Error("reconciled hooks as enabled, want disabled")
	}
}

// A publisher that started before a settings update must not overwrite the
// value that update produced. The lock is what rules that out: the verdict is
// read INSIDE it, so a publisher parked before its read is holding the lock and
// no fresher publisher can slip past and be overwritten afterwards.
//
// The ordering is driven by channels, not sleeps: A parks inside its verdict
// read, B is started afterwards, and B reaching the cache while A is parked is
// itself the failure — it can only happen if A read before taking the lock.
func TestPersistCoAuthoredByStateReadsVerdictUnderPublishLock(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	d, cache := newCoAuthoredByStateDaemon(t, workspaceID, nil)
	ws := newWorkspaceState(workspaceID, nil, "", nil, json.RawMessage(`{"co_authored_by_enabled":true}`))
	d.workspaces[workspaceID] = ws

	parked := make(chan struct{})
	resume := make(chan struct{})
	staleDone := make(chan struct{})
	go func() {
		defer close(staleDone)
		d.publishCoAuthoredByState(workspaceID, func(id string) bool {
			enabled := d.workspaceCoAuthoredByEnabled(id)
			close(parked)
			<-resume
			return enabled
		})
	}()

	// A has read "enabled" and is parked. Under the contract it holds the
	// publish lock while parked.
	<-parked

	d.mu.Lock()
	ws.settings = json.RawMessage(`{"co_authored_by_enabled":false}`)
	d.mu.Unlock()

	freshDone := make(chan struct{})
	go func() {
		defer close(freshDone)
		d.persistCoAuthoredByState(workspaceID)
	}()

	// The fresh publisher must not be able to publish while A is parked. This
	// wait is the assertion: it is expected to expire, and any write arriving
	// inside it means the verdict was read outside the lock.
	select {
	case <-freshDone:
		close(resume)
		<-staleDone
		t.Fatal("a publisher completed while another was parked mid-publish: the verdict is being read outside the lock")
	case <-time.After(300 * time.Millisecond):
	}

	close(resume)
	<-staleDone
	select {
	case <-freshDone:
	case <-time.After(5 * time.Second):
		t.Fatal("publication never completed")
	}

	if cache.lastWrite(t) {
		t.Error("last published state = enabled: a stale publisher overwrote the value the settings update produced")
	}
}

// A settings edit made while the daemon's websocket was down produces a hint
// nobody receives. Reconnect reconcile must re-read settings for tracked
// workspaces, or the daemon keeps republishing the stale verdict and existing
// checkouts stay wrong until their next checkout.
func TestWorkspaceSyncLoop_ReconcilePicksUpSettingsChangedWhileOffline(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	var repoCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/workspaces":
			json.NewEncoder(w).Encode([]WorkspaceInfo{{ID: workspaceID, Name: "ws"}})
		case "/api/daemon/workspaces/" + workspaceID + "/repos":
			repoCalls.Add(1)
			// What the server has believed since the user flipped the toggle
			// during the websocket outage.
			json.NewEncoder(w).Encode(WorkspaceReposResponse{
				WorkspaceID:  workspaceID,
				ReposVersion: "v2",
				Settings:     json.RawMessage(`{"co_authored_by_enabled":false}`),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cache := &coAuthoredByStateCache{}
	d := &Daemon{
		client:       NewClient(srv.URL),
		logger:       slog.Default(),
		repoCache:    cache,
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
		runtimeSet:   newRuntimeSetWatcher(),
		reconcile:    newReconcileBroadcaster(),
	}
	// Cached from before the outage: a live runtime ID keeps the sync from
	// short-circuiting into a re-register.
	d.workspaces[workspaceID] = newWorkspaceState(
		workspaceID,
		[]string{"rt-1"},
		"v1",
		nil,
		json.RawMessage(`{"co_authored_by_enabled":true}`),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		d.workspaceSyncLoop(ctx)
	}()

	d.reconcile.broadcast()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && d.workspaceCoAuthoredByEnabled(workspaceID) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-loopDone

	if repoCalls.Load() == 0 {
		t.Fatal("reconnect reconcile never re-read workspace settings")
	}
	if d.workspaceCoAuthoredByEnabled(workspaceID) {
		t.Fatal("daemon still reports the trailer as enabled after reconnecting")
	}
	if cache.lastWrite(t) {
		t.Error("published state = enabled, want the value the server held during the outage")
	}
}

// Isolated checkouts (Codex on Linux, the Windows sandbox) keep their hook
// inside the task workdir, so publishing a value cannot reach a workdir
// prepared by an earlier release. The daemon walks its env roots to find them —
// and must reconcile only the ones the workspace owns.
func TestPersistCoAuthoredByStateReconcilesIsolatedCheckouts(t *testing.T) {
	t.Parallel()

	const workspaceID = "ws-1"
	settings := `{"co_authored_by_enabled":false}`
	d, cache := newCoAuthoredByStateDaemon(t, workspaceID, &settings)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, nil, "", nil, json.RawMessage(settings))

	root := t.TempDir()
	d.cfg.WorkspacesRoot = root

	// Two env roots that look identical on disk and differ only in the
	// workspace that owns them, plus a daemon-internal cache directory the walk
	// must not treat as a workspace.
	ours := seedEnvRootCheckout(t, root, "multica-ws-1", "env-a", workspaceAwareOwner(workspaceID))
	theirs := seedEnvRootCheckout(t, root, "other-ws", "env-b", workspaceAwareOwner("ws-2"))
	if err := os.MkdirAll(filepath.Join(root, ".repos", "ws-1"), 0o755); err != nil {
		t.Fatalf("seed cache dir: %v", err)
	}

	d.persistCoAuthoredByState(workspaceID)

	got := cache.reconciledCheckouts()
	if len(got) != 1 {
		t.Fatalf("reconciled %d checkouts, want exactly the one this workspace owns: %+v", len(got), got)
	}
	if got[0].path != ours {
		t.Errorf("reconciled %q, want %q", got[0].path, ours)
	}
	if got[0].enabled {
		t.Error("reconciled the isolated checkout as enabled, want disabled")
	}
	for _, checkout := range got {
		if checkout.path == theirs {
			t.Error("reconciled a checkout owned by another workspace")
		}
	}
}

// seedEnvRootCheckout builds <root>/<wsDir>/<envRoot> with a checkout that owns
// its git metadata, plus the owner marker the release that created it would
// have written. marker == "" seeds no marker at all, which is what releases
// before .task_owner left behind.
func seedEnvRootCheckout(t *testing.T, root, wsDir, envRootName, marker string) string {
	t.Helper()
	envRoot := filepath.Join(root, wsDir, envRootName)
	checkout := filepath.Join(envRoot, "workdir", "repo")
	if err := os.MkdirAll(filepath.Join(checkout, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("seed checkout: %v", err)
	}
	if marker != "" {
		if err := os.WriteFile(filepath.Join(envRoot, ".task_owner"), []byte(marker), 0o644); err != nil {
			t.Fatalf("seed env root owner: %v", err)
		}
	}
	return checkout
}

// workspaceAwareOwner is the marker written since v0.4.35: JSON carrying the
// workspace the env root belongs to.
func workspaceAwareOwner(workspaceID string) string {
	return fmt.Sprintf(`{"workspace_id":%q,"task_id":"task-1"}`, workspaceID)
}

// legacyTaskOnlyOwner is the marker v0.4.32–v0.4.34 wrote: the bare task ID,
// with nothing that names a workspace.
const legacyTaskOnlyOwner = "01a05b87-32d9-741c-b7ce-fb90fbd8c451"

// Env roots prepared before v0.4.35 name no workspace: v0.4.32–v0.4.34 wrote a
// bare task ID into .task_owner, and older releases wrote no marker at all.
// Both live under that era's layout — <workspaces root>/<workspace ID>/<task
// key> — and both survive upgrades untouched, so the sweep has to attribute
// them by directory name or the trailer never stops in the very workdirs this
// issue was reported from (Linux Codex, Windows sandbox).
func TestPersistCoAuthoredByStateReconcilesLegacyEnvRoots(t *testing.T) {
	t.Parallel()

	// The legacy layout used the raw workspace UUID as the directory name.
	const workspaceID = "01a05b87-a8f3-7eea-8c17-61070ea7e840"
	const otherWorkspaceID = "01a05b87-0000-7000-8000-000000000002"
	settings := `{"co_authored_by_enabled":false}`
	d, cache := newCoAuthoredByStateDaemon(t, workspaceID, &settings)
	d.workspaces[workspaceID] = newWorkspaceState(workspaceID, nil, "", nil, json.RawMessage(settings))

	root := t.TempDir()
	d.cfg.WorkspacesRoot = root

	noMarker := seedEnvRootCheckout(t, root, workspaceID, "v0431-style", "")
	taskOnlyMarker := seedEnvRootCheckout(t, root, workspaceID, "v0434-style", legacyTaskOnlyOwner)
	// Same two shapes under another workspace's legacy directory, plus a
	// modern readable directory whose marker names no workspace: neither can
	// be attributed to this workspace, so neither may be touched.
	otherNoMarker := seedEnvRootCheckout(t, root, otherWorkspaceID, "v0431-style", "")
	otherTaskOnly := seedEnvRootCheckout(t, root, otherWorkspaceID, "v0434-style", legacyTaskOnlyOwner)
	readableUnattributed := seedEnvRootCheckout(t, root, "multica-61070eA_", "env-x", legacyTaskOnlyOwner)

	d.persistCoAuthoredByState(workspaceID)

	reconciled := make(map[string]bool)
	for _, checkout := range cache.reconciledCheckouts() {
		reconciled[checkout.path] = checkout.enabled
	}
	for _, want := range []string{noMarker, taskOnlyMarker} {
		enabled, ok := reconciled[want]
		if !ok {
			t.Errorf("legacy env root was skipped: %s", want)
			continue
		}
		if enabled {
			t.Errorf("reconciled %s as enabled, want disabled", want)
		}
	}
	for _, forbidden := range []string{otherNoMarker, otherTaskOnly, readableUnattributed} {
		if _, ok := reconciled[forbidden]; ok {
			t.Errorf("reconciled a checkout this workspace cannot claim: %s", forbidden)
		}
	}
}
