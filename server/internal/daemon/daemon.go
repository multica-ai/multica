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
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/selfexec"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// ErrRepoNotConfigured is returned by ensureRepoReady when the requested repo
// URL is not present in the workspace's repo configuration after a fresh
// server refresh.
var ErrRepoNotConfigured = errors.New("repo is not configured for this workspace")

// ErrNoRuntimesToRegister is returned by registerRuntimesForWorkspace when
// the daemon has nothing to host on a workspace — typically a custom-only
// daemon whose only enabled custom runtime profile was just disabled, leaving
// zero built-in agents and zero resolvable profiles. Callers must
// differentiate by intent: initial registration (syncWorkspacesFromAPI's
// new-workspace branch) treats this as a config error and skips the
// workspace until something changes; the profile-drift refresh path
// (refreshWorkspaceRuntimeProfiles) treats it as a legitimate converged
// state and explicitly deregisters the now-stale local runtime IDs so the
// server marks them offline immediately instead of waiting on the 150 s
// stale-heartbeat sweep.
var ErrNoRuntimesToRegister = errors.New("no agent runtimes could be registered")

const (
	taskSlotWaitTimeout     = 2 * time.Second
	taskSlotCapacityBackoff = 5 * time.Second
)

var (
	taskPrepareLeaseRefresh = 15 * time.Second
	taskPrepareLeaseTimeout = 10 * time.Second
)

func taskScopedAuthToken(task Task) (string, error) {
	token := strings.TrimSpace(task.AuthToken)
	if token == "" {
		return "", errors.New("server did not provide task-scoped auth token")
	}
	if !strings.HasPrefix(token, "mat_") {
		return "", errors.New("server provided non-task-scoped auth token")
	}
	return token, nil
}

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
	isBrewInstall         = cli.IsBrewInstall
	getBrewPrefix         = cli.GetBrewPrefix
	matchKnownBrewPrefix  = cli.MatchKnownBrewPrefix
	resolveSelfExecutable = selfexec.Resolve

	// detectAgentVersion / checkAgentMinVersion are indirections over the
	// real agent helpers so tests can run the registration path without
	// shelling out to a real CLI. Mirrors the pattern used for the brew
	// helpers above.
	detectAgentVersion   = agent.DetectVersion
	checkAgentMinVersion = agent.CheckMinVersion

	// lookPath is an indirection over exec.LookPath so registration tests can
	// resolve custom runtime-profile commands without manipulating the
	// process PATH. Mirrors the detectAgentVersion hook above.
	lookPath = exec.LookPath

	// profilePathExecutable reports whether path points at an existing,
	// non-directory file with at least one executable bit set. It is the
	// gate appendProfileRuntimes uses before trusting a per-machine command
	// path override (MUL-3284) — a stale or mistyped override must fall back
	// to the PATH lookup rather than register a runtime that can't launch.
	// Indirected as a package var so tests can assert override preference
	// without staging a real executable on disk.
	profilePathExecutable = func(path string) bool {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return false
		}
		return info.Mode().Perm()&0o111 != 0
	}
)

// workspaceState tracks registered runtimes for a single workspace.
//
// allowedRepoURLs covers the workspace-level repo bindings; it gets rebuilt on
// every refresh from the server. taskRepoURLs covers repos that the server
// surfaced through a per-task claim (project github_repo resources today,
// possibly other typed sources later) — those don't show up in
// GetWorkspaceRepos, so they would be wiped on refresh if we shared one map.
// taskRepoRefs tracks optional checkout refs for the specific task that
// surfaced each project repo so two projects using the same URL don't leak refs
// into each other.
type workspaceState struct {
	workspaceID     string
	runtimeIDs      []string
	reposVersion    string // stored for future use: skip refresh when version unchanged
	allowedRepoURLs map[string]struct{}
	taskRepoURLs    map[string]struct{}
	taskRepoRefs    map[string]map[string]string // taskID -> repo URL -> checkout ref
	settings        json.RawMessage              // workspace settings (JSONB)
	lastRepoSyncErr string
	repoRefreshMu   sync.Mutex
	// profileSetSig is a content hash of the workspace's custom runtime
	// profile list (MUL-3332) as last seen from the server. An on-demand
	// refresh compares the live signature with this cached value; any drift
	// triggers a re-register so newly-added (or edited / disabled) custom
	// runtimes appear without a daemon restart. Empty before the first
	// successful profile fetch (older server / network blip); guarded by
	// Daemon.mu like every other field on this struct.
	profileSetSig string
}

type repoCacheBackend interface {
	Lookup(workspaceID, url string) string
	Sync(workspaceID string, repos []repocache.RepoInfo) error
	WithRepoLock(barePath string, fn func() error) error
	CreateWorktree(params repocache.WorktreeParams) (*repocache.WorktreeResult, error)
}

// Daemon is the local agent runtime that polls for and executes tasks.
type Daemon struct {
	cfg        Config
	client     *Client
	repoCache  repoCacheBackend
	skillCache *SkillBundleCache
	logger     *slog.Logger

	mu           sync.Mutex
	workspaces   map[string]*workspaceState
	runtimeIndex map[string]Runtime // runtimeID -> Runtime for provider lookups
	// profileLaunchSpecs maps a custom runtime profile_id -> the absolute
	// executable path plus fixed launch args resolved for that profile
	// (MUL-3284). Populated in registerRuntimesForWorkspace when a profile's
	// command resolves; read by runTask to launch the custom command for a
	// claimed task. Guarded by mu.
	profileLaunchSpecs map[string]profileLaunchSpec
	reloading          sync.Mutex         // prevents concurrent workspace syncs
	runtimeSet         *runtimeSetWatcher // multi-subscriber pub/sub for runtime-set changes

	versionsMu    sync.RWMutex      // guards agentVersions
	agentVersions map[string]string // provider -> detected CLI version (set during registration)

	// resolvedPathsMu guards resolvedPaths, the self-healed executable paths.
	// The daemon pins each agent's absolute path at startup so a later PATH
	// change can't redirect a task launch. When that pinned path later vanishes
	// (a version manager did an in-place upgrade — Homebrew Cask, nvm/fnm),
	// resolveAgentEntry re-resolves the original command once and records the
	// result here so subsequent launches, model lists, and registrations reuse
	// it without re-resolving. Path and detected version are stored together so
	// any reader that observes the new path also observes the matching version
	// (no "new binary under stale version policy" window). Keyed by provider.
	// See MUL-4486.
	resolvedPathsMu sync.RWMutex
	resolvedPaths   map[string]healedAgent
	// healGroup coalesces concurrent self-heal re-resolutions per provider so a
	// just-upgraded agent seen by many queued tasks at once pays for a single
	// login-shell probe + version detection instead of one per task (MUL-4486).
	healGroup singleflight.Group

	wsHBMu      sync.RWMutex         // guards wsHBLastAck
	wsHBLastAck map[string]time.Time // runtime_id -> last successful WS heartbeat ack timestamp

	// reconcile fans out a "re-check server state now" signal to subscribers
	// (watchTaskCancellation, workspaceSyncLoop) so the WS connect/reconnect
	// path can shrink coarse fallback reconciliation gaps to sub-second. See
	// reconcile.go and runTaskWakeupConnection.
	reconcile *reconcileBroadcaster
	// workspaceChanges is the account-scoped server hint for membership-set
	// changes. It stays separate from reconcile because a membership hint only
	// needs the minimal workspace list, while a WS reconnect also reconciles
	// runtime profiles that may have changed during the gap.
	workspaceChanges *workspaceChangeSignal

	// wsRPC carries generic request/response RPCs (e.g. tasks.claim, MUL-4257)
	// over the task-wakeup WS connection. It is attached to the live
	// connection in runTaskWakeupConnection and detached on disconnect; when
	// detached, callers fall back to HTTP.
	wsRPC *wsRPCClient

	// batchClaimUnsupported is set once a batch claim gets a 404 from the
	// server (no /api/daemon/tasks/claim route — an un-upgraded server), so
	// subsequent polls skip WS+batch and use the legacy per-runtime claim
	// directly. Reset when the WS (re)connects, so a server upgrade that
	// bounces the connection re-probes the batch route (MUL-4257).
	batchClaimUnsupported atomic.Bool

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
	pauseClaims    bool // when true, the batch poller skips claiming
	claimsInFlight int  // pollers that have decided to claim but haven't yet handed the task off to handleTask

	activeEnvRootsMu   sync.Mutex
	activeEnvRootsCond *sync.Cond      // signalled when an in-flight env-root GC mutation finishes
	activeEnvRoots     map[string]int  // env root path -> reference count (handles reuse paths marked twice)
	deletingEnvRoots   map[string]bool // env roots reserved by GC; new tasks wait until the mutation finishes

	activeCodexStoresMu   sync.Mutex
	activeCodexStoresCond *sync.Cond      // signalled when an in-flight store deletion finishes, so a blocked markActive can proceed
	activeCodexStores     map[string]int  // per-issue Codex session store path -> live-task refcount; guards the store from GC mid-task (MUL-4424)
	deletingCodexStores   map[string]bool // store paths a GC delete has reserved; markActive waits these out so a task never mounts a store mid-removal

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

type profileLaunchSpec struct {
	path      string
	version   string
	fixedArgs []string
}

// New creates a new Daemon instance.
func New(cfg Config, logger *slog.Logger) *Daemon {
	cacheRoot := filepath.Join(cfg.WorkspacesRoot, ".repos")
	skillCacheRoot := filepath.Join(cfg.WorkspacesRoot, ".skill-cache", "v1")
	client := NewClient(cfg.ServerBaseURL)
	// Tag every daemon HTTP request with the daemon's CLI version so the
	// server can split logs/metrics by client version (parallel to the CLI).
	client.SetVersion(cfg.CLIVersion)
	d := &Daemon{
		cfg:                       cfg,
		client:                    client,
		repoCache:                 repocache.New(cacheRoot, logger),
		skillCache:                NewSkillBundleCache(skillCacheRoot),
		logger:                    logger,
		workspaces:                make(map[string]*workspaceState),
		runtimeIndex:              make(map[string]Runtime),
		profileLaunchSpecs:        make(map[string]profileLaunchSpec),
		runtimeSet:                newRuntimeSetWatcher(),
		agentVersions:             make(map[string]string),
		resolvedPaths:             make(map[string]healedAgent),
		wsHBLastAck:               make(map[string]time.Time),
		activeEnvRoots:            make(map[string]int),
		deletingEnvRoots:          make(map[string]bool),
		activeCodexStores:         make(map[string]int),
		deletingCodexStores:       make(map[string]bool),
		localPathLocks:            NewLocalPathLocker(),
		runtimeGoneInflight:       make(map[string]struct{}),
		reregisterNextAttempt:     make(map[string]time.Time),
		reregisterLastCompletedAt: make(map[string]time.Time),
		cancelPollInterval:        5 * time.Second,
		reconcile:                 newReconcileBroadcaster(),
		workspaceChanges:          newWorkspaceChangeSignal(),
		wsRPC:                     newWSRPCClient(wsRPCResponseGrace),
	}
	d.activeEnvRootsCond = sync.NewCond(&d.activeEnvRootsMu)
	d.activeCodexStoresCond = sync.NewCond(&d.activeCodexStoresMu)
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

// healedAgent bundles a self-healed executable path with the CLI version
// detected for it. The two are published together under resolvedPathsMu and
// returned together from resolveAgentEntry, so a caller that observes the new
// path necessarily observes the matching version — closing the window where a
// just-upgraded binary would run under the daemon's previous version policy
// (MUL-4486 review).
type healedAgent struct {
	path    string
	version string
}

// resolveAgentEntry returns entry with a usable executable path plus the CLI
// version that corresponds to that path, self-healing the pinned Path when it
// has vanished from disk (MUL-4486).
//
// The daemon pins each agent's absolute, symlink-resolved path at startup so a
// later PATH change cannot redirect a task launch. But a version manager
// (Homebrew Cask, nvm/fnm) upgrading in place deletes the old versioned
// directory that pinned path points into and repoints the stable command name
// at the new version — leaving the daemon holding a path that no longer exists
// until it restarts. Every consumer of entry.Path (task launch, model listing,
// version detection at registration) then hard-fails with "executable not
// found".
//
// The returned version always matches the returned path, so callers key
// version-sensitive policy (e.g. the Codex sandbox) off it directly rather than
// re-reading the shared version cache, which a concurrent heal could still be
// updating.
//
// Behaviour:
//   - A previous self-heal that is still live -> returned with its paired
//     version. This is checked first so that once we've re-resolved to a new
//     binary, a reappearing stale path (a downgrade / reinstall recreating the
//     old versioned directory) cannot re-pair that old binary with the healed
//     version — a mismatched {old path, new version} (MUL-4486 review).
//   - Otherwise, pinned Path still present -> returned unchanged, paired with
//     its registration-detected version. The anti-redirect guarantee holds for
//     the normal (never-healed) case: a live pinned binary is never
//     second-guessed even if PATH now points elsewhere.
//   - Pinned Path gone and no live heal -> re-resolve entry.Command once
//     (preserving the ~/.multica/hooks exclusion and the login-shell fallback).
//     Before adopting the re-resolved binary it is version-detected and run
//     through the same minimum-version gate registration applies. This
//     reproduces exactly what a daemon restart would resolve, so it is no less
//     safe than the documented restart workaround — only automatic.
//   - Re-resolution fails, the candidate can't be version-detected, or it is
//     below the minimum supported version -> entry is returned unchanged so the
//     candidate is never launched and the downstream error still surfaces.
func (d *Daemon) resolveAgentEntry(ctx context.Context, provider string, entry AgentEntry) (AgentEntry, string) {
	// A prior self-heal wins over the original pinned path: it carries a
	// {path, version} pair we already verified together, so it can never regress
	// to the mismatched pairing a reappearing stale path would produce.
	d.resolvedPathsMu.RLock()
	healed, ok := d.resolvedPaths[provider]
	d.resolvedPathsMu.RUnlock()
	if ok && agentExecutablePresent(healed.path) {
		entry.Path = healed.path
		return entry, healed.version
	}

	if agentExecutablePresent(entry.Path) {
		return entry, d.agentVersion(provider)
	}

	if entry.Command == "" {
		return entry, d.agentVersion(provider)
	}

	// Coalesce concurrent heals for the same provider: the first task through
	// pays for the re-resolve + version detection, the rest share its result
	// instead of each spawning their own login shell and `--version` probe.
	command := entry.Command
	v, _, _ := d.healGroup.Do(provider, func() (any, error) {
		return d.healAgentPath(ctx, provider, command), nil
	})
	healed, _ = v.(healedAgent)
	if healed.path == "" {
		return entry, d.agentVersion(provider)
	}
	entry.Path = healed.path
	return entry, healed.version
}

// healAgentPath re-resolves command for provider and, if a usable binary is
// found, records it and returns it. "Usable" means: it resolves, its version
// can be detected, and that version meets the same minimum-version gate
// registration enforces. Path and version are published together under
// resolvedPathsMu so any observer of the path also sees the matching version;
// the shared d.agentVersion cache is refreshed too, for registration hygiene.
// It returns a zero healedAgent when nothing usable was found, so the caller
// keeps the (stale) pinned entry and the candidate is never launched. Runs
// under healGroup, one invocation at a time per provider.
func (d *Daemon) healAgentPath(ctx context.Context, provider, command string) healedAgent {
	// Re-check the cache: a predecessor under the same singleflight key may have
	// already populated it, or a prior heal completed between the read above and
	// entering here.
	d.resolvedPathsMu.RLock()
	cached, ok := d.resolvedPaths[provider]
	d.resolvedPathsMu.RUnlock()
	if ok && agentExecutablePresent(cached.path) {
		return cached
	}

	newPath, found := reresolveAgentCommand(command)
	if !found {
		return healedAgent{}
	}

	// Verify before adopting. An in-place "upgrade" that actually repoints at an
	// older or broken install must not be launched under the daemon's stale
	// version policy, and must not slip past the minimum-version gate that the
	// registration path applies (MUL-4486 review).
	version, err := detectAgentVersion(ctx, newPath)
	if err != nil {
		d.logger.Warn("re-resolved agent executable failed version detection; keeping pinned path",
			"provider", provider, "command", command, "new_path", newPath, "error", err)
		return healedAgent{}
	}
	if err := checkAgentMinVersion(provider, version); err != nil {
		d.logger.Warn("re-resolved agent executable is below the minimum supported version; not adopting it",
			"provider", provider, "command", command, "new_path", newPath, "version", version, "error", err)
		return healedAgent{}
	}

	adopted := healedAgent{path: newPath, version: version}
	// Publish path + version atomically: any reader that sees the new path in
	// resolveAgentEntry gets the matching version out of the same struct value.
	d.resolvedPathsMu.Lock()
	if d.resolvedPaths == nil {
		d.resolvedPaths = make(map[string]healedAgent)
	}
	d.resolvedPaths[provider] = adopted
	d.resolvedPathsMu.Unlock()
	// Keep the registration version cache fresh too. The task path reads the
	// version returned alongside the resolved path (above), not this map, so its
	// staleness can never gate a launch — this is hygiene for the registration
	// report and any future d.agentVersion reader.
	d.setAgentVersion(provider, version)

	d.logger.Info("re-resolved agent executable after pinned path vanished (in-place upgrade)",
		"provider", provider, "command", command, "new_path", newPath, "version", version)
	return adopted
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
// applyRegisterResponseInPlace folds a fresh /api/daemon/register response
// back into the workspaceState and runtimeIndex without replacing the
// workspaceState pointer (see syncWorkspacesFromAPI's invariant about
// repoRefreshMu). It is the shared converger used by both the runtime_gone
// recovery and the profile-drift refresh; the two callers differ only in
// follow-up side effects (RecoverOrphans / Deregister), so those stay at the
// call site.
//
// Returns:
//   - newIDs:     the runtime IDs the server returned in this response, in
//     the order they were returned. These are the daemon's authoritative
//     current runtime set after the call.
//   - droppedIDs: runtime IDs that were tracked before this call but did
//     NOT survive the response. Drift callers Deregister these so the
//     server marks them offline immediately instead of waiting on the 150 s
//     stale-heartbeat sweep; the runtime_gone path can ignore them because
//     those rows were already deleted server-side.
//   - ok:         false when the workspace was forgotten between the
//     register call and this apply (e.g. the user left the workspace and
//     syncWorkspacesFromAPI removed it). The caller must abort silently in
//     that case — there is no state left to update.
//
// profileSig is the digest captured during the register; an empty value is
// the explicit "fetch failed, keep the previous signature" sentinel from
// appendProfileRuntimes.
func (d *Daemon) applyRegisterResponseInPlace(workspaceID string, resp *RegisterResponse, profileSig string) (newIDs, droppedIDs []string, ok bool) {
	newIDs = make([]string, 0, len(resp.Runtimes))
	newIDSet := make(map[string]struct{}, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		newIDs = append(newIDs, rt.ID)
		newIDSet[rt.ID] = struct{}{}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	ws, exists := d.workspaces[workspaceID]
	if !exists {
		return nil, nil, false
	}
	// Drop runtimeIndex entries for prior runtime IDs that the server did not
	// return — typically there are none for upsert-on-existing-provider, but
	// a daemon config change (provider removed) or a profile disable would
	// leak entries otherwise.
	for _, oldID := range ws.runtimeIDs {
		if _, kept := newIDSet[oldID]; !kept {
			delete(d.runtimeIndex, oldID)
			droppedIDs = append(droppedIDs, oldID)
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
	// Refresh the cached profile signature only when the fetch succeeded;
	// an empty sig means the GetRuntimeProfiles call failed and we must
	// preserve the previous signature so the next sync tick can still
	// detect a real drift instead of falsely thinking everything is in sync.
	if profileSig != "" {
		ws.profileSetSig = profileSig
	}
	return newIDs, droppedIDs, true
}

func (d *Daemon) reregisterWorkspaceAfterRuntimeGone(ctx context.Context, workspaceID string) error {
	resp, profileSig, err := d.registerRuntimesForWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("register runtimes: %w", err)
	}

	newIDs, _, ok := d.applyRegisterResponseInPlace(workspaceID, resp, profileSig)
	if !ok {
		return fmt.Errorf("workspace %s no longer tracked", workspaceID)
	}

	for _, rid := range newIDs {
		d.logger.Info("re-registered runtime after server-side deletion",
			"workspace_id", workspaceID, "runtime_id", rid)
	}
	d.notifyRuntimeSetChanged()

	// Tell the server about any tasks the previous (now-deleted) runtime
	// was working on, mirroring the registration path's recover-orphans call.
	// This is intentionally scoped to the runtime_gone recovery: the
	// runtimes were truly gone server-side, so anything still in
	// dispatched/running/waiting_local_directory on those rows is an orphan
	// that needs to be failed-and-retried. The drift-refresh path (which
	// also feeds applyRegisterResponseInPlace) deliberately skips this step
	// because its surviving runtime IDs may still be actively executing
	// tasks for the user (MUL-3332).
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
		"opencode_idle_watchdog", d.cfg.OpenCodeIdleWatchdog,
		"max_concurrent_tasks", d.cfg.MaxConcurrentTasks,
		"gc_enabled", d.cfg.GCEnabled,
		"auto_update", d.cfg.AutoUpdateEnabled,
		"launched_by", d.cfg.LaunchedBy,
	)

	// Mark the daemon-owned workspaces tree before any task runs. A sandbox
	// fault can strip every MULTICA_* env var from an agent subprocess; the
	// per-workdir marker then only protects cwds inside the workdir, and a
	// subprocess that escaped to the workdir's parent would fall back to the
	// user's config PAT. The root marker makes the CLI fail closed anywhere
	// under the tree. Non-fatal: Prepare re-ensures it per task.
	if err := execenv.EnsureWorkspacesRootMarker(d.cfg.WorkspacesRoot); err != nil {
		d.logger.Warn("workspaces root marker not written; CLI fail-closed guard limited to task workdirs", "error", err)
	}

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

// recordProfileLaunch remembers the absolute executable path and fixed launch
// args resolved for a custom runtime profile. Called from
// registerRuntimesForWorkspace. Lazily initializes the map so test fixtures
// that build a Daemon literal without seeding every map don't panic.
func (d *Daemon) recordProfileLaunch(profileID, path, version string, fixedArgs []string) {
	if profileID == "" || path == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.profileLaunchSpecs == nil {
		d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	}
	d.profileLaunchSpecs[profileID] = profileLaunchSpec{
		path:      path,
		version:   version,
		fixedArgs: append([]string(nil), fixedArgs...),
	}
}

// customProfileLaunchForRuntime returns the resolved custom executable path and
// fixed args for a claimed task's RuntimeID, and whether the runtime is a
// custom-profile runtime. It returns false for built-in runtimes (no profile)
// and for runtimes whose profile command was never resolved on this host.
func (d *Daemon) customProfileLaunchForRuntime(runtimeID string) (profileLaunchSpec, bool) {
	if runtimeID == "" {
		return profileLaunchSpec{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rt, ok := d.runtimeIndex[runtimeID]
	if !ok || rt.ProfileID == "" {
		return profileLaunchSpec{}, false
	}
	spec, ok := d.profileLaunchSpecs[rt.ProfileID]
	if !ok || spec.path == "" {
		return profileLaunchSpec{}, false
	}
	spec.fixedArgs = append([]string(nil), spec.fixedArgs...)
	return spec, true
}

func (d *Daemon) registerRuntimesForWorkspace(ctx context.Context, workspaceID string) (*RegisterResponse, string, error) {
	d.logger.Debug("registering runtimes for workspace", "workspace_id", workspaceID, "agent_count", len(d.cfg.Agents))
	var runtimes []map[string]string
	var failedProfiles []map[string]string
	for name, entry := range d.cfg.Agents {
		// Self-heal a pinned executable path an in-place upgrade deleted
		// (MUL-4486) so version detection — and thus staying registered/online
		// — recovers without a daemon restart. resolveAgentEntry already
		// version-gates the healed binary; the detect + min-version check below
		// still runs to produce the version string this registration reports.
		entry, _ = d.resolveAgentEntry(ctx, name, entry)
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
		displayName := providerDisplayName(name)
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

	// Append any workspace custom runtime profiles whose command resolves on
	// this host (MUL-3284). This is best-effort: a fetch error (e.g. an older
	// server returning 404) must never fail registration — the daemon simply
	// continues with the built-in runtimes it already collected. A profile
	// whose command_name is not on PATH is skipped (the host doesn't have it).
	//
	// profileSig is a content hash of the workspace's profile list captured
	// here so an on-demand server notification can skip re-registration when
	// the effective profile set is already current (MUL-3332). An empty string
	// means the fetch failed and the caller must keep whatever signature was
	// previously cached on the workspaceState.
	profileSig := d.appendProfileRuntimes(ctx, workspaceID, &runtimes, &failedProfiles)

	if len(runtimes) == 0 && len(failedProfiles) == 0 {
		// profileSig is still meaningful even when nothing resolves: the
		// refresh path uses it to remember "we already converged on the
		// disabled-everywhere state" so duplicate change notifications are a
		// no-op instead of a re-empty-register loop. Initial-registration
		// callers that don't care about the sig discard it via _.
		return nil, profileSig, ErrNoRuntimesToRegister
	}

	req := map[string]any{
		"workspace_id":      workspaceID,
		"daemon_id":         d.cfg.DaemonID,
		"legacy_daemon_ids": d.cfg.LegacyDaemonIDs,
		"device_name":       d.cfg.DeviceName,
		"cli_version":       d.cfg.CLIVersion,
		"launched_by":       d.cfg.LaunchedBy,
		"runtimes":          runtimes,
		"failed_profiles":   failedProfiles,
	}

	resp, err := d.client.Register(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("register runtimes: %w", err)
	}
	if len(resp.Runtimes) == 0 && len(failedProfiles) == 0 {
		return nil, "", fmt.Errorf("register runtimes: empty response")
	}
	d.logger.Debug("register response", "workspace_id", workspaceID, "runtimes", len(resp.Runtimes), "repos", len(resp.Repos), "repos_version", resp.ReposVersion)
	return resp, profileSig, nil
}

// appendProfileRuntimes fetches the workspace's enabled custom runtime
// profiles (MUL-3284) and appends a runtime registration entry for each one
// whose command_name resolves on this host's PATH. For each resolved profile
// it records the absolute command path and fixed args keyed by profile_id (via
// recordProfileLaunch) so runTask can later launch the custom executable for a
// claimed task.
//
// Best-effort by contract: any error fetching profiles (older server, network
// blip) is logged and swallowed — registration proceeds with the built-in
// runtimes already collected. A profile whose command is not on PATH is
// skipped with an Info log (this host simply doesn't have that command).
//
// The registration entry mirrors the built-in shape: name = display_name
// (suffixed with the device name like the built-in path), type =
// protocol_family (the routing provider), version = best-effort detected
// version, status = "online", plus the profile_id the server validates.
//
// Returns a content signature of the fetched profile list (MUL-3332). The
// signature is used by on-demand profile refreshes to ignore duplicate change
// notifications while still triggering a re-register without a daemon
// restart. Returns the empty string when the fetch failed — callers must treat
// that as "unknown, do not overwrite a previously-stored signature" (otherwise
// a transient 5xx would silently flip the daemon into thinking the workspace
// has zero profiles).
func (d *Daemon) appendProfileRuntimes(ctx context.Context, workspaceID string, runtimes *[]map[string]string, failedProfiles *[]map[string]string) string {
	resp, err := d.client.GetRuntimeProfiles(ctx, workspaceID)
	if err != nil {
		// Best-effort: never fail registration because profiles couldn't be
		// fetched. An older server with no profiles route returns 404.
		d.logger.Info("skip custom runtime profiles: fetch failed (continuing with built-in runtimes)",
			"workspace_id", workspaceID, "error", err)
		return ""
	}
	if resp == nil {
		// Empty payload — same shape as "server has zero profiles". Return
		// the digest of an empty list so the sync loop can still detect a
		// later transition (zero → first profile added).
		return profileSetSignature(nil)
	}
	for _, profile := range resp.RuntimeProfiles {
		if profile.CommandName == "" || profile.ProtocolFamily == "" {
			d.logger.Warn("skip custom runtime profile: missing command_name or protocol_family",
				"workspace_id", workspaceID, "profile_id", profile.ID, "display_name", profile.DisplayName)
			continue
		}
		if !agent.IsSupportedType(profile.ProtocolFamily) {
			reason := "unsupported protocol_family: " + profile.ProtocolFamily
			d.logger.Warn("skip custom runtime profile: unsupported protocol_family",
				"workspace_id", workspaceID, "profile_id", profile.ID,
				"display_name", profile.DisplayName, "protocol_family", profile.ProtocolFamily)
			*failedProfiles = append(*failedProfiles, map[string]string{
				"profile_id":   profile.ID,
				"command_name": profile.CommandName,
				"reason":       reason,
			})
			continue
		}
		// Resolve the executable to launch for this profile. A per-machine
		// path override (MUL-3284, `multica runtime profile set-path`) wins
		// over the PATH lookup when it is set AND points at a real
		// executable — this is how an operator pins a profile to a binary
		// that isn't on the daemon's PATH, or selects between multiple
		// installs on the same host. A configured-but-unusable override
		// (deleted/moved/non-executable) is logged and falls back to PATH
		// rather than registering a runtime that can't launch. When neither
		// the override nor PATH resolves, the profile is skipped (existing
		// behavior).
		var resolved string
		var failureReason string
		if override := strings.TrimSpace(d.cfg.ProfileCommandOverrides[profile.ID]); override != "" {
			if profilePathExecutable(override) {
				resolved = override
				d.logger.Info("custom runtime profile: using per-machine command path override",
					"workspace_id", workspaceID, "profile_id", profile.ID, "command_path", resolved)
			} else {
				failureReason = "Configured path override is not executable: " + override
				d.logger.Warn("custom runtime profile: command path override not executable; falling back to PATH",
					"workspace_id", workspaceID, "profile_id", profile.ID,
					"override_path", override, "command_name", profile.CommandName)
			}
		}
		if resolved == "" {
			r, err := lookPath(profile.CommandName)
			if err != nil {
				// Host doesn't have this command — expected on hosts that aren't
				// provisioned for this profile. Skip without failing.
				d.logger.Info("skip custom runtime profile: command not found on PATH",
					"workspace_id", workspaceID, "profile_id", profile.ID,
					"command_name", profile.CommandName, "error", err)
				if failureReason != "" {
					failureReason += "; "
				}
				failureReason += "command not found on PATH: " + profile.CommandName
				*failedProfiles = append(*failedProfiles, map[string]string{
					"profile_id":   profile.ID,
					"command_name": profile.CommandName,
					"reason":       failureReason,
				})
				continue
			}
			resolved = r
		}
		// Best-effort version detection; an empty version is acceptable.
		version, verErr := detectAgentVersion(ctx, resolved)
		if verErr != nil {
			d.logger.Debug("custom runtime profile: version probe failed (registering with empty version)",
				"workspace_id", workspaceID, "profile_id", profile.ID, "path", resolved, "error", verErr)
			version = ""
		}
		displayName := profile.DisplayName
		if d.cfg.DeviceName != "" {
			displayName = fmt.Sprintf("%s (%s)", displayName, d.cfg.DeviceName)
		}
		d.recordProfileLaunch(profile.ID, resolved, version, profile.FixedArgs)
		d.logger.Info("registering custom runtime profile",
			"workspace_id", workspaceID, "profile_id", profile.ID,
			"protocol_family", profile.ProtocolFamily, "command_path", resolved)
		*runtimes = append(*runtimes, map[string]string{
			"name":       displayName,
			"type":       profile.ProtocolFamily,
			"version":    version,
			"status":     "online",
			"profile_id": profile.ID,
		})
	}
	return profileSetSignature(resp.RuntimeProfiles)
}

// profileSetSignature is a stable content hash of the workspace's custom
// runtime profile list (MUL-3332). An on-demand refresh diffs this against the
// cached value after the server reports a create, edit, disable, or delete; a
// mismatch makes the daemon re-register so the new runtime instance appears
// without a restart.
//
// The hashed projection covers exactly the fields that affect what the
// daemon sends in a Register call: ID, Enabled, ProtocolFamily, CommandName,
// FixedArgs (the launch args every agent on this runtime inherits) and
// Visibility (so a hypothetical future per-creator filter still triggers
// drift). Profiles are sorted by ID first so the digest is order-independent
// (the server is allowed to return them in any order).
func profileSetSignature(profiles []RuntimeProfile) string {
	if len(profiles) == 0 {
		return "0"
	}
	sorted := append([]RuntimeProfile(nil), profiles...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	h := fnv.New64a()
	// Field separator chosen to never appear in a UUID, slug, or arg.
	const sep = "\x1f"
	for _, p := range sorted {
		fmt.Fprintf(h, "%s%s%t%s%s%s%s%s%s%s",
			p.ID, sep,
			p.Enabled, sep,
			p.ProtocolFamily, sep,
			p.CommandName, sep,
			p.Visibility, sep,
		)
		for _, a := range p.FixedArgs {
			fmt.Fprintf(h, "%s%s", a, sep)
		}
		// Record list end so [a,b] and [ab] hash differently.
		h.Write([]byte("\x1e"))
	}
	return strconv.FormatUint(h.Sum64(), 16)
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
func (d *Daemon) registerTaskRepos(workspaceID, taskID string, repos []RepoData) {
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
	if taskID != "" && ws.taskRepoRefs == nil {
		ws.taskRepoRefs = make(map[string]map[string]string)
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
		if taskID != "" {
			if ws.taskRepoRefs[taskID] == nil {
				ws.taskRepoRefs[taskID] = make(map[string]string, len(repos))
			}
			if _, exists := ws.taskRepoRefs[taskID][url]; !exists {
				ws.taskRepoRefs[taskID][url] = strings.TrimSpace(repo.Ref)
			}
		}
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

func (d *Daemon) taskRepoDefaultRef(workspaceID, taskID, repoURL string) string {
	taskID = strings.TrimSpace(taskID)
	repoURL = strings.TrimSpace(repoURL)
	if taskID == "" || repoURL == "" {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok || ws.taskRepoRefs == nil {
		return ""
	}
	return strings.TrimSpace(ws.taskRepoRefs[taskID][repoURL])
}

func (d *Daemon) clearTaskRepoRefs(workspaceID, taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ws, ok := d.workspaces[workspaceID]; ok && ws.taskRepoRefs != nil {
		delete(ws.taskRepoRefs, taskID)
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

// refreshWorkspaceRuntimeProfiles fetches the workspace's enabled custom
// runtime profile list (MUL-3332), compares its content signature against
// the value cached on the workspaceState, and triggers a re-register when
// the signature has drifted. This is the entry point used by the daemon
// WebSocket change notification so profiles added / edited / disabled via the
// web UI or CLI become visible without a daemon restart.
//
// Best-effort: a fetch error (older server, network blip) preserves the cached
// signature. A successfully-fetched-but-unchanged signature can result from a
// duplicate notification and short-circuits without any further work.
//
// On drift the function takes a path that deliberately differs from
// reregisterWorkspaceAfterRuntimeGone in two ways:
//
//  1. It does NOT call RecoverOrphans for the returned runtime IDs. The
//     server's RecoverOrphanedTasksForRuntime hard-fails every
//     dispatched/running/waiting_local_directory task on a runtime, which is
//     the correct response when a runtime row was actually deleted server-
//     side, but a catastrophic false positive on profile drift: a built-in
//     runtime still actively executing tasks would have its work killed
//     just because the user added a sibling custom profile.
//
//  2. It tolerates ErrNoRuntimesToRegister (custom-only daemon disables its
//     only profile) by Deregistering the now-stale local runtime IDs and
//     clearing local tracking. Without this, registerRuntimesForWorkspace
//     would short-circuit on the empty list, the daemon would keep polling
//     and heartbeating runtimes that should be offline, and the server
//     would leave them online for the full 150 s stale-heartbeat window.
//
// The workspaceState pointer is never replaced (matches the invariant
// documented on syncWorkspacesFromAPI and reregisterWorkspaceAfterRuntimeGone).
func (d *Daemon) refreshWorkspaceRuntimeProfiles(ctx context.Context, workspaceID string) error {
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := d.client.GetRuntimeProfiles(refreshCtx, workspaceID)
	if err != nil {
		// Older server (no profiles route) returns 404; let the on-demand caller
		// log the failure at debug level.
		return err
	}
	var profiles []RuntimeProfile
	if resp != nil {
		profiles = resp.RuntimeProfiles
	}
	live := profileSetSignature(profiles)

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()
		// Workspace was removed while the refresh was in flight — nothing to do.
		return nil
	}
	cached := ws.profileSetSig
	d.mu.Unlock()

	if cached == live {
		return nil
	}

	d.logger.Info("custom runtime profile set changed; refreshing workspace runtimes",
		"workspace_id", workspaceID, "previous_sig", cached, "current_sig", live,
		"profile_count", len(profiles))

	regResp, profileSig, err := d.registerRuntimesForWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, ErrNoRuntimesToRegister) {
			// Convergence-to-zero: a custom-only daemon's only enabled
			// profile was just disabled / deleted, and there are no built-in
			// agents to fall back on. Drop the daemon's local tracking and
			// proactively Deregister the orphaned server-side rows so the
			// runtime list converges to empty without waiting on the 150 s
			// stale-heartbeat sweep.
			return d.convergeWorkspaceRuntimesToZero(ctx, workspaceID, profileSig)
		}
		return err
	}

	newIDs, droppedIDs, ok := d.applyRegisterResponseInPlace(workspaceID, regResp, profileSig)
	if !ok {
		return fmt.Errorf("workspace %s no longer tracked", workspaceID)
	}

	for _, rid := range newIDs {
		d.logger.Info("re-registered runtime after profile drift",
			"workspace_id", workspaceID, "runtime_id", rid)
	}
	d.notifyRuntimeSetChanged()

	// Drift may have shrunk the runtime set (a profile got disabled while
	// other runtimes survive). Eagerly mark those server-side rows offline
	// so the runtime list reflects reality immediately; a 5xx blip here is
	// fine because the server's stale-heartbeat sweep will pick them up
	// within ~150 s as a backstop.
	if len(droppedIDs) > 0 {
		if err := d.client.Deregister(ctx, droppedIDs); err != nil {
			d.logger.Warn("deregister of dropped runtimes after profile drift failed",
				"workspace_id", workspaceID, "runtime_ids", droppedIDs, "error", err)
		}
	}

	// Intentionally NO RecoverOrphans here: see method doc.
	return nil
}

// convergeWorkspaceRuntimesToZero handles the drift-refresh case where
// registerRuntimesForWorkspace would have short-circuited because the daemon
// has nothing to host on this workspace anymore. It Deregisters the
// previously-tracked runtime IDs (best-effort) and clears the daemon's local
// tracking so taskWakeup / heartbeat / poll loops stop attempting work
// against runtimes that should now be offline.
//
// The workspaceState pointer is preserved: the workspace itself is still a
// valid workspace the user belongs to, just one with no agents on this
// daemon for the moment. If the user re-enables a profile or installs a
// built-in agent, the profile-change notification or the next daemon WS
// reconnect will register it again.
func (d *Daemon) convergeWorkspaceRuntimesToZero(ctx context.Context, workspaceID, profileSig string) error {
	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()
		return nil
	}
	oldRuntimeIDs := append([]string(nil), ws.runtimeIDs...)
	for _, rid := range oldRuntimeIDs {
		delete(d.runtimeIndex, rid)
	}
	ws.runtimeIDs = nil
	if profileSig != "" {
		// Cache the converged-empty signature so we don't loop into
		// re-converging on every subsequent sync tick.
		ws.profileSetSig = profileSig
	}
	d.mu.Unlock()

	d.logger.Info("custom runtime profile drift converged to zero; clearing local tracking",
		"workspace_id", workspaceID, "deregistered_runtime_ids", oldRuntimeIDs)

	if len(oldRuntimeIDs) > 0 {
		if err := d.client.Deregister(ctx, oldRuntimeIDs); err != nil {
			// Best-effort: the server's stale-heartbeat sweep marks the rows
			// offline within ~150 s as a backstop, and on the daemon side
			// we have already stopped heartbeating them.
			d.logger.Warn("deregister after zero-runtime convergence failed",
				"workspace_id", workspaceID, "runtime_ids", oldRuntimeIDs, "error", err)
		}
	}
	d.notifyRuntimeSetChanged()
	return nil
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
	//     handler reads workspaceCoAuthoredByEnabled right after this. The
	//     periodic workspace sync deliberately does not refresh repos or
	//     settings, so this is the point at which a freshly-flipped GitHub
	//     master switch / `co_authored_by_enabled` toggle becomes live.
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
	return d.syncWorkspacesFromAPI(ctx, false)
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

// workspaceSyncLoop reconciles the user's workspace membership set. Daemons
// with runtimes and account-scoped WS support use a thirty-minute jittered
// consistency check; daemons talking to older servers retain a five-minute
// fallback. Bootstrap daemons without runtimes keep the shorter interval needed
// to discover their first workspace. Account-scoped WS hints trigger an
// immediate minimal sync, while a WS reconnect also reconciles runtime profiles
// changed during the connection gap.
func (d *Daemon) workspaceSyncLoop(ctx context.Context) {
	timer := time.NewTimer(jitterDuration(d.workspaceSyncBaseInterval()))
	defer timer.Stop()

	var reconcileCh <-chan struct{}
	if d.reconcile != nil {
		reconcileCh = d.reconcile.notify()
	}
	var workspaceChangesCh <-chan struct{}
	if d.workspaceChanges != nil {
		workspaceChangesCh = d.workspaceChanges.notify()
	}

	var consecutiveFailures int
	resetTimer := func() {
		interval := workspaceSyncBackoff(d.workspaceSyncBaseInterval(), consecutiveFailures)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(jitterDuration(interval))
	}

	syncNow := func(reconcileProfiles bool) {
		if err := d.syncWorkspacesFromAPI(ctx, reconcileProfiles); err != nil {
			consecutiveFailures++
			d.logger.Debug("workspace sync failed", "error", err)
		} else {
			consecutiveFailures = 0
		}
		resetTimer()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileCh:
			if d.reconcile != nil {
				reconcileCh = d.reconcile.notify()
			}
			syncNow(true)
		case <-workspaceChangesCh:
			syncNow(false)
		case <-timer.C:
			syncNow(false)
		}
	}
}

func (d *Daemon) workspaceSyncBaseInterval() time.Duration {
	if len(d.allRuntimeIDs()) == 0 {
		return DefaultWorkspaceBootstrapSyncInterval
	}
	if d.client.usesLegacyWorkspaceEndpoint() {
		return DefaultWorkspaceLegacySyncInterval
	}
	return DefaultWorkspaceSyncInterval
}

func workspaceSyncBackoff(base time.Duration, consecutiveFailures int) time.Duration {
	maxInterval := DefaultWorkspaceSyncMaxBackoff
	if base == DefaultWorkspaceBootstrapSyncInterval {
		maxInterval = DefaultWorkspaceLegacySyncInterval
	}
	interval := base
	for i := 0; i < consecutiveFailures; i++ {
		if interval >= maxInterval/2 {
			return maxInterval
		}
		interval *= 2
	}
	if interval > maxInterval {
		return maxInterval
	}
	return interval
}

// syncWorkspacesFromAPI fetches all workspaces the user belongs to and
// registers runtimes for any that aren't already tracked. When
// reconcileProfiles is true (after a daemon WS connect/reconnect), tracked
// workspaces reconcile custom runtime profiles once so a change made while the
// WS was unavailable is not lost. Normal timers and workspace-change hints
// pass false and make no runtime-profile requests. Workspaces the user has
// left are cleaned up.
func (d *Daemon) syncWorkspacesFromAPI(ctx context.Context, reconcileProfiles bool) error {
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
			if reconcileProfiles {
				if err := d.refreshWorkspaceRuntimeProfiles(ctx, id); err != nil {
					d.logger.Debug("workspace reconcile: profile refresh failed", "workspace_id", id, "error", err)
				}
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
		resp, profileSig, err := d.registerRuntimesForWorkspace(ctx, id)
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
		ws := newWorkspaceState(id, runtimeIDs, resp.ReposVersion, resp.Repos, resp.Settings)
		// Seed the profile signature so later on-demand change notifications can
		// detect drift without re-registering on duplicates (empty sig is the
		// explicit "unknown — keep the previous value" sentinel from
		// appendProfileRuntimes; on first registration there is no previous
		// value, so empty stays empty).
		ws.profileSetSig = profileSig
		d.workspaces[id] = ws
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

	consecutiveTransientFailures := 0
	tick := func() {
		if d.runHeartbeatTick(ctx, rid) {
			consecutiveTransientFailures++
			if consecutiveTransientFailures == 2 {
				d.client.CloseIdleConnections()
			}
			return
		}
		consecutiveTransientFailures = 0
	}

	tick()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

// runHeartbeatTick returns true when the HTTP heartbeat hit a transient
// failure that should count toward stale idle-connection cleanup.
func (d *Daemon) runHeartbeatTick(ctx context.Context, rid string) bool {
	// Skip HTTP heartbeat for runtimes that successfully acked a recent
	// WebSocket heartbeat. The WS path keeps last_seen_at fresh and delivers
	// actions, so the HTTP write would be a duplicate DB update. If the WS
	// heartbeat goes silent the freshness window expires and HTTP resumes
	// automatically on the next tick — that is the fallback the WS path
	// relies on.
	if d.wsHeartbeatRecentlyAcked(rid) {
		d.logger.Debug("heartbeat: skipping HTTP tick, WS recently acked", "runtime_id", rid)
		return false
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
				return false
			}
			d.logger.Warn("heartbeat failed", "runtime_id", rid, "error", err)
		}
		return ctx.Err() == nil && isTransientError(err)
	}
	if resp != nil && resp.RuntimeGone {
		// The WS path returns a successful ack with RuntimeGone=true for the
		// same scenario; treat it the same way here in case HTTP starts
		// surfacing this signal too.
		go d.handleRuntimeGone(rid)
		return false
	}
	d.handleHeartbeatActions(ctx, rid, resp)
	return false
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
	// Self-heal a pinned executable path an in-place upgrade deleted (MUL-4486).
	entry, _ = d.resolveAgentEntry(ctx, rt.Provider, entry)

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
	mcpServers, mcpSupported, err := listRuntimeLocalMcpServers(rt.Provider)
	if err != nil {
		d.logger.Warn("runtime local MCP discovery failed", "runtime_id", rt.ID, "provider", rt.Provider, "error", err)
		mcpServers = []runtimeLocalMcpServerSummary{}
		mcpSupported = false
	}

	d.reportLocalSkillListResult(ctx, rt, requestID, map[string]any{
		"status":        "completed",
		"skills":        skills,
		"supported":     supported,
		"mcp_servers":   mcpServers,
		"mcp_supported": mcpSupported,
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
	newBin, err := resolveSelfExecutable()
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

// pollLoop runs the machine-level batch claim poller (MUL-4257): a single
// goroutine claims across ALL of the daemon's runtimes per cycle via
// ClaimTasksWSFirst (WS-first, HTTP fallback), replacing the previous
// one-HTTP-poller-per-runtime model. Wake-up signals — a WS task_available /
// catch-up nudge or a runtime-set change — all collapse to one nudge because a
// batch claim already covers every runtime. On shutdown it stops the poller,
// then drains in-flight tasks.
//
// This trades the per-runtime isolation the old model gave (MUL-1744) for a
// single request; the head-of-line risk is bounded by ClaimTasksWSFirst's short
// per-request timeout (WS) / the client's timeout (HTTP fallback), and the
// server-side batch claim is index-backed + short.
func (d *Daemon) pollLoop(ctx context.Context, taskWakeups <-chan taskWakeup) error {
	sem := newTaskSlotSemaphore(d.cfg.MaxConcurrentTasks)
	var taskWG sync.WaitGroup // tracks in-flight handleTask goroutines

	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	wakeup := make(chan struct{}, 1)
	nudge := func() {
		signalPollerWakeup(wakeup)
	}

	pollerCtx, pollerCancel := context.WithCancel(ctx)
	pollerDone := make(chan struct{})
	go func() {
		defer close(pollerDone)
		d.runBatchPoller(pollerCtx, ctx, sem, wakeup, &taskWG)
	}()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("poll loop stopping, waiting for in-flight tasks", "max_wait", "30s")
			pollerCancel()
			// Wait for the poller to fully return before waiting on taskWG so a
			// poller between ClaimTasksWSFirst and taskWG.Add(1) cannot race
			// taskWG.Wait at a zero counter.
			<-pollerDone
			waitDone := make(chan struct{})
			go func() { taskWG.Wait(); close(waitDone) }()
			select {
			case <-waitDone:
			case <-time.After(30 * time.Second):
				d.logger.Warn("timed out waiting for in-flight tasks")
			}
			return ctx.Err()
		case <-runtimeSetCh:
			// The batch poller re-derives allRuntimeIDs() each cycle; nudge it to
			// pick up a registered/removed runtime promptly.
			nudge()
		case <-taskWakeups:
			// Targeted-runtime and catch-up wakeups both trigger one batch claim
			// across the whole runtime set.
			nudge()
		}
	}
}

// runBatchPoller is the single machine-level claim+dispatch loop. Each cycle it
// acquires whatever execution slots are free (slot-before-claim, so a claimed
// task never sits server-side `dispatched` without local capacity to run it and
// race the dispatch-timeout sweeper), asks the server for up to that many tasks
// across all of the daemon's runtimes in one call, and dispatches each returned
// task — routed to its runtime by handleTask — into a slot.
//
// pollerCtx is cancelled on shutdown. parentCtx is the daemon root ctx passed to
// handleTask so an in-flight task is not killed just because the poll loop is
// stopping mid-flight.
func (d *Daemon) runBatchPoller(pollerCtx, parentCtx context.Context, sem chan int, wakeup chan struct{}, taskWG *sync.WaitGroup) {
	releaseSlots := func(slots []int) {
		for _, sl := range slots {
			sem <- sl
		}
	}

	for {
		if pollerCtx.Err() != nil {
			return
		}

		runtimeIDs := d.allRuntimeIDs()
		if len(runtimeIDs) == 0 {
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		// Acquire at least one slot (blocking briefly), then grab any other free
		// slots so a single batch claim can fill them all.
		slot, acquired, woke, err := waitForTaskSlot(pollerCtx, sem, wakeup, taskSlotWaitTimeout)
		if err != nil {
			return
		}
		if !acquired {
			if woke {
				continue
			}
			if err := sleepWithContextOrWakeup(pollerCtx, capacityBackoff(d.cfg.PollInterval), wakeup); err != nil {
				return
			}
			continue
		}
		slots := append([]int{slot}, drainAvailableSlots(sem, d.cfg.MaxConcurrentTasks-1)...)

		// Auto-update barrier: refuse to claim while an update prepares to roll
		// the process (paired with the re-check in tryAutoUpdate).
		if !d.tryEnterClaim() {
			releaseSlots(slots)
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		tasks, err := d.ClaimTasksWSFirst(pollerCtx, d.cfg.DaemonID, runtimeIDs, len(slots))
		if err != nil {
			d.exitClaim()
			releaseSlots(slots)
			if pollerCtx.Err() == nil {
				d.logger.Warn("batch claim failed", "error", err)
			}
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		// Dispatch each claimed task into a slot. activeTasks is incremented for
		// every dispatched task BEFORE exitClaim so the auto-update barrier never
		// sees a zero-claims / zero-active window between claim and dispatch.
		dispatched := 0
		for i := range tasks {
			if i >= len(slots) || tasks[i] == nil {
				break
			}
			t := *tasks[i]
			slot := slots[i]
			taskTarget := t.IssueID
			if taskTarget == "" && t.ChatSessionID != "" {
				taskTarget = "chat:" + shortID(t.ChatSessionID)
			}
			d.logger.Info("task received", "task", shortID(t.ID), "target", taskTarget)
			taskWG.Add(1)
			d.activeTasks.Add(1)
			go func(t Task, slot int) {
				defer taskWG.Done()
				defer d.activeTasks.Add(-1)
				defer func() {
					// Release local capacity before waking the poller. The task's
					// terminal callback and local cleanup have both finished at this
					// point, so a successor that was previously blocked by agent
					// capacity or per-(issue, agent) serialization can be claimed
					// immediately instead of waiting for PollInterval.
					sem <- slot
					signalPollerWakeup(wakeup)
				}()
				d.handleTask(parentCtx, t, slot)
			}(t, slot)
			dispatched++
		}
		d.exitClaim()
		if dispatched < len(slots) {
			releaseSlots(slots[dispatched:])
		}

		// If we filled every slot, more work may be queued — loop immediately.
		// Otherwise wait for the next wakeup / poll interval.
		if dispatched > 0 && dispatched == len(slots) {
			continue
		}
		if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
			return
		}
	}
}

func signalPollerWakeup(wakeup chan<- struct{}) {
	select {
	case wakeup <- struct{}{}:
	default:
	}
}

// drainAvailableSlots non-blockingly pulls up to max additional slots from the
// semaphore, returning immediately when none are free.
func drainAvailableSlots(sem chan int, max int) []int {
	if max <= 0 {
		return nil
	}
	var slots []int
	for len(slots) < max {
		select {
		case s := <-sem:
			slots = append(slots, s)
		default:
			return slots
		}
	}
	return slots
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
	// Subscribe to the reconcile broadcaster before launching the inner
	// goroutine. A WS reconnect that fires between the goroutine starting
	// and its first notify() call would otherwise be dropped; the ticker
	// still bounds the worst case, but the whole point of the broadcast is
	// to avoid waiting on that ticker.
	var reconcileCh <-chan struct{}
	if d.reconcile != nil {
		reconcileCh = d.reconcile.notify()
	}
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		check := func() bool {
			status, err := d.client.GetTaskStatus(ctx, taskID)
			if !shouldInterruptAgent(status, err) {
				return false
			}
			if err != nil {
				taskLog.Info("task gone server-side, interrupting agent", "error", err)
			} else {
				taskLog.Info("task reached terminal state server-side, interrupting agent", "status", status)
			}
			close(cancelled)
			return true
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-reconcileCh:
				// Refresh the subscription before issuing the request so a
				// second broadcast that overlaps GetTaskStatus is not lost.
				if d.reconcile != nil {
					reconcileCh = d.reconcile.notify()
				}
				if check() {
					return
				}
			case <-ticker.C:
				if check() {
					return
				}
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
	// is pinned to this daemon, acquire the path mutex before runner.run
	// so the server-side state machine is dispatched →
	// waiting_local_directory → running rather than backwards-transitioning
	// from running into the wait state. The release is deferred so a panic
	// or early return always frees the lock for the next waiter.
	//
	// StartTask itself now lives in runTask (see issue #3999 race A) and
	// fires only after execenv.Prepare/Reuse has put env.WorkDir on disk,
	// so consumers that read status==running can resolve the workdir path
	// without racing the daemon's os.MkdirAll.
	localRelease, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, task, taskLog)
	if abort {
		return
	}
	if localRelease != nil {
		defer localRelease()
	}

	// Hold a process-wide active-root guard for the rest of this task so
	// the GC loop never sees a window where the env root has neither the
	// in-process guard nor .gc_meta.json (issue #3999 race B). runTask
	// installs its own ref-counted mark/unmark internally; without this
	// outer guard the inner unmark fires when runTask returns, leaving
	// the directory protected only by the 72h orphan TTL through
	// reportTaskResult and execenv.WriteGCMeta below. markActiveEnvRoot
	// is reference-counted, so the duplicate marks runTask installs are
	// correctly nested within these.
	predictedEnvRoot := execenv.PredictRootDir(d.cfg.WorkspacesRoot, task.WorkspaceID, task.ID)
	if predictedEnvRoot != "" {
		d.markActiveEnvRoot(predictedEnvRoot)
		defer d.unmarkActiveEnvRoot(predictedEnvRoot)
	}
	if task.PriorWorkDir != "" {
		if priorRoot := filepath.Dir(task.PriorWorkDir); priorRoot != "" && priorRoot != predictedEnvRoot {
			d.markActiveEnvRoot(priorRoot)
			defer d.unmarkActiveEnvRoot(priorRoot)
		}
	}

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
		// runner.run has returned, so the transcript flush is complete —
		// tell the server it can settle its deferred chat finalization
		// (#5219). Best-effort: the sweeper grace period covers a lost ack.
		if ackErr := d.client.AckTaskCancelled(ctx, task.ID); ackErr != nil {
			taskLog.Warn("cancel ack failed; server sweeper will finalize", "error", ackErr)
		}
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
		// Same contract as the poll-cancelled path above: the transcript is
		// flushed, so let the server settle its deferred chat finalization.
		if ackErr := d.client.AckTaskCancelled(ctx, task.ID); ackErr != nil {
			taskLog.Warn("cancel ack failed; server sweeper will finalize", "error", ackErr)
		}
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
			if assignment, _ := localDirectoryAssignmentForTask(task, d.cfg.DaemonID); assignment != nil {
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
	assignment, err := localDirectoryAssignmentForTask(task, d.cfg.DaemonID)
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
	// per-task watcher only starts after the lock is acquired. If the user
	// cancels the issue or it gets reassigned during the wait, we need to
	// notice promptly so the daemon slot isn't pinned by a phantom waiter.
	// We spin up the cancellation watcher lazily inside onWait so the
	// no-contention fast path still costs nothing.
	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	pollInterval := d.cancelPollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	var (
		watcherOnce      sync.Once
		prepareLeaseOnce sync.Once
		cancelledByPoll  <-chan struct{}
		stopPrepareLease func()
	)
	defer func() {
		if stopPrepareLease != nil {
			stopPrepareLease()
		}
	}()

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
		prepareLeaseOnce.Do(func() {
			stopPrepareLease = d.startTaskPrepareLeaseExtender(waitCtx, task, taskLog)
		})
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

// runtimeDisplayNameOverrides maps a provider key to the human-facing runtime
// name when simple title-casing would read awkwardly. Providers not listed
// here fall back to capitalizing the key (claude → "Claude", codex → "Codex").
var runtimeDisplayNameOverrides = map[string]string{
	"traecli": "Trae",
	"grok":    "Grok",
}

// providerDisplayName returns the human-facing runtime name for a provider key.
func providerDisplayName(name string) string {
	if name == "" {
		return name
	}
	if friendly, ok := runtimeDisplayNameOverrides[name]; ok {
		return friendly
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func providerNeedsInlineSystemPrompt(provider string) bool {
	switch provider {
	case "openclaw", "kiro", "kimi", "traecli":
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
		// The user expected this run to continue the prior conversation; surface
		// the loss in the brief instead of silently restarting (MUL-4424).
		taskCtx.PriorSessionResumeUnavailable = true
	}
	return reused
}

// shouldReusePriorWorkdir keeps the local_directory lock invariant without
// forcing every squad-leader follow-up onto a fresh provider session. Worker
// tasks already expose their current local-directory assignment, so their
// existing reuse behavior remains unchanged. Leader tasks intentionally skip
// that assignment and its lock; they may therefore reuse only directories
// that resolve to the {workspace}/{task}/workdir shape, carry Prepare-time
// managed-env provenance for the same workspace/issue/agent, and carry a
// matching daemon task-context marker.
//
// Reuse eligibility is deliberately keyed off .managed_env.json (written by
// execenv.Prepare) and NOT .gc_meta.json (written only after the task reaches
// terminal state). The server's task-complete handler reconciles a follow-up
// and wakes the runtime before the prior task's daemon handler writes the GC
// file, so a successor can be claimed inside that window; keying off the
// terminal file raced and dropped the session (MUL-4886). Both proofs this
// function reads — the env-root provenance and the workdir task-context marker
// — are written at Prepare time, so neither depends on completion ordering.
func shouldReusePriorWorkdir(task Task, localAssignment *localDirectoryAssignment, workspacesRoot string) bool {
	if task.PriorWorkDir == "" || localAssignment != nil {
		return false
	}
	if !task.IsLeaderTask {
		return true
	}

	root, err := filepath.EvalSymlinks(workspacesRoot)
	if err != nil {
		return false
	}
	workdir, err := filepath.EvalSymlinks(task.PriorWorkDir)
	if err != nil {
		return false
	}
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		return false
	}
	rel, err := filepath.Rel(root, workdir)
	if err != nil || !filepath.IsLocal(rel) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 || parts[0] != task.WorkspaceID || parts[1] == "" || parts[2] != "workdir" {
		return false
	}
	if task.AgentID == "" || task.IssueID == "" {
		return false
	}
	// Managed-env provenance is written only for non-local managed issue envs,
	// so its presence (plus the workspace/issue/agent match) proves this is a
	// safe daemon-managed reuse target and not a residual local_directory path.
	prov, err := execenv.ReadManagedEnvProvenance(filepath.Dir(workdir))
	if err != nil || prov.ManagedBy != execenv.ManagedEnvProvenanceManagedBy ||
		prov.WorkspaceID != task.WorkspaceID || prov.IssueID != task.IssueID ||
		prov.AgentID != task.AgentID {
		return false
	}

	data, err := os.ReadFile(filepath.Join(workdir, execenv.TaskContextMarkerRelPath))
	if err != nil {
		return false
	}
	var marker struct {
		ManagedBy string `json:"managed_by"`
		AgentID   string `json:"agent_id"`
		IssueID   string `json:"issue_id"`
	}
	if json.Unmarshal(data, &marker) != nil {
		return false
	}
	return marker.ManagedBy == execenv.TaskContextMarkerManagedBy &&
		marker.AgentID == task.AgentID && marker.IssueID == task.IssueID
}

// gateCodexResumeToRolloutPresence drops the prior Codex session when its
// rollout is not actually present in the task's CODEX_HOME sessions. A reused
// workdir keeps PriorSessionID (gateResumeToReusedWorkdir), but Codex session
// isolation (MUL-4424) means the rollout may be missing: a migrated legacy home
// that could not locate it, or a local_directory task whose shared history was
// pruned. Codex would then silently thread/start from scratch, so we clear the
// resume claim from both the backend (PriorSessionID) and the brief
// (PriorSessionResumed) instead of pretending the conversation continues.
// No-op for non-Codex providers or when there is nothing to resume.
func gateCodexResumeToRolloutPresence(task *Task, taskCtx *execenv.TaskContextForEnv, provider, codexHome string, taskLog *slog.Logger) {
	if provider != "codex" || task.PriorSessionID == "" || codexHome == "" {
		return
	}
	if execenv.CodexResumeRolloutPresent(codexHome, task.PriorSessionID) {
		return
	}
	taskLog.Warn("dropping prior codex session: rollout not present in task CODEX_HOME; starting a fresh thread",
		"session_id", task.PriorSessionID, "codex_home", codexHome)
	task.PriorSessionID = ""
	taskCtx.PriorSessionResumed = false
	// The user expected this run to continue the prior conversation; surface the
	// loss in the brief instead of silently restarting (MUL-4424).
	taskCtx.PriorSessionResumeUnavailable = true
}

func (d *Daemon) ensureTaskSkillBundles(ctx context.Context, task *Task) error {
	if task == nil || task.Agent == nil || len(task.Agent.SkillRefs) == 0 {
		return nil
	}
	resolved := make(map[string]SkillData, len(task.Agent.SkillRefs))
	misses := make([]SkillRefData, 0)
	for _, ref := range task.Agent.SkillRefs {
		ref := ref
		var bundle SkillData
		if err := d.skillCache.WithRefLock(task.WorkspaceID, ref, func() error {
			if cached, ok := d.skillCache.Load(task.WorkspaceID, ref); ok {
				bundle = cached
				return nil
			}
			misses = append(misses, ref)
			return nil
		}); err != nil {
			return fmt.Errorf("load skill bundle cache: %w", err)
		}
		if bundle.ID != "" {
			resolved[skillRefKey(ref.Source, ref.ID)] = bundle
		}
	}

	// Resolve each missing bundle in its own request, caching it the moment it
	// arrives. The download is the slow part on jittery links, so fetching the
	// whole set in one atomic body read meant a single timeout discarded all
	// progress and the cache never converged — every dispatch re-downloaded
	// everything and timed out again. Per-skill, each download fits its own
	// size-scaled deadline and is persisted independently, so even a dispatch
	// that ultimately fails leaves the skills it did fetch cached for the next
	// one. (GitHub #4505 / MUL-3650)
	for _, ref := range misses {
		bundle, err := d.resolveSkillBundle(ctx, task, ref)
		if err != nil {
			return fmt.Errorf("resolve skill bundles: %w", err)
		}
		resolved[skillRefKey(bundle.Source, bundle.ID)] = bundle
	}

	skills := make([]SkillData, 0, len(task.Agent.SkillRefs))
	for _, ref := range task.Agent.SkillRefs {
		bundle, ok := resolved[skillRefKey(ref.Source, ref.ID)]
		if !ok {
			return fmt.Errorf("skill bundle missing after resolve: skill_id=%s source=%s hash=%s", ref.ID, ref.Source, ref.Hash)
		}
		skills = append(skills, bundle)
	}
	task.Agent.Skills = skills
	return nil
}

// resolveSkillBundle downloads one skill bundle and writes it to the on-disk
// cache before returning. The request runs under its own deadline, scaled to
// the bundle's declared size rather than the daemon's fixed 30s control-plane
// timeout, so a large bundle on a slow link is given room to finish instead of
// being cut off mid-body. Caching on success is what lets the resolve converge
// across dispatches. (GitHub #4505 / MUL-3650)
func (d *Daemon) resolveSkillBundle(ctx context.Context, task *Task, ref SkillRefData) (SkillData, error) {
	reqCtx, cancel := context.WithTimeout(ctx, skillBundleResolveTimeout(ref.SizeBytes))
	defer cancel()

	bundle, err := d.client.ResolveSkillBundle(reqCtx, task.RuntimeID, task.ID, ref)
	if err != nil {
		return SkillData{}, err
	}
	// The resolve endpoint serves the agent's *current* bundle and hash, which
	// may differ from the claim-time ref when the skill was edited between
	// claim and prepare (see ResolveTaskSkillBundles). So confirm only that the
	// server returned the skill we asked for (source/id), then validate the
	// bundle for self-consistency against a ref derived from itself — pinning
	// it to the possibly-stale requested hash would reject a legitimate update.
	if bundle.Source != ref.Source || bundle.ID != ref.ID {
		return SkillData{}, fmt.Errorf("resolve skill bundle returned wrong skill: requested source=%s id=%s, got source=%s id=%s", ref.Source, ref.ID, bundle.Source, bundle.ID)
	}
	bundleRef := skillRefFromBundle(bundle)
	if !validateSkillBundle(bundleRef, bundle) {
		return SkillData{}, fmt.Errorf("resolve skill bundle returned invalid bundle: skill_id=%s source=%s hash=%s", bundle.ID, bundle.Source, bundle.Hash)
	}
	if err := d.skillCache.WithRefLock(task.WorkspaceID, bundleRef, func() error {
		return d.skillCache.Store(task.WorkspaceID, bundle)
	}); err != nil {
		return SkillData{}, fmt.Errorf("store skill bundle cache: %w", err)
	}
	return bundle, nil
}

const (
	// skillBundleResolveMinTimeout floors the per-skill resolve deadline so a
	// tiny bundle still tolerates connection setup and round-trip latency.
	skillBundleResolveMinTimeout = 30 * time.Second
	// skillBundleResolveMaxTimeout caps it so a wedged download cannot pin a
	// task in prepare indefinitely.
	skillBundleResolveMaxTimeout = 5 * time.Minute
	// skillBundleResolveMinThroughput is the pessimistic floor throughput
	// (bytes/sec) used to scale the deadline to bundle size — deliberately low
	// to cover slow, jittery links rather than ideal bandwidth.
	skillBundleResolveMinThroughput = 50 * 1024
)

// skillBundleResolveTimeout returns the deadline budget for downloading a
// bundle of the given size: at least skillBundleResolveMinTimeout, scaled up at
// skillBundleResolveMinThroughput, and capped at skillBundleResolveMaxTimeout.
func skillBundleResolveTimeout(sizeBytes int64) time.Duration {
	if sizeBytes <= 0 {
		return skillBundleResolveMinTimeout
	}
	scaled := time.Duration(sizeBytes/skillBundleResolveMinThroughput) * time.Second
	if scaled < skillBundleResolveMinTimeout {
		return skillBundleResolveMinTimeout
	}
	if scaled > skillBundleResolveMaxTimeout {
		return skillBundleResolveMaxTimeout
	}
	return scaled
}

func (d *Daemon) startTaskPrepareLeaseExtender(ctx context.Context, task Task, taskLog *slog.Logger) func() {
	leaseCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(taskPrepareLeaseRefresh)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				reqCtx, reqCancel := context.WithTimeout(leaseCtx, taskPrepareLeaseTimeout)
				err := d.client.ExtendTaskPrepareLease(reqCtx, task.RuntimeID, task.ID)
				reqCancel()
				if err != nil {
					taskLog.Warn("extend task prepare lease failed", "error", err)
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func skillRefKey(source, id string) string {
	return source + "\x00" + id
}

func skillRefFromBundle(bundle SkillData) SkillRefData {
	files := make([]skillbundle.File, 0, len(bundle.Files))
	for _, file := range bundle.Files {
		files = append(files, skillbundle.File{Path: file.Path, Content: file.Content})
	}
	manifest := skillbundle.BuildManifest(skillbundle.Skill{
		ID:          bundle.ID,
		Source:      bundle.Source,
		Name:        bundle.Name,
		Description: bundle.Description,
		Content:     bundle.Content,
		Files:       files,
	})
	fileRefs := make([]SkillFileRefData, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		fileRefs = append(fileRefs, SkillFileRefData{Path: file.Path, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
	}
	return SkillRefData{
		ID:        bundle.ID,
		Source:    bundle.Source,
		Hash:      manifest.Hash,
		SizeBytes: manifest.SizeBytes,
		FileCount: manifest.FileCount,
		Files:     fileRefs,
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
	d.registerTaskRepos(task.WorkspaceID, task.ID, task.Repos)
	defer d.clearTaskRepoRefs(task.WorkspaceID, task.ID)

	entry, ok := d.cfg.Agents[provider]
	// A custom runtime profile (MUL-3284) overrides the executable path: the
	// runtime's protocol_family is the provider (so agent.New still selects
	// the right backend), but the actual binary on PATH is the profile's
	// command_name, resolved at registration time and keyed by RuntimeID here.
	// Critically, a custom runtime can live on a host that has NO built-in
	// agent of the same provider installed, so when the runtime is custom we
	// synthesize an AgentEntry instead of hard-failing on the !ok lookup.
	var profileFixedArgs []string
	// resolvedVersion is the CLI version of the built-in binary entry.Path
	// resolves to, paired with the path by resolveAgentEntry so a just-upgraded
	// codex is never launched under the previous version's policy (MUL-4486).
	var resolvedVersion string
	if customSpec, isCustom := d.customProfileLaunchForRuntime(task.RuntimeID); isCustom {
		entry.Path = customSpec.path
		resolvedVersion = customSpec.version
		profileFixedArgs = customSpec.fixedArgs
		ok = true
		d.logger.Info("task uses custom runtime profile command",
			"task_id", task.ID, "runtime_id", task.RuntimeID,
			"provider", provider, "command_path", customSpec.path,
			"fixed_args", len(profileFixedArgs))
	} else if ok {
		// Built-in provider: self-heal a pinned executable path that an in-place
		// upgrade deleted (MUL-4486). Only reached when no custom profile owns
		// the launch, so a custom runtime's path is never second-guessed and a
		// custom-only host pays no wasted re-resolution.
		entry, resolvedVersion = d.resolveAgentEntry(ctx, provider, entry)
	}
	if !ok {
		return TaskResult{}, fmt.Errorf("no agent configured for provider %q", provider)
	}

	stopPrepareLease := d.startTaskPrepareLeaseExtender(ctx, task, taskLog)
	defer stopPrepareLease()

	if err := d.ensureTaskSkillBundles(ctx, &task); err != nil {
		return TaskResult{}, err
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
		CommentReplyTargets:              commentReplyThreads(task),
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
		ProjectDescription:               task.ProjectDescription,
		ProjectResources:                 convertProjectResourcesForEnv(task.ProjectResources),
		ChatSessionID:                    task.ChatSessionID,
		ChatChannelType:                  task.ChatChannelType,
		AutopilotRunID:                   task.AutopilotRunID,
		AutopilotID:                      task.AutopilotID,
		AutopilotTitle:                   task.AutopilotTitle,
		AutopilotDescription:             task.AutopilotDescription,
		AutopilotSource:                  task.AutopilotSource,
		AutopilotTriggerPayload:          strings.TrimSpace(string(task.AutopilotTriggerPayload)),
		QuickCreatePrompt:                task.QuickCreatePrompt,
		HandoffNote:                      task.HandoffNote,
		IsSquadLeader:                    strings.Contains(instructions, "## Squad Operating Protocol"),
		RequestingUserName:               task.RequestingUserName,
		RequestingUserProfileDescription: task.RequestingUserProfileDescription,
		InitiatorType:                    task.InitiatorType,
		InitiatorID:                      task.InitiatorID,
		InitiatorName:                    task.InitiatorName,
		InitiatorEmail:                   task.InitiatorEmail,
		WorkspaceContext:                 task.WorkspaceContext,
		ConnectedApps:                    task.ConnectedApps,
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
	// For a built-in codex task, use the version paired with the resolved path
	// so an in-place upgrade can't leave the sandbox policy on the old version
	// (MUL-4486). A custom codex runtime skips the self-heal, so resolvedVersion
	// is empty and it keeps the existing cached-version fallback — its binary is
	// the profile's own command, which the daemon never pins or version-detects.
	// Non-codex providers carry the value through without consuming it.
	codexVersion := d.agentVersion("codex")
	if provider == "codex" && resolvedVersion != "" {
		codexVersion = resolvedVersion
	}
	openclawBin := ""
	if provider == "openclaw" {
		openclawBin = entry.Path
	}
	// Resolve any local_directory assignment again here so runTask can plumb
	// LocalWorkDir into execenv. handleTask already validated + locked the
	// path for worker tasks; leader tasks intentionally skip the assignment.
	localAssignment, _ := localDirectoryAssignmentForTask(task, d.cfg.DaemonID)
	// Reuse intentionally skipped for local_directory tasks: the prior
	// WorkDir is the user's own path (always present) but the reuse path
	// loses the envRoot association the GC loop needs, and re-running
	// Prepare against a stable user path is cheap (no clone, no copy).
	// Leader tasks have no localAssignment; shouldReusePriorWorkdir separately
	// requires Prepare-time managed-env provenance and a daemon-owned marker
	// before allowing reuse, so a pre-fix leader session recorded against
	// local_directory still fails closed.
	var agentMcpConfig json.RawMessage
	var effectiveMcpConfig json.RawMessage
	var cursorMcpAuthSource string
	if task.Agent != nil {
		agentMcpConfig = task.Agent.McpConfig
		effectiveMcpConfig = agentMcpConfig
		if merged, mergeErr := mergeRuntimeAndAgentMcpConfig(provider, agentMcpConfig); mergeErr != nil {
			taskLog.Warn("mcp_config: runtime merge failed; using agent configuration only",
				"provider", provider,
				"error", mergeErr,
			)
		} else {
			effectiveMcpConfig = merged
		}
		if provider == "cursor" {
			cursorMcpAuthSource = strings.TrimSpace(task.Agent.CustomEnv[execenv.CursorMcpAuthSourceEnv])
		}
	}
	// Decode openclaw-specific runtime_config knobs once so reuse / prepare /
	// ExecOptions all see the same mode + gateway pin (issue #3260). Parse
	// failures fail soft to local mode — a broken JSON blob must never block
	// task dispatch.
	var openclawMode string
	var openclawGateway execenv.OpenclawGatewayPin
	if task.Agent != nil && provider == "openclaw" {
		openclawMode, openclawGateway = decodeOpenclawRuntimeConfig(task.Agent.RuntimeConfig, d.logger)
	}
	var agentEnvOverrides map[string]string
	var agentCustomArgs []string
	if task.Agent != nil {
		agentEnvOverrides = task.Agent.CustomEnv
		agentCustomArgs = task.Agent.CustomArgs
	}
	// Hermes: resolve the overlay source home through one resolver contract —
	// the selection parsed from custom_args (agent.ParseHermesProfileArgs) plus
	// the agent's custom_env HERMES_HOME feed execenv.ResolveHermesProfile, which
	// reproduces Hermes' own profile semantics (root derivation, explicit vs.
	// sticky selection, reserved/invalid failure). A reserved/invalid selection
	// fails the task closed, matching Hermes' sys.exit(1). The selected source
	// home is exported to hermesEnv["HERMES_HOME"] so ${HERMES_HOME} in a
	// profile's skills.external_dirs expands against the selected profile home,
	// as native Hermes does before loading config.yaml. The parsed occurrence is
	// stripped from the acp argv at launch (only when the overlay is built) so
	// the flag can't re-point HERMES_HOME past the overlay.
	var hermesSourceHome string
	var hermesSourceMustExist bool
	var hermesEnv map[string]string
	if provider == "hermes" {
		sel := agent.ParseHermesProfileArgs(agentCustomArgs)
		res := execenv.ResolveHermesProfile(agentEnvOverrides["HERMES_HOME"], sel.Name, sel.Found, sel.Inline)
		if res.Err != nil {
			return TaskResult{}, fmt.Errorf("resolve hermes profile: %w", res.Err)
		}
		hermesSourceHome = res.SourceHome
		hermesSourceMustExist = res.MustExist
		hermesEnv = sanitizeAgentEnv(agentEnvOverrides)
		if hermesEnv == nil {
			hermesEnv = map[string]string{}
		}
		hermesEnv["HERMES_HOME"] = res.SourceHome
	}
	// Guard this task's per-issue Codex session store from the GC for the whole
	// task, starting before Prepare/Reuse mounts it — so a prune that samples the
	// store's stale (pre-remount) mtime cannot reclaim it out from under a resume
	// of a long-idle issue (MUL-4424). No-op for non-Codex tasks / no stable key.
	if provider == "codex" {
		if store := execenv.CodexSessionStorePath(d.cfg.Profile, task.AgentID, task.IssueID); store != "" {
			d.markActiveCodexStore(store)
			defer d.unmarkActiveCodexStore(store)
		}
	}
	if shouldReusePriorWorkdir(task, localAssignment, d.cfg.WorkspacesRoot) {
		env = execenv.Reuse(execenv.ReuseParams{
			WorkspacesRoot:        d.cfg.WorkspacesRoot,
			Profile:               d.cfg.Profile,
			WorkDir:               task.PriorWorkDir,
			Provider:              provider,
			CodexVersion:          codexVersion,
			ResumeSessionID:       task.PriorSessionID,
			OpenclawBin:           openclawBin,
			McpConfig:             effectiveMcpConfig,
			CursorMcpAuthSource:   cursorMcpAuthSource,
			OpenclawGateway:       openclawGateway,
			HermesSourceHome:      hermesSourceHome,
			HermesSourceMustExist: hermesSourceMustExist,
			HermesEnv:             hermesEnv,
			Task:                  taskCtx,
		}, d.logger)
	}
	if env == nil {
		var err error
		prepParams := execenv.PrepareParams{
			WorkspacesRoot:        d.cfg.WorkspacesRoot,
			Profile:               d.cfg.Profile,
			WorkspaceID:           task.WorkspaceID,
			TaskID:                task.ID,
			AgentName:             agentName,
			Provider:              provider,
			CodexVersion:          codexVersion,
			OpenclawBin:           openclawBin,
			McpConfig:             effectiveMcpConfig,
			CursorMcpAuthSource:   cursorMcpAuthSource,
			OpenclawGateway:       openclawGateway,
			HermesSourceHome:      hermesSourceHome,
			HermesSourceMustExist: hermesSourceMustExist,
			HermesEnv:             hermesEnv,
			Task:                  taskCtx,
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
	taskTempDir, err := ensureTaskTempDir(env.RootDir, task.WorkspaceID, task.ID)
	if err != nil {
		return TaskResult{}, fmt.Errorf("prepare task temp dir: %w", err)
	}
	defer func() {
		if cerr := os.RemoveAll(taskTempDir); cerr != nil {
			taskLog.Warn("task temp dir cleanup failed", "path", taskTempDir, "error", cerr)
		}
	}()

	// Issue #3999 race A: now that env.WorkDir is on disk, transition the
	// server-side state machine dispatched (or waiting_local_directory) →
	// running. Calling StartTask before Prepare/Reuse let any consumer
	// that read status==running and resolved
	// /multica_workspaces/{ws}/{short-id}/workdir hit FileNotFoundError in
	// the microsecond window before os.MkdirAll ran.
	//
	// On error we return early so handleTask's existing FailTask +
	// taskfailure.Classify path records the failure with the same
	// "start task failed: <…>" string and the same failure_reason
	// taxonomy as before — see MUL-2946 for the classifier contract.
	if err := d.client.StartTask(ctx, task.ID); err != nil {
		stopPrepareLease()
		return TaskResult{}, fmt.Errorf("start task failed: %w", err)
	}
	stopPrepareLease()
	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Launching %s", provider), 1, 2)

	reused := gateResumeToReusedWorkdir(&task, &taskCtx, env.WorkDir, taskLog)
	// A reused workdir is necessary but not sufficient for a Codex resume: the
	// prior thread's rollout must actually be present in this task's CODEX_HOME
	// sessions (MUL-4424 isolates them). Drop the resume before the brief is
	// generated below if it isn't, so we never tell the agent it is continuing a
	// conversation Codex will silently restart from scratch.
	if reused {
		gateCodexResumeToRolloutPresence(&task, &taskCtx, provider, env.CodexHome, taskLog)
	}

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
	// manual `claude` / `codex` run in that directory would pick
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
			// CLAUDE.md / AGENTS.md; CleanupSidecars handles
			// every other file Prepare placed under WorkDir. Together
			// they round-trip the workdir to its exact pre-task bytes.
			if cerr := execenv.CleanupSidecars(env.RootDir); cerr != nil {
				d.logger.Warn("execenv: cleanup sidecars failed (non-fatal)", "error", cerr)
			}
		}()
	}

	prompt := BuildPrompt(task, provider)

	// Pass task-scoped auth credentials and context so the spawned agent CLI
	// can call the Multica API and the local daemon (e.g. `multica repo checkout`).
	// MULTICA_TASK_SLOT is allocated from the daemon-wide concurrency pool, not
	// per-agent. When one daemon hosts multiple agents, slots index shared
	// daemon-level resources such as GPUs.
	// MULTICA_TOKEN is bound to (agent, task) by the server. Never fall back
	// to the daemon's own credential here: doing so lets agent CLI writes land
	// as the runtime owner's member actor and can retrigger the same agent.
	agentToken, err := taskScopedAuthToken(task)
	if err != nil {
		taskLog.Error("task auth token invalid; refusing to start agent", "error", err)
		return TaskResult{}, err
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
		"TMPDIR":               taskTempDir,
		"TMP":                  taskTempDir,
		"TEMP":                 taskTempDir,
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
		if len(task.QuickCreateAttachmentIDs) > 0 {
			if raw, err := json.Marshal(task.QuickCreateAttachmentIDs); err == nil {
				agentEnv["MULTICA_QUICK_CREATE_ATTACHMENT_IDS"] = string(raw)
			} else {
				taskLog.Warn("quick-create attachment ids: marshal failed; skipping env injection", "error", err)
			}
		}
	}
	// Ensure the multica CLI is on PATH inside the agent's environment.
	// Some runtimes (e.g. Codex) run in an isolated sandbox that may not
	// inherit the daemon's PATH. Prepend the directory of the running
	// multica binary so that `multica` commands in the agent always resolve.
	if selfBin, err := resolveSelfExecutable(); err == nil {
		binDir := filepath.Dir(selfBin)
		agentEnv["PATH"] = binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	// Point Codex to the per-task CODEX_HOME so it discovers skills natively
	// without polluting the system ~/.codex/skills/.
	if env.CodexHome != "" {
		agentEnv["CODEX_HOME"] = env.CodexHome
	}
	// Redirect HOME/XDG/npm_config_cache to the per-task writable home under the
	// Linux codex workspace-write sandbox, where the real home is read-only. This
	// lets tools that write to `~` (npm, Prisma, …) succeed without per-tool env
	// tweaks. Set before custom_env below so a user override still wins for the
	// non-blocklisted XDG keys; HOME itself stays blocklisted. Empty TaskHome
	// (macOS/Windows, non-sandboxed providers) leaves the real HOME untouched
	// (MUL-4856).
	if env.TaskHome != "" {
		for k, v := range execenv.TaskHomeEnv(env.TaskHome) {
			agentEnv[k] = v
		}
	}
	// (Hermes HERMES_HOME is applied after custom_env below so the per-task
	// overlay can win over a user-set HERMES_HOME; see
	// layerCustomEnvAndHermesHome.)
	// Point Cursor at per-task project state when managed MCP is present.
	// The workdir .cursor/mcp.json carries the managed server list, while
	// CURSOR_DATA_DIR isolates the matching project approvals from the user's
	// persistent ~/.cursor/projects state.
	if env.CursorDataDir != "" {
		agentEnv["CURSOR_DATA_DIR"] = env.CursorDataDir
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
	var agentCustomEnv map[string]string
	if task.Agent != nil {
		agentCustomEnv = task.Agent.CustomEnv
	}
	layerCustomEnvAndHermesHome(agentEnv, agentCustomEnv, env.HermesHome, d.logger)
	backend, err := agent.New(provider, agent.Config{
		ExecutablePath: entry.Path,
		CLIVersion:     resolvedVersion,
		Env:            agentEnv,
		Logger:         d.logger,
		TaskID:         task.ID,
		RuntimeID:      task.RuntimeID,
		DaemonVersion:  d.cfg.CLIVersion,
		CodexVersion:   codexVersion,
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
	if len(profileFixedArgs) > 0 {
		extraArgs = append(append([]string{}, profileFixedArgs...), extraArgs...)
	}
	var mcpConfig json.RawMessage
	if task.Agent != nil {
		customArgs = task.Agent.CustomArgs
		mcpConfig = effectiveMcpConfig
	}
	if provider == "hermes" {
		customArgs = hermesLaunchArgs(customArgs, env != nil && env.HermesHome != "")
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
	// stale persisted value never blocks execution. An empty model is
	// resolved by ValidateThinkingLevel to the provider's default model so
	// default-model tasks aren't misjudged — except for codex, whose empty
	// model follows config.toml (any model) and so fails closed, dropping the
	// level here. Discovery errors fail open for resolved models: if we can't
	// list models, we keep the persisted level and let the CLI object.
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
	var idleWatchdogTimeout time.Duration
	if provider == "opencode" {
		idleWatchdogTimeout = d.cfg.OpenCodeIdleWatchdog
	}
	execOpts := agent.ExecOptions{
		Cwd:                       env.WorkDir,
		Model:                     model,
		ThreadName:                deriveTaskThreadName(task),
		Timeout:                   d.cfg.AgentTimeout,
		SemanticInactivityTimeout: d.cfg.CodexSemanticInactivityTimeout,
		IdleWatchdogTimeout:       idleWatchdogTimeout,
		HandshakeTimeout:          d.cfg.CodexHandshakeTimeout,
		ResumeSessionID:           task.PriorSessionID,
		// Post-gate intent: PriorSessionID here already reflects the pre-flight
		// resume gates (a dropped resume is surfaced via the brief instead). If it
		// survived to here, the backend must disclose to the user when the live
		// resume still fails — even across the fresh-session retry below, which
		// clears ResumeSessionID but not this (MUL-4424).
		ResumeExpected: task.PriorSessionID != "",
		ExtraArgs:      extraArgs,
		CustomArgs:     customArgs,
		McpConfig:      mcpConfig,
		ThinkingLevel:  thinkingLevel,
		OpenclawMode:   openclawMode,
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
		"idle_watchdog", execOpts.IdleWatchdogTimeout,
	)

	// Shared across the resume-retry below so the retry's transcript rows
	// keep ascending seq values for the same task.
	var msgSeq atomic.Int32
	result, tools, err := d.executeAndDrain(ctx, backend, prompt, execOpts, taskLog, task.ID, &msgSeq)
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
		retryResult, retryTools, retryErr := d.executeAndDrain(ctx, backend, prompt, execOpts, taskLog, task.ID, &msgSeq)
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
// server), and waits for the final result. msgSeq numbers the reported task
// messages and is owned by the caller so a same-task retry continues the
// sequence instead of restarting at 1 — the server orders the transcript by
// seq alone, and duplicate seqs would interleave the two attempts' rows.
func (d *Daemon) executeAndDrain(ctx context.Context, backend agent.Backend, prompt string, opts agent.ExecOptions, taskLog *slog.Logger, taskID string, msgSeq *atomic.Int32) (agent.Result, int32, error) {
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
	idleWindow := d.cfg.AgentIdleWatchdog
	// A provider may opt into a shorter per-run no-message budget. The global
	// zero remains authoritative so MULTICA_AGENT_IDLE_WATCHDOG=0 still disables
	// the entire watchdog suite. Tool calls continue to use AgentToolWatchdog.
	if idleWindow > 0 && opts.IdleWatchdogTimeout > 0 && opts.IdleWatchdogTimeout < idleWindow {
		idleWindow = opts.IdleWatchdogTimeout
	}
	var idleWatchdogThreshold atomic.Int64
	idleWatchdogThreshold.Store(int64(idleWindow))
	if idleWindow > 0 {
		go d.runIdleWatchdog(agentCtx, idleWindow, d.cfg.AgentToolWatchdog, &lastActivityAt, &inFlightTools, &idleWatchdogFired, &idleWatchdogThreshold, agentCancel, session.Messages, taskLog, taskID)
	}

	// drainFinished closes after the drain goroutine has flushed the last
	// message batch, so the result hand-off below can wait for the transcript
	// tail to be persisted.
	drainFinished := make(chan struct{})
	go func() {
		defer close(drainFinished)
		var mu sync.Mutex
		var pendingText strings.Builder
		var pendingThinking strings.Builder
		var batch []TaskMessageData
		callIDToTool := map[string]string{}

		flush := func() {
			mu.Lock()
			if pendingThinking.Len() > 0 {
				s := msgSeq.Add(1)
				batch = append(batch, TaskMessageData{
					Seq:     int(s),
					Type:    "thinking",
					Content: pendingThinking.String(),
				})
				pendingThinking.Reset()
			}
			if pendingText.Len() > 0 {
				s := msgSeq.Add(1)
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
		tickerDone := make(chan struct{})
		go func() {
			defer close(tickerDone)
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
					s := msgSeq.Add(1)
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
					s := msgSeq.Add(1)
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
					s := msgSeq.Add(1)
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
		// Let any tick-driven flush finish before the final one: a flush still
		// in flight would otherwise keep posting batches after this goroutine
		// signalled that the transcript tail was persisted.
		<-tickerDone
		flush()
	}()

	// waitForDrain blocks until the drain goroutine has flushed the transcript
	// tail, so every terminal return below hands control back only after the
	// task's reported messages are persisted — a consumer reading them at the
	// terminal transition would otherwise see a transcript that is non-empty
	// but truncated, indistinguishable from a complete one. Bounded so a
	// backend that never closes its message channel cannot stall the terminal
	// transition: after 10s the drain loop is cancelled and given a window
	// wide enough for its worst-case exit — an in-flight tick flush plus the
	// final one, each capped by the 5s ReportTaskMessages timeout and neither
	// interruptible by the cancel (they post on context.Background()).
	waitForDrain := func() {
		select {
		case <-drainFinished:
		case <-time.After(10 * time.Second):
			drainCancel()
			select {
			case <-drainFinished:
			case <-time.After(12 * time.Second):
				taskLog.Warn("transcript drain did not stop after cancel; completing anyway")
			}
		}
	}

	select {
	case result := <-session.Result:
		waitForDrain()
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
		// The drain loop is exiting on this same Done signal; wait for its
		// final flush so the timeout/watchdog/cancel terminals below cannot
		// hand back (and let runTask fail-and-broadcast) a still-flushing
		// transcript either.
		waitForDrain()
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
		result[i] = execenv.RepoContextForEnv{URL: r.URL, Description: r.Description, Ref: r.Ref}
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
	d.ensureActiveEnvRootStateLocked()
	for d.deletingEnvRoots[envRoot] {
		d.activeEnvRootsCond.Wait()
	}
	d.activeEnvRoots[envRoot]++
}

func (d *Daemon) unmarkActiveEnvRoot(envRoot string) {
	if envRoot == "" {
		return
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	if d.activeEnvRoots[envRoot] <= 1 {
		delete(d.activeEnvRoots, envRoot)
		return
	}
	d.activeEnvRoots[envRoot]--
}

func (d *Daemon) isActiveEnvRoot(envRoot string) bool {
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	return d.activeEnvRoots[envRoot] > 0
}

func (d *Daemon) ensureActiveEnvRootStateLocked() {
	if d.activeEnvRoots == nil {
		d.activeEnvRoots = make(map[string]int)
	}
	if d.deletingEnvRoots == nil {
		d.deletingEnvRoots = make(map[string]bool)
	}
	if d.activeEnvRootsCond == nil {
		d.activeEnvRootsCond = sync.NewCond(&d.activeEnvRootsMu)
	}
}

// reserveEnvRootForGC atomically confirms that no live task is using envRoot
// and prevents a new task from entering until release runs. This closes the
// check-then-remove race between the GC loop and task startup: either GC sees
// the active task and skips, or task startup waits for the mutation to finish
// and recreates/uses the post-GC environment.
func (d *Daemon) reserveEnvRootForGC(envRoot string) (release func(), ok bool) {
	if envRoot == "" {
		return nil, false
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	if d.activeEnvRoots[envRoot] > 0 || d.deletingEnvRoots[envRoot] {
		return nil, false
	}
	d.deletingEnvRoots[envRoot] = true
	return func() {
		d.activeEnvRootsMu.Lock()
		delete(d.deletingEnvRoots, envRoot)
		d.activeEnvRootsCond.Broadcast()
		d.activeEnvRootsMu.Unlock()
	}, true
}

// markActiveCodexStore records that a task is about to use the given per-issue
// Codex session store, so the GC never reclaims it mid-task — the store lives
// outside the env root, so isActiveEnvRoot does not cover it (MUL-4424). If a GC
// delete has already reserved this store, we wait for that removal to finish
// before claiming it, so a task never mounts a store mid-removal; the store is
// then recreated fresh by Prepare. Reference-counted like the env-root guard.
func (d *Daemon) markActiveCodexStore(store string) {
	if store == "" {
		return
	}
	d.activeCodexStoresMu.Lock()
	defer d.activeCodexStoresMu.Unlock()
	for d.deletingCodexStores[store] {
		d.activeCodexStoresCond.Wait()
	}
	d.activeCodexStores[store]++
}

func (d *Daemon) unmarkActiveCodexStore(store string) {
	if store == "" {
		return
	}
	d.activeCodexStoresMu.Lock()
	defer d.activeCodexStoresMu.Unlock()
	if d.activeCodexStores[store] <= 1 {
		delete(d.activeCodexStores, store)
		return
	}
	d.activeCodexStores[store]--
}

// reserveCodexStoreForDeletion atomically checks that no live task holds store
// and, if so, marks it reserved so no task can claim it until the caller runs
// the returned commit (after the actual removal). ok=false means a task holds it
// — do not delete. This is the exclusive protocol PruneCodexSessionStores needs:
// the "confirm inactive" and the mark happen under one lock acquisition, so a
// markActiveCodexStore either loses the check (store stays) or blocks on the
// reservation, closing the stat->remove race (MUL-4424).
func (d *Daemon) reserveCodexStoreForDeletion(store string) (commit func(), ok bool) {
	d.activeCodexStoresMu.Lock()
	defer d.activeCodexStoresMu.Unlock()
	if d.activeCodexStores[store] > 0 || d.deletingCodexStores[store] {
		return nil, false
	}
	d.deletingCodexStores[store] = true
	return func() {
		d.activeCodexStoresMu.Lock()
		delete(d.deletingCodexStores, store)
		d.activeCodexStoresCond.Broadcast()
		d.activeCodexStoresMu.Unlock()
	}, true
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

func ensureTaskTempDir(envRoot string, workspaceID string, taskID string) (string, error) {
	envRoot = strings.TrimSpace(envRoot)
	if envRoot == "" {
		return "", errors.New("env root is empty")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", errors.New("workspace id is empty")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", errors.New("task id is empty")
	}
	dir, err := os.MkdirTemp(socketSafeTempBaseDir(), "multica-task-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func socketSafeTempBaseDir() string {
	if os.PathSeparator == '/' {
		if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
			return "/tmp"
		}
	}
	return os.TempDir()
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
	case "HOME", "PATH", "USER", "SHELL", "TERM", "TMPDIR", "TMP", "TEMP", "CODEX_HOME", "CURSOR_DATA_DIR", execenv.CursorMcpAuthSourceEnv, "OPENCLAW_CONFIG_PATH", "OPENCLAW_INCLUDE_ROOTS":
		return true
	}
	return false
}

// layerCustomEnvAndHermesHome applies the agent's custom_env onto the child env
// (skipping daemon-internal blocklisted keys), then overrides HERMES_HOME with
// the per-task overlay when one was built. HERMES_HOME is intentionally NOT
// blocklisted: with skills bound the overlay is built FROM the user's
// HERMES_HOME and must win here; with no skills bound (overlayHome empty) the
// user's own HERMES_HOME passes through unchanged, so a skill-less Hermes task
// keeps its original behavior.
// sanitizeAgentEnv returns the agent custom_env with daemon-blocklisted keys
// removed — the effective env the Hermes child actually sees, used to expand
// ${VAR} in external_dirs consistently (blocked keys like HOME resolve to the
// daemon process value, not the dropped custom one). Uses the same
// isBlockedEnvKey rule as layerCustomEnvAndHermesHome so the two agree.
func sanitizeAgentEnv(customEnv map[string]string) map[string]string {
	if len(customEnv) == 0 {
		return nil
	}
	out := make(map[string]string, len(customEnv))
	for k, v := range customEnv {
		if isBlockedEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

// hermesLaunchArgs decides the final Hermes custom_args: with the per-task
// overlay active, the -p/--profile flags are stripped (the overlay was seeded
// from that profile's home and exports its own HERMES_HOME, so the flag must not
// re-resolve the profile past it); with no overlay, the flags pass through so a
// skill-less task's profile behavior is unchanged.
func hermesLaunchArgs(customArgs []string, overlayActive bool) []string {
	if !overlayActive {
		return customArgs
	}
	// Strip exactly the occurrence the resolver acted on. This re-parses with the
	// same authoritative parser used to resolve the source home, so parsing and
	// stripping never diverge.
	sel := agent.ParseHermesProfileArgs(customArgs)
	return agent.StripHermesProfileArgs(customArgs, sel)
}

func layerCustomEnvAndHermesHome(agentEnv, customEnv map[string]string, overlayHome string, logger *slog.Logger) {
	for k, v := range customEnv {
		if isBlockedEnvKey(k) {
			if logger != nil {
				logger.Warn("custom_env: blocked key skipped", "key", k)
			}
			continue
		}
		agentEnv[k] = v
	}
	if overlayHome != "" {
		agentEnv["HERMES_HOME"] = overlayHome
	}
}

func defaultArgsForProvider(cfg Config, provider string) []string {
	var args []string
	switch provider {
	case "claude":
		args = cfg.ClaudeArgs
	case "codex":
		args = cfg.CodexArgs
	case "codebuddy":
		args = cfg.CodebuddyArgs
	default:
		return nil
	}
	return append([]string(nil), args...)
}
