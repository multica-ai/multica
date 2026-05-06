package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// ErrRepoNotConfigured is returned by ensureRepoReady when the requested repo
// URL is not present in the workspace's repo configuration after a fresh
// server refresh.
var ErrRepoNotConfigured = errors.New("repo is not configured for this workspace")

// workspaceState tracks registered runtimes for a single workspace.
//
// allowedRepoURLs covers the workspace-level repo bindings; it gets rebuilt on
// every refresh from the server. taskRepoURLs covers repos that the server
// surfaced through a per-task claim (project github_repo resources today,
// possibly other typed sources later) — those don't show up in
// GetWorkspaceRepos, so they would be wiped on refresh if we shared one map.
type workspaceState struct {
	workspaceID     string
	runtimeIDs      []string
	reposVersion    string // stored for future use: skip refresh when version unchanged
	allowedRepoURLs map[string]struct{}
	taskRepoURLs    map[string]struct{}
	settings        json.RawMessage // workspace settings (JSONB)
	lastRepoSyncErr string
	repoRefreshMu   sync.Mutex
}

// Daemon is the local agent runtime that polls for and executes tasks.
type Daemon struct {
	cfg       Config
	client    *Client
	repoCache *repocache.Cache
	logger    *slog.Logger

	mu           sync.Mutex
	workspaces   map[string]*workspaceState
	runtimeIndex map[string]Runtime // runtimeID -> Runtime for provider lookups
	reloading    sync.Mutex         // prevents concurrent workspace syncs
	runtimeSet   *runtimeSetWatcher // multi-subscriber pub/sub for runtime-set changes

	versionsMu    sync.RWMutex      // guards agentVersions
	agentVersions map[string]string // provider -> detected CLI version (set during registration)

	wsHBMu      sync.RWMutex         // guards wsHBLastAck
	wsHBLastAck map[string]time.Time // runtime_id -> last successful WS heartbeat ack timestamp

	cancelFunc    context.CancelFunc // set by Run(); called by triggerRestart
	restartBinary string             // non-empty after a successful update; path to the new binary
	updating      atomic.Bool        // prevents concurrent update attempts
	activeTasks   atomic.Int64       // number of tasks currently in handleTask; exposed via /health

	activeEnvRootsMu sync.Mutex
	activeEnvRoots   map[string]int // env root path -> reference count (handles reuse paths marked twice)

	// bgSyncs tracks background goroutines started by registerTaskRepos so
	// callers (notably tests using t.TempDir-backed cache roots) can wait for
	// them to drain before tearing the daemon down. Without this the bg
	// goroutine can race against t.TempDir cleanup, leaving a partially
	// deleted bare clone and an unrelated `not empty` cleanup failure.
	bgSyncs sync.WaitGroup
}

// New creates a new Daemon instance.
func New(cfg Config, logger *slog.Logger) *Daemon {
	cacheRoot := filepath.Join(cfg.WorkspacesRoot, ".repos")
	client := NewClient(cfg.ServerBaseURL)
	// Tag every daemon HTTP request with the daemon's CLI version so the
	// server can split logs/metrics by client version (parallel to the CLI).
	client.SetVersion(cfg.CLIVersion)
	return &Daemon{
		cfg:            cfg,
		client:         client,
		repoCache:      repocache.New(cacheRoot, logger),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   make(map[string]Runtime),
		runtimeSet:     newRuntimeSetWatcher(),
		agentVersions:  make(map[string]string),
		wsHBLastAck:    make(map[string]time.Time),
		activeEnvRoots: make(map[string]int),
	}
}

// setAgentVersion records the detected CLI version for an agent provider so
// later task-dispatch code (e.g. Codex sandbox policy) can read it.
func (d *Daemon) setAgentVersion(provider, version string) {
	d.versionsMu.Lock()
	defer d.versionsMu.Unlock()
	d.agentVersions[provider] = version
}

// agentVersion returns the last-detected CLI version for an agent provider,
// or an empty string if unknown.
func (d *Daemon) agentVersion(provider string) string {
	d.versionsMu.RLock()
	defer d.versionsMu.RUnlock()
	return d.agentVersions[provider]
}

func (d *Daemon) notifyRuntimeSetChanged() {
	d.runtimeSet.notify()
}

// runtimeSetWatcher is a tiny pub/sub for runtime-set changes. It exists
// because more than one supervisor (taskWakeupLoop, heartbeatLoop, pollLoop)
// needs to react to runtime-set changes; a single buffered channel would
// race so only the first listener would learn about each change.
//
// Each subscriber gets a 1-slot channel; missed nudges coalesce into a
// single signal — the subscriber is expected to re-derive the current
// runtime set via allRuntimeIDs() rather than relying on edge counts.
type runtimeSetWatcher struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func newRuntimeSetWatcher() *runtimeSetWatcher {
	return &runtimeSetWatcher{subscribers: make(map[chan struct{}]struct{})}
}

// Subscribe returns a channel that receives a non-blocking nudge whenever
// the runtime set changes, and an unsubscribe func the caller must invoke
// when done.
func (w *runtimeSetWatcher) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		delete(w.subscribers, ch)
		w.mu.Unlock()
	}
}

func (w *runtimeSetWatcher) notify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// wsHeartbeatFreshness defines how long a WS heartbeat ack is considered
// "fresh enough" to suppress the HTTP heartbeat for that runtime. The window
// is 2× HeartbeatInterval so a single dropped WS ack still keeps HTTP
// suppressed, but two missed acks (~30s of WS silence) re-enable HTTP — well
// inside the server-side 45s offline threshold.
func (d *Daemon) wsHeartbeatFreshness() time.Duration {
	if d.cfg.HeartbeatInterval <= 0 {
		return 30 * time.Second
	}
	return 2 * d.cfg.HeartbeatInterval
}

// recordWSHeartbeatAck stamps the runtime as having received a fresh WS
// heartbeat ack from the server. Called by the WS read pump.
func (d *Daemon) recordWSHeartbeatAck(runtimeID string) {
	if runtimeID == "" {
		return
	}
	d.wsHBMu.Lock()
	d.wsHBLastAck[runtimeID] = time.Now()
	d.wsHBMu.Unlock()
}

// wsHeartbeatRecentlyAcked reports whether the runtime received a WS
// heartbeat ack inside the freshness window. The HTTP heartbeat loop uses
// this to skip duplicate work when WS is already keeping the runtime alive.
func (d *Daemon) wsHeartbeatRecentlyAcked(runtimeID string) bool {
	d.wsHBMu.RLock()
	last, ok := d.wsHBLastAck[runtimeID]
	d.wsHBMu.RUnlock()
	if !ok {
		return false
	}
	return time.Since(last) < d.wsHeartbeatFreshness()
}

// clearWSHeartbeatAcks drops all WS heartbeat freshness records. Called on
// WS disconnect so HTTP heartbeats resume on the next tick.
func (d *Daemon) clearWSHeartbeatAcks() {
	d.wsHBMu.Lock()
	for k := range d.wsHBLastAck {
		delete(d.wsHBLastAck, k)
	}
	d.wsHBMu.Unlock()
}

// Run starts the daemon: resolves auth, registers runtimes, then polls for tasks.
func (d *Daemon) Run(ctx context.Context) error {
	// Wrap context so handleUpdate can cancel the daemon for restart.
	ctx, cancel := context.WithCancel(ctx)
	d.cancelFunc = cancel

	// Bind health port early to detect another running daemon.
	healthLn, err := d.listenHealth()
	if err != nil {
		return err
	}

	agentNames := make([]string, 0, len(d.cfg.Agents))
	for name := range d.cfg.Agents {
		agentNames = append(agentNames, name)
	}
	logFields := []any{"version", d.cfg.CLIVersion, "agents", agentNames, "server", d.cfg.ServerBaseURL}
	if d.cfg.Profile != "" {
		logFields = append(logFields, "profile", d.cfg.Profile)
	}
	d.logger.Info("starting daemon", logFields...)

	// Load auth token from CLI config.
	if err := d.resolveAuth(); err != nil {
		return err
	}

	// Fetch all user workspaces from the API and register runtimes for any
	// that exist. Zero workspaces is a valid state — a newly-signed-up user
	// may start the daemon before creating their first workspace. The
	// workspaceSyncLoop below polls every 30s and will register runtimes
	// when a workspace appears, so the daemon stays useful as a long-lived
	// background process rather than crashing at startup.
	if err := d.syncWorkspacesFromAPI(ctx); err != nil {
		return err
	}

	// Deregister runtimes on shutdown (uses a fresh context since ctx will be cancelled).
	defer d.deregisterRuntimes()

	// Start workspace sync loop to discover newly created workspaces.
	go d.workspaceSyncLoop(ctx)

	taskWakeups := make(chan struct{}, 1)
	go d.taskWakeupLoop(ctx, taskWakeups)
	go d.heartbeatLoop(ctx)
	go d.gcLoop(ctx)
	go d.serveHealth(ctx, healthLn, time.Now())
	return d.pollLoop(ctx, taskWakeups)
}

// RestartBinary returns the path to the new binary if the daemon needs to restart
// after a successful update, or empty string if no restart is needed.
func (d *Daemon) RestartBinary() string {
	return d.restartBinary
}

// deregisterRuntimes notifies the server that all runtimes are going offline.
func (d *Daemon) deregisterRuntimes() {
	runtimeIDs := d.allRuntimeIDs()
	if len(runtimeIDs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.client.Deregister(ctx, runtimeIDs); err != nil {
		d.logger.Warn("failed to deregister runtimes on shutdown", "error", err)
	} else {
		d.logger.Info("deregistered runtimes", "count", len(runtimeIDs))
	}
}

// resolveAuth loads the auth token from the CLI config for the active profile.
func (d *Daemon) resolveAuth() error {
	cfg, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.Token == "" {
		loginHint := "'multica login'"
		if d.cfg.Profile != "" {
			loginHint = fmt.Sprintf("'multica login --profile %s'", d.cfg.Profile)
		}
		d.logger.Warn("not authenticated — run " + loginHint + " to authenticate, then restart the daemon")
		return fmt.Errorf("not authenticated: run %s first", loginHint)
	}
	d.client.SetToken(cfg.Token)
	d.logger.Info("authenticated")
	return nil
}

// allRuntimeIDs returns all runtime IDs across all watched workspaces.
func (d *Daemon) allRuntimeIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for _, ws := range d.workspaces {
		ids = append(ids, ws.runtimeIDs...)
	}
	return ids
}

// findRuntime looks up a Runtime by its ID.
func (d *Daemon) findRuntime(id string) *Runtime {
	d.mu.Lock()
	defer d.mu.Unlock()
	if rt, ok := d.runtimeIndex[id]; ok {
		return &rt
	}
	return nil
}

func (d *Daemon) registerRuntimesForWorkspace(ctx context.Context, workspaceID string) (*RegisterResponse, error) {
	var runtimes []map[string]string
	for name, entry := range d.cfg.Agents {
		version, err := agent.DetectVersion(ctx, entry.Path)
		if err != nil {
			d.logger.Warn("skip registering runtime", "name", name, "error", err)
			continue
		}
		if err := agent.CheckMinVersion(name, version); err != nil {
			d.logger.Warn("skip registering runtime: version too old", "name", name, "version", version, "error", err)
			continue
		}
		d.setAgentVersion(name, version)
		displayName := strings.ToUpper(name[:1]) + name[1:]
		if d.cfg.DeviceName != "" {
			displayName = fmt.Sprintf("%s (%s)", displayName, d.cfg.DeviceName)
		}
		runtimes = append(runtimes, map[string]string{
			"name":    displayName,
			"type":    name,
			"version": version,
			"status":  "online",
		})
	}
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no agent runtimes could be registered")
	}

	req := map[string]any{
		"workspace_id":      workspaceID,
		"daemon_id":         d.cfg.DaemonID,
		"legacy_daemon_ids": d.cfg.LegacyDaemonIDs,
		"device_name":       d.cfg.DeviceName,
		"cli_version":       d.cfg.CLIVersion,
		"launched_by":       d.cfg.LaunchedBy,
		"runtimes":          runtimes,
	}

	resp, err := d.client.Register(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("register runtimes: %w", err)
	}
	if len(resp.Runtimes) == 0 {
		return nil, fmt.Errorf("register runtimes: empty response")
	}
	return resp, nil
}

func newWorkspaceState(workspaceID string, runtimeIDs []string, reposVersion string, repos []RepoData, settings json.RawMessage) *workspaceState {
	return &workspaceState{
		workspaceID:     workspaceID,
		runtimeIDs:      runtimeIDs,
		reposVersion:    reposVersion,
		allowedRepoURLs: repoAllowlist(repos),
		settings:        settings,
	}
}

func repoAllowlist(repos []RepoData) map[string]struct{} {
	allowed := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		if repo.URL == "" {
			continue
		}
		allowed[repo.URL] = struct{}{}
	}
	return allowed
}

func (d *Daemon) setWorkspaceRepoSyncError(workspaceID, syncErr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ws, ok := d.workspaces[workspaceID]; ok {
		ws.lastRepoSyncErr = syncErr
	}
}

func (d *Daemon) workspaceRepoAllowed(workspaceID, repoURL string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return false
	}
	if _, allowed := ws.allowedRepoURLs[repoURL]; allowed {
		return true
	}
	if _, allowed := ws.taskRepoURLs[repoURL]; allowed {
		return true
	}
	return false
}

func (d *Daemon) workspaceLastRepoSyncErr(workspaceID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return ""
	}
	return ws.lastRepoSyncErr
}

// workspaceCoAuthoredByEnabled returns whether the Co-authored-by hook should
// be installed for the given workspace. Defaults to true when the setting is
// absent (new workspaces, older servers that don't send settings).
func (d *Daemon) workspaceCoAuthoredByEnabled(workspaceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok || len(ws.settings) == 0 {
		return true // default: enabled
	}
	var s struct {
		CoAuthoredByEnabled *bool `json:"co_authored_by_enabled"`
	}
	if err := json.Unmarshal(ws.settings, &s); err != nil || s.CoAuthoredByEnabled == nil {
		return true // default: enabled
	}
	return *s.CoAuthoredByEnabled
}

// registerTaskRepos merges task-scoped repos (e.g. project github_repo
// resources lifted into resp.Repos by the claim handler) into the workspace's
// allowlist and kicks off a cache sync for any URLs that aren't yet cached.
//
// It's safe to call with the workspace's own repos — duplicates are
// idempotent. Called from runTask before the agent spawns so
// `multica repo checkout` accepts project-only URLs without an extra round
// trip back to GetWorkspaceRepos (which doesn't carry project resources).
func (d *Daemon) registerTaskRepos(workspaceID string, repos []RepoData) {
	if len(repos) == 0 {
		return
	}

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()
		return
	}
	if ws.taskRepoURLs == nil {
		ws.taskRepoURLs = make(map[string]struct{}, len(repos))
	}
	toSync := make([]RepoData, 0, len(repos))
	for _, repo := range repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}
		// Don't re-sync if the URL is already tracked (workspace or task-scoped)
		// AND the cache already has it.
		_, inWorkspace := ws.allowedRepoURLs[url]
		_, inTask := ws.taskRepoURLs[url]
		if (inWorkspace || inTask) && d.repoCache != nil && d.repoCache.Lookup(workspaceID, url) != "" {
			ws.taskRepoURLs[url] = struct{}{}
			continue
		}
		ws.taskRepoURLs[url] = struct{}{}
		toSync = append(toSync, RepoData{URL: url})
	}
	d.mu.Unlock()

	if d.repoCache != nil && len(toSync) > 0 {
		// Sync in the background — same shape used at workspace registration.
		// `ensureRepoReady` reports a meaningful error if the cache isn't ready
		// yet, so the agent's first checkout will surface a sync failure
		// without silently treating it as a config bug.
		d.bgSyncs.Add(1)
		go func() {
			defer d.bgSyncs.Done()
			d.syncWorkspaceRepos(workspaceID, toSync)
		}()
	}
}

// waitBackgroundSyncs blocks until every background sync started by
// registerTaskRepos has finished. Intended for test teardown: tests that
// hand the daemon a t.TempDir-backed repo cache must call this before
// returning, otherwise an in-flight clone/fetch can race against TempDir
// cleanup and surface as an unrelated "directory not empty" failure.
func (d *Daemon) waitBackgroundSyncs() {
	d.bgSyncs.Wait()
}

func (d *Daemon) syncWorkspaceRepos(workspaceID string, repos []RepoData) {
	if d.repoCache == nil {
		return
	}
	if err := d.repoCache.Sync(workspaceID, repoDataToInfo(repos)); err != nil {
		d.setWorkspaceRepoSyncError(workspaceID, err.Error())
		d.logger.Warn("repo cache sync failed", "workspace_id", workspaceID, "error", err)
		return
	}
	d.setWorkspaceRepoSyncError(workspaceID, "")
}

func (d *Daemon) refreshWorkspaceRepos(ctx context.Context, workspaceID string) (*WorkspaceReposResponse, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := d.client.GetWorkspaceRepos(refreshCtx, workspaceID)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	if ws, ok := d.workspaces[workspaceID]; ok {
		ws.reposVersion = resp.ReposVersion
		ws.allowedRepoURLs = repoAllowlist(resp.Repos)
	}
	d.mu.Unlock()

	return resp, nil
}

func (d *Daemon) ensureRepoReady(ctx context.Context, workspaceID, repoURL string) error {
	if d.repoCache == nil {
		return fmt.Errorf("repo cache not initialized")
	}

	repoURL = strings.TrimSpace(repoURL)

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("workspace is not watched by this daemon: %s", workspaceID)
	}

	if d.workspaceRepoAllowed(workspaceID, repoURL) && d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	ws.repoRefreshMu.Lock()
	defer ws.repoRefreshMu.Unlock()

	if d.workspaceRepoAllowed(workspaceID, repoURL) && d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	resp, err := d.refreshWorkspaceRepos(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("refresh workspace repos: %w", err)
	}

	if !d.workspaceRepoAllowed(workspaceID, repoURL) {
		return ErrRepoNotConfigured
	}

	d.syncWorkspaceRepos(workspaceID, resp.Repos)

	if d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	if syncErr := d.workspaceLastRepoSyncErr(workspaceID); syncErr != "" {
		return fmt.Errorf("repo is configured but not synced: %s", syncErr)
	}

	return fmt.Errorf("repo is configured but not synced")
}

// workspaceSyncLoop periodically fetches the user's workspaces from the API
// and registers runtimes for any new ones.
func (d *Daemon) workspaceSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(DefaultWorkspaceSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.syncWorkspacesFromAPI(ctx); err != nil {
				d.logger.Debug("workspace sync failed", "error", err)
			}
		}
	}
}

// syncWorkspacesFromAPI fetches all workspaces the user belongs to and
// registers runtimes for any that aren't already tracked. Workspaces the user
// has left are cleaned up.
func (d *Daemon) syncWorkspacesFromAPI(ctx context.Context) error {
	d.reloading.Lock()
	defer d.reloading.Unlock()

	apiCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	workspaces, err := d.client.ListWorkspaces(apiCtx)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	apiIDs := make(map[string]string, len(workspaces)) // id -> name
	for _, ws := range workspaces {
		apiIDs[ws.ID] = ws.Name
	}

	d.mu.Lock()
	currentIDs := make(map[string]bool, len(d.workspaces))
	for id := range d.workspaces {
		currentIDs[id] = true
	}
	d.mu.Unlock()

	var registered int
	var removed int
	for id, name := range apiIDs {
		if currentIDs[id] {
			continue // important: never replace existing workspaceState; ensureRepoReady holds ws.repoRefreshMu from the original pointer
		}
		resp, err := d.registerRuntimesForWorkspace(ctx, id)
		if err != nil {
			d.logger.Error("failed to register runtimes", "workspace_id", id, "name", name, "error", err)
			continue
		}
		runtimeIDs := make([]string, len(resp.Runtimes))
		for i, rt := range resp.Runtimes {
			runtimeIDs[i] = rt.ID
			d.logger.Info("registered runtime", "workspace_id", id, "runtime_id", rt.ID, "provider", rt.Provider)
		}
		d.mu.Lock()
		d.workspaces[id] = newWorkspaceState(id, runtimeIDs, resp.ReposVersion, resp.Repos, resp.Settings)
		for _, rt := range resp.Runtimes {
			d.runtimeIndex[rt.ID] = rt
		}
		d.mu.Unlock()

		if d.repoCache != nil && len(resp.Repos) > 0 {
			go d.syncWorkspaceRepos(id, resp.Repos)
		}

		// Tell the server about any tasks the previous daemon process was
		// running on these runtimes. Without this, an issue can stay stuck
		// at in_progress until the slow heartbeat sweeper or the in-flight
		// task timeout (2.5h) kicks in.
		for _, rid := range runtimeIDs {
			if err := d.client.RecoverOrphans(ctx, rid); err != nil {
				d.logger.Warn("recover-orphans failed", "runtime_id", rid, "error", err)
			}
		}

		d.logger.Info("watching workspace", "workspace_id", id, "name", name, "runtimes", len(resp.Runtimes), "repos", len(resp.Repos))
		registered++
	}

	// Remove workspaces the user no longer belongs to.
	for id := range currentIDs {
		if _, ok := apiIDs[id]; !ok {
			d.mu.Lock()
			if ws, exists := d.workspaces[id]; exists {
				for _, rid := range ws.runtimeIDs {
					delete(d.runtimeIndex, rid)
				}
			}
			delete(d.workspaces, id)
			d.mu.Unlock()
			d.logger.Info("stopped watching workspace", "workspace_id", id)
			removed++
		}
	}
	if registered > 0 || removed > 0 {
		d.notifyRuntimeSetChanged()
	}

	if len(d.allRuntimeIDs()) == 0 && registered == 0 && len(workspaces) > 0 {
		return fmt.Errorf("failed to register runtimes for any of the %d workspace(s)", len(workspaces))
	}
	return nil
}

// heartbeatLoop supervises per-runtime HTTP heartbeat goroutines. Each runtime
// gets an independent ticker so a slow heartbeat for one runtime cannot block
// heartbeats for any other runtime — this matters when a single daemon serves
// multiple workspaces, because the previous shared loop would serialize an
// up-to-30s HTTP timeout across every runtime in the set.
func (d *Daemon) heartbeatLoop(ctx context.Context) {
	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	cancels := make(map[string]context.CancelFunc)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	sync := func() {
		want := make(map[string]struct{})
		for _, rid := range d.allRuntimeIDs() {
			want[rid] = struct{}{}
		}
		for rid, cancel := range cancels {
			if _, ok := want[rid]; !ok {
				cancel()
				delete(cancels, rid)
			}
		}
		for rid := range want {
			if _, ok := cancels[rid]; ok {
				continue
			}
			rctx, rcancel := context.WithCancel(ctx)
			cancels[rid] = rcancel
			go d.runRuntimeHeartbeat(rctx, rid)
		}
	}

	sync()
	for {
		select {
		case <-ctx.Done():
			return
		case <-runtimeSetCh:
			sync()
		}
	}
}

// runRuntimeHeartbeat owns the HTTP heartbeat schedule for a single runtime.
// The first tick fires after a small jittered delay (up to one full interval)
// to avoid a thundering herd when the daemon registers many runtimes at once.
func (d *Daemon) runRuntimeHeartbeat(ctx context.Context, rid string) {
	interval := d.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	// Jittered initial delay; cap at the interval so the first beat still
	// happens within one period.
	if jitter := time.Duration(rand.Int63n(int64(interval))); jitter > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter):
		}
	}

	d.runHeartbeatTick(ctx, rid)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runHeartbeatTick(ctx, rid)
		}
	}
}

func (d *Daemon) runHeartbeatTick(ctx context.Context, rid string) {
	// Skip HTTP heartbeat for runtimes that successfully acked a recent
	// WebSocket heartbeat. The WS path keeps last_seen_at fresh and delivers
	// actions, so the HTTP write would be a duplicate DB update. If the WS
	// heartbeat goes silent the freshness window expires and HTTP resumes
	// automatically on the next tick — that is the fallback the WS path
	// relies on.
	if d.wsHeartbeatRecentlyAcked(rid) {
		return
	}
	resp, err := d.client.SendHeartbeat(ctx, rid)
	if err != nil {
		if ctx.Err() == nil {
			d.logger.Warn("heartbeat failed", "runtime_id", rid, "error", err)
		}
		return
	}
	d.handleHeartbeatActions(ctx, rid, resp)
}

// handleHeartbeatActions dispatches the pending-action set returned by either
// transport (HTTP POST /api/daemon/heartbeat or WS daemon:heartbeat_ack).
// Each action is dispatched in its own goroutine so a slow handler cannot
// block subsequent heartbeats.
func (d *Daemon) handleHeartbeatActions(ctx context.Context, runtimeID string, resp *HeartbeatResponse) {
	if resp == nil {
		return
	}
	if resp.PendingUpdate != nil {
		go d.handleUpdate(ctx, runtimeID, resp.PendingUpdate)
	}
	if resp.PendingModelList != nil {
		if rt := d.findRuntime(runtimeID); rt != nil {
			go d.handleModelList(ctx, *rt, resp.PendingModelList.ID)
		}
	}
	if resp.PendingLocalSkills != nil {
		if rt := d.findRuntime(runtimeID); rt != nil {
			go d.handleLocalSkillList(ctx, *rt, resp.PendingLocalSkills.ID)
		}
	}
	if resp.PendingLocalSkillImport != nil {
		if rt := d.findRuntime(runtimeID); rt != nil {
			go d.handleLocalSkillImport(ctx, *rt, *resp.PendingLocalSkillImport)
		}
	}
}

// handleModelList resolves the provider's supported models (via static
// catalog or by shelling out to the agent CLI) and reports the result
// back to the server. Model discovery failures are reported as empty
// lists rather than errors so the UI can still render a creatable
// dropdown.
func (d *Daemon) handleModelList(ctx context.Context, rt Runtime, requestID string) {
	d.logger.Info("model list requested", "runtime_id", rt.ID, "request_id", requestID, "provider", rt.Provider)

	entry, ok := d.cfg.Agents[rt.Provider]
	if !ok {
		d.reportModelListResult(ctx, rt, requestID, map[string]any{
			"status": "failed",
			"error":  fmt.Sprintf("no agent configured for provider %q", rt.Provider),
		})
		return
	}

	models, err := agent.ListModels(ctx, rt.Provider, entry.Path)
	if err != nil {
		d.reportModelListResult(ctx, rt, requestID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	// Wire format matches handler.ModelEntry. Use a struct (not
	// map[string]string) so the Default bool round-trips — without
	// it the UI loses its "default" badge on the advertised pick.
	type modelWire struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Provider string `json:"provider,omitempty"`
		Default  bool   `json:"default,omitempty"`
	}
	wire := make([]modelWire, 0, len(models))
	for _, m := range models {
		wire = append(wire, modelWire{
			ID:       m.ID,
			Label:    m.Label,
			Provider: m.Provider,
			Default:  m.Default,
		})
	}
	d.reportModelListResult(ctx, rt, requestID, map[string]any{
		"status":    "completed",
		"models":    wire,
		"supported": agent.ModelSelectionSupported(rt.Provider),
	})
}

func (d *Daemon) handleLocalSkillList(ctx context.Context, rt Runtime, requestID string) {
	d.logger.Info("runtime local skills requested", "runtime_id", rt.ID, "request_id", requestID, "provider", rt.Provider)

	skills, supported, err := listRuntimeLocalSkills(rt.Provider)
	if err != nil {
		d.reportLocalSkillListResult(ctx, rt, requestID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	d.reportLocalSkillListResult(ctx, rt, requestID, map[string]any{
		"status":    "completed",
		"skills":    skills,
		"supported": supported,
	})
}

func (d *Daemon) handleLocalSkillImport(ctx context.Context, rt Runtime, pending PendingLocalSkillImport) {
	d.logger.Info("runtime local skill import requested", "runtime_id", rt.ID, "request_id", pending.ID, "provider", rt.Provider, "skill_key", pending.SkillKey)

	skill, supported, err := loadRuntimeLocalSkillBundle(rt.Provider, pending.SkillKey)
	if err != nil {
		d.reportLocalSkillImportResult(ctx, rt, pending.ID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}
	if !supported {
		d.reportLocalSkillImportResult(ctx, rt, pending.ID, map[string]any{
			"status": "failed",
			"error":  fmt.Sprintf("provider %q does not expose runtime local skills", rt.Provider),
		})
		return
	}

	d.reportLocalSkillImportResult(ctx, rt, pending.ID, map[string]any{
		"status": "completed",
		"skill":  skill,
	})
}

// runtimeReportBackoffs defines the retry schedule for delivering any
// daemon→server async result (model list, local-skill list, local-skill
// import). First attempt runs immediately, then we back off. The sum
// (≈6.5s) stays well under the server-side running timeout (60s) so a
// report that eventually lands still updates the request instead of
// racing a timeout transition.
//
// Overridable for tests to avoid real sleeps.
var runtimeReportBackoffs = []time.Duration{0, 500 * time.Millisecond, 2 * time.Second, 4 * time.Second}

// reportLocalSkillListResult delivers a list-report to the server with retry
// on transient failures. See reportRuntimeResultWithRetry for semantics.
func (d *Daemon) reportLocalSkillListResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "local_skill_list", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportLocalSkillListResult(ctx, rt.ID, requestID, payload)
	})
}

// reportLocalSkillImportResult delivers an import-report to the server with
// retry on transient failures.
func (d *Daemon) reportLocalSkillImportResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "local_skill_import", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportLocalSkillImportResult(ctx, rt.ID, requestID, payload)
	})
}

// reportModelListResult delivers a model-list report to the server with retry
// on transient failures. Without this the daemon used to fire once and
// swallow any 5xx, leaving the request stranded in "running" on the server
// until its 60s timeout — defeating the multi-node store fix.
func (d *Daemon) reportModelListResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "model_list", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportModelListResult(ctx, rt.ID, requestID, payload)
	})
}

// reportRuntimeResultWithRetry retries `fn` on 5xx / network errors and
// stops on success, 4xx, or after exhausting runtimeReportBackoffs.
//
// Why this exists: the server persists the report through a Redis / DB
// write; on a transient store failure it correctly returns 500. Without a
// client-side retry the daemon would fire once, swallow the error, and the
// pending request stays in "running" on the server until its timeout — which
// is exactly the "daemon did not respond" failure mode the multi-node store
// fix was meant to eliminate. 4xx is treated as permanent (request-not-found,
// cross-workspace token rejected, bad body) — retrying those just wastes
// heartbeat cycles.
func (d *Daemon) reportRuntimeResultWithRetry(ctx context.Context, kind, runtimeID, requestID string, fn func(context.Context) error) {
	var lastErr error
	for attempt, wait := range runtimeReportBackoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				d.logger.Error("runtime async report cancelled",
					"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
					"attempt", attempt, "error", ctx.Err())
				return
			case <-time.After(wait):
			}
		}
		err := fn(ctx)
		if err == nil {
			if attempt > 0 {
				d.logger.Info("runtime async report succeeded after retry",
					"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
					"attempt", attempt+1)
			}
			return
		}
		lastErr = err

		// 4xx is permanent (request expired, workspace mismatch, malformed
		// body). No amount of retrying will make it succeed.
		var reqErr *requestError
		if errors.As(err, &reqErr) && reqErr.StatusCode >= 400 && reqErr.StatusCode < 500 {
			d.logger.Error("runtime async report rejected — not retrying",
				"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
				"status", reqErr.StatusCode, "error", err)
			return
		}

		d.logger.Warn("runtime async report failed — will retry",
			"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
			"attempt", attempt+1, "error", err)
	}
	d.logger.Error("runtime async report exhausted retries",
		"kind", kind, "runtime_id", runtimeID, "request_id", requestID, "error", lastErr)
}

// handleUpdate performs the CLI update when triggered by the server via heartbeat.
func (d *Daemon) handleUpdate(ctx context.Context, runtimeID string, update *PendingUpdate) {
	// Desktop-managed daemons share their CLI binary with the Electron app,
	// which is responsible for shipping and replacing it. Letting the daemon
	// self-update would just get overwritten on the next Desktop launch and
	// could brick the embedded binary mid-update. Refuse cleanly.
	if d.cfg.LaunchedBy == "desktop" {
		d.logger.Info("refusing CLI self-update: daemon is managed by Desktop", "runtime_id", runtimeID, "update_id", update.ID)
		d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
			"status": "failed",
			"error":  "CLI is managed by Multica Desktop — update the Desktop app to upgrade the CLI",
		})
		return
	}

	// Prevent concurrent update attempts.
	if !d.updating.CompareAndSwap(false, true) {
		d.logger.Warn("update already in progress, ignoring", "runtime_id", runtimeID, "update_id", update.ID)
		return
	}
	defer d.updating.Store(false)

	d.logger.Info("CLI update requested", "runtime_id", runtimeID, "update_id", update.ID, "target_version", update.TargetVersion)

	// Report running status.
	d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
		"status": "running",
	})

	// Try Homebrew first, fall back to direct download.
	var output string
	if cli.IsBrewInstall() {
		d.logger.Info("updating CLI via Homebrew...")
		var err error
		output, err = cli.UpdateViaBrew()
		if err != nil {
			d.logger.Error("CLI update failed", "error", err, "output", output)
			d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
				"status": "failed",
				"error":  fmt.Sprintf("brew upgrade failed: %v", err),
			})
			return
		}
	} else {
		d.logger.Info("updating CLI via direct download...", "target_version", update.TargetVersion)
		var err error
		output, err = cli.UpdateViaDownload(update.TargetVersion)
		if err != nil {
			d.logger.Error("CLI update failed", "error", err)
			d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
				"status": "failed",
				"error":  fmt.Sprintf("download update failed: %v", err),
			})
			return
		}
	}

	d.logger.Info("CLI update completed successfully", "output", output)
	d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
		"status": "completed",
		"output": fmt.Sprintf("Updated to %s", update.TargetVersion),
	})

	// Trigger daemon restart with the new binary.
	d.triggerRestart()
}

// updateReportBackoffs defines the retry schedule for delivering CLI update
// status back to the server. This mirrors localSkillReportBackoffs because
// both features have the same user-visible failure mode: the daemon completed
// work locally, but a transient report failure leaves the UI waiting until the
// server-side request times out.
//
// Overridable for tests to avoid real sleeps.
var updateReportBackoffs = []time.Duration{0, 500 * time.Millisecond, 2 * time.Second, 4 * time.Second}

func (d *Daemon) reportUpdateResult(ctx context.Context, runtimeID, updateID string, payload map[string]any) {
	d.reportUpdateResultWithRetry(ctx, runtimeID, updateID, func(ctx context.Context) error {
		return d.client.ReportUpdateResult(ctx, runtimeID, updateID, payload)
	})
}

func (d *Daemon) reportUpdateResultWithRetry(ctx context.Context, runtimeID, updateID string, fn func(context.Context) error) {
	var lastErr error
	for attempt, wait := range updateReportBackoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				d.logger.Error("CLI update report cancelled",
					"runtime_id", runtimeID, "update_id", updateID,
					"attempt", attempt, "error", ctx.Err())
				return
			case <-time.After(wait):
			}
		}

		err := fn(ctx)
		if err == nil {
			if attempt > 0 {
				d.logger.Info("CLI update report succeeded after retry",
					"runtime_id", runtimeID, "update_id", updateID,
					"attempt", attempt+1)
			}
			return
		}
		lastErr = err

		var reqErr *requestError
		if errors.As(err, &reqErr) && reqErr.StatusCode >= 400 && reqErr.StatusCode < 500 {
			d.logger.Error("CLI update report rejected — not retrying",
				"runtime_id", runtimeID, "update_id", updateID,
				"status", reqErr.StatusCode, "error", err)
			return
		}

		d.logger.Warn("CLI update report failed — will retry",
			"runtime_id", runtimeID, "update_id", updateID,
			"attempt", attempt+1, "error", err)
	}
	d.logger.Error("CLI update report exhausted retries",
		"runtime_id", runtimeID, "update_id", updateID, "error", lastErr)
}

// triggerRestart initiates a graceful daemon restart after a successful CLI update.
// For brew installs, it keeps the symlink path (e.g. /opt/homebrew/bin/multica)
// so the restarted daemon picks up the new Cellar version automatically.
// For non-brew installs, it resolves to the absolute path of the replaced binary.
// The caller (cmd_daemon.go) checks RestartBinary() and launches the new process.
func (d *Daemon) triggerRestart() {
	newBin, err := os.Executable()
	if err != nil {
		d.logger.Error("could not resolve executable path for restart", "error", err)
		return
	}
	// Only resolve symlinks for non-brew installs. Brew uses a symlink that
	// points to the latest Cellar version, so we must preserve it.
	if !cli.IsBrewInstall() {
		if resolved, err := filepath.EvalSymlinks(newBin); err == nil {
			newBin = resolved
		}
	}

	d.logger.Info("scheduling daemon restart", "new_binary", newBin)
	d.restartBinary = newBin

	// Cancel the main context to trigger graceful shutdown.
	if d.cancelFunc != nil {
		d.cancelFunc()
	}
}

// pollLoop supervises one runtimePoller goroutine per registered runtime,
// fans wake-up signals out to all of them, and waits for in-flight tasks to
// drain on shutdown. Per-runtime workers replace the previous round-robin
// loop so that a slow ClaimTask call (HTTP 30s timeout) for one runtime no
// longer delays claims on every other runtime — that was the cross-workspace
// stall mode reported in MUL-1744.
func (d *Daemon) pollLoop(ctx context.Context, taskWakeups <-chan struct{}) error {
	sem := newTaskSlotSemaphore(d.cfg.MaxConcurrentTasks)
	var taskWG sync.WaitGroup   // tracks in-flight handleTask goroutines
	var pollerWG sync.WaitGroup // tracks runRuntimePoller goroutines

	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	type pollerHandle struct {
		cancel context.CancelFunc
		wakeup chan struct{}
	}
	pollers := make(map[string]*pollerHandle)

	syncPollers := func() {
		want := make(map[string]struct{})
		for _, rid := range d.allRuntimeIDs() {
			want[rid] = struct{}{}
		}
		for rid, h := range pollers {
			if _, ok := want[rid]; !ok {
				h.cancel()
				delete(pollers, rid)
			}
		}
		for rid := range want {
			if _, ok := pollers[rid]; ok {
				continue
			}
			pctx, pcancel := context.WithCancel(ctx)
			wakeup := make(chan struct{}, 1)
			pollers[rid] = &pollerHandle{cancel: pcancel, wakeup: wakeup}
			pollerWG.Add(1)
			go func(rid string, pctx context.Context, wakeup <-chan struct{}) {
				defer pollerWG.Done()
				d.runRuntimePoller(pctx, ctx, rid, sem, wakeup, &taskWG)
			}(rid, pctx, wakeup)
		}
	}

	syncPollers()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("poll loop stopping, waiting for in-flight tasks", "max_wait", "30s")
			for _, h := range pollers {
				h.cancel()
			}
			// Wait for all pollers to fully return before waiting on taskWG.
			// Otherwise a poller that's between ClaimTask and taskWG.Add(1)
			// could race with taskWG.Wait when the counter is zero, which
			// is an undefined sync.WaitGroup misuse.
			pollerWG.Wait()

			waitDone := make(chan struct{})
			go func() { taskWG.Wait(); close(waitDone) }()
			select {
			case <-waitDone:
			case <-time.After(30 * time.Second):
				d.logger.Warn("timed out waiting for in-flight tasks")
			}
			return ctx.Err()
		case <-runtimeSetCh:
			syncPollers()
		case <-taskWakeups:
			// Fan out to every runtime poller. Any of them might have a queued
			// task; the per-poller wakeup channel coalesces (cap 1) so a burst
			// of wake-ups doesn't pile up.
			for _, h := range pollers {
				select {
				case h.wakeup <- struct{}{}:
				default:
				}
			}
		}
	}
}

// runRuntimePoller is the per-runtime claim+dispatch loop. It owns its own
// poll cadence and wakeup channel so that a slow HTTP claim for this runtime
// cannot delay any other runtime's claims.
//
// The execution slot is acquired BEFORE ClaimTask. The alternative —
// claiming first and then waiting for a slot — would let claimed tasks pile
// up in the server-side `dispatched` state without a corresponding
// StartTask, and the server's sweeper would fail them as `failed/timeout`
// after dispatchTimeoutSeconds=300s (runtime_sweeper.go:25). That is the
// exact user-visible failure this issue is fixing, so we cannot risk
// recreating it under load.
//
// Slot-before-claim does mean a slow claim holds a slot during its HTTP
// roundtrip; the upper bound is `client.Timeout = 30s` (client.go:59), well
// below the 300s dispatch timeout, so other runtimes' tasks stay in
// server-side `queued` state (which has no timeout) rather than entering
// `dispatched` and racing the sweeper.
//
// pollerCtx is cancelled when this runtime is removed from the watched set
// (e.g. workspace de-registered). parentCtx is the daemon's root ctx and is
// passed to handleTask so an in-flight task is not killed just because the
// runtime set changed mid-flight — the task continues to run until the
// daemon itself shuts down (or the server cancels it).
func (d *Daemon) runRuntimePoller(
	pollerCtx, parentCtx context.Context,
	rid string,
	sem chan int,
	wakeup <-chan struct{},
	taskWG *sync.WaitGroup,
) {
	for {
		if pollerCtx.Err() != nil {
			return
		}

		// Acquire an execution slot before claiming. If at capacity, sleep
		// without claiming so we don't push a task into `dispatched` and
		// then race the 5-min server-side dispatch timeout while waiting.
		var slot int
		select {
		case slot = <-sem:
		case <-pollerCtx.Done():
			return
		default:
			d.logger.Debug("poll: at capacity", "runtime_id", rid, "running", d.cfg.MaxConcurrentTasks)
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		task, err := d.client.ClaimTask(pollerCtx, rid)
		if err != nil {
			sem <- slot
			if pollerCtx.Err() == nil {
				d.logger.Warn("claim task failed", "runtime_id", rid, "error", err)
			}
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		if task == nil {
			sem <- slot
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		taskTarget := task.IssueID
		if taskTarget == "" && task.ChatSessionID != "" {
			taskTarget = "chat:" + shortID(task.ChatSessionID)
		}
		d.logger.Info("task received", "task", shortID(task.ID), "target", taskTarget)
		taskWG.Add(1)
		d.activeTasks.Add(1)
		go func(t Task, slot int) {
			defer taskWG.Done()
			defer d.activeTasks.Add(-1)
			defer func() { sem <- slot }()
			d.handleTask(parentCtx, t, slot)
		}(*task, slot)
		// Loop immediately: more tasks may already be queued for this runtime.
	}
}

// newTaskSlotSemaphore returns a buffered channel pre-populated with stable
// slot indices [0, n). Receive to acquire a slot, send the same slot back to
// release. Used by pollLoop to expose MULTICA_TASK_SLOT to spawned tasks.
func newTaskSlotSemaphore(maxConcurrentTasks int) chan int {
	sem := make(chan int, maxConcurrentTasks)
	for i := 0; i < maxConcurrentTasks; i++ {
		sem <- i
	}
	return sem
}

func (d *Daemon) handleTask(ctx context.Context, task Task, slot int) {
	d.mu.Lock()
	rt := d.runtimeIndex[task.RuntimeID]
	d.mu.Unlock()
	provider := rt.Provider

	// Task-scoped logger with short ID for readable concurrent logs.
	taskLog := d.logger.With("task", shortID(task.ID))
	agentName := "agent"
	if task.Agent != nil {
		agentName = task.Agent.Name
	}
	if task.ChatSessionID != "" {
		taskLog.Info("picked chat task", "chat_session", shortID(task.ChatSessionID), "agent", agentName, "provider", provider)
	} else {
		taskLog.Info("picked task", "issue", task.IssueID, "agent", agentName, "provider", provider)
	}

	if err := d.client.StartTask(ctx, task.ID); err != nil {
		taskLog.Error("start task failed", "error", err)
		if failErr := d.client.FailTask(ctx, task.ID, fmt.Sprintf("start task failed: %s", err.Error()), "", "", "agent_error"); failErr != nil {
			taskLog.Error("fail task after start error", "error", failErr)
		}
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Launching %s", provider), 1, 2)

	// Create a cancellable context so we can interrupt the running agent
	// when the server-side task status changes to 'cancelled'.
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	// Poll for cancellation every 5 seconds while the task is running.
	cancelledByPoll := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if status, err := d.client.GetTaskStatus(ctx, task.ID); err == nil && status == "cancelled" {
					taskLog.Info("task cancelled by server, interrupting agent")
					runCancel()
					close(cancelledByPoll)
					return
				}
			}
		}
	}()

	result, err := d.runTask(runCtx, task, provider, slot, taskLog)

	// Check if we were cancelled by the polling goroutine.
	select {
	case <-cancelledByPoll:
		taskLog.Info("task cancelled during execution, discarding result")
		return
	default:
	}

	if err != nil {
		taskLog.Error("task failed", "error", err)
		// runTask returned without a TaskResult, so we don't have a SessionID
		// to forward — best we can do is record the failure.
		if failErr := d.client.FailTask(ctx, task.ID, err.Error(), "", "", "agent_error"); failErr != nil {
			taskLog.Error("fail task callback failed", "error", failErr)
		}
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, "Finishing task", 2, 2)

	// Check if the task was cancelled while it was running (e.g. issue
	// was reassigned). If so, skip reporting results — the server already
	// moved the task to 'cancelled' so complete/fail would fail anyway.
	if status, err := d.client.GetTaskStatus(ctx, task.ID); err == nil && status == "cancelled" {
		taskLog.Info("task cancelled during execution, discarding result")
		return
	}

	// Report usage independently so it's captured even for failed/blocked tasks.
	if len(result.Usage) > 0 {
		if err := d.client.ReportTaskUsage(ctx, task.ID, result.Usage); err != nil {
			taskLog.Warn("report task usage failed", "error", err)
		}
	}

	switch result.Status {
	case "blocked":
		// Forward SessionID/WorkDir even on the blocked path: the agent may
		// have built a real session before getting stuck (rate-limit, tool
		// error, etc.) and we want the next chat turn to resume there
		// rather than start over and "forget" the conversation.
		failureReason := result.FailureReason
		if failureReason == "" {
			failureReason = "agent_error"
		}
		if err := d.client.FailTask(ctx, task.ID, result.Comment, result.SessionID, result.WorkDir, failureReason); err != nil {
			taskLog.Error("report blocked task failed", "error", err)
		}
	default:
		taskLog.Info("task completed", "status", result.Status)
		if err := d.client.CompleteTask(ctx, task.ID, result.Comment, result.BranchName, result.SessionID, result.WorkDir); err != nil {
			taskLog.Error("complete task failed, falling back to fail", "error", err)
			if failErr := d.client.FailTask(ctx, task.ID, fmt.Sprintf("complete task failed: %s", err.Error()), result.SessionID, result.WorkDir, "agent_error"); failErr != nil {
				taskLog.Error("fail task fallback also failed", "error", failErr)
			}
		}
	}

	// Write GC metadata after the task finishes so the periodic GC loop
	// can look up the issue later. Written last so that a mid-task crash
	// leaves the directory as an orphan (cleaned up by GCOrphanTTL).
	if result.EnvRoot != "" {
		if err := execenv.WriteGCMeta(result.EnvRoot, task.IssueID, task.WorkspaceID); err != nil {
			taskLog.Warn("write gc meta failed (non-fatal)", "error", err)
		}
	}
}

func (d *Daemon) runTask(ctx context.Context, task Task, provider string, slot int, taskLog *slog.Logger) (TaskResult, error) {
	// Refuse to spawn an agent without a workspace. An empty workspace_id
	// here would make MULTICA_WORKSPACE_ID empty in the agent env, and the
	// CLI would otherwise silently fall back to the user-global config — a
	// path that can leak operations into an unrelated workspace when
	// multiple workspaces share a host.
	if task.WorkspaceID == "" {
		return TaskResult{}, fmt.Errorf("refusing to spawn agent: task has no workspace_id (task_id=%s)", task.ID)
	}

	// task.Repos is the authoritative repo list for this task — when the
	// claimed task belongs to a project with github_repo resources the server
	// has already narrowed it to project repos only. Make sure those URLs are
	// in the per-workspace allowlist and the local cache, otherwise
	// `multica repo checkout` would reject project-only URLs that aren't also
	// bound at the workspace level.
	d.registerTaskRepos(task.WorkspaceID, task.Repos)

	entry, ok := d.cfg.Agents[provider]
	if !ok {
		return TaskResult{}, fmt.Errorf("no agent configured for provider %q", provider)
	}

	agentName := "agent"
	var agentID string
	var skills []SkillData
	var instructions string
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
		skills = task.Agent.Skills
		instructions = task.Agent.Instructions
	}

	// Prepare isolated execution environment.
	// Repos are passed as metadata only — the agent checks them out on demand
	// via `multica repo checkout <url>`.
	taskCtx := execenv.TaskContextForEnv{
		IssueID:                 task.IssueID,
		TriggerCommentID:        task.TriggerCommentID,
		AgentID:                 agentID,
		AgentName:               agentName,
		AgentInstructions:       instructions,
		AgentSkills:             convertSkillsForEnv(skills),
		Repos:                   convertReposForEnv(task.Repos),
		ProjectID:               task.ProjectID,
		ProjectTitle:            task.ProjectTitle,
		ProjectResources:        convertProjectResourcesForEnv(task.ProjectResources),
		ChatSessionID:           task.ChatSessionID,
		AutopilotRunID:          task.AutopilotRunID,
		AutopilotID:             task.AutopilotID,
		AutopilotTitle:          task.AutopilotTitle,
		AutopilotDescription:    task.AutopilotDescription,
		AutopilotSource:         task.AutopilotSource,
		AutopilotTriggerPayload: strings.TrimSpace(string(task.AutopilotTriggerPayload)),
		QuickCreatePrompt:       task.QuickCreatePrompt,
	}

	// Mark candidate env roots as active before any env work so the GC loop
	// can't reclaim artifacts inside them mid-execution. We mark both the
	// predicted root for a fresh Prepare and the prior root for Reuse — they
	// usually differ (Reuse keeps the original task's directory).
	predictedRoot := execenv.PredictRootDir(d.cfg.WorkspacesRoot, task.WorkspaceID, task.ID)
	d.markActiveEnvRoot(predictedRoot)
	defer d.unmarkActiveEnvRoot(predictedRoot)
	if task.PriorWorkDir != "" {
		priorRoot := filepath.Dir(task.PriorWorkDir)
		if priorRoot != predictedRoot {
			d.markActiveEnvRoot(priorRoot)
			defer d.unmarkActiveEnvRoot(priorRoot)
		}
	}

	// Try to reuse the workdir from a previous task on the same (agent, issue) pair.
	var env *execenv.Environment
	codexVersion := d.agentVersion("codex")
	if task.PriorWorkDir != "" {
		env = execenv.Reuse(task.PriorWorkDir, provider, codexVersion, taskCtx, d.logger)
	}
	if env == nil {
		var err error
		env, err = execenv.Prepare(execenv.PrepareParams{
			WorkspacesRoot: d.cfg.WorkspacesRoot,
			WorkspaceID:    task.WorkspaceID,
			TaskID:         task.ID,
			AgentName:      agentName,
			Provider:       provider,
			CodexVersion:   codexVersion,
			Task:           taskCtx,
		}, d.logger)
		if err != nil {
			return TaskResult{}, fmt.Errorf("prepare execution environment: %w", err)
		}
	}
	// Belt-and-suspenders: also mark whatever root we ended up with, in case
	// future changes diverge from PredictRootDir.
	if env.RootDir != predictedRoot && env.RootDir != "" {
		d.markActiveEnvRoot(env.RootDir)
		defer d.unmarkActiveEnvRoot(env.RootDir)
	}

	// Inject runtime-specific config (meta skill) so the agent discovers .agent_context/.
	if err := execenv.InjectRuntimeConfig(env.WorkDir, provider, taskCtx); err != nil {
		d.logger.Warn("execenv: inject runtime config failed (non-fatal)", "error", err)
	}
	// NOTE: No cleanup — workdir is preserved for reuse by future tasks on
	// the same (agent, issue) pair. The work_dir path is stored in DB on
	// task completion and passed back via PriorWorkDir on the next claim.

	prompt := BuildPrompt(task)

	// Pass the daemon's auth credentials and context so the spawned agent CLI
	// can call the Multica API and the local daemon (e.g. `multica repo checkout`).
	// MULTICA_TASK_SLOT is allocated from the daemon-wide concurrency pool, not
	// per-agent. When one daemon hosts multiple agents, slots index shared
	// daemon-level resources such as GPUs.
	agentEnv := map[string]string{
		"MULTICA_TOKEN":        d.client.Token(),
		"MULTICA_SERVER_URL":   d.cfg.ServerBaseURL,
		"MULTICA_DAEMON_PORT":  fmt.Sprintf("%d", d.cfg.HealthPort),
		"MULTICA_WORKSPACE_ID": task.WorkspaceID,
		"MULTICA_AGENT_NAME":   agentName,
		"MULTICA_AGENT_ID":     task.AgentID,
		"MULTICA_TASK_ID":      task.ID,
		"MULTICA_TASK_SLOT":    strconv.Itoa(slot),
	}
	if task.AutopilotRunID != "" {
		agentEnv["MULTICA_AUTOPILOT_RUN_ID"] = task.AutopilotRunID
	}
	if task.AutopilotID != "" {
		agentEnv["MULTICA_AUTOPILOT_ID"] = task.AutopilotID
	}
	// Quick-create marker — when set, the multica CLI's `issue create`
	// command stamps the new issue with origin_type=quick_create +
	// origin_id=<task_id> so the completion handler can find it
	// deterministically (see GetIssueByOrigin).
	if task.QuickCreatePrompt != "" {
		agentEnv["MULTICA_QUICK_CREATE_TASK_ID"] = task.ID
	}
	// Ensure the multica CLI is on PATH inside the agent's environment.
	// Some runtimes (e.g. Codex) run in an isolated sandbox that may not
	// inherit the daemon's PATH. Prepend the directory of the running
	// multica binary so that `multica` commands in the agent always resolve.
	if selfBin, err := os.Executable(); err == nil {
		binDir := filepath.Dir(selfBin)
		agentEnv["PATH"] = binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	// Point Codex to the per-task CODEX_HOME so it discovers skills natively
	// without polluting the system ~/.codex/skills/.
	if env.CodexHome != "" {
		agentEnv["CODEX_HOME"] = env.CodexHome
	}
	// Inject user-configured custom environment variables (e.g. ANTHROPIC_API_KEY,
	// ANTHROPIC_BASE_URL for router/proxy mode, or CLAUDE_CODE_USE_BEDROCK for
	// Bedrock). These are set per-agent via the agent settings UI.
	// Critical internal variables are blocklisted to prevent accidental or
	// malicious override of daemon-set values.
	if task.Agent != nil {
		for k, v := range task.Agent.CustomEnv {
			if isBlockedEnvKey(k) {
				d.logger.Warn("custom_env: blocked key skipped", "key", k)
				continue
			}
			agentEnv[k] = v
		}
	}
	backend, err := agent.New(provider, agent.Config{
		ExecutablePath: entry.Path,
		Env:            agentEnv,
		Logger:         d.logger,
	})
	if err != nil {
		return TaskResult{}, fmt.Errorf("create agent backend: %w", err)
	}

	reused := task.PriorWorkDir != "" && env.WorkDir == task.PriorWorkDir
	taskLog.Info("starting agent",
		"provider", provider,
		"workdir", env.WorkDir,
		"model", entry.Model,
		"reused", reused,
	)
	if task.PriorSessionID != "" {
		taskLog.Info("resuming session", "session_id", task.PriorSessionID)
	}

	taskStart := time.Now()

	var customArgs []string
	extraArgs := defaultArgsForProvider(d.cfg, provider)
	var mcpConfig json.RawMessage
	if task.Agent != nil {
		customArgs = task.Agent.CustomArgs
		mcpConfig = task.Agent.McpConfig
	}
	// Two-tier model resolution: an explicit agent.model wins,
	// then the daemon-wide MULTICA_<PROVIDER>_MODEL env var. If
	// both are empty we deliberately pass "" through — each
	// backend omits `--model` from the CLI invocation, so the
	// provider picks its own default (Claude Code's shipped
	// default, codex app-server's account-scoped default, etc.).
	// Baking a Go-side "recommended default" here is how the
	// cursor regression happened — static guesses drift from
	// whatever the upstream CLI actually accepts.
	model := ""
	if task.Agent != nil && task.Agent.Model != "" {
		model = task.Agent.Model
	}
	if model == "" {
		model = entry.Model
	}
	execOpts := agent.ExecOptions{
		Cwd:                       env.WorkDir,
		Model:                     model,
		Timeout:                   d.cfg.AgentTimeout,
		SemanticInactivityTimeout: d.cfg.CodexSemanticInactivityTimeout,
		ResumeSessionID:           task.PriorSessionID,
		ExtraArgs:                 extraArgs,
		CustomArgs:                customArgs,
		McpConfig:                 mcpConfig,
	}
	// openclaw loads its bootstrap files (AGENTS.md, SOUL.md, ...) from its own
	// workspace dir rather than the task workdir, so the AGENTS.md written by
	// execenv.InjectRuntimeConfig is never read. Pass agent instructions inline
	// via SystemPrompt so the backend can prepend them to the --message payload.
	// Other providers already surface instructions through their runtime config
	// file and don't need this.
	if provider == "openclaw" {
		execOpts.SystemPrompt = instructions
	}

	result, tools, err := d.executeAndDrain(ctx, backend, prompt, execOpts, taskLog, task.ID)
	if err != nil {
		return TaskResult{}, err
	}

	// Fallback: if session resume failed before establishing a session, retry
	// with a fresh session. We check SessionID == "" to distinguish a resume
	// failure (no session established) from a failure during actual execution.
	if result.Status == "failed" && task.PriorSessionID != "" && result.SessionID == "" {
		firstUsage := result.Usage
		taskLog.Warn("session resume failed, retrying with fresh session", "error", result.Error)
		execOpts.ResumeSessionID = ""
		retryResult, retryTools, retryErr := d.executeAndDrain(ctx, backend, prompt, execOpts, taskLog, task.ID)
		if retryErr != nil {
			taskLog.Error("fresh session also failed to start", "error", retryErr)
		} else {
			result = retryResult
			result.Usage = mergeUsage(firstUsage, result.Usage)
			tools = retryTools
		}
	}

	elapsed := time.Since(taskStart).Round(time.Second)
	taskLog.Info("agent finished",
		"status", result.Status,
		"duration", elapsed.String(),
		"tools", tools,
	)

	// Convert agent usage map to task usage entries.
	var usageEntries []TaskUsageEntry
	for model, u := range result.Usage {
		if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 && u.CacheWriteTokens == 0 {
			continue
		}
		usageEntries = append(usageEntries, TaskUsageEntry{
			Provider:         provider,
			Model:            model,
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
		})
	}

	switch result.Status {
	case "completed":
		if result.Output == "" {
			// Even an empty-output completion may have established a real
			// session — surface it through the blocked path so the next chat
			// turn can still resume from where this one left off.
			return TaskResult{
				Status:    "blocked",
				Comment:   fmt.Sprintf("%s returned empty output", provider),
				SessionID: result.SessionID,
				WorkDir:   env.WorkDir,
				EnvRoot:   env.RootDir,
				Usage:     usageEntries,
			}, nil
		}
		// Detect "poisoned" terminal output: the agent didn't reach a real
		// conclusion but emitted a known fallback marker (iteration limit,
		// fallback meta message). Route through the blocked path with a
		// specific failure_reason so the server can exclude this session
		// from the (agent_id, issue_id) resume lookup — otherwise a manual
		// rerun would inherit the same poisoned session and reproduce the
		// same bad output.
		if reason, ok := classifyPoisonedOutput(result.Output); ok {
			taskLog.Warn("agent finished with poisoned fallback output, classifying as blocked",
				"failure_reason", reason,
			)
			return TaskResult{
				Status:        "blocked",
				Comment:       result.Output,
				SessionID:     result.SessionID,
				WorkDir:       env.WorkDir,
				EnvRoot:       env.RootDir,
				Usage:         usageEntries,
				FailureReason: reason,
			}, nil
		}
		return TaskResult{
			Status:    "completed",
			Comment:   result.Output,
			SessionID: result.SessionID,
			WorkDir:   env.WorkDir,
			EnvRoot:   env.RootDir,
			Usage:     usageEntries,
		}, nil
	case "timeout":
		// Surface session_id/work_dir so the chat resume pointer is kept
		// in sync even when the agent times out after building a session.
		// We mark as "blocked" (not a hard error return) so handleTask
		// goes through the FailTask path that forwards session info.
		comment := result.Error
		if comment == "" {
			comment = fmt.Sprintf("%s timed out after %s", provider, d.cfg.AgentTimeout)
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       comment,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			FailureReason: "timeout",
			Usage:         usageEntries,
		}, nil
	case "cancelled":
		// Server cancelled the task (e.g. issue reassignment, user cancel).
		// handleTask's cancelledByPoll branch already discards this result,
		// so this case is mainly defensive — and preserves the "cancelled"
		// status string for the "agent finished" log line so operators can
		// distinguish "task cancelled by server" from a real timeout.
		return TaskResult{
			Status:    "cancelled",
			Comment:   "task cancelled by server",
			SessionID: result.SessionID,
			WorkDir:   env.WorkDir,
			EnvRoot:   env.RootDir,
			Usage:     usageEntries,
		}, nil
	default:
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("%s execution %s", provider, result.Status)
		}
		// Forward SessionID/WorkDir on the blocked path: backends commonly
		// emit a real session_id before failing (rate-limit, tool error,
		// model reject, …). Without this the chat_session resume pointer
		// would either be left stale or overwritten with NULL on the
		// server, causing the next chat turn to lose context.
		return TaskResult{
			Status:    "blocked",
			Comment:   errMsg,
			SessionID: result.SessionID,
			WorkDir:   env.WorkDir,
			EnvRoot:   env.RootDir,
			Usage:     usageEntries,
		}, nil
	}
}

// executeAndDrain runs a backend, drains its message stream (forwarding to the
// server), and waits for the final result.
func (d *Daemon) executeAndDrain(ctx context.Context, backend agent.Backend, prompt string, opts agent.ExecOptions, taskLog *slog.Logger, taskID string) (agent.Result, int32, error) {
	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		return agent.Result{}, 0, err
	}

	// Create an independent drain deadline so we don't block forever if the
	// backend's internal timeout fails to produce a Result (e.g. scanner
	// stuck on a hung stdout pipe). The extra 30 s gives the backend time
	// to clean up after its own timeout fires.
	drainTimeout := opts.Timeout + 30*time.Second
	if opts.Timeout == 0 {
		drainTimeout = 21 * time.Minute
	}
	drainCtx, drainCancel := context.WithTimeout(ctx, drainTimeout)
	defer drainCancel()

	var toolCount atomic.Int32
	go func() {
		var seq atomic.Int32
		var mu sync.Mutex
		var pendingText strings.Builder
		var pendingThinking strings.Builder
		var batch []TaskMessageData
		callIDToTool := map[string]string{}

		flush := func() {
			mu.Lock()
			if pendingThinking.Len() > 0 {
				s := seq.Add(1)
				batch = append(batch, TaskMessageData{
					Seq:     int(s),
					Type:    "thinking",
					Content: pendingThinking.String(),
				})
				pendingThinking.Reset()
			}
			if pendingText.Len() > 0 {
				s := seq.Add(1)
				batch = append(batch, TaskMessageData{
					Seq:     int(s),
					Type:    "text",
					Content: pendingText.String(),
				})
				pendingText.Reset()
			}
			toSend := batch
			batch = nil
			mu.Unlock()

			if len(toSend) > 0 {
				sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := d.client.ReportTaskMessages(sendCtx, taskID, toSend); err != nil {
					taskLog.Debug("failed to report task messages", "error", err)
				} else {
					taskLog.Debug("reported task messages", "count", len(toSend), "last_seq", toSend[len(toSend)-1].Seq)
				}
				cancel()
			}
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-ticker.C:
					flush()
				case <-done:
					return
				}
			}
		}()

		var sessionPinned atomic.Bool
		for {
			select {
			case msg, ok := <-session.Messages:
				if !ok {
					goto drainDone
				}
				switch msg.Type {
				case agent.MessageStatus:
					// Persist the session/work_dir as soon as the backend
					// reveals them. Without this, a daemon crash mid-run
					// loses the resume pointer and the auto-retry fires
					// without context.
					if msg.SessionID != "" && !sessionPinned.Swap(true) {
						sid := msg.SessionID
						wd := opts.Cwd
						go func() {
							pinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							if err := d.client.PinTaskSession(pinCtx, taskID, sid, wd); err != nil {
								taskLog.Debug("pin session failed", "error", err)
							}
						}()
					}
				case agent.MessageToolUse:
					n := toolCount.Add(1)
					taskLog.Info(fmt.Sprintf("tool #%d: %s", n, msg.Tool))
					if msg.CallID != "" {
						mu.Lock()
						callIDToTool[msg.CallID] = msg.Tool
						mu.Unlock()
					}
					s := seq.Add(1)
					mu.Lock()
					batch = append(batch, TaskMessageData{
						Seq:   int(s),
						Type:  "tool_use",
						Tool:  msg.Tool,
						Input: msg.Input,
					})
					mu.Unlock()
				case agent.MessageToolResult:
					s := seq.Add(1)
					output := msg.Output
					if len(output) > 8192 {
						output = output[:8192]
					}
					toolName := msg.Tool
					if toolName == "" && msg.CallID != "" {
						mu.Lock()
						toolName = callIDToTool[msg.CallID]
						mu.Unlock()
					}
					taskLog.Info("tool_result observed", "seq", s, "tool", toolName, "call_id", msg.CallID)
					mu.Lock()
					batch = append(batch, TaskMessageData{
						Seq:    int(s),
						Type:   "tool_result",
						Tool:   toolName,
						Output: output,
					})
					mu.Unlock()
				case agent.MessageThinking:
					if msg.Content != "" {
						mu.Lock()
						pendingThinking.WriteString(msg.Content)
						mu.Unlock()
					}
				case agent.MessageText:
					if msg.Content != "" {
						taskLog.Debug("agent", "text", truncateLog(msg.Content, 200))
						mu.Lock()
						pendingText.WriteString(msg.Content)
						mu.Unlock()
					}
				case agent.MessageError:
					taskLog.Error("agent error", "content", msg.Content)
					s := seq.Add(1)
					mu.Lock()
					batch = append(batch, TaskMessageData{
						Seq:     int(s),
						Type:    "error",
						Content: msg.Content,
					})
					mu.Unlock()
				}
			case <-drainCtx.Done():
				goto drainDone
			}
		}
	drainDone:
		close(done)
		flush()
	}()

	select {
	case result := <-session.Result:
		return result, toolCount.Load(), nil
	case <-drainCtx.Done():
		// Distinguish external cancellation (e.g. server-initiated cancel
		// because the issue was reassigned, or the user invoked CancelTask)
		// from genuine drain-deadline timeouts. context.Canceled means the
		// upstream runCtx fired runCancel(); context.DeadlineExceeded is the
		// drain deadline expiring on its own.
		if errors.Is(drainCtx.Err(), context.Canceled) {
			return agent.Result{
				Status: "cancelled",
				Error:  "task cancelled by upstream context (server cancel or daemon shutdown)",
			}, toolCount.Load(), nil
		}
		return agent.Result{
			Status: "timeout",
			Error:  "agent did not produce result within drain timeout",
		}, toolCount.Load(), nil
	}
}

func mergeUsage(a, b map[string]agent.TokenUsage) map[string]agent.TokenUsage {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	merged := make(map[string]agent.TokenUsage, len(a)+len(b))
	for model, u := range a {
		merged[model] = u
	}
	for model, u := range b {
		existing := merged[model]
		existing.InputTokens += u.InputTokens
		existing.OutputTokens += u.OutputTokens
		existing.CacheReadTokens += u.CacheReadTokens
		existing.CacheWriteTokens += u.CacheWriteTokens
		merged[model] = existing
	}
	return merged
}

// repoDataToInfo converts daemon RepoData to repocache RepoInfo.
func repoDataToInfo(repos []RepoData) []repocache.RepoInfo {
	info := make([]repocache.RepoInfo, len(repos))
	for i, r := range repos {
		info[i] = repocache.RepoInfo{URL: r.URL}
	}
	return info
}

func convertReposForEnv(repos []RepoData) []execenv.RepoContextForEnv {
	if len(repos) == 0 {
		return nil
	}
	result := make([]execenv.RepoContextForEnv, len(repos))
	for i, r := range repos {
		result[i] = execenv.RepoContextForEnv{URL: r.URL}
	}
	return result
}

func convertProjectResourcesForEnv(resources []ProjectResourceData) []execenv.ProjectResourceForEnv {
	if len(resources) == 0 {
		return nil
	}
	result := make([]execenv.ProjectResourceForEnv, len(resources))
	for i, r := range resources {
		result[i] = execenv.ProjectResourceForEnv{
			ID:           r.ID,
			ResourceType: r.ResourceType,
			ResourceRef:  r.ResourceRef,
			Label:        r.Label,
		}
	}
	return result
}

// markActiveEnvRoot records that a task is currently using the given env root,
// so the GC loop won't reclaim its artifacts mid-execution. Calls are
// reference-counted so a reuse path marked twice (predicted + prior) only
// becomes inactive after both unmark calls.
func (d *Daemon) markActiveEnvRoot(envRoot string) {
	if envRoot == "" {
		return
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.activeEnvRoots[envRoot]++
}

func (d *Daemon) unmarkActiveEnvRoot(envRoot string) {
	if envRoot == "" {
		return
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	if d.activeEnvRoots[envRoot] <= 1 {
		delete(d.activeEnvRoots, envRoot)
		return
	}
	d.activeEnvRoots[envRoot]--
}

func (d *Daemon) isActiveEnvRoot(envRoot string) bool {
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	return d.activeEnvRoots[envRoot] > 0
}

// shortID returns the first 8 characters of an ID for readable logs.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// truncateLog truncates a string to maxLen, appending "…" if truncated.
// Also collapses newlines to spaces for single-line log output.
func truncateLog(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

func convertSkillsForEnv(skills []SkillData) []execenv.SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	result := make([]execenv.SkillContextForEnv, len(skills))
	for i, s := range skills {
		result[i] = execenv.SkillContextForEnv{
			Name:    s.Name,
			Content: s.Content,
		}
		for _, f := range s.Files {
			result[i].Files = append(result[i].Files, execenv.SkillFileContextForEnv{
				Path:    f.Path,
				Content: f.Content,
			})
		}
	}
	return result
}

// isBlockedEnvKey returns true if the key must not be overridden by user-
// configured custom_env. This prevents accidental or malicious override of
// daemon-internal variables and critical system paths.
func isBlockedEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "MULTICA_") {
		return true
	}
	switch upper {
	case "HOME", "PATH", "USER", "SHELL", "TERM", "CODEX_HOME":
		return true
	}
	return false
}

func defaultArgsForProvider(cfg Config, provider string) []string {
	var args []string
	switch provider {
	case "claude":
		args = cfg.ClaudeArgs
	case "codex":
		args = cfg.CodexArgs
	default:
		return nil
	}
	return append([]string(nil), args...)
}
