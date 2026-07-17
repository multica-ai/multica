package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-shellwords"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	DefaultServerURL         = "ws://localhost:8080/ws"
	DefaultPollInterval      = 30 * time.Second
	DefaultHeartbeatInterval = 15 * time.Second
	// DefaultAgentTimeout is the optional absolute wall-clock cap on a single
	// agent run. 0 = no cap: a run is bounded only by the inactivity watchdogs
	// (DefaultAgentIdleWatchdog / DefaultAgentToolWatchdog), so a session that keeps emitting events is
	// never killed merely for running long (MUL-3064). Operators who want a
	// hard ceiling for cost/resource control can set MULTICA_AGENT_TIMEOUT.
	DefaultAgentTimeout                   = 0
	DefaultCodexSemanticInactivityTimeout = 10 * time.Minute
	DefaultCodexHandshakeTimeout          = 30 * time.Second
	// DefaultOpenCodeIdleWatchdog shortens the no-message budget for OpenCode
	// runs while they are not executing a tool. OpenCode streams text and tool
	// events incrementally, so a completely silent interval here covers both a
	// missing first model token and a stalled response stream. The generic
	// AgentIdleWatchdog remains the global enable/disable switch.
	DefaultOpenCodeIdleWatchdog = 10 * time.Minute
	// DefaultAgentIdleWatchdog is the per-task safety net that force-stops a
	// run when the backend has emitted no message for this long AND its
	// message queue is empty. Backends like Claude Code can hang indefinitely
	// on a stuck child process (e.g. `docker ps` against a frozen dockerd),
	// in which case `cmd.Wait()` never returns. With no wall-clock cap
	// (DefaultAgentTimeout = 0) such a run would otherwise sit at "running"
	// forever, so this watchdog is its sole liveness net. The previous 5 min default
	// killed legitimate long assistant outputs (e.g. RFC-length writeups)
	// where the model streams a single message for many minutes without any
	// daemon-visible activity — see MUL-2300. 30 min keeps the safety net for
	// truly stuck runs (dockerd hang) while leaving headroom for long writes.
	// Set MULTICA_AGENT_IDLE_WATCHDOG=0 to disable.
	DefaultAgentIdleWatchdog = 30 * time.Minute
	// DefaultAgentToolWatchdog bounds how long a single tool call may stay in
	// flight (tool_use emitted, no tool_result and no other message) before the
	// idle watchdog force-stops the run. The idle watchdog ignores its normal
	// window while a tool is in flight, because a real build/install/test
	// legitimately runs silently for many minutes — but with no wall-clock cap
	// (DefaultAgentTimeout = 0) a backend that emits tool_use and never the
	// matching tool_result would otherwise run forever. This is the backstop for
	// that stuck-tool case (MUL-3064). Set MULTICA_AGENT_TOOL_WATCHDOG=0 to
	// disable, in which case an in-flight tool never force-stops the run.
	DefaultAgentToolWatchdog              = 2 * time.Hour
	DefaultRuntimeName                    = "Local Agent"
	DefaultWorkspaceBootstrapSyncInterval = 30 * time.Second
	DefaultWorkspaceLegacySyncInterval    = 5 * time.Minute
	DefaultWorkspaceSyncInterval          = 30 * time.Minute
	DefaultWorkspaceSyncMaxBackoff        = 30 * time.Minute
	DefaultHealthPort                     = 19514
	DefaultMaxConcurrentTasks             = 20
	DefaultGCInterval                     = 2 * time.Hour
	DefaultGCTTL                          = 24 * time.Hour      // 1 day — AI-coding issues rarely stay open long
	DefaultGCOrphanTTL                    = 72 * time.Hour      // 3 days — orphans with no meta (crashes, pre-GC leftovers)
	DefaultGCArtifactTTL                  = 12 * time.Hour      // 12h — drop regenerable artifacts on completed but still-open issues
	DefaultGCCodexSessionTTL              = 14 * 24 * time.Hour // 14 days — reclaim per-issue Codex session stores untouched this long
	DefaultAutoUpdateCheckInterval        = 6 * time.Hour       // how often the daemon polls GitHub for a newer CLI release
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
	Agents                         map[string]AgentEntry // keyed by provider: claude, codebuddy, codex, copilot, opencode, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, traecli, grok
	WorkspacesRoot                 string                // base path for execution envs (default: ~/multica_workspaces)
	KeepEnvAfterTask               bool                  // preserve env after task for debugging
	HealthPort                     int                   // local HTTP port for health checks (default: 19514)
	MaxConcurrentTasks             int                   // max tasks running in parallel (default: 20)
	GCEnabled                      bool                  // enable periodic workspace garbage collection (default: true)
	GCInterval                     time.Duration         // how often the GC loop runs (default: 2h)
	GCTTL                          time.Duration         // clean dirs whose issue is done/cancelled and updated_at < now()-TTL (default: 24h)
	GCOrphanTTL                    time.Duration         // clean orphan dirs with no meta, or dirs whose issue gc-check returns 404, once they exceed this age (default: 72h). The 404 path uses the same TTL — a scoped-down token can't instantly wipe live workspaces.
	GCArtifactTTL                  time.Duration         // when a task has been completed for at least this long but its issue is still open, drop regenerable artifacts (default: 12h, set 0 to disable)
	GCArtifactPatterns             []string              // basename patterns whose subtrees are removed during artifact cleanup (default: node_modules, .next, .turbo)
	GCCodexSessionTTL              time.Duration         // reclaim a per-issue Codex session store (~/.codex/multica-sessions/<agent>/<issue>) untouched for at least this long, so a done/abandoned issue's conversation history does not accumulate forever (default: 14d, set 0 to disable)
	AutoUpdateEnabled              bool                  // periodically check for a newer CLI release and self-update when idle (default: true on Multica Cloud, false on self-host)
	AutoUpdateCheckInterval        time.Duration         // how often the auto-update loop polls for a new release (default: 6h)
	PollInterval                   time.Duration
	HeartbeatInterval              time.Duration
	AgentTimeout                   time.Duration
	CodexSemanticInactivityTimeout time.Duration
	CodexHandshakeTimeout          time.Duration
	OpenCodeIdleWatchdog           time.Duration // OpenCode-specific no-message window; 0 falls back to AgentIdleWatchdog and values above it cannot extend the global bound
	AgentIdleWatchdog              time.Duration // force-stop a run when the backend goes silent this long with an empty queue (0 = disabled)
	AgentToolWatchdog              time.Duration // force-stop a run when a single tool call stays in flight (silent) this long (0 = disabled); backstop for hung tools now that there is no wall-clock cap
	ClaudeArgs                     []string
	CodexArgs                      []string
	CodebuddyArgs                  []string

	// ProfileCommandOverrides maps a custom runtime profile_id -> the absolute
	// executable path to use for that profile on THIS machine (MUL-3284).
	// Sourced from the local CLI config (cli.CLIConfig.ProfileCommandOverrides),
	// written by `multica runtime profile set-path`. appendProfileRuntimes
	// prefers a matching, executable override over resolving the profile's
	// command_name on PATH. nil/empty means "always resolve via PATH".
	ProfileCommandOverrides map[string]string
}

// Overrides allows CLI flags to override environment variables and defaults.
// Zero values are ignored and the env/default value is used instead.
type Overrides struct {
	ServerURL         string
	WorkspacesRoot    string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	// AgentTimeout is a pointer so an explicit `--agent-timeout 0` (no cap) is
	// distinguishable from "flag not passed". nil = use env/default.
	AgentTimeout                   *time.Duration
	CodexSemanticInactivityTimeout time.Duration
	CodexHandshakeTimeout          time.Duration
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

	// Apply backend overrides from the CLI config file (issue #3875).
	//
	// CLIConfig.Backends.OpenClaw lets users record "which OpenClaw on this
	// machine, and where its state lives" in a versioned, UI-editable file
	// instead of a launchctl env hack. We translate those fields into the
	// same env vars the rest of LoadConfig already honors:
	//
	//   - MULTICA_OPENCLAW_PATH: read by probe() via envOrDefault for the
	//     binary lookup; pre-existing path.
	//   - OPENCLAW_STATE_DIR:    OpenClaw's own env var; the daemon already
	//     forwards it to spawned children via mergeEnv (server/pkg/agent/...).
	//
	// Precedence is "env wins over config wins over default" — same shape
	// users already get with MULTICA_OPENCLAW_PATH today. We achieve it with
	// LookupEnv guards: if the user already exported the env var (in their
	// shell, via launchctl, or via the systemd unit), we leave it alone;
	// otherwise we Setenv from the config file. This keeps every downstream
	// consumer (probe, buildEnv, child processes) on the existing code path
	// without inventing a new plumbing channel.
	//
	// Errors loading CLIConfig are non-fatal: a missing or malformed config
	// file should not prevent daemon startup, since the daemon can still run
	// purely from env-var configuration. We log a warning and proceed with
	// no overrides.
	var profileCommandOverrides map[string]string
	if cliCfg, err := cli.LoadCLIConfigForProfile(overrides.Profile); err != nil {
		slog.Warn("could not load CLI config for backend overrides; proceeding without",
			"profile", overrides.Profile, "err", err)
	} else {
		if oc := openclawOverrideFrom(cliCfg); oc != nil {
			applyOpenclawOverride(oc)
		}
		// Per-machine custom-runtime command path overrides (MUL-3284).
		// Copy into our own map so later mutation of the loaded config can't
		// alias daemon state, and so an empty map normalizes to nil.
		if len(cliCfg.ProfileCommandOverrides) > 0 {
			profileCommandOverrides = make(map[string]string, len(cliCfg.ProfileCommandOverrides))
			for id, path := range cliCfg.ProfileCommandOverrides {
				if id == "" || strings.TrimSpace(path) == "" {
					continue
				}
				profileCommandOverrides[id] = path
			}
		}
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
		if path, err := resolveAgentExecutablePath(cmd); err == nil {
			return AgentEntry{
				Path:    path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
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
				Path:    path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}
		if defaultCmd == "codex" && cmd == defaultCmd {
			// Codex Desktop bundles its CLI inside the macOS app instead of
			// installing it onto PATH.
			for _, p := range codexDesktopAppBundlePaths() {
				if _, err := os.Stat(p); err == nil {
					return AgentEntry{
						Path:    p,
						Command: cmd,
						Model:   strings.TrimSpace(os.Getenv(modelEnv)),
					}, true
				}
			}
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
	if e, ok := probe("MULTICA_DEVECO_PATH", "deveco", "MULTICA_DEVECO_MODEL"); ok {
		agents["deveco"] = e
	}
	if e, ok := probe("MULTICA_OPENCLAW_PATH", "openclaw", "MULTICA_OPENCLAW_MODEL"); ok {
		agents["openclaw"] = e
	}
	if e, ok := probe("MULTICA_HERMES_PATH", "hermes", "MULTICA_HERMES_MODEL"); ok {
		agents["hermes"] = e
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
	if e, ok := probe("MULTICA_CODEBUDDY_PATH", "codebuddy", "MULTICA_CODEBUDDY_MODEL"); ok {
		agents["codebuddy"] = e
	}
	// agy 1.0.6 added a `--model` flag (MUL-3125), so Antigravity now takes a
	// model env like every other backend. MULTICA_ANTIGRAVITY_MODEL seeds the
	// daemon-wide default; its value is the exact `agy models` display string
	// (e.g. "Claude Opus 4.6 (Thinking)"), not a provider/model slug.
	if e, ok := probe("MULTICA_ANTIGRAVITY_PATH", "agy", "MULTICA_ANTIGRAVITY_MODEL"); ok {
		agents["antigravity"] = e
	}
	qoderPath := envOrDefault("MULTICA_QODER_PATH", "qodercli")
	if path, err := resolveAgentExecutablePath(qoderPath); err == nil {
		agents["qoder"] = AgentEntry{
			Path:    path,
			Command: qoderPath,
			Model:   strings.TrimSpace(os.Getenv("MULTICA_QODER_MODEL")),
		}
	}
	// ByteDance official TRAE CLI (the `traecli` binary from https://docs.trae.cn/cli),
	// driven over ACP via `traecli acp serve --yolo`. MULTICA_TRAECLI_MODEL seeds
	// the daemon-wide default model (a model id from the user's logged-in traecli
	// catalog).
	if e, ok := probe("MULTICA_TRAECLI_PATH", "traecli", "MULTICA_TRAECLI_MODEL"); ok {
		agents["traecli"] = e
	}
	// xAI Grok Build CLI (`grok`), driven over ACP via
	// `grok agent --always-approve stdio`. MULTICA_GROK_MODEL seeds the
	// daemon-wide default (e.g. grok-4.5).
	if e, ok := probe("MULTICA_GROK_PATH", "grok", "MULTICA_GROK_MODEL"); ok {
		agents["grok"] = e
	}
	if len(agents) == 0 {
		return Config{}, fmt.Errorf("no agent CLI found: install claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor-agent, kimi, kiro-cli, agy, qodercli, traecli, or grok and ensure it is on PATH")
	}

	claudeArgs, err := shellArgsFromEnv("MULTICA_CLAUDE_ARGS")
	if err != nil {
		return Config{}, err
	}
	codexArgs, err := shellArgsFromEnv("MULTICA_CODEX_ARGS")
	if err != nil {
		return Config{}, err
	}
	codebuddyArgs, err := shellArgsFromEnv("MULTICA_CODEBUDDY_ARGS")
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
	if overrides.AgentTimeout != nil {
		agentTimeout = *overrides.AgentTimeout
	}

	codexSemanticInactivityTimeout, err := durationFromEnv("MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT", DefaultCodexSemanticInactivityTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.CodexSemanticInactivityTimeout > 0 {
		codexSemanticInactivityTimeout = overrides.CodexSemanticInactivityTimeout
	}

	codexHandshakeTimeout, err := durationFromEnv("MULTICA_CODEX_HANDSHAKE_TIMEOUT", DefaultCodexHandshakeTimeout)
	if err != nil {
		return Config{}, err
	}
	if codexHandshakeTimeout <= 0 {
		codexHandshakeTimeout = DefaultCodexHandshakeTimeout
	}
	if overrides.CodexHandshakeTimeout > 0 {
		codexHandshakeTimeout = overrides.CodexHandshakeTimeout
	}

	// MULTICA_AGENT_IDLE_WATCHDOG=0 disables the per-task idle watchdog. We
	// route 0 through durationFromEnv so the operator can opt out without
	// patching the binary; any positive duration overrides DefaultAgentIdleWatchdog.
	agentIdleWatchdog, err := durationFromEnv("MULTICA_AGENT_IDLE_WATCHDOG", DefaultAgentIdleWatchdog)
	if err != nil {
		return Config{}, err
	}
	// MULTICA_OPENCODE_IDLE_WATCHDOG narrows the no-message window for
	// OpenCode's streamed model responses. Zero removes the provider-specific
	// override and falls back to MULTICA_AGENT_IDLE_WATCHDOG; positive values
	// cannot extend the global bound, and the global zero still disables the
	// whole mechanism.
	openCodeIdleWatchdog, err := durationFromEnv("MULTICA_OPENCODE_IDLE_WATCHDOG", DefaultOpenCodeIdleWatchdog)
	if err != nil {
		return Config{}, err
	}

	// MULTICA_AGENT_TOOL_WATCHDOG=0 disables the in-flight-tool backstop; any
	// positive duration overrides DefaultAgentToolWatchdog.
	agentToolWatchdog, err := durationFromEnv("MULTICA_AGENT_TOOL_WATCHDOG", DefaultAgentToolWatchdog)
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
	gcCodexSessionTTL, err := durationFromEnv("MULTICA_GC_CODEX_SESSION_TTL", DefaultGCCodexSessionTTL)
	if err != nil {
		return Config{}, err
	}
	gcArtifactPatterns := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", DefaultGCArtifactPatterns)

	// Auto-update config: default -> env override -> CLI override.
	//
	// Default is opt-in on Multica Cloud (api.multica.ai) and opt-out for
	// self-hosted instances. Self-host operators frequently run a fork with
	// their own patches, and silently upgrading their daemon to an upstream
	// GitHub release would clobber that work; they also commonly stay on an
	// older server build, which a fresh CLI may no longer talk to. Keeping
	// auto-update off by default for self-host avoids both footguns (MUL-2381).
	// Operators on either side can flip the default with MULTICA_DAEMON_AUTO_UPDATE.
	autoUpdateEnabled := isOfficialCloudServer(serverBaseURL)
	if v := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_AUTO_UPDATE")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "no", "off":
			autoUpdateEnabled = false
		case "true", "1", "yes", "on":
			autoUpdateEnabled = true
		}
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
		GCCodexSessionTTL:              gcCodexSessionTTL,
		AutoUpdateEnabled:              autoUpdateEnabled,
		AutoUpdateCheckInterval:        autoUpdateInterval,
		HealthPort:                     healthPort,
		MaxConcurrentTasks:             maxConcurrentTasks,
		PollInterval:                   pollInterval,
		HeartbeatInterval:              heartbeatInterval,
		AgentTimeout:                   agentTimeout,
		CodexSemanticInactivityTimeout: codexSemanticInactivityTimeout,
		CodexHandshakeTimeout:          codexHandshakeTimeout,
		OpenCodeIdleWatchdog:           openCodeIdleWatchdog,
		AgentIdleWatchdog:              agentIdleWatchdog,
		AgentToolWatchdog:              agentToolWatchdog,
		ClaudeArgs:                     claudeArgs,
		CodexArgs:                      codexArgs,
		CodebuddyArgs:                  codebuddyArgs,
		ProfileCommandOverrides:        profileCommandOverrides,
	}, nil
}

// officialCloudHost is the hostname of Multica's hosted cloud. It's the only
// origin we treat as "official" for the auto-update default — staging,
// preview, and any future *.multica.ai subdomains are deliberately excluded
// so they inherit the safer self-host default until explicitly opted in.
const officialCloudHost = "api.multica.ai"

// isOfficialCloudServer reports whether the resolved server base URL points
// at Multica's hosted cloud. Used to pick the auto-update default: cloud
// users run a server that publishes the matching CLI release, so opt-in
// self-update is safe; self-host users may run a fork or pin to an older
// server, so the default flips to off. Matching is host-only and
// case-insensitive — port and path are ignored.
func isOfficialCloudServer(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), officialCloudHost)
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

// resolveAgentExecutablePath returns the concrete executable path the daemon
// should keep for an agent command. Bare command names are pinned to the path
// resolved during startup so later PATH changes cannot redirect task launches.
// When ~/.multica/hooks shadows a real agent binary, skip that hooks directory:
// previously generated hook wrappers can execute the same command name and
// recurse forever if the daemon records or launches the wrapper.
func resolveAgentExecutablePath(cmd string) (string, error) {
	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(cmd, "/\\") {
		return resolved, nil
	}
	if isInMulticaHooksDir(resolved) {
		if unshadowed, err := lookPathExcludingMulticaHooks(cmd); err == nil {
			return unshadowed, nil
		}
	}
	return canonicalExecutablePath(resolved), nil
}

// agentExecutablePresent reports whether path currently resolves to a runnable
// executable, using the exact check the agent backends apply at launch
// (exec.LookPath). A pinned AgentEntry.Path that fails this has vanished from
// disk — typically because a version manager did an in-place upgrade and
// deleted the old versioned directory the path pointed into (MUL-4486).
func agentExecutablePresent(path string) bool {
	if path == "" {
		return false
	}
	_, err := exec.LookPath(path)
	return err == nil
}

// reresolveAgentCommand re-runs the startup resolution for a single agent
// command name, returning the freshly resolved absolute path. It mirrors the
// probe() order in LoadConfig: exec.LookPath (with the ~/.multica/hooks
// exclusion preserved via resolveAgentExecutablePath) first, then the login
// shell fallback for a bare command name a GUI-launched daemon can't see on
// its own PATH. It is only called on the miss path — when a previously pinned
// path has disappeared — so the login-shell cost is paid rarely, never on a
// normal launch.
func reresolveAgentCommand(cmd string) (string, bool) {
	if cmd == "" {
		return "", false
	}
	if path, err := resolveAgentExecutablePath(cmd); err == nil {
		return path, true
	}
	// A bare command name the daemon's own PATH can't see: retry via the
	// user's login shell, exactly as the startup probe does for
	// fnm/nvm/native-installer prefixes. Absolute/relative overrides skip
	// this — an operator-pinned MULTICA_*_PATH that no longer exists should
	// stay a hard miss rather than silently resolve a different binary.
	if !strings.ContainsAny(cmd, "/\\") {
		if path, ok := resolveAgentsViaLoginShell([]string{cmd})[cmd]; ok {
			return path, true
		}
	}
	return "", false
}

func lookPathExcludingMulticaHooks(cmd string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if isMulticaHooksDir(dir) {
			continue
		}
		candidate := filepath.Join(dir, cmd)
		if isExecutableFile(candidate) {
			return canonicalExecutablePath(candidate), nil
		}
	}
	return "", exec.ErrNotFound
}

func isInMulticaHooksDir(path string) bool {
	if path == "" {
		return false
	}
	return isMulticaHooksDir(filepath.Dir(path))
}

func isMulticaHooksDir(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	return samePathDir(dir, filepath.Join(home, ".multica", "hooks"))
}

func samePathDir(a, b string) bool {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	absA = filepath.Clean(absA)
	absB = filepath.Clean(absB)
	if realA, err := filepath.EvalSymlinks(absA); err == nil {
		absA = realA
	}
	if realB, err := filepath.EvalSymlinks(absB); err == nil {
		absB = realB
	}
	return absA == absB
}

func canonicalExecutablePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// defaultAgentCommandNames lists the command names the agent probe loop tries
// before any MULTICA_*_PATH override is applied. Kept in sync with the
// `probe(...)` calls in LoadConfig — the shell-fallback resolver uses this
// list to pre-fetch canonical paths for every known agent in a single shell
// invocation, instead of paying the cost-per-miss.
var defaultAgentCommandNames = []string{
	"claude", "codex", "opencode", "deveco", "openclaw", "hermes",
	"pi", "cursor-agent", "copilot", "kimi", "kiro-cli", "codebuddy", "agy", "traecli", "grok",
}

// codexDesktopAppBundlePaths returns candidate macOS app-bundle locations for
// the bundled Codex CLI. OpenAI relocated the Desktop app from Codex.app to
// ChatGPT.app (#5205). Candidates are ordered by install location first
// (system /Applications before user ~/Applications); within each location the
// new ChatGPT.app path is tried before the legacy Codex.app path, so updated
// installs win while older installs still resolve.
var codexDesktopAppBundlePaths = func() []string {
	paths := []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex"),
			filepath.Join(home, "Applications", "Codex.app", "Contents", "Resources", "codex"),
		)
	}
	return paths
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
//  6. if the resolved path lives in ~/.multica/hooks, searches the same
//     shell-expanded PATH for the first executable outside that hooks dir,
//  7. prints `<name>\t<canonical_path>` one entry per line for the caller.
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
	b.WriteString("  hc=\"\"\n")
	b.WriteString("  if [ -n \"${HOME:-}\" ]; then hd=\"$HOME/.multica/hooks\"; hc=$(cd \"$hd\" 2>/dev/null && pwd -P) || hc=\"\"; fi\n")
	b.WriteString("  if [ -n \"$hc\" ] && [ \"$c\" = \"$hc\" ]; then\n")
	b.WriteString("    oldIFS=$IFS; IFS=:\n")
	b.WriteString("    for d2 in $PATH; do\n")
	b.WriteString("      [ -n \"$d2\" ] || d2=.\n")
	b.WriteString("      c2=$(cd \"$d2\" 2>/dev/null && pwd -P) || continue\n")
	b.WriteString("      [ \"$c2\" = \"$hc\" ] && continue\n")
	b.WriteString("      if [ -f \"$c2/$n\" ] && [ -x \"$c2/$n\" ]; then c=\"$c2\"; f=\"$n\"; break; fi\n")
	b.WriteString("    done\n")
	b.WriteString("    IFS=$oldIFS\n")
	b.WriteString("  fi\n")
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

// openclawOverrideFrom returns the OpenClaw override block from a loaded
// CLIConfig, or nil when no override is configured. Centralized here so
// the LoadConfig path and tests share one navigation predicate over the
// nullable-pointer chain.
func openclawOverrideFrom(cfg cli.CLIConfig) *cli.OpenClawOverride {
	if cfg.Backends == nil {
		return nil
	}
	return cfg.Backends.OpenClaw
}

// applyOpenclawOverride translates the config-file overrides into process
// env vars, which the existing probe() / buildEnv code paths already honor.
// Env-set-by-user wins over config-set-by-file: we only Setenv when the var
// is not already present, preserving the back-compat contract documented
// on cli.OpenClawOverride.
//
// Side-effecting on os.Setenv is intentional and scoped:
//
//   - The two vars touched (MULTICA_OPENCLAW_PATH, OPENCLAW_STATE_DIR) are
//     OpenClaw-specific. Other backends do not read them; setting them in the
//     daemon process has no observable effect on, e.g., Claude Code or Codex
//     spawn behavior.
//   - LoadConfig runs once during daemon startup, before any backend Execute.
//     Concurrent reads of os.Environ() in spawned children see a stable view.
//   - We deliberately do not unset on later reload: the daemon's lifecycle is
//     "exit and respawn" (cmd_daemon.go), not in-process reconfigure.
func applyOpenclawOverride(oc *cli.OpenClawOverride) {
	if oc == nil {
		return
	}
	if oc.BinaryPath != "" {
		if _, set := os.LookupEnv("MULTICA_OPENCLAW_PATH"); !set {
			_ = os.Setenv("MULTICA_OPENCLAW_PATH", oc.BinaryPath)
		}
	}
	if oc.StateDir != "" {
		if _, set := os.LookupEnv("OPENCLAW_STATE_DIR"); !set {
			_ = os.Setenv("OPENCLAW_STATE_DIR", oc.StateDir)
		}
	}
}
