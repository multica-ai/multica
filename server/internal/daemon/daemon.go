package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// ErrRepoNotConfigured is returned by ensureRepoReady when the requested repo
// URL is not present in the workspace's repo configuration after a fresh
// server refresh.
var ErrRepoNotConfigured = errors.New("repo is not configured for this workspace")

const (
	taskSlotWaitTimeout     = 2 * time.Second
	taskSlotCapacityBackoff = 5 * time.Second
)

// taskRunner executes a single agent task and returns the result.
// Extracted as an interface so tests can inject a fake without spawning real
// agent processes, while keeping test scaffolding out of the production struct.
type taskRunner interface {
	run(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error)
}

// taskRunnerFunc adapts a plain function to the taskRunner interface.
type taskRunnerFunc func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error)

func (f taskRunnerFunc) run(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error) {
	return f(ctx, task, provider, slot, log)
}

var (
	isBrewInstall        = cli.IsBrewInstall
	getBrewPrefix        = cli.GetBrewPrefix
	matchKnownBrewPrefix = cli.MatchKnownBrewPrefix

	// detectAgentVersion / checkAgentMinVersion are indirections over the
	// real agent helpers so tests can run the registration path without
	// shelling out to a real CLI. Mirrors the pattern used for the brew
	// helpers above.
	detectAgentVersion   = agent.DetectVersion
	checkAgentMinVersion = agent.CheckMinVersion
)

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

type repoCacheBackend interface {
	Lookup(workspaceID, url string) string
	Sync(workspaceID string, repos []repocache.RepoInfo) error
	WithRepoLock(barePath string, fn func() error) error
	CreateWorktree(params repocache.WorktreeParams) (*repocache.WorktreeResult, error)
}

// Daemon is the local agent runtime that polls for and executes tasks.
type Daemon struct {
	cfg       Config
	client    *Client
	repoCache repoCacheBackend
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

	// runtimeGoneMu guards runtimeGoneInflight, reregisterNextAttempt, and
	// reregisterLastCompletedAt. The state lets heartbeat / poller / WS-ack
	// handlers converge on a single recovery path when they each detect that a
	// runtime row was deleted server-side without three of them stampeding
	// registerRuntimesForWorkspace.
	runtimeGoneMu             sync.Mutex
	runtimeGoneInflight       map[string]struct{}  // runtime_id -> currently recovering
	reregisterNextAttempt     map[string]time.Time // workspace_id -> earliest time the next re-register attempt may run
	reregisterLastCompletedAt map[string]time.Time // workspace_id -> wall-clock at which the last SUCCESSFUL re-register call returned (failures intentionally not stamped — see recordRegisterCompletion)

	cancelFunc    context.CancelFunc // set by Run(); called by triggerRestart
	rootCtx       context.Context    // set by Run(); used by long-running recoveries that must survive per-runtime ctx cancellation
	restartBinary string             // non-empty after a successful update; path to the new binary
	updating      atomic.Bool        // prevents concurrent update attempts
	activeTasks   atomic.Int64       // number of tasks currently in handleTask; exposed via /health
	ready         atomic.Bool        // false until preflight completes; gates /health status (starting -> running)

	// claimMu guards pauseClaims and claimsInFlight. It is held only for the
	// microseconds it takes to make a decision; ClaimTask itself runs without
	// the lock so a slow per-runtime claim cannot stall auto-update or any
	// other poller.
	//
	// The pair is the auto-update path's barrier against the issue's
	// requirement that "升级过程中如果有 task 进来，会延后升级而不是中断 task":
	// runRuntimePoller refuses to call ClaimTask while pauseClaims is set, and
	// tryAutoUpdate refuses to flip pauseClaims while any poller is mid-claim
	// or any task is in handleTask. Together that closes the fetch-then-claim
	// race where a new task slipping in during the release-metadata fetch
	// would be cancelled by triggerRestart's root-ctx cancel.
	claimMu        sync.Mutex
	pauseClaims    bool // when true, runRuntimePoller skips ClaimTask
	claimsInFlight int  // pollers that have decided to claim but haven't yet handed the task off to handleTask

	activeEnvRootsMu sync.Mutex
	activeEnvRoots   map[string]int // env root path -> reference count (handles reuse paths marked twice)

	// localPathLocks serialises agent tasks whose project resource is a
	// local_directory pinned to this daemon. Two tasks targeting the same
	// on-disk path run sequentially; the second blocks on the lock and is
	// surfaced via the server-side waiting_local_directory status while it
	// waits. See MUL-2663.
	localPathLocks *LocalPathLocker

	// bgSyncs tracks background goroutines started by registerTaskRepos so
	// callers (notably tests using t.TempDir-backed cache roots) can wait for
	// them to drain before tearing the daemon down. Without this the bg
	// goroutine can race against t.TempDir cleanup, leaving a partially
	// deleted bare clone and an unrelated `not empty` cleanup failure.
	bgSyncs sync.WaitGroup

	runner             taskRunner    // executes agent tasks; set to d.runTask by New(), overridable in tests
	cancelPollInterval time.Duration // how often handleTask polls for server-side cancellation; overridable in tests
	// runUpdateFn executes the brew-or-download upgrade. Set to d.runUpdate by
	// New() and overridable in tests so the auto-update poller can be exercised
	// without touching the real network or the brew CLI.
	runUpdateFn func(targetVersion string) (string, error)
}

// New creates a new Daemon instance.
func New(cfg Config, logger *slog.Logger) *Daemon {
	cacheRoot := filepath.Join(cfg.WorkspacesRoot, ".repos")
	client := NewClient(cfg.ServerBaseURL)
	// Tag every daemon HTTP request with the daemon's CLI version so the
	// server can split logs/metrics by client version (parallel to the CLI).
	client.SetVersion(cfg.CLIVersion)
	d := &Daemon{
		cfg:                       cfg,
		client:                    client,
		repoCache:                 repocache.New(cacheRoot, logger),
		logger:                    logger,
		workspaces:                make(map[string]*workspaceState),
		runtimeIndex:              make(map[string]Runtime),
		runtimeSet:                newRuntimeSetWatcher(),
		agentVersions:             make(map[string]string),
		wsHBLastAck:               make(map[string]time.Time),
		activeEnvRoots:            make(map[string]int),
		localPathLocks:            NewLocalPathLocker(),
		runtimeGoneInflight:       make(map[string]struct{}),
		reregisterNextAttempt:     make(map[string]time.Time),
		reregisterLastCompletedAt: make(map[string]time.Time),
		cancelPollInterval:        5 * time.Second,
	}
	d.runner = taskRunnerFunc(d.runTask)
	d.runUpdateFn = d.runUpdate
	return d
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

// reregisterCoalesceWindow caps how often the daemon re-registers a workspace
// after detecting a runtime_not_found response. Many stale runtime IDs may be
// reported within seconds of each other (one delete clears all of a daemon's
// runtimes), and a single re-register call replaces every runtime in the
// workspace, so concurrent recoveries must collapse to one API call.
const reregisterCoalesceWindow = 30 * time.Second

// reregisterFailureBackoff is the additional wait inserted before the next
// re-register attempt when the previous one failed. This prevents heartbeat
// ticks (~15s) from converting a server-side log flood into a re-register
// flood when re-registration itself is failing (workspace removed, server
// unreachable, ...).
const reregisterFailureBackoff = 60 * time.Second

// handleRuntimeGone is the single recovery entry point shared by the HTTP
// heartbeat path, the runtime poller, and the WebSocket runtime_gone ack
// handler. All three may notice the same stale runtime within a few ms of
// each other, so this function:
//
//   - keys an in-flight set on runtimeID to drop concurrent calls for the same
//     ID after the first one is already cleaning up;
//   - keys a per-workspace next-attempt timestamp on workspaceID so that
//     concurrent recoveries triggered by the SAME initial event coalesce to a
//     single registerRuntimesForWorkspace call. The slot is cleared on success
//     so a later distinct runtime deletion in the same workspace can trigger
//     its own recovery without waiting for the coalesce window to expire; and
//   - keys a per-workspace last-completed timestamp so that a straggler whose
//     removeStaleRuntime took long enough that a sibling fully ran AND cleared
//     the slot can still recognize itself as same-wave and bail. Without this,
//     the success-case slot clear opens a race where the late caller re-claims
//     an empty slot and double-registers.
//
// On failure of the underlying re-register, the next-attempt timestamp is
// extended by reregisterFailureBackoff so we don't replace a server-side log
// flood with a daemon-side register flood. workspaceSyncLoop will retry
// independently every DefaultWorkspaceSyncInterval as a safety net.
//
// The recovery HTTP call uses the daemon root context, not the caller's. The
// heartbeat path's per-runtime ctx is cancelled by notifyRuntimeSetChanged the
// moment we prune the dead UUID, and if we forwarded that ctx the in-flight
// register would self-cancel mid-flight.
func (d *Daemon) handleRuntimeGone(runtimeID string) {
	if runtimeID == "" {
		return
	}

	// entryAt anchors the same-wave-straggler check at the bottom of the
	// function. Captured at the very top so removeStaleRuntime mutex
	// contention can't push it past a sibling's register completion.
	entryAt := time.Now()

	// Stampede control per runtime ID.
	d.runtimeGoneMu.Lock()
	if _, inflight := d.runtimeGoneInflight[runtimeID]; inflight {
		d.runtimeGoneMu.Unlock()
		return
	}
	d.runtimeGoneInflight[runtimeID] = struct{}{}
	d.runtimeGoneMu.Unlock()
	defer func() {
		d.runtimeGoneMu.Lock()
		delete(d.runtimeGoneInflight, runtimeID)
		d.runtimeGoneMu.Unlock()
	}()

	workspaceID, removed := d.removeStaleRuntime(runtimeID)
	if !removed {
		// Already gone from local state — a parallel recovery already
		// cleaned this up, or workspaceSyncLoop pruned the whole workspace.
		return
	}

	d.logger.Info("runtime deleted server-side; pruned from local state",
		"runtime_id", runtimeID, "workspace_id", workspaceID)
	d.notifyRuntimeSetChanged()

	if !d.tryClaimRegisterSlot(workspaceID, entryAt, time.Now()) {
		d.logger.Debug("skip re-register: coalescing with recent attempt",
			"workspace_id", workspaceID)
		return
	}

	err := d.reregisterWorkspaceAfterRuntimeGone(d.recoveryContext(), workspaceID)
	d.recordRegisterCompletion(workspaceID, time.Now(), err)
	if err != nil {
		// Logged at Warn (not Error) because workspaceSyncLoop retries
		// independently every DefaultWorkspaceSyncInterval, so a transient
		// failure here is not a stuck state — just an extra wait.
		d.logger.Warn("re-register after runtime gone failed",
			"workspace_id", workspaceID, "error", err)
	}
}

// tryClaimRegisterSlot atomically decides whether the calling goroutine should
// run registerRuntimesForWorkspace. Returns true and claims the in-flight slot
// when the caller may proceed; returns false (without mutating state) when the
// call must be coalesced with a peer.
//
// Two gates are checked under runtimeGoneMu:
//
//  1. reregisterNextAttempt: a future timestamp means a peer holds the slot or
//     a previous attempt failed and we are inside the failure backoff window.
//  2. reregisterLastCompletedAt: a timestamp at or after our entryAt means a
//     peer's register SUCCEEDED after we entered handleRuntimeGone, so the
//     workspace state is already covered for our wave and we can bail.
//     Failures intentionally don't stamp this field (see
//     recordRegisterCompletion), so a same-wave straggler whose entryAt
//     predates a failed sibling can still retry once the failure backoff
//     expires — failures don't cover anything.
//
// entryAt is the wall-clock captured at the top of handleRuntimeGone. now is
// passed in (rather than read inside) so tests can drive the gate
// deterministically without sleeping.
func (d *Daemon) tryClaimRegisterSlot(workspaceID string, entryAt, now time.Time) bool {
	d.runtimeGoneMu.Lock()
	defer d.runtimeGoneMu.Unlock()
	if next, ok := d.reregisterNextAttempt[workspaceID]; ok && now.Before(next) {
		return false
	}
	if last, ok := d.reregisterLastCompletedAt[workspaceID]; ok && !last.Before(entryAt) {
		return false
	}
	d.reregisterNextAttempt[workspaceID] = now.Add(reregisterCoalesceWindow)
	return true
}

// recordRegisterCompletion records the outcome of a register call. On success
// it stamps lastCompletedAt (which suppresses same-wave stragglers via
// tryClaimRegisterSlot) and clears the in-flight slot so a genuinely later
// runtime deletion can claim immediately. On failure it extends
// reregisterNextAttempt by the failure backoff and intentionally does NOT
// stamp lastCompletedAt — a failed register did not cover any workspace
// state, so a same-wave straggler whose entryAt predates the failure must
// still be allowed to retry once the backoff expires. workspaceSyncLoop only
// retries when the workspace's runtimeIDs fully drain, so partial-deletion
// recovery has to come from the straggler path.
func (d *Daemon) recordRegisterCompletion(workspaceID string, completedAt time.Time, err error) {
	d.runtimeGoneMu.Lock()
	defer d.runtimeGoneMu.Unlock()
	if err != nil {
		d.reregisterNextAttempt[workspaceID] = completedAt.Add(reregisterFailureBackoff)
		return
	}
	d.reregisterLastCompletedAt[workspaceID] = completedAt
	delete(d.reregisterNextAttempt, workspaceID)
}

// recoveryContext returns the daemon root context for long-running recovery
// HTTP calls (re-register, recover-orphans) that must survive the heartbeat
// loop tearing down a per-runtime context. Falls back to Background when the
// daemon was not started via Run(), e.g. unit-test fixtures.
func (d *Daemon) recoveryContext() context.Context {
	if d.rootCtx != nil {
		return d.rootCtx
	}
	return context.Background()
}

// removeStaleRuntime drops a runtime ID from its owning workspace's runtimeIDs
// list, the daemon-level runtimeIndex, and the WS heartbeat freshness map.
// Returns the workspace ID and true if the runtime was tracked, "" and false
// otherwise.
//
// Callers must NOT replace workspaceState pointers — only mutate fields in
// place — because ensureRepoReady holds workspaceState.repoRefreshMu through
// long repo-sync calls. See syncWorkspacesFromAPI for the same invariant.
func (d *Daemon) removeStaleRuntime(runtimeID string) (string, bool) {
	d.mu.Lock()
	var workspaceID string
	for wsID, ws := range d.workspaces {
		found := false
		filtered := ws.runtimeIDs[:0:0]
		for _, rid := range ws.runtimeIDs {
			if rid == runtimeID {
				found = true
				continue
			}
			filtered = append(filtered, rid)
		}
		if found {
			ws.runtimeIDs = filtered
			workspaceID = wsID
			break
		}
	}
	if workspaceID == "" {
		d.mu.Unlock()
		return "", false
	}
	delete(d.runtimeIndex, runtimeID)
	d.mu.Unlock()

	d.wsHBMu.Lock()
	delete(d.wsHBLastAck, runtimeID)
	d.wsHBMu.Unlock()

	return workspaceID, true
}

// workspaceNeedsRuntimeRecovery reports whether a tracked workspace currently
// has zero runtime IDs — the state reached when handleRuntimeGone pruned every
// runtime and its inline re-register failed. workspaceSyncLoop calls this on
// each tick so the workspace can recover without waiting for an external
// trigger.
func (d *Daemon) workspaceNeedsRuntimeRecovery(workspaceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return false
	}
	return len(ws.runtimeIDs) == 0
}

// reregisterWorkspaceAfterRuntimeGone calls registerRuntimesForWorkspace and
// updates the existing workspaceState in place. The register response is
// authoritative for this workspace's runtime set — every configured provider
// is included, with UpsertAgentRuntime returning the same row ID for surviving
// providers and a fresh ID for any that were deleted server-side. Replacing
// (rather than appending) is required: a partial recovery, where only one
// runtime in a multi-provider workspace was deleted, would otherwise produce
// duplicates for every provider that wasn't deleted.
//
// The workspaceState pointer is NEVER replaced (see syncWorkspacesFromAPI's
// invariant about repoRefreshMu). Only fields are mutated.
func (d *Daemon) reregisterWorkspaceAfterRuntimeGone(ctx context.Context, workspaceID string) error {
	resp, err := d.registerRuntimesForWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("register runtimes: %w", err)
	}

	newIDs := make([]string, 0, len(resp.Runtimes))
	newIDSet := make(map[string]struct{}, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		newIDs = append(newIDs, rt.ID)
		newIDSet[rt.ID] = struct{}{}
	}

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("workspace %s no longer tracked", workspaceID)
	}
	// Drop runtimeIndex entries for prior runtime IDs that the server did not
	// return — typically there are none for upsert-on-existing-provider, but
	// a daemon config change (provider removed) would leak entries otherwise.
	for _, oldID := range ws.runtimeIDs {
		if _, kept := newIDSet[oldID]; !kept {
			delete(d.runtimeIndex, oldID)
		}
	}
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
	}
	// Response is authoritative — replace, do not append. Replacing also
	// catches the rare case where UpsertAgentRuntime returns a different ID
	// for a surviving provider (e.g. schema change); the daemon converges on
	// what the server says without leaving stale heartbeat goroutines.
	ws.runtimeIDs = newIDs
	if resp.ReposVersion != "" {
		ws.reposVersion = resp.ReposVersion
		ws.allowedRepoURLs = repoAllowlist(resp.Repos)
	}
	if len(resp.Settings) > 0 {
		ws.settings = resp.Settings
	}
	d.mu.Unlock()

	for _, rid := range newIDs {
		d.logger.Info("re-registered runtime after server-side deletion",
			"workspace_id", workspaceID, "runtime_id", rid)
	}
	d.notifyRuntimeSetChanged()

	// Tell the server about any tasks the previous (now-deleted) runtime
	// was working on, mirroring the registration path's recover-orphans call.
	for _, rid := range newIDs {
		if err := d.client.RecoverOrphans(ctx, rid); err != nil {
			d.logger.Warn("recover-orphans after re-register failed",
				"runtime_id", rid, "error", err)
		}
	}
	return nil
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
	d.rootCtx = ctx

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
	d.logger.Debug("daemon config resolved",
		"daemon_id", d.cfg.DaemonID,
		"device_name", d.cfg.DeviceName,
		"workspaces_root", d.cfg.WorkspacesRoot,
		"health_port", d.cfg.HealthPort,
		"poll_interval", d.cfg.PollInterval,
		"heartbeat_interval", d.cfg.HeartbeatInterval,
		"agent_timeout", d.cfg.AgentTimeout,
		"idle_watchdog", d.cfg.AgentIdleWatchdog,
		"max_concurrent_tasks", d.cfg.MaxConcurrentTasks,
		"gc_enabled", d.cfg.GCEnabled,
		"auto_update", d.cfg.AutoUpdateEnabled,
		"launched_by", d.cfg.LaunchedBy,
	)

	// Load auth token from CLI config.
	if err := d.resolveAuth(); err != nil {
		return err
	}

	// Bind and serve the health port before the (potentially slow) preflight,
	// so `daemon start` and the desktop see a live "starting" daemon instead
	// of connection-refused while preflightAuth runs. preflightAuth's initial
	// workspace sync detects every configured agent's version by exec'ing it,
	// which on a cold cache with many agents takes ~20s. Liveness (port up) and
	// readiness (status:"running") are reported separately: /health stays
	// "starting" until d.ready is set after preflight, so a slow or *failing*
	// preflight is never misreported as a started daemon. resolveAuth has
	// already run, so a missing token still fails fast before we begin serving.
	go d.serveHealth(ctx, healthLn, time.Now())

	// Renew the PAT before the first API call, then do the initial
	// workspace sync. Both steps live in preflightAuth so the ordering
	// invariant (renew first) is enforced at one site instead of
	// scattered into Run, and tests can exercise the failure paths
	// without the full Run setup.
	if err := d.preflightAuth(ctx); err != nil {
		return err
	}

	// Deregister runtimes on shutdown (uses a fresh context since ctx will be cancelled).
	defer d.deregisterRuntimes()

	// Start workspace sync loop to discover newly created workspaces.
	go d.workspaceSyncLoop(ctx)

	taskWakeups := make(chan taskWakeup, 256)
	go d.taskWakeupLoop(ctx, taskWakeups)
	go d.heartbeatLoop(ctx)
	go d.gcLoop(ctx)
	go d.autoUpdateLoop(ctx)
	go d.tokenRenewalLoop(ctx)

	// Preflight succeeded and the background loops are up: the daemon has
	// registered its runtimes and can now claim and run tasks. Flip /health
	// from "starting" to "running" — this is the signal `daemon start`'s
	// readiness wait blocks on, so success is reported only after startup
	// actually completed, not merely because the health port came up.
	d.ready.Store(true)
	d.logger.Debug("background loops launched (workspace-sync, task-wakeup, heartbeat, gc, auto-update, token-renewal); health now reporting ready")
	err = d.pollLoop(ctx, taskWakeups)
	d.logger.Debug("daemon main loop returning", "error", err)
	return err
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
		d.logger.Debug("deregister: no runtimes to deregister")
		return
	}

	d.logger.Debug("deregistering runtimes on shutdown", "count", len(runtimeIDs), "runtime_ids", runtimeIDs)
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
	d.logger.Debug("auth token loaded", "profile", d.cfg.Profile, "token_len", len(cfg.Token))
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
	d.logger.Debug("registering runtimes for workspace", "workspace_id", workspaceID, "agent_count", len(d.cfg.Agents))
	var runtimes []map[string]string
	for name, entry := range d.cfg.Agents {
		version, err := detectAgentVersion(ctx, entry.Path)
		if err != nil {
			d.logger.Warn("skip registering runtime", "name", name, "error", err)
			continue
		}
		if err := checkAgentMinVersion(name, version); err != nil {
			d.logger.Warn("skip registering runtime: version too old", "name", name, "version", version, "error", err)
			continue
		}
		d.setAgentVersion(name, version)
		d.logger.Debug("agent version detected", "name", name, "version", version, "path", entry.Path)
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
	d.logger.Debug("register response", "workspace_id", workspaceID, "runtimes", len(resp.Runtimes), "repos", len(resp.Repos), "repos_version", resp.ReposVersion)
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
// be installed for the given workspace. Defaults to true when either setting
// is absent (new workspaces, older servers that don't send settings).
//
// The hook is gated by BOTH the GitHub master switch (`github_enabled`) and
// the dedicated co-author switch (`co_authored_by_enabled`) so flipping the
// workspace's master GitHub toggle off also stops new trailers from landing
// in commits, matching the contract documented in RFC MUL-2414 §4.8.
func (d *Daemon) workspaceCoAuthoredByEnabled(workspaceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok || len(ws.settings) == 0 {
		return true // default: enabled
	}
	var s struct {
		GitHubEnabled       *bool `json:"github_enabled"`
		CoAuthoredByEnabled *bool `json:"co_authored_by_enabled"`
	}
	if err := json.Unmarshal(ws.settings, &s); err != nil {
		return true // default: enabled when payload is malformed
	}
	if s.GitHubEnabled != nil && !*s.GitHubEnabled {
		return false
	}
	if s.CoAuthoredByEnabled == nil {
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

	type repoCandidate struct {
		url     string
		tracked bool
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
	candidates := make([]repoCandidate, 0, len(repos))
	for _, repo := range repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}
		// Don't re-sync if the URL is already tracked (workspace or task-scoped)
		// AND the cache already has it.
		_, inWorkspace := ws.allowedRepoURLs[url]
		_, inTask := ws.taskRepoURLs[url]
		ws.taskRepoURLs[url] = struct{}{}
		candidates = append(candidates, repoCandidate{
			url:     url,
			tracked: inWorkspace || inTask,
		})
	}
	d.mu.Unlock()

	toSync := make([]RepoData, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.tracked && d.repoCache != nil && d.repoCache.Lookup(workspaceID, candidate.url) != "" {
			continue
		}
		toSync = append(toSync, RepoData{URL: candidate.url})
	}

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
		// Keep the cached settings in sync with the server. The daemon's
		// feature gates (e.g. workspaceCoAuthoredByEnabled) read directly from
		// this field, so toggling a Setting in the web UI must update it here
		// without requiring a daemon restart. An empty payload from the server
		// clears the override and falls back to defaults.
		ws.settings = resp.Settings
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

	// Record whether the cache already had this repo before we took the
	// per-workspace mutex. The two states behave differently below:
	//
	//   - cacheHitOnEntry=true: the repo is already cloned; we still must
	//     refresh `workspaceState.settings` because the /repo/checkout
	//     handler reads workspaceCoAuthoredByEnabled right after this and
	//     the 30s workspaceSyncLoop tick is too slow for a freshly-flipped
	//     GitHub master switch / `co_authored_by_enabled` toggle to feel
	//     live (RFC MUL-2414 §4.8; PR #2847 review by Emacs).
	//
	//   - cacheHitOnEntry=false but cache hit *after* we acquire the mutex:
	//     a sibling goroutine on a concurrent cold-miss already refreshed
	//     and populated the cache. We can skip the duplicate refresh — the
	//     sibling's refresh is fresh enough for our gate read.
	cacheHitOnEntry := d.workspaceRepoAllowed(workspaceID, repoURL) && d.repoCache.Lookup(workspaceID, repoURL) != ""

	ws.repoRefreshMu.Lock()
	defer ws.repoRefreshMu.Unlock()

	if !cacheHitOnEntry && d.workspaceRepoAllowed(workspaceID, repoURL) && d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	resp, err := d.refreshWorkspaceRepos(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("refresh workspace repos: %w", err)
	}

	if !d.workspaceRepoAllowed(workspaceID, repoURL) {
		return ErrRepoNotConfigured
	}

	if d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
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

// DefaultTokenRenewalInterval is how often the daemon asks the server to
// extend its PAT. The server-side threshold is 7 days of remaining lifetime;
// polling every ~3 days gives at least two chances to renew before the
// window closes, so a single failed call (network blip, server restart) does
// not push the token out of the renewal window.
const DefaultTokenRenewalInterval = 3 * 24 * time.Hour

// preflightAuth runs the two auth-sensitive startup steps in their
// required order: a synchronous PAT renewal first, then the initial
// workspace sync. The order matters — running tryRenewToken before any
// other API call is what surfaces a user-actionable "run multica login"
// WARN when the PAT is already revoked or expired. If we let the
// workspace sync go first, its 401 would short-circuit Run before the
// renewal loop's first tick ever fires, and the operator would see only
// a generic auth failure in the workspace-sync log with no hint that
// re-login is the fix.
//
// The renewal is best-effort: tryRenewToken logs and returns, never
// propagating errors. preflightAuth's exit status is driven entirely by
// the workspace sync — so a transient renewal failure (network blip,
// 500) does not by itself block startup. A successful sync with zero
// workspaces is fine: a newly-signed-up user may start the daemon
// before creating their first workspace, and workspaceSyncLoop will
// register runtimes once one appears.
func (d *Daemon) preflightAuth(ctx context.Context) error {
	d.tryRenewToken(ctx)
	return d.syncWorkspacesFromAPI(ctx)
}

// tokenRenewalLoop keeps the daemon's PAT alive by periodically asking the
// server to extend its expires_at in-place. The startup renewal happens
// synchronously in preflightAuth so a daemon coming back online after a
// week of downtime gets a fresh expiry before its next heartbeat could
// 401; this loop owns the long-running ~3-day cadence after that.
//
// The server is authoritative on the renewal threshold (it sees expires_at;
// we don't), so this loop is intentionally dumb: call, log, sleep, repeat.
// On 401 we surface a clear "re-login required" warning because the daemon
// has no way to recover automatically — but we keep the loop running so the
// user sees the same warning on every cycle until they fix it, rather than
// silently exiting and forcing them to read scrollback to find the cause.
func (d *Daemon) tokenRenewalLoop(ctx context.Context) {
	ticker := time.NewTicker(DefaultTokenRenewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.tryRenewToken(ctx)
		}
	}
}

// tryRenewToken performs one renewal round-trip with a short, isolated
// timeout. Errors are logged but never propagated — there is no caller to
// handle them. Failures are debug-level except for 401, which gets a
// user-actionable warning.
func (d *Daemon) tryRenewToken(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := d.client.RenewToken(reqCtx)
	if err != nil {
		if isUnauthorizedError(err) {
			loginHint := "'multica login'"
			if d.cfg.Profile != "" {
				loginHint = fmt.Sprintf("'multica login --profile %s'", d.cfg.Profile)
			}
			d.logger.Warn("auth token rejected by server — run "+loginHint+" to re-authenticate, then restart the daemon", "error", err)
			return
		}
		d.logger.Debug("token renewal failed; will retry on next cycle", "error", err)
		return
	}
	if resp.Renewed {
		d.logger.Info("auth token renewed", "expires_at", resp.ExpiresAt)
	} else {
		d.logger.Debug("auth token not yet eligible for renewal", "expires_at", resp.ExpiresAt)
	}
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
	d.logger.Debug("workspace sync: fetched workspaces", "count", len(workspaces))

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
			// Already tracked: refresh the cached workspace settings so
			// feature toggles flipped in the web UI take effect on the next
			// gated operation without a daemon restart (see RFC MUL-2414 §4.8;
			// reviewed in PR #2847). refreshWorkspaceRepos covers settings +
			// repos in a single round trip.
			if _, err := d.refreshWorkspaceRepos(ctx, id); err != nil {
				d.logger.Debug("workspace sync: refresh settings failed", "workspace_id", id, "error", err)
			}
			// Only intervene further if the workspace lost all of its
			// runtimes (most commonly because handleRuntimeGone pruned them
			// and its inline re-register failed). The pointer is not replaced
			// here either — ensureRepoReady holds repoRefreshMu from the
			// original pointer.
			if !d.workspaceNeedsRuntimeRecovery(id) {
				continue
			}
			d.logger.Info("workspace has no runtimes; retrying registration", "workspace_id", id, "name", name)
			if err := d.reregisterWorkspaceAfterRuntimeGone(ctx, id); err != nil {
				d.logger.Warn("retry register failed", "workspace_id", id, "error", err)
				continue
			}
			registered++
			continue
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
	if registered > 0 || removed > 0 {
		d.logger.Debug("workspace sync done", "registered", registered, "removed", removed, "tracked", len(apiIDs))
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
		d.logger.Debug("heartbeat: skipping HTTP tick, WS recently acked", "runtime_id", rid)
		return
	}
	d.logger.Debug("heartbeat: HTTP tick", "runtime_id", rid)
	resp, err := d.client.SendHeartbeat(ctx, rid)
	if err != nil {
		if ctx.Err() == nil {
			if isRuntimeNotFoundError(err) {
				// Server says this runtime is gone — recover instead of
				// looping on the dead UUID. handleRuntimeGone coalesces
				// concurrent callers and runs the recovery HTTP call under
				// the daemon root context so notifyRuntimeSetChanged
				// tearing down this heartbeat goroutine cannot abort it.
				go d.handleRuntimeGone(rid)
				return
			}
			d.logger.Warn("heartbeat failed", "runtime_id", rid, "error", err)
		}
		return
	}
	if resp != nil && resp.RuntimeGone {
		// The WS path returns a successful ack with RuntimeGone=true for the
		// same scenario; treat it the same way here in case HTTP starts
		// surfacing this signal too.
		go d.handleRuntimeGone(rid)
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
	if resp.PendingUpdate != nil || resp.PendingModelList != nil || resp.PendingLocalSkills != nil || resp.PendingLocalSkillImport != nil {
		d.logger.Debug("heartbeat: pending actions",
			"runtime_id", runtimeID,
			"update", resp.PendingUpdate != nil,
			"model_list", resp.PendingModelList != nil,
			"local_skills", resp.PendingLocalSkills != nil,
			"local_skill_import", resp.PendingLocalSkillImport != nil,
		)
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
	// Prefer the batch field (new backend); fall back to singular (old backend).
	if len(resp.PendingLocalSkillImports) > 0 {
		if rt := d.findRuntime(runtimeID); rt != nil {
			for _, imp := range resp.PendingLocalSkillImports {
				go d.handleLocalSkillImport(ctx, *rt, imp)
			}
		}
	} else if resp.PendingLocalSkillImport != nil {
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
	// map[string]string) so the Default bool and the per-model
	// Thinking catalog round-trip — without it the UI loses its
	// "default" badge on the advertised pick and the thinking-level
	// picker for claude/codex (MUL-2339).
	type thinkingLevelWire struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	}
	type modelThinkingWire struct {
		SupportedLevels []thinkingLevelWire `json:"supported_levels"`
		DefaultLevel    string              `json:"default_level,omitempty"`
	}
	type modelWire struct {
		ID       string             `json:"id"`
		Label    string             `json:"label"`
		Provider string             `json:"provider,omitempty"`
		Default  bool               `json:"default,omitempty"`
		Thinking *modelThinkingWire `json:"thinking,omitempty"`
	}
	wire := make([]modelWire, 0, len(models))
	for _, m := range models {
		entry := modelWire{
			ID:       m.ID,
			Label:    m.Label,
			Provider: m.Provider,
			Default:  m.Default,
		}
		if m.Thinking != nil {
			levels := make([]thinkingLevelWire, 0, len(m.Thinking.SupportedLevels))
			for _, lvl := range m.Thinking.SupportedLevels {
				levels = append(levels, thinkingLevelWire{
					Value:       lvl.Value,
					Label:       lvl.Label,
					Description: lvl.Description,
				})
			}
			entry.Thinking = &modelThinkingWire{
				SupportedLevels: levels,
				DefaultLevel:    m.Thinking.DefaultLevel,
			}
		}
		wire = append(wire, entry)
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

	output, err := d.runUpdateFn(update.TargetVersion)
	if err != nil {
		d.logger.Error("CLI update failed", "error", err, "output", output)
		d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	d.logger.Info("CLI update completed successfully", "output", output)
	d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
		"status": "completed",
		"output": fmt.Sprintf("Updated to %s", update.TargetVersion),
	})

	// Trigger daemon restart with the new binary.
	d.triggerRestart()
}

// runUpdate executes the brew-or-download upgrade against targetVersion and
// returns the human-readable output (always populated, even on failure when
// brew gives us a useful diagnostic). The caller is responsible for the
// `updating` CAS guard and for reporting status back to the server / triggering
// the restart — extracted so the server-triggered path (handleUpdate) and the
// auto-update poller (autoUpdateLoop) share the exact same execution body.
func (d *Daemon) runUpdate(targetVersion string) (string, error) {
	if cli.IsBrewInstall() {
		d.logger.Info("updating CLI via Homebrew...")
		out, err := cli.UpdateViaBrew()
		if err != nil {
			return out, fmt.Errorf("brew upgrade failed: %w", err)
		}
		return out, nil
	}
	d.logger.Info("updating CLI via direct download...", "target_version", targetVersion)
	out, err := cli.UpdateViaDownload(targetVersion)
	if err != nil {
		return out, fmt.Errorf("download update failed: %w", err)
	}
	return out, nil
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

// tryEnterClaim records the intent to call ClaimTask. Returns true if the
// caller may proceed, false if the auto-update barrier is in effect. Every
// successful call MUST be paired with an exitClaim() on every exit path —
// either right after a failed/empty claim, or via the handleTask goroutine's
// defer once the task is handed off.
func (d *Daemon) tryEnterClaim() bool {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	if d.pauseClaims {
		return false
	}
	d.claimsInFlight++
	return true
}

// exitClaim releases the in-flight claim recorded by tryEnterClaim.
func (d *Daemon) exitClaim() {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.claimsInFlight--
}

// trySetClaimBarrier atomically pauses new ClaimTask calls if the daemon is
// fully idle (no claims in flight, no tasks running). Returns true if the
// caller now holds the barrier and must release it with releaseClaimBarrier
// on every non-restart exit path; false if the daemon is busy and the caller
// should defer to the next tick. Used by tryAutoUpdate to close the race
// where a task slips in between the cheap pre-fetch idle check and the
// actual upgrade kick-off.
func (d *Daemon) trySetClaimBarrier() bool {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	if d.claimsInFlight > 0 || d.activeTasks.Load() > 0 {
		return false
	}
	d.pauseClaims = true
	return true
}

// releaseClaimBarrier clears the auto-update claim barrier so pollers may
// resume claiming. Called on failure paths only — a successful upgrade leaves
// the barrier set because triggerRestart is about to take the process down
// and clearing it would open a window for new claims during shutdown.
func (d *Daemon) releaseClaimBarrier() {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.pauseClaims = false
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
	// On Linux, os.Executable() reads /proc/self/exe, which the kernel resolves
	// to the Cellar path. brew cleanup deletes that path after upgrade, so we
	// must use the stable <brew-prefix>/bin/multica symlink instead.
	if isBrewInstall() {
		if brewPrefix := getBrewPrefix(); brewPrefix != "" {
			newBin = filepath.Join(brewPrefix, "bin", "multica")
		} else if prefix := matchKnownBrewPrefix(newBin); prefix != "" {
			newBin = filepath.Join(prefix, "bin", "multica")
		} else {
			d.logger.Warn("brew install detected but prefix could not be resolved; restart may fail",
				"executable", newBin)
		}
	} else {
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
func (d *Daemon) pollLoop(ctx context.Context, taskWakeups <-chan taskWakeup) error {
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
		case wakeup := <-taskWakeups:
			if wakeup.runtimeID != "" {
				if h, ok := pollers[wakeup.runtimeID]; ok {
					d.logger.Debug("task wakeup: signaling runtime poller", "runtime_id", wakeup.runtimeID)
					select {
					case h.wakeup <- struct{}{}:
					default:
					}
				} else {
					d.logger.Debug("task wakeup: runtime poller not found", "runtime_id", wakeup.runtimeID, "pollers", len(pollers))
				}
				continue
			}

			// A wakeup without a runtime_id is a catch-up signal (for example,
			// immediately after the websocket connects). Fan it out so queued
			// work that existed before the connection is still discovered.
			d.logger.Debug("task wakeup: fanning out to pollers", "pollers", len(pollers))
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
	if offset := runtimePollOffset(rid, d.cfg.PollInterval); offset > 0 {
		d.logger.Debug("poll: initial offset", "runtime_id", rid, "offset", offset)
		if err := sleepWithContextOrWakeup(pollerCtx, offset, wakeup); err != nil {
			return
		}
	}

	for {
		if pollerCtx.Err() != nil {
			return
		}

		// Acquire an execution slot before claiming. If at capacity, sleep
		// without claiming so we don't push a task into `dispatched` and
		// then race the 5-min server-side dispatch timeout while waiting.
		slot, acquired, woke, err := waitForTaskSlot(pollerCtx, sem, wakeup, taskSlotWaitTimeout)
		if err != nil {
			return
		}
		if !acquired {
			d.logger.Debug("poll: at capacity", "runtime_id", rid, "running", d.cfg.MaxConcurrentTasks)
			if woke {
				continue
			}
			if err := sleepWithContextOrWakeup(pollerCtx, capacityBackoff(d.cfg.PollInterval), wakeup); err != nil {
				return
			}
			continue
		}

		// Refuse new claims while an auto-update is preparing to roll the
		// process. The barrier is paired with a re-check of claimsInFlight +
		// activeTasks inside tryAutoUpdate, so once we get past tryEnterClaim
		// the auto-update path is guaranteed to defer until this poller has
		// handed the task off (or given up).
		if !d.tryEnterClaim() {
			sem <- slot
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		task, err := d.client.ClaimTask(pollerCtx, rid)
		if err != nil {
			d.exitClaim()
			sem <- slot
			if pollerCtx.Err() == nil {
				if isRuntimeNotFoundError(err) {
					// Server says this runtime is gone — recover and exit
					// the poller; the runtime-set watcher will tear this
					// goroutine down via pollerCtx once the workspace is
					// re-registered with a new runtime ID.
					go d.handleRuntimeGone(rid)
					return
				}
				d.logger.Warn("claim task failed", "runtime_id", rid, "error", err)
			}
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		if task == nil {
			d.exitClaim()
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
			defer d.exitClaim()
			defer d.activeTasks.Add(-1)
			defer func() { sem <- slot }()
			d.handleTask(parentCtx, t, slot)
		}(*task, slot)
		// Loop immediately: more tasks may already be queued for this runtime.
	}
}

func runtimePollOffset(runtimeID string, interval time.Duration) time.Duration {
	if interval <= 0 || runtimeID == "" {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(runtimeID))
	return time.Duration(h.Sum64() % uint64(interval))
}

func capacityBackoff(pollInterval time.Duration) time.Duration {
	if pollInterval <= 0 || pollInterval > taskSlotCapacityBackoff {
		return taskSlotCapacityBackoff
	}
	return pollInterval
}

func waitForTaskSlot(ctx context.Context, sem chan int, wakeup <-chan struct{}, wait time.Duration) (slot int, acquired, woke bool, err error) {
	select {
	case slot = <-sem:
		return slot, true, false, nil
	case <-ctx.Done():
		return 0, false, false, ctx.Err()
	default:
	}

	if wait <= 0 {
		return 0, false, false, nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case slot = <-sem:
		return slot, true, false, nil
	case <-wakeup:
		return 0, false, true, nil
	case <-ctx.Done():
		return 0, false, false, ctx.Err()
	case <-timer.C:
		return 0, false, false, nil
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

// shouldInterruptAgent decides whether the running agent should be cancelled
// based on the latest GetTaskStatus call. Pure function so the decision is
// trivially testable; the polling goroutine in watchTaskCancellation is just
// I/O around it.
//
// Two conditions trigger cancellation:
//
//  1. status is a terminal state — "completed", "failed", or "cancelled"
//     (isAgentTaskTerminal). The server has already finalized the task: user
//     cancel, issue reassignment, the runtime offline sweeper flipping
//     running → failed during a disconnect, or a duplicate execution that
//     already completed it. Letting the local agent run on is pure waste —
//     CompleteAgentTask only accepts status == "running", so its eventual
//     CompleteTask/FailTask callback is guaranteed to fail and just adds log
//     noise. Reusing isAgentTaskTerminal keeps this set in lockstep with the
//     GC's notion of a terminal task.
//  2. err is a 404 with "task not found" — the task row was deleted while
//     the agent was running. Without this we'd let the local agent keep
//     emitting tool calls against a dead task for its full timeout window.
//
// All other errors (transient network, 5xx, ...) intentionally do NOT
// trigger cancellation — the next tick will retry and we don't want a
// flaky link to kill an in-flight agent.
func shouldInterruptAgent(status string, err error) bool {
	if err != nil {
		return isTaskNotFoundError(err)
	}
	return isAgentTaskTerminal(status)
}

// watchTaskCancellation polls the server for the task's status on the given
// interval and returns a channel that is closed when the running agent
// should be interrupted. The polling goroutine stops when ctx is cancelled,
// so callers should pass the runCtx that was set up around the agent run.
func (d *Daemon) watchTaskCancellation(ctx context.Context, taskID string, pollInterval time.Duration, taskLog *slog.Logger) <-chan struct{} {
	cancelled := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				status, err := d.client.GetTaskStatus(ctx, taskID)
				if !shouldInterruptAgent(status, err) {
					continue
				}
				if err != nil {
					taskLog.Info("task gone server-side, interrupting agent", "error", err)
				} else {
					taskLog.Info("task reached terminal state server-side, interrupting agent", "status", status)
				}
				close(cancelled)
				return
			}
		}
	}()
	return cancelled
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
	taskLog.Debug("task context",
		"workspace_id", task.WorkspaceID,
		"runtime_id", task.RuntimeID,
		"agent_id", task.AgentID,
		"repos", len(task.Repos),
		"project_id", task.ProjectID,
		"autopilot_run_id", task.AutopilotRunID,
		"trigger_comment_id", task.TriggerCommentID,
		"resume_session", task.PriorSessionID != "",
		"reuse_workdir", task.PriorWorkDir != "",
	)

	// If the task targets a project_resource of type local_directory that
	// is pinned to this daemon, acquire the path mutex BEFORE StartTask so
	// the server-side state machine is dispatched → waiting_local_directory
	// → running rather than backwards-transitioning from running into the
	// wait state. The release is deferred so a panic or early return
	// always frees the lock for the next waiter.
	localRelease, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, task, taskLog)
	if abort {
		return
	}
	if localRelease != nil {
		defer localRelease()
	}

	if err := d.client.StartTask(ctx, task.ID); err != nil {
		taskLog.Error("start task failed", "error", err)
		startErrMsg := fmt.Sprintf("start task failed: %s", err.Error())
		// MUL-2946: classify the wrapper error so the failure_reason
		// column lands in the canonical refined taxonomy rather than
		// the legacy coarse "agent_error" bucket. A start-task failure
		// most commonly surfaces as ReasonAgentUnknown (no rule
		// matches "start task failed: <…>"), but a future provider /
		// network blip in the wrapper layer would still classify
		// correctly without us touching this site.
		if failErr := d.client.FailTask(ctx, task.ID, startErrMsg, "", "", taskfailure.Classify(startErrMsg).String()); failErr != nil {
			taskLog.Error("fail task after start error", "error", failErr)
		}
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Launching %s", provider), 1, 2)

	// Create a cancellable context so we can interrupt the running agent
	// when the server signals the task should stop — either the task reached
	// a terminal state (completed/failed/cancelled) or the task row is
	// deleted (404).
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	// Poll interval is d.cancelPollInterval (5s in production, reduced in tests
	// via direct field override). Guard against zero so a misconfigured daemon
	// doesn't panic time.NewTicker.
	pollInterval := d.cancelPollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	cancelledByPoll := d.watchTaskCancellation(runCtx, task.ID, pollInterval, taskLog)
	go func() {
		select {
		case <-cancelledByPoll:
			runCancel()
		case <-runCtx.Done():
		}
	}()

	result, err := d.runner.run(runCtx, task, provider, slot, taskLog)

	// Report usage before any early return — the agent accumulates tokens
	// whether the task completes, errors, or is cancelled mid-run by the poll
	// goroutine. Both claude.go and codex.go populate result.Usage even when
	// runCtx is cancelled, so dropping this on the cancelled path silently
	// under-reports billing.
	if len(result.Usage) > 0 {
		if usageErr := d.client.ReportTaskUsage(ctx, task.ID, result.Usage); usageErr != nil {
			taskLog.Warn("report task usage failed", "error", usageErr)
		}
	}

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
		// MUL-2946: route the bare error string through the canonical
		// classifier so the failure_reason column reflects the actual
		// shape of the failure (provider 5xx, network, process crash,
		// …) rather than the coarse legacy "agent_error" bucket.
		if failErr := d.client.FailTask(ctx, task.ID, err.Error(), "", "", taskfailure.Classify(err.Error()).String()); failErr != nil {
			taskLog.Error("fail task callback failed", "error", failErr)
		}
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, "Finishing task", 2, 2)

	// Final pre-completion check: if the server already moved the task to a
	// terminal state (completed/failed/cancelled) or deleted the row
	// outright, skip reporting — the complete/fail callbacks would fail
	// anyway. Reuse shouldInterruptAgent so this guard honors the same
	// signals as the in-flight watcher.
	if status, err := d.client.GetTaskStatus(ctx, task.ID); shouldInterruptAgent(status, err) {
		taskLog.Info("task cancelled during execution, discarding result", "status", status, "error", err)
		return
	}

	d.reportTaskResult(ctx, task.ID, result, taskLog)

	// Write GC metadata after the task finishes so the periodic GC loop
	// can look up the parent record (issue / chat session / autopilot run /
	// task itself for quick-create) later. Written last so that a mid-task
	// crash leaves the directory as an orphan (cleaned up by GCOrphanTTL).
	if result.EnvRoot != "" {
		if meta, ok := gcMetaForTask(task); ok {
			// A local_directory project_resource matched this daemon
			// means the agent ran in the user's own tree. Stamp the
			// meta so the GC loop never tries to RemoveAll envRoot's
			// sibling workdir (which is the user's path) or the envRoot
			// itself (we want output/ and logs/ to linger for forensic
			// access).
			if assignment, _ := findLocalDirectoryAssignment(task.ProjectResources, d.cfg.DaemonID); assignment != nil {
				meta.LocalDirectory = true
			}
			if err := execenv.WriteGCMeta(result.EnvRoot, meta, taskLog); err != nil {
				taskLog.Warn("write gc meta failed (non-fatal)", "error", err)
			}
		}
	}
}

// acquireLocalDirectoryLockIfNeeded inspects the task's project resources for
// a local_directory pinned to this daemon, validates the path, and takes the
// path mutex. Returns a release callback (nil when no local_directory
// resource applies) and abort=true when the caller must bail without
// starting the task (the helper has already reported the failure to the
// server).
//
// The helper covers four distinct failure modes:
//
//  1. The project_resource JSON is structurally broken — fail the task fast.
//  2. The path fails validation (missing, not a directory, no R/W, system
//     blacklist) — fail the task fast with a user-facing reason.
//  3. The mutex is held by another task — call MarkTaskWaitingLocalDirectory
//     so the row flips to waiting_local_directory while we block on the
//     lock, then return the release callback once we win.
//  4. The blocking wait is cancelled (daemon shutdown, server-side cancel)
//     — fail the task with the ctx error.
func (d *Daemon) acquireLocalDirectoryLockIfNeeded(ctx context.Context, task Task, taskLog *slog.Logger) (release func(), abort bool) {
	if len(task.ProjectResources) == 0 || d.cfg.DaemonID == "" {
		return nil, false
	}
	assignment, err := findLocalDirectoryAssignment(task.ProjectResources, d.cfg.DaemonID)
	if err != nil {
		taskLog.Error("local_directory: resolve resource failed", "error", err)
		if failErr := d.client.FailTask(ctx, task.ID, err.Error(), "", "", "local_directory_error"); failErr != nil {
			taskLog.Error("fail task after local_directory resolve error", "error", failErr)
		}
		return nil, true
	}
	if assignment == nil {
		return nil, false
	}
	taskLog = taskLog.With("local_directory", assignment.AbsPath)
	if err := validateLocalPath(assignment.AbsPath); err != nil {
		taskLog.Error("local_directory: path validation failed", "error", err)
		if failErr := d.client.FailTask(ctx, task.ID, err.Error(), "", "", "local_directory_error"); failErr != nil {
			taskLog.Error("fail task after local_directory validation error", "error", failErr)
		}
		return nil, true
	}

	// While the lock is contended the daemon would otherwise sit blocked on
	// the path mutex with no signal back from the server — the main
	// per-task watcher only starts after StartTask. If the user cancels
	// the issue or it gets reassigned during the wait, we need to notice
	// promptly so the daemon slot isn't pinned by a phantom waiter. We
	// spin up the cancellation watcher lazily inside onWait so the
	// no-contention fast path still costs nothing.
	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	pollInterval := d.cancelPollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	var (
		watcherOnce     sync.Once
		cancelledByPoll <-chan struct{}
	)

	onWait := func(holder string) {
		reason := fmt.Sprintf("local_directory %s", assignment.AbsPath)
		if holder != "" {
			reason = fmt.Sprintf("%s (held by task %s)", reason, shortID(holder))
		}
		taskLog.Info("local_directory: waiting on path mutex", "holder", shortID(holder))
		if waitErr := d.client.MarkTaskWaitingLocalDirectory(ctx, task.ID, reason); waitErr != nil {
			// Non-fatal: even if the server-side flag fails to update,
			// we still want to block on the lock and proceed when free.
			// The UI just won't see the explicit "waiting" badge.
			taskLog.Warn("local_directory: mark waiting status failed", "error", waitErr)
		}
		// Start polling once we actually park. shouldInterruptAgent inside
		// watchTaskCancellation already handles both server-side terminal
		// states (completed/failed/cancelled) and the row-deleted
		// reassignment case (404), which is the full set of "this task
		// shouldn't run anymore" signals we need to react to during the wait.
		watcherOnce.Do(func() {
			cancelledByPoll = d.watchTaskCancellation(waitCtx, task.ID, pollInterval, taskLog)
			go func() {
				select {
				case <-cancelledByPoll:
					waitCancel()
				case <-waitCtx.Done():
				}
			}()
		})
	}
	release, err = d.localPathLocks.Acquire(waitCtx, assignment.RealPath, task.ID, onWait)
	if err != nil {
		// If the wait was cut short because the server finalized the task
		// (terminal state) or deleted the row, the row is already in a
		// terminal state — return silently the same way the run-phase poller
		// does at lines ~2104. Issuing FailTask here would be a no-op at best
		// and a confusing redundant log line at worst.
		if cancelledByPoll != nil {
			select {
			case <-cancelledByPoll:
				taskLog.Info("local_directory: wait aborted by server-side terminal state")
				return nil, true
			default:
			}
		}
		taskLog.Error("local_directory: lock acquire failed", "error", err)
		failureReason := "local_directory_error"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			failureReason = "cancelled"
		}
		if failErr := d.client.FailTask(ctx, task.ID, fmt.Sprintf("local_directory wait cancelled: %s", err.Error()), "", "", failureReason); failErr != nil {
			taskLog.Error("fail task after local_directory lock cancel", "error", failErr)
		}
		return nil, true
	}
	taskLog.Info("local_directory: lock acquired")
	return release, false
}

// reportTaskResult writes the final task disposition back to the server.
//
// Fail closed: only an explicit "completed" status is reported as success.
// Anything else — "blocked", "cancelled", or any future status we forget to
// enumerate — must go through FailTask, so a run that never produced a real
// result can never be displayed as "Completed" in the UI (e.g. provider 429 /
// out-of-credit / runtime crash). Forward SessionID/WorkDir on every path:
// the agent may have built a real session before getting stuck, and we want
// the next chat turn to resume there rather than start over and "forget"
// the conversation.
func (d *Daemon) reportTaskResult(ctx context.Context, taskID string, result TaskResult, taskLog *slog.Logger) {
	switch result.Status {
	case "completed":
		taskLog.Info("task completed", "status", result.Status)
		err := d.client.CompleteTask(ctx, taskID, result.Comment, result.BranchName, result.SessionID, result.WorkDir)
		if err == nil {
			return
		}
		// CompleteTask retries transient errors internally. A transient
		// error reaching us here means the schedule was exhausted while
		// the upstream was still 5xx / unreachable. Converting that into
		// a fail would lose the agent's actual result and surface a
		// misleading red badge in the UI — leave the task in running
		// instead so a future fix (server-side stuck-task reaper, or a
		// daemon-side persistent pending queue) can recover it. Only
		// permanent server-side rejections (4xx other than 408/429)
		// warrant the legacy fallback, because at that point the server
		// has already refused this task and the only useful UI signal
		// left is a concrete failure.
		if isTransientError(err) {
			taskLog.Error("complete task failed after retries; leaving task in running rather than falling back to fail", "error", err)
			return
		}
		taskLog.Error("complete task rejected by server, falling back to fail", "error", err)
		// MUL-2946: this fallback fires when a server-side complete
		// callback was permanently rejected (4xx other than 408/429)
		// — the agent itself succeeded, so the err here describes the
		// server response rather than an agent failure. The classifier
		// is unlikely to match anything in the server's error text and
		// will land at ReasonAgentUnknown ("agent_error.unknown"),
		// which is the canonical replacement for the legacy
		// "agent_error" coarse bucket.
		fallbackErrMsg := fmt.Sprintf("complete task failed: %s", err.Error())
		if failErr := d.client.FailTask(ctx, taskID, fallbackErrMsg, result.SessionID, result.WorkDir, taskfailure.Classify(fallbackErrMsg).String()); failErr != nil {
			taskLog.Error("fail task fallback also failed", "error", failErr)
		}
	default:
		failureReason := result.FailureReason
		if failureReason == "" {
			if result.Status == "cancelled" {
				// "cancelled" is a deliberate non-failure terminal
				// state masquerading as a failure_reason — preserved
				// outside the canonical taxonomy so the UI can render
				// it differently from a real failure.
				failureReason = "cancelled"
			} else {
				// MUL-2946: classify the agent's comment text so the
				// failure_reason lands in the refined taxonomy
				// (provider_auth_or_access, context_overflow,
				// process_failure, …) instead of the legacy coarse
				// "agent_error" bucket. Empty comment lands in
				// ReasonAgentUnknown.
				failureReason = taskfailure.Classify(result.Comment).String()
			}
		}
		taskLog.Info("task did not complete, reporting failure", "status", result.Status, "failure_reason", failureReason)
		if err := d.client.FailTask(ctx, taskID, result.Comment, result.SessionID, result.WorkDir, failureReason); err != nil {
			taskLog.Error("report failed task failed", "error", err)
		}
	}
}

// gcMetaForTask classifies a finished task and produces a GCMeta of the right
// kind. The discriminator order matters: a task carrying both an issue_id
// and a chat_session_id (theoretical, not produced today) should be treated
// as a chat task because the chat session is the longer-lived parent record.
//
// Returns ok=false when the task has no recognizable parent (e.g. an
// internal task with no IDs at all). The caller skips writing a meta file
// in that case so the directory falls back to mtime-based orphan cleanup.
func gcMetaForTask(task Task) (execenv.GCMeta, bool) {
	meta := execenv.GCMeta{WorkspaceID: task.WorkspaceID}
	switch {
	case task.ChatSessionID != "":
		meta.Kind = execenv.GCKindChat
		meta.ChatSessionID = task.ChatSessionID
	case task.AutopilotRunID != "":
		meta.Kind = execenv.GCKindAutopilotRun
		meta.AutopilotRunID = task.AutopilotRunID
	case task.IssueID != "":
		meta.Kind = execenv.GCKindIssue
		meta.IssueID = task.IssueID
	case task.QuickCreatePrompt != "":
		// Quick-create tasks reach WriteGCMeta before the server runs
		// LinkTaskToIssue, so IssueID is always empty here. Persist the
		// task ID instead and let the GC loop ask the server for terminal
		// state via the task gc-check endpoint.
		meta.Kind = execenv.GCKindQuickCreate
		meta.TaskID = task.ID
	default:
		return execenv.GCMeta{}, false
	}
	return meta, true
}

func providerNeedsInlineSystemPrompt(provider string) bool {
	switch provider {
	case "openclaw", "kiro", "kimi":
		return true
	default:
		return false
	}
}

// gateResumeToReusedWorkdir clears the task's prior session unless the task
// runs in the exact workdir the session was recorded against, and reports
// whether that workdir was reused. CLI backends key their session stores to
// the cwd (Claude Code looks sessions up under ~/.claude/projects/<encoded-cwd>/),
// so a session id from a different workdir can never resolve: the CLI exits
// within a second and the run fails before doing any work — permanently,
// because the failed run records no session and the next claim serves the
// same stale pointer again. This fires whenever the prior workdir no longer
// exists (GC'd after the issue went done, daemon reinstall, manual cleanup)
// and execenv.Reuse fell back to a fresh Prepare (GitHub #3854).
func gateResumeToReusedWorkdir(task *Task, taskCtx *execenv.TaskContextForEnv, envWorkDir string, taskLog *slog.Logger) bool {
	reused := task.PriorWorkDir != "" && envWorkDir == task.PriorWorkDir
	if !reused && task.PriorSessionID != "" {
		taskLog.Info("dropping prior session: workdir not reused, per-cwd session cannot resolve",
			"session_id", task.PriorSessionID,
			"prior_workdir", task.PriorWorkDir,
			"workdir", envWorkDir,
		)
		task.PriorSessionID = ""
		taskCtx.PriorSessionResumed = false
	}
	return reused
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
		IssueID:                          task.IssueID,
		TriggerCommentID:                 task.TriggerCommentID,
		TriggerThreadID:                  task.TriggerThreadID,
		NewCommentCount:                  task.NewCommentCount,
		NewCommentsSince:                 task.NewCommentsSince,
		PriorSessionResumed:              task.PriorSessionID != "",
		AgentID:                          agentID,
		AgentName:                        agentName,
		AgentInstructions:                instructions,
		AgentSkills:                      convertSkillsForEnv(skills),
		Repos:                            convertReposForEnv(task.Repos),
		ProjectID:                        task.ProjectID,
		ProjectTitle:                     task.ProjectTitle,
		ProjectResources:                 convertProjectResourcesForEnv(task.ProjectResources),
		ChatSessionID:                    task.ChatSessionID,
		AutopilotRunID:                   task.AutopilotRunID,
		AutopilotID:                      task.AutopilotID,
		AutopilotTitle:                   task.AutopilotTitle,
		AutopilotDescription:             task.AutopilotDescription,
		AutopilotSource:                  task.AutopilotSource,
		AutopilotTriggerPayload:          strings.TrimSpace(string(task.AutopilotTriggerPayload)),
		QuickCreatePrompt:                task.QuickCreatePrompt,
		IsSquadLeader:                    strings.Contains(instructions, "## Squad Operating Protocol"),
		RequestingUserName:               task.RequestingUserName,
		RequestingUserProfileDescription: task.RequestingUserProfileDescription,
		InitiatorType:                    task.InitiatorType,
		InitiatorID:                      task.InitiatorID,
		InitiatorName:                    task.InitiatorName,
		InitiatorEmail:                   task.InitiatorEmail,
		WorkspaceContext:                 task.WorkspaceContext,
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
	openclawBin := ""
	if provider == "openclaw" {
		openclawBin = entry.Path
	}
	// Resolve any local_directory assignment again here so runTask can plumb
	// LocalWorkDir into execenv. handleTask already validated + locked the
	// path; this call is a pure JSON parse over the same task payload.
	localAssignment, _ := findLocalDirectoryAssignment(task.ProjectResources, d.cfg.DaemonID)
	// Reuse intentionally skipped for local_directory tasks: the prior
	// WorkDir is the user's own path (always present) but the reuse path
	// loses the envRoot association the GC loop needs, and re-running
	// Prepare against a stable user path is cheap (no clone, no copy).
	var agentMcpConfig json.RawMessage
	if task.Agent != nil {
		agentMcpConfig = task.Agent.McpConfig
	}
	if task.PriorWorkDir != "" && localAssignment == nil {
		env = execenv.Reuse(execenv.ReuseParams{
			WorkDir:      task.PriorWorkDir,
			Provider:     provider,
			CodexVersion: codexVersion,
			OpenclawBin:  openclawBin,
			McpConfig:    agentMcpConfig,
			Task:         taskCtx,
		}, d.logger)
	}
	if env == nil {
		var err error
		prepParams := execenv.PrepareParams{
			WorkspacesRoot: d.cfg.WorkspacesRoot,
			WorkspaceID:    task.WorkspaceID,
			TaskID:         task.ID,
			AgentName:      agentName,
			Provider:       provider,
			CodexVersion:   codexVersion,
			OpenclawBin:    openclawBin,
			McpConfig:      agentMcpConfig,
			Task:           taskCtx,
		}
		if localAssignment != nil {
			prepParams.LocalWorkDir = localAssignment.AbsPath
		}
		env, err = execenv.Prepare(prepParams, d.logger)
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

	reused := gateResumeToReusedWorkdir(&task, &taskCtx, env.WorkDir, taskLog)

	// Inject runtime-specific config (meta skill) so the agent discovers .agent_context/.
	runtimeBrief, err := execenv.InjectRuntimeConfig(env.WorkDir, provider, taskCtx)
	if err != nil {
		d.logger.Warn("execenv: inject runtime config failed (non-fatal)", "error", err)
	}
	// Workdir is preserved for reuse by future tasks on the same (agent,
	// issue) pair in cloud mode; the work_dir path is stored in DB on task
	// completion and passed back via PriorWorkDir on the next claim, so
	// rewriting the marker block in place is the right behavior.
	//
	// In local_directory mode the workdir is the user's own repo, reuse is
	// already disabled above (see localAssignment == nil), and the brief
	// would otherwise live on inside the user's repository — a subsequent
	// manual `claude` / `codex` / `gemini` run in that directory would pick
	// up stale Multica instructions (issue id, trigger comment id, reply
	// rules) and start acting on the previous task's context. Excise the
	// marker block on the way out instead.
	if env.LocalDirectory {
		defer func() {
			if cerr := execenv.CleanupRuntimeConfig(env.WorkDir, provider); cerr != nil {
				d.logger.Warn("execenv: cleanup runtime config failed (non-fatal)", "error", cerr)
			}
			// Excise the sidecar tree (.agent_context/, .multica/,
			// provider-specific .claude/skills/ etc.) that Prepare wrote
			// into the user's repo. Without this pass the user's tree
			// accumulates one directory layer per task — see MUL-2784.
			// CleanupRuntimeConfig handles the runtime brief inside
			// CLAUDE.md / AGENTS.md / GEMINI.md; CleanupSidecars handles
			// every other file Prepare placed under WorkDir. Together
			// they round-trip the workdir to its exact pre-task bytes.
			if cerr := execenv.CleanupSidecars(env.RootDir); cerr != nil {
				d.logger.Warn("execenv: cleanup sidecars failed (non-fatal)", "error", cerr)
			}
		}()
	}

	prompt := BuildPrompt(task, provider)

	// Pass the daemon's auth credentials and context so the spawned agent CLI
	// can call the Multica API and the local daemon (e.g. `multica repo checkout`).
	// MULTICA_TASK_SLOT is allocated from the daemon-wide concurrency pool, not
	// per-agent. When one daemon hosts multiple agents, slots index shared
	// daemon-level resources such as GPUs.
	// MULTICA_TOKEN is the credential the agent process will use to call the
	// Multica API. Prefer the task-scoped token the server minted at claim
	// time — that token is bound to (agent, task) and the auth middleware
	// rejects it on owner-only endpoints (e.g. `/api/agents/{id}/env`), so
	// the agent cannot use it to read another agent's secrets. Falls back
	// to the daemon's own credential only when the server returned no
	// auth_token (older server, or cloud / system runtime with no owner) —
	// in that legacy mode lateral-movement protection relies on the
	// runtime not handing the daemon a workspace-owner PAT in the first
	// place. See MUL-2600.
	agentToken := task.AuthToken
	if agentToken == "" {
		agentToken = d.client.Token()
	}
	agentEnv := map[string]string{
		"MULTICA_TOKEN":        agentToken,
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
	// Point OpenClaw at the per-task synthesized config. The config pins
	// agents.defaults.workspace (and any agents.list[].workspace) to the
	// task workdir, so the CLI's native skill scanner picks up the per-task
	// skills written under {workDir}/skills/. Falls back silently when the
	// preparer didn't run (non-openclaw provider, or write failure).
	if env.OpenclawConfigPath != "" {
		agentEnv["OPENCLAW_CONFIG_PATH"] = env.OpenclawConfigPath
	}
	// Grant the wrapper config permission to $include the user's active
	// config across directories. OpenClaw's $include defaults to confining
	// resolution to the wrapper's own directory; without this, the
	// wrapper-out-of-envRoot $include into ~/.openclaw/openclaw.json is
	// rejected and the run boots with no user-registered agents.
	if rootsValue, ok := composeOpenclawIncludeRoots(env.OpenclawIncludeRoot, os.Getenv("OPENCLAW_INCLUDE_ROOTS")); ok {
		agentEnv["OPENCLAW_INCLUDE_ROOTS"] = rootsValue
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
	thinkingLevel := ""
	if task.Agent != nil {
		thinkingLevel = task.Agent.ThinkingLevel
	}
	// Per-model guard: the server validates the literal token against the
	// provider's enum, but per-model gaps (Claude's `xhigh` on a non-Opus
	// model, Codex's per-model `supported_reasoning_levels`) only resolve
	// here, against the daemon's local CLI catalog. Invalid combinations
	// log a warning and drop the level rather than failing the task, so a
	// stale persisted value never blocks execution. Empty model is passed
	// through unchanged — ValidateThinkingLevel resolves it to the
	// provider's default model internally so default-model tasks aren't
	// misjudged. Discovery errors fail open: if we can't list models, we
	// keep the persisted level and let the CLI surface any objection.
	if thinkingLevel != "" {
		ok, err := agent.ValidateThinkingLevel(ctx, provider, entry.Path, model, thinkingLevel)
		if err != nil {
			taskLog.Warn("thinking_level: catalog lookup failed; passing through",
				"provider", provider,
				"model", model,
				"thinking_level", thinkingLevel,
				"error", err,
			)
		} else if !ok {
			taskLog.Warn("thinking_level: not valid for this (provider, model); skipping injection",
				"provider", provider,
				"model", model,
				"thinking_level", thinkingLevel,
			)
			thinkingLevel = ""
		}
	}
	execOpts := agent.ExecOptions{
		Cwd:                       env.WorkDir,
		Model:                     model,
		ThreadName:                deriveTaskThreadName(task),
		Timeout:                   d.cfg.AgentTimeout,
		SemanticInactivityTimeout: d.cfg.CodexSemanticInactivityTimeout,
		ResumeSessionID:           task.PriorSessionID,
		ExtraArgs:                 extraArgs,
		CustomArgs:                customArgs,
		McpConfig:                 mcpConfig,
		ThinkingLevel:             thinkingLevel,
	}
	// Some providers do not reliably load the per-task runtime config files we
	// write into the task workdir:
	//   - openclaw is pinned to the task workdir via the per-task config we
	//     synthesize (see prepareOpenclawConfig), so AGENTS.md / .agent_context/
	//     in the workdir ARE picked up by the CLI. Inline injection is retained
	//     as a belt-and-suspenders for older openclaw releases until that load
	//     path stabilises in production; remove this once a release tracks the
	//     workdir bootstrap reliably end-to-end.
	//   - kiro and kimi are wrapped through their own CLIs whose cwd handling
	//     is opaque enough that we can't trust the file-based path either.
	// Pass the full runtime brief inline (CLI catalog + workflow steps + agent
	// identity/persona + skills + project context) so the backend prepends the
	// same payload that file-based runtimes pick up from disk. Without this,
	// these providers silently miss the workflow section and never call
	// `multica issue status` / `multica issue comment add`, leaving issues
	// stuck in `todo`.
	//
	// Hermes is intentionally excluded: ACP sessions start in the task cwd and
	// Hermes loads AGENTS.md / .agent_context itself. Prepending the full runtime
	// brief into the ACP user prompt duplicates that context, bloats every turn,
	// and has triggered upstream safety filters on harmless tasks.
	if providerNeedsInlineSystemPrompt(provider) {
		execOpts.SystemPrompt = runtimeBrief
	}

	taskLog.Debug("invoking backend",
		"provider", provider,
		"model", model,
		"prompt_bytes", len(prompt),
		"custom_args", len(customArgs),
		"extra_args", len(extraArgs),
		"mcp_config", len(mcpConfig) > 0,
		"inline_system_prompt", execOpts.SystemPrompt != "",
		"resume_session", execOpts.ResumeSessionID != "",
		"timeout", execOpts.Timeout,
	)

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
	taskLog.Debug("agent result detail",
		"status", result.Status,
		"output_bytes", len(result.Output),
		"session_id", result.SessionID,
		"models_with_usage", len(result.Usage),
		"agent_error", result.Error,
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
			// The agent completed successfully but produced no text output.
			// This is valid — the agent may have done all its work via tool
			// calls (e.g. posting comments via CLI, pushing code). Treat as
			// a normal completion so the task is not incorrectly marked as
			// blocked.
			return TaskResult{
				Status:    "completed",
				Comment:   "",
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
		failureReason := "timeout"
		if reason, ok := classifyResumeUnsafeTimeout(provider, comment); ok {
			taskLog.Warn("agent timed out with resume-unsafe session, classifying as blocked",
				"failure_reason", reason,
			)
			failureReason = reason
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       comment,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			FailureReason: failureReason,
			Usage:         usageEntries,
		}, nil
	case "idle_watchdog":
		// The idle watchdog force-stopped the run because the backend
		// went silent (e.g. claude blocked on a tool call against a
		// frozen child process). Route through the blocked path with a
		// dedicated failure_reason so the run leaves "running" state and
		// operators can tell idle-stop apart from a real timeout.
		comment := result.Error
		if comment == "" {
			comment = idleWatchdogReason(d.cfg.AgentIdleWatchdog)
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       comment,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			FailureReason: "idle_watchdog",
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
		//
		// Classify upstream API 400 invalid_request_error failures with a
		// dedicated failure_reason so GetLastTaskSession excludes the
		// task from the (agent_id, issue_id) resume lookup. Without this
		// classifier a corrupt image or oversized payload baked into the
		// conversation permanently blocks the issue: every follow-up
		// task resumes the same poisoned session and hits the same 400.
		failureReason, _ := classifyPoisonedError(errMsg)
		if failureReason != "" {
			taskLog.Warn("agent failed with poisoned API error, classifying as blocked",
				"failure_reason", failureReason,
			)
		} else {
			// MUL-2946: classifyPoisonedError only matches the
			// session-poisoning Anthropic 400 shape. Everything else
			// falls through to taskfailure.Classify, which maps the
			// raw error string to one of the 14 agent_error.*
			// sub-reasons (provider auth, capacity, context overflow,
			// runner crash, …) or to ReasonAgentUnknown. This keeps
			// the failure_reason column in the canonical refined
			// taxonomy at write time instead of waiting on the
			// MUL-1949 offline backfill to re-classify after the
			// fact.
			failureReason = taskfailure.Classify(errMsg).String()
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       errMsg,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			Usage:         usageEntries,
			FailureReason: failureReason,
		}, nil
	}
}

// executeAndDrain runs a backend, drains its message stream (forwarding to the
// server), and waits for the final result.
func (d *Daemon) executeAndDrain(ctx context.Context, backend agent.Backend, prompt string, opts agent.ExecOptions, taskLog *slog.Logger, taskID string) (agent.Result, int32, error) {
	// Wrap the caller's ctx so the idle watchdog (below) can interrupt both
	// the agent subprocess (via the ctx passed to backend.Execute) AND the
	// drain loop with a single cancel. Without this layer the backend would
	// stay tied to the parent ctx and our cancellation could only abort
	// drain, leaving the subprocess running.
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()

	session, err := backend.Execute(agentCtx, prompt, opts)
	if err != nil {
		taskLog.Debug("backend execute returned error", "error", err)
		return agent.Result{}, 0, err
	}
	taskLog.Debug("backend started, draining messages")

	// Bound the drain loop only when there is a wall-clock cap. With a positive
	// opts.Timeout, give the drain a slightly longer deadline than the backend
	// so it can still collect the backend's own timeout Result if the scanner
	// is stuck on a hung stdout pipe (the extra 30 s covers cleanup after the
	// backend's own deadline fires). With no cap (opts.Timeout <= 0) the
	// inactivity watchdog is the only liveness net, so the drain must NOT
	// impose its own deadline either — otherwise an actively streaming long run
	// would be cut off here regardless of progress (MUL-3064).
	var drainCtx context.Context
	var drainCancel context.CancelFunc
	if opts.Timeout > 0 {
		drainCtx, drainCancel = context.WithTimeout(agentCtx, opts.Timeout+30*time.Second)
	} else {
		drainCtx, drainCancel = context.WithCancel(agentCtx)
	}
	defer drainCancel()

	var toolCount atomic.Int32
	// lastActivityAt records (as unix nanos) when the drain loop most
	// recently received a message from the backend. The idle watchdog
	// reads this to decide whether the agent has gone silent for too long.
	// Initialise to the start so a backend that never emits a single
	// message also trips the watchdog.
	var lastActivityAt atomic.Int64
	lastActivityAt.Store(time.Now().UnixNano())
	// inFlightTools counts tool_use messages that haven't yet been paired
	// with a matching tool_result. A non-zero count means the agent is
	// legitimately waiting on a tool (e.g. `npm install`, `docker build`)
	// that may run far longer than the idle window without emitting any
	// message — so while a tool is in flight the watchdog applies the larger
	// AgentToolWatchdog budget instead of treating that silence as a hang.
	var inFlightTools atomic.Int32
	var idleWatchdogFired atomic.Bool
	// idleWatchdogThreshold records (as nanos) which silence budget actually
	// tripped the watchdog — the idle window or the larger in-flight-tool
	// window — so the failure message reports the real duration.
	var idleWatchdogThreshold atomic.Int64
	idleWatchdogThreshold.Store(int64(d.cfg.AgentIdleWatchdog))
	idleWindow := d.cfg.AgentIdleWatchdog
	if idleWindow > 0 {
		go d.runIdleWatchdog(agentCtx, idleWindow, d.cfg.AgentToolWatchdog, &lastActivityAt, &inFlightTools, &idleWatchdogFired, &idleWatchdogThreshold, agentCancel, session.Messages, taskLog, taskID)
	}

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
				// Stamp activity as soon as a message lands. The idle
				// watchdog reads this to decide whether the backend has
				// gone silent — stamping before processing makes sure a
				// slow downstream call (mu.Lock contention, batch resize)
				// can't be misattributed to backend silence.
				lastActivityAt.Store(time.Now().UnixNano())
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
					inFlightTools.Add(1)
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
					// Decrement only when the count would stay >= 0. A stray
					// tool_result with no matching tool_use (backend bug or
					// reconnect mid-stream) shouldn't push the counter
					// negative — that would re-arm the watchdog one tool_use
					// too early on the next call.
					for {
						cur := inFlightTools.Load()
						if cur <= 0 {
							break
						}
						if inFlightTools.CompareAndSwap(cur, cur-1) {
							break
						}
					}
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
		if idleWatchdogFired.Load() {
			// The backend's wait goroutine (e.g. claude.go) translates the
			// SIGKILL we delivered via agentCancel into Status="aborted".
			// Re-tag it as "idle_watchdog" so runTask routes the
			// disposition through a dedicated failure_reason, not the
			// generic "agent_error" bucket the aborted path falls into.
			result.Status = "idle_watchdog"
			if result.Error == "" {
				result.Error = idleWatchdogReason(time.Duration(idleWatchdogThreshold.Load()))
			}
		}
		return result, toolCount.Load(), nil
	case <-drainCtx.Done():
		// Idle watchdog cancels via agentCancel(), which propagates here as
		// context.Canceled. Check this BEFORE the generic cancelled/timeout
		// classifiers so a watchdog-induced stop isn't misreported as
		// "task cancelled by server".
		if idleWatchdogFired.Load() {
			return agent.Result{
				Status: "idle_watchdog",
				Error:  idleWatchdogReason(time.Duration(idleWatchdogThreshold.Load())),
			}, toolCount.Load(), nil
		}
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

// idleWatchdogReason formats the human-facing explanation surfaced on
// idle_watchdog dispositions. Centralised so the result-arrival branch and the
// drain-timeout branch in executeAndDrain emit identical wording.
func idleWatchdogReason(window time.Duration) string {
	return fmt.Sprintf("agent produced no new messages for %s and message queue was empty; force-stopped by idle watchdog", window)
}

// runIdleWatchdog ticks until either agentCtx is cancelled or the backend has
// been silent past the applicable budget. On firing, it records the tripped
// threshold, sets fired, and calls cancel, which propagates to the agent
// subprocess (via the ctx passed to backend.Execute) and to drainCtx. The
// silence budget depends on whether a tool call is in flight:
//
//  1. No tool in flight — a silent backend is a hang after `window`.
//  2. A tool in flight (tool_use with no matching tool_result yet) — a real
//     tool (e.g. `npm install`, `docker build`) legitimately runs silently for
//     many minutes, so the larger `toolWindow` applies instead. toolWindow <= 0
//     keeps the historical behavior of never force-stopping while a tool is in
//     flight. Without this in-flight budget a backend that emits tool_use and
//     never the matching tool_result would run forever now that there is no
//     wall-clock cap (MUL-3064).
//
// In both cases the watchdog also requires the session.Messages buffer to be
// empty — a buffered-but-undrained message means the drain loop is behind, not
// the backend.
//
// Tick interval is window/2 (floored at 30 s in production, but the floor only
// kicks in for windows >= 1 min so tests can pass tiny windows like 50 ms and
// see the watchdog fire within a few ticks).
func (d *Daemon) runIdleWatchdog(agentCtx context.Context, window, toolWindow time.Duration, lastActivityAt *atomic.Int64, inFlightTools *atomic.Int32, fired *atomic.Bool, firedThreshold *atomic.Int64, cancel context.CancelFunc, messages <-chan agent.Message, taskLog *slog.Logger, taskID string) {
	interval := window / 2
	if window >= time.Minute && interval < 30*time.Second {
		interval = 30 * time.Second
	}
	if interval <= 0 {
		interval = window
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-agentCtx.Done():
			return
		case <-ticker.C:
			// Pick the silence budget. A tool in flight is expected to be
			// silent (a long build/install/test emits nothing between
			// tool_use and tool_result), so it gets the larger toolWindow;
			// toolWindow <= 0 disables the in-flight bound entirely.
			threshold := window
			toolInFlight := inFlightTools.Load() > 0
			if toolInFlight {
				if toolWindow <= 0 {
					continue
				}
				threshold = toolWindow
			}
			last := time.Unix(0, lastActivityAt.Load())
			idleFor := time.Since(last)
			if idleFor < threshold {
				continue
			}
			// A buffered-but-undrained message means the drain loop is
			// behind, not the backend. Wait one more tick rather than
			// killing a backend that is still producing output.
			if len(messages) > 0 {
				continue
			}
			taskLog.Warn("idle watchdog firing: no agent activity, force-stopping run",
				"task", shortID(taskID),
				"idle_for", idleFor.Round(time.Second).String(),
				"threshold", threshold.String(),
				"tool_in_flight", toolInFlight,
			)
			firedThreshold.Store(int64(threshold))
			fired.Store(true)
			cancel()
			return
		}
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
		result[i] = execenv.RepoContextForEnv{URL: r.URL, Description: r.Description}
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
			Name:        s.Name,
			Description: s.Description,
			Content:     s.Content,
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

// composeOpenclawIncludeRoots returns the value the daemon should set for
// OPENCLAW_INCLUDE_ROOTS on the child openclaw process so its `$include`
// loader will follow the wrapper's reference out of envRoot into the
// user's active config directory.
//
// addRoot is the directory we must grant (typically dirname of the user's
// active openclaw.json). userValue is whatever the daemon's own
// environment already has under OPENCLAW_INCLUDE_ROOTS — the user's own
// cross-directory layout. We prepend addRoot, dedupe by string equality,
// drop empty path segments, and return ok=false when there's nothing to
// grant (addRoot is empty — fresh install case), so callers can leave the
// env var alone in that case.
//
// Path separator is the OS-native list separator (`:` on Unix, `;` on
// Windows) to match how OpenClaw splits the env var.
func composeOpenclawIncludeRoots(addRoot, userValue string) (string, bool) {
	if addRoot == "" {
		return "", false
	}
	parts := []string{addRoot}
	seen := map[string]struct{}{addRoot: {}}
	for _, p := range strings.Split(userValue, string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		parts = append(parts, p)
	}
	return strings.Join(parts, string(os.PathListSeparator)), true
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
	case "HOME", "PATH", "USER", "SHELL", "TERM", "CODEX_HOME", "OPENCLAW_CONFIG_PATH", "OPENCLAW_INCLUDE_ROOTS":
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
