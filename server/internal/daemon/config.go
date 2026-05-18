package daemon

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-shellwords"
)

const (
	DefaultServerURL                      = "ws://localhost:8080/ws"
	DefaultPollInterval                   = 30 * time.Second
	DefaultHeartbeatInterval              = 15 * time.Second
	DefaultAgentTimeout                   = 2 * time.Hour
	DefaultCodexSemanticInactivityTimeout = 10 * time.Minute
	// DefaultAgentIdleWatchdog is the per-task safety net that force-stops a
	// run when the backend has emitted no message for this long AND its
	// message queue is empty. Backends like Claude Code can hang indefinitely
	// on a stuck child process (e.g. `docker ps` against a frozen dockerd),
	// in which case `cmd.Wait()` never returns and the task sits at "running"
	// for its full DefaultAgentTimeout (2 h). The previous 5 min default
	// killed legitimate long assistant outputs (e.g. RFC-length writeups)
	// where the model streams a single message for many minutes without any
	// daemon-visible activity — see MUL-2300. 30 min keeps the safety net for
	// truly stuck runs (dockerd hang) while leaving headroom for long writes.
	// Set MULTICA_AGENT_IDLE_WATCHDOG=0 to disable.
	DefaultAgentIdleWatchdog = 30 * time.Minute
	DefaultRuntimeName                    = "Local Agent"
	DefaultWorkspaceSyncInterval          = 30 * time.Second
	DefaultHealthPort                     = 19514
	DefaultMaxConcurrentTasks             = 20
	DefaultGCInterval                     = 1 * time.Hour
	DefaultGCTTL                          = 24 * time.Hour // 1 day — AI-coding issues rarely stay open long
	DefaultGCOrphanTTL                    = 72 * time.Hour // 3 days — orphans with no meta (crashes, pre-GC leftovers)
	DefaultGCArtifactTTL                  = 12 * time.Hour // 12h — drop regenerable artifacts on completed but still-open issues
	DefaultAutoUpdateCheckInterval        = 6 * time.Hour  // how often the daemon polls GitHub for a newer CLI release
)

// DefaultGCArtifactPatterns lists basename matches that the GC loop treats as
// regenerable build artifacts. Kept conservative: only directories that are
// always cheap to recreate (`pnpm install`, `next build`, `turbo build`). Things
// like `dist/`, `build/`, `.cache/` or `.venv/` may legitimately hold source or
// release output in some repos and are NOT included by default — set
// MULTICA_GC_ARTIFACT_PATTERNS to extend the list per deployment.
var DefaultGCArtifactPatterns = []string{"node_modules", ".next", ".turbo"}

// Config holds all daemon configuration.
type Config struct {
	ServerBaseURL                  string
	DaemonID                       string
	LegacyDaemonIDs                []string // historical daemon_ids this machine may have registered under; reported at register time so the server can merge old runtime rows
	DeviceName                     string
	RuntimeName                    string
	CLIVersion                     string                // multica CLI version (e.g. "0.1.13")
	LaunchedBy                     string                // "desktop" when spawned by the Electron app, empty for standalone
	Profile                        string                // profile name (empty = default)
	Agents                         map[string]AgentEntry // keyed by provider: claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor, kimi, kiro
	WorkspacesRoot                 string                // base path for execution envs (default: ~/multica_workspaces)
	KeepEnvAfterTask               bool                  // preserve env after task for debugging
	HealthPort                     int                   // local HTTP port for health checks (default: 19514)
	MaxConcurrentTasks             int                   // max tasks running in parallel (default: 20)
	GCEnabled                      bool                  // enable periodic workspace garbage collection (default: true)
	GCInterval                     time.Duration         // how often the GC loop runs (default: 1h)
	GCTTL                          time.Duration         // clean dirs whose issue is done/cancelled and updated_at < now()-TTL (default: 24h)
	GCOrphanTTL                    time.Duration         // clean orphan dirs with no meta, or dirs whose issue gc-check returns 404, once they exceed this age (default: 72h). The 404 path uses the same TTL — a scoped-down token can't instantly wipe live workspaces.
	GCArtifactTTL                  time.Duration         // when a task has been completed for at least this long but its issue is still open, drop regenerable artifacts (default: 12h, set 0 to disable)
	GCArtifactPatterns             []string              // basename patterns whose subtrees are removed during artifact cleanup (default: node_modules, .next, .turbo)
	AutoUpdateEnabled              bool                  // periodically check for a newer CLI release and self-update when idle (default: true)
	AutoUpdateCheckInterval        time.Duration         // how often the auto-update loop polls for a new release (default: 6h)
	PollInterval                   time.Duration
	HeartbeatInterval              time.Duration
	AgentTimeout                   time.Duration
	CodexSemanticInactivityTimeout time.Duration
	AgentIdleWatchdog              time.Duration // force-stop a run when the backend goes silent this long with an empty queue (0 = disabled)
	ClaudeArgs                     []string
	CodexArgs                      []string
}

// Overrides allows CLI flags to override environment variables and defaults.
// Zero values are ignored and the env/default value is used instead.
type Overrides struct {
	ServerURL                      string
	WorkspacesRoot                 string
	PollInterval                   time.Duration
	HeartbeatInterval              time.Duration
	AgentTimeout                   time.Duration
	CodexSemanticInactivityTimeout time.Duration
	MaxConcurrentTasks             int
	DaemonID                       string
	DeviceName                     string
	RuntimeName                    string
	Profile                        string // profile name (empty = default)
	HealthPort                     int    // health check port (0 = use default)
	// DisableAutoUpdate, when true, forces the auto-update poller off. There
	// is no symmetric "force on" override because the env/default already
	// resolves to enabled; the flag exists so users can opt out from the CLI.
	DisableAutoUpdate       bool
	AutoUpdateCheckInterval time.Duration // 0 = use env/default
}

// LoadConfig builds the daemon configuration from environment variables
// and optional CLI flag overrides.
func LoadConfig(overrides Overrides) (Config, error) {
	// Server URL: override > env > default
	rawServerURL := envOrDefault("MULTICA_SERVER_URL", DefaultServerURL)
	if overrides.ServerURL != "" {
		rawServerURL = overrides.ServerURL
	}
	serverBaseURL, err := NormalizeServerBaseURL(rawServerURL)
	if err != nil {
		return Config{}, err
	}

	// Probe available agent CLIs. exec.LookPath is the primary path, but on
	// macOS/Linux a GUI-launched daemon (Electron, Launchpad) does not
	// inherit the user's interactive shell PATH — fnm/nvm/volta multishells,
	// the Anthropic native installer prefix, and per-user npm prefixes all
	// live in dirs that only get added to PATH by ~/.zshrc or ~/.bashrc.
	// shellResolvedAgents asks the user's login shell, lazily on first miss,
	// to resolve every standard agent name to its canonical absolute path,
	// so we can find binaries the bare daemon process can't see. See
	// resolveAgentsViaLoginShell for the details and constraints.
	//
	// Laziness matters: the happy path (every agent on the daemon's PATH or
	// pinned to an explicit MULTICA_*_PATH) must not pay the cost of
	// spawning the user's login shell — that touches their rc files and
	// adds startup latency that scales with whatever they put in there. We
	// only fork a shell when a bare command name actually missed LookPath.
	var (
		shellResolveOnce sync.Once
		shellResolved    map[string]string
	)
	getShellResolved := func() map[string]string {
		shellResolveOnce.Do(func() {
			shellResolved = resolveAgentsViaLoginShell(defaultAgentCommandNames)
		})
		return shellResolved
	}
	probe := func(envVar, defaultCmd, modelEnv string) (AgentEntry, bool) {
		cmd := envOrDefault(envVar, defaultCmd)
		if _, err := exec.LookPath(cmd); err == nil {
			return AgentEntry{
				Path:  cmd,
				Model: strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}
		// The shell fallback only rescues bare command names. An operator
		// who pinned MULTICA_*_PATH to an absolute or relative path that
		// doesn't exist should hard-miss, not silently get a different
		// binary.
		if strings.ContainsAny(cmd, "/\\") {
			return AgentEntry{}, false
		}
		if path, ok := getShellResolved()[cmd]; ok {
			return AgentEntry{
				Path:  path,
				Model: strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}
		return AgentEntry{}, false
	}

	agents := map[string]AgentEntry{}
	if e, ok := probe("MULTICA_CLAUDE_PATH", "claude", "MULTICA_CLAUDE_MODEL"); ok {
		agents["claude"] = e
	}
	if e, ok := probe("MULTICA_CODEX_PATH", "codex", "MULTICA_CODEX_MODEL"); ok {
		agents["codex"] = e
	}
	if e, ok := probe("MULTICA_OPENCODE_PATH", "opencode", "MULTICA_OPENCODE_MODEL"); ok {
		agents["opencode"] = e
	}
	if e, ok := probe("MULTICA_OPENCLAW_PATH", "openclaw", "MULTICA_OPENCLAW_MODEL"); ok {
		agents["openclaw"] = e
	}
	if e, ok := probe("MULTICA_HERMES_PATH", "hermes", "MULTICA_HERMES_MODEL"); ok {
		agents["hermes"] = e
	}
	if e, ok := probe("MULTICA_GEMINI_PATH", "gemini", "MULTICA_GEMINI_MODEL"); ok {
		agents["gemini"] = e
	}
	if e, ok := probe("MULTICA_PI_PATH", "pi", "MULTICA_PI_MODEL"); ok {
		agents["pi"] = e
	}
	if e, ok := probe("MULTICA_CURSOR_PATH", "cursor-agent", "MULTICA_CURSOR_MODEL"); ok {
		agents["cursor"] = e
	}
	if e, ok := probe("MULTICA_COPILOT_PATH", "copilot", "MULTICA_COPILOT_MODEL"); ok {
		agents["copilot"] = e
	}
	if e, ok := probe("MULTICA_KIMI_PATH", "kimi", "MULTICA_KIMI_MODEL"); ok {
		agents["kimi"] = e
	}
	if e, ok := probe("MULTICA_KIRO_PATH", "kiro-cli", "MULTICA_KIRO_MODEL"); ok {
		agents["kiro"] = e
	}
	if len(agents) == 0 {
		return Config{}, fmt.Errorf("no agent CLI found: install claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor-agent, kimi, or kiro-cli and ensure it is on PATH")
	}

	claudeArgs, err := shellArgsFromEnv("MULTICA_CLAUDE_ARGS")
	if err != nil {
		return Config{}, err
	}
	codexArgs, err := shellArgsFromEnv("MULTICA_CODEX_ARGS")
	if err != nil {
		return Config{}, err
	}

	// Host info
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "local-machine"
	}

	// Durations: override > env > default
	pollInterval, err := durationFromEnv("MULTICA_DAEMON_POLL_INTERVAL", DefaultPollInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.PollInterval > 0 {
		pollInterval = overrides.PollInterval
	}

	heartbeatInterval, err := durationFromEnv("MULTICA_DAEMON_HEARTBEAT_INTERVAL", DefaultHeartbeatInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.HeartbeatInterval > 0 {
		heartbeatInterval = overrides.HeartbeatInterval
	}

	agentTimeout, err := durationFromEnv("MULTICA_AGENT_TIMEOUT", DefaultAgentTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.AgentTimeout > 0 {
		agentTimeout = overrides.AgentTimeout
	}

	codexSemanticInactivityTimeout, err := durationFromEnv("MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT", DefaultCodexSemanticInactivityTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.CodexSemanticInactivityTimeout > 0 {
		codexSemanticInactivityTimeout = overrides.CodexSemanticInactivityTimeout
	}

	// MULTICA_AGENT_IDLE_WATCHDOG=0 disables the per-task idle watchdog. We
	// route 0 through durationFromEnv so the operator can opt out without
	// patching the binary; any positive duration overrides DefaultAgentIdleWatchdog.
	agentIdleWatchdog, err := durationFromEnv("MULTICA_AGENT_IDLE_WATCHDOG", DefaultAgentIdleWatchdog)
	if err != nil {
		return Config{}, err
	}

	maxConcurrentTasks, err := intFromEnv("MULTICA_DAEMON_MAX_CONCURRENT_TASKS", DefaultMaxConcurrentTasks)
	if err != nil {
		return Config{}, err
	}
	if overrides.MaxConcurrentTasks > 0 {
		maxConcurrentTasks = overrides.MaxConcurrentTasks
	}

	// Profile
	profile := overrides.Profile

	// daemon_id resolution: override > env > persistent UUID on disk.
	// The persistent UUID is written once to `<profile-dir>/daemon.id` and
	// then reused forever so hostname drift (.local suffix, system rename,
	// mDNS state, profile switch) no longer mints a new runtime identity.
	// Callers may still pin a specific id via MULTICA_DAEMON_ID or the
	// override field (e.g. for tests or embedded environments).
	daemonID := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_ID"))
	if overrides.DaemonID != "" {
		daemonID = overrides.DaemonID
	}
	if daemonID == "" {
		persisted, err := EnsureDaemonID(profile)
		if err != nil {
			return Config{}, fmt.Errorf("ensure daemon id: %w", err)
		}
		daemonID = persisted
	}
	// Historical daemon_ids derived from the current hostname/profile. The
	// server uses these at register time to merge any pre-UUID runtime rows
	// for this machine into the new UUID-keyed row and delete the stale ones.
	legacyDaemonIDs := LegacyDaemonIDs(host, profile)
	// Pre-change (#1220) daemon identity was stored per profile, which means
	// the same machine could end up with multiple leftover daemon.id files
	// — e.g. ~/.multica/daemon.id (default) plus ~/.multica/profiles/<x>/
	// daemon.id. Surface those UUIDs so the server can merge their runtime
	// rows into the canonical machine UUID. Fatal-free: a broken profiles
	// dir shouldn't block startup.
	if uuids, err := LegacyDaemonUUIDs(); err == nil {
		legacyDaemonIDs = append(legacyDaemonIDs, uuids...)
	}
	// Strip anything that collides with the resolved daemon_id (e.g. when
	// the user explicitly pins MULTICA_DAEMON_ID=<hostname>, or when the
	// canonical id was itself promoted from a pre-change profile file).
	legacyDaemonIDs = filterLegacyIDs(legacyDaemonIDs, daemonID)

	deviceName := envOrDefault("MULTICA_DAEMON_DEVICE_NAME", host)
	if overrides.DeviceName != "" {
		deviceName = overrides.DeviceName
	}

	runtimeName := envOrDefault("MULTICA_AGENT_RUNTIME_NAME", DefaultRuntimeName)
	if overrides.RuntimeName != "" {
		runtimeName = overrides.RuntimeName
	}

	// Workspaces root: override > env > default (~/multica_workspaces or ~/multica_workspaces_<profile>)
	workspacesRoot, err := ResolveWorkspacesRoot(profile, overrides.WorkspacesRoot)
	if err != nil {
		return Config{}, err
	}

	// Health port: override > default
	healthPort := DefaultHealthPort
	if overrides.HealthPort > 0 {
		healthPort = overrides.HealthPort
	}

	// Keep env after task: env > default (false)
	keepEnv := os.Getenv("MULTICA_KEEP_ENV_AFTER_TASK") == "true" || os.Getenv("MULTICA_KEEP_ENV_AFTER_TASK") == "1"

	// GC config: env > defaults
	gcEnabled := true
	if v := os.Getenv("MULTICA_GC_ENABLED"); v == "false" || v == "0" {
		gcEnabled = false
	}
	gcInterval, err := durationFromEnv("MULTICA_GC_INTERVAL", DefaultGCInterval)
	if err != nil {
		return Config{}, err
	}
	gcTTL, err := durationFromEnv("MULTICA_GC_TTL", DefaultGCTTL)
	if err != nil {
		return Config{}, err
	}
	gcOrphanTTL, err := durationFromEnv("MULTICA_GC_ORPHAN_TTL", DefaultGCOrphanTTL)
	if err != nil {
		return Config{}, err
	}
	gcArtifactTTL, err := durationFromEnv("MULTICA_GC_ARTIFACT_TTL", DefaultGCArtifactTTL)
	if err != nil {
		return Config{}, err
	}
	gcArtifactPatterns := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", DefaultGCArtifactPatterns)

	// Auto-update config: env > defaults > CLI override.
	autoUpdateEnabled := true
	if v := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_AUTO_UPDATE")); v == "false" || v == "0" {
		autoUpdateEnabled = false
	}
	if overrides.DisableAutoUpdate {
		autoUpdateEnabled = false
	}
	autoUpdateInterval, err := durationFromEnv("MULTICA_DAEMON_AUTO_UPDATE_INTERVAL", DefaultAutoUpdateCheckInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.AutoUpdateCheckInterval > 0 {
		autoUpdateInterval = overrides.AutoUpdateCheckInterval
	}

	return Config{
		ServerBaseURL:                  serverBaseURL,
		DaemonID:                       daemonID,
		LegacyDaemonIDs:                legacyDaemonIDs,
		DeviceName:                     deviceName,
		RuntimeName:                    runtimeName,
		Profile:                        profile,
		Agents:                         agents,
		WorkspacesRoot:                 workspacesRoot,
		KeepEnvAfterTask:               keepEnv,
		GCEnabled:                      gcEnabled,
		GCInterval:                     gcInterval,
		GCTTL:                          gcTTL,
		GCOrphanTTL:                    gcOrphanTTL,
		GCArtifactTTL:                  gcArtifactTTL,
		GCArtifactPatterns:             gcArtifactPatterns,
		AutoUpdateEnabled:              autoUpdateEnabled,
		AutoUpdateCheckInterval:        autoUpdateInterval,
		HealthPort:                     healthPort,
		MaxConcurrentTasks:             maxConcurrentTasks,
		PollInterval:                   pollInterval,
		HeartbeatInterval:              heartbeatInterval,
		AgentTimeout:                   agentTimeout,
		CodexSemanticInactivityTimeout: codexSemanticInactivityTimeout,
		AgentIdleWatchdog:              agentIdleWatchdog,
		ClaudeArgs:                     claudeArgs,
		CodexArgs:                      codexArgs,
	}, nil
}

// NormalizeServerBaseURL converts a WebSocket or HTTP URL to a base HTTP URL.
func NormalizeServerBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid MULTICA_SERVER_URL: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("MULTICA_SERVER_URL must use ws, wss, http, or https")
	}
	if u.Path == "/ws" {
		u.Path = ""
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// ResolveWorkspacesRoot returns the absolute path that the daemon and CLI
// should treat as the workspaces root. Resolution order: explicit override >
// MULTICA_WORKSPACES_ROOT env > default ($HOME/multica_workspaces, or
// $HOME/multica_workspaces_<profile> for a named profile). Read-only callers
// (e.g. `multica daemon disk-usage`) use this directly so they pick the same
// directory the running daemon would have picked.
func ResolveWorkspacesRoot(profile, override string) (string, error) {
	root := strings.TrimSpace(os.Getenv("MULTICA_WORKSPACES_ROOT"))
	if override != "" {
		root = override
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w (set MULTICA_WORKSPACES_ROOT to override)", err)
		}
		if profile != "" {
			root = filepath.Join(home, "multica_workspaces_"+profile)
		} else {
			root = filepath.Join(home, "multica_workspaces")
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve absolute workspaces root: %w", err)
	}
	return abs, nil
}

// ArtifactPatternsFromEnv returns the configured artifact patternSet — the
// same list the GC loop consults when it runs the artifact-only cleanup. The
// disk-usage CLI uses this to make sure the "artifact size" it reports
// matches what the GC would actually reclaim.
func ArtifactPatternsFromEnv() []string {
	return patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", DefaultGCArtifactPatterns)
}

// patternsFromEnv reads a comma-separated list from env. Patterns containing
// path separators are silently dropped — the GC artifact cleanup only matches
// directory basenames, never paths, so a pattern like "foo/bar" is meaningless
// and accepting it would just be a footgun.
func patternsFromEnv(name string, defaults []string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		out := make([]string, len(defaults))
		copy(out, defaults)
		return out
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, "/\\") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func shellArgsFromEnv(name string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	args, err := shellwords.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return args, nil
}

// defaultAgentCommandNames lists the command names the agent probe loop tries
// before any MULTICA_*_PATH override is applied. Kept in sync with the
// `probe(...)` calls in LoadConfig — the shell-fallback resolver uses this
// list to pre-fetch canonical paths for every known agent in a single shell
// invocation, instead of paying the cost-per-miss.
var defaultAgentCommandNames = []string{
	"claude", "codex", "opencode", "openclaw", "hermes",
	"gemini", "pi", "cursor-agent", "copilot", "kimi", "kiro-cli",
}

// loginShellResolveTimeout caps how long the daemon will wait for the user's
// login shell to print canonical agent paths. A broken rc file should not
// block startup — if the shell takes longer than this, we proceed without
// shell-resolved fallbacks and the daemon falls back to the same behaviour
// it had before this code was added.
const loginShellResolveTimeout = 3 * time.Second

// loginShellResolveWaitDelay is the hard cap that runs *after*
// loginShellResolveTimeout has elapsed and `CommandContext` has signalled the
// shell to exit. The context kills the shell process itself, but rc files in
// the wild routinely background things that inherit stdout (`nvm` shims,
// `direnv hook`, `eval $(starship init)`, plain `&`). Those survivors keep
// the stdout pipe open and `cmd.Output()` will block on EOF for as long as
// they live. Cmd.WaitDelay (Go 1.20+) forcibly closes the pipes and returns
// once this delay elapses, so the total daemon-startup penalty caused by a
// pathological rc file is bounded by `timeout + waitDelay`, not by however
// long the user's background processes happen to run.
const loginShellResolveWaitDelay = 2 * time.Second

// supportedLoginShells limits which interpreters we will invoke via
// `<shell> -ilc <script>`. Sticking to POSIX-compatible shells means the
// resolver script below works unchanged. Notably absent: fish (uses
// `command -s` and a different syntax for command substitution).
var supportedLoginShells = map[string]struct{}{
	"bash": {},
	"zsh":  {},
	"sh":   {},
	"dash": {},
	"ksh":  {},
}

// resolveAgentsViaLoginShell asks the user's login shell to print the canonical
// (symlink-resolved) absolute path to each name in `names`. It returns a map
// of name → path for whatever the shell could find, and an empty map if the
// shell is unavailable / unsupported / times out / produces no usable output.
//
// Why we need this:
//
// Daemon-style processes on macOS/Linux do not inherit the user's interactive
// PATH. `claude --version` working in Terminal.app is no guarantee that
// exec.LookPath("claude") will work from a binary spawned by Launchpad, the
// Electron app, or `launchctl`. The most common offenders are fnm/nvm/volta
// "multishell" prefix dirs (per-shell, ephemeral) and the Anthropic native
// installer (`~/.claude/local/`) — both leave their binaries on a path that
// only `.zshrc` knows about.
//
// Implementation notes:
//
//   - We invoke `$SHELL -ilc <script>` with both -i (interactive) and -l
//     (login) so we pick up PATH set in either ~/.zshrc / ~/.bashrc OR
//     ~/.zprofile / ~/.bash_profile. Real users put it in both places.
//   - The script resolves symlinks via `cd "$dirname" && pwd -P` while the
//     spawned shell is still alive. fnm/nvm "multishell" directories vanish
//     on shell exit, so the canonical path must be captured before stdout is
//     returned to Go — by then the original path is already gone.
//   - We only trust outputs that look like an absolute path AND still pass a
//     fresh exec.LookPath check from the daemon's vantage point. That filters
//     out aliases (`command -v` prints the alias definition for those, not a
//     path) and per-shell paths the shell happened not to fully canonicalise.
//   - Agent names are restricted to the bare set in defaultAgentCommandNames
//     (`[A-Za-z0-9._-]` only); we inline them into the script unquoted to
//     keep the script readable. Custom MULTICA_*_PATH values never reach this
//     resolver — those go through exec.LookPath directly.
func resolveAgentsViaLoginShell(names []string) map[string]string {
	out := map[string]string{}
	if len(names) == 0 {
		return out
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		return out
	}
	if _, ok := supportedLoginShells[filepath.Base(shell)]; !ok {
		return out
	}

	safe := make([]string, 0, len(names))
	for _, n := range names {
		if isSafeAgentName(n) {
			safe = append(safe, n)
		}
	}
	if len(safe) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(context.Background(), loginShellResolveTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-ilc", buildLoginShellResolveScript(safe))
	cmd.WaitDelay = loginShellResolveWaitDelay
	raw, err := cmd.Output()
	if err != nil {
		return out
	}

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name, path := parts[0], strings.TrimSpace(parts[1])
		if !filepath.IsAbs(path) {
			continue
		}
		// Final reality check: the path the shell gave us must still be
		// executable from the daemon's perspective right now. fnm
		// multishells are the motivating example — pwd -P inside the
		// helper shell can fail to break out of the per-session bin dir,
		// and we'd rather report "not found" than hand back a path that
		// vanishes between detection and execution.
		if _, err := exec.LookPath(path); err != nil {
			continue
		}
		out[name] = path
	}
	return out
}

// buildLoginShellResolveScript returns the shell script that resolveAgentsViaLoginShell
// runs inside `$SHELL -ilc`. The script:
//
//  1. iterates the provided command names,
//  2. strips any locally-defined alias and shell function with that name so
//     `command -v` reaches through to a real binary on PATH (see below),
//  3. uses POSIX `command -v` to find each one on the interactive PATH,
//  4. rejects results that are not absolute paths (defence in depth — if the
//     unalias/unset -f pair somehow didn't take effect, `command -v` would
//     still print the alias/function definition, and we'd rather drop it
//     than hand back garbage),
//  5. canonicalises the directory via `cd ... && pwd -P` so symlinked prefix
//     dirs (fnm/nvm/volta) collapse to stable paths,
//  6. prints `<name>\t<canonical_path>` one entry per line for the caller.
//
// Why steps 2 is important — and why this PR's first revision missed #2512:
// the motivating case has `alias claude=...` in ~/.zshrc *and* fnm's real
// claude binary further down on PATH. With `-i` set, the alias loads, and
// `command -v claude` returns `claude: aliased to ...` (zsh) or `alias
// claude='...'` (bash) — neither starts with `/`, so step 4 drops them, and
// the loop never looks at PATH again. Unaliasing inside the same shell makes
// `command -v` fall back to the PATH search the daemon actually wants.
// Shell functions exhibit the same shadowing in bash/zsh, hence `unset -f`.
// Both calls are wrapped in `2>/dev/null` so the harmless "no such alias"
// error never reaches stderr.
//
// All input names are vetted by isSafeAgentName before they reach this
// function, so inlining them unquoted into the for-loop word list is safe.
func buildLoginShellResolveScript(names []string) string {
	var b strings.Builder
	b.WriteString("for n in")
	for _, n := range names {
		b.WriteByte(' ')
		b.WriteString(n)
	}
	b.WriteString("; do\n")
	b.WriteString("  unalias \"$n\" 2>/dev/null\n")
	b.WriteString("  unset -f \"$n\" 2>/dev/null\n")
	b.WriteString("  p=$(command -v \"$n\" 2>/dev/null) || continue\n")
	b.WriteString("  [ -n \"$p\" ] || continue\n")
	b.WriteString("  case \"$p\" in /*) ;; *) continue ;; esac\n")
	b.WriteString("  d=$(dirname \"$p\") && f=$(basename \"$p\") && c=$(cd \"$d\" 2>/dev/null && pwd -P) || continue\n")
	b.WriteString("  printf '%s\\t%s\\n' \"$n\" \"$c/$f\"\n")
	b.WriteString("done\n")
	return b.String()
}

// isSafeAgentName checks that `s` is a bare command name composed only of
// characters that are safe to inline into a shell script (ASCII letters,
// digits, dot, dash, underscore). The agent names this daemon ships with all
// satisfy the predicate; it exists to guard against future drift, not to
// constrain operator-supplied paths (those never reach the shell resolver).
func isSafeAgentName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
