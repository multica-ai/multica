package daemon

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-shellwords"
)

const (
	DefaultServerURL                      = "ws://localhost:8080/ws"
	DefaultPollInterval                   = 30 * time.Second
	DefaultHeartbeatInterval              = 15 * time.Second
	DefaultAgentTimeout                   = 2 * time.Hour
	DefaultCodexSemanticInactivityTimeout = 10 * time.Minute
	DefaultRuntimeName                    = "Local Agent"
	DefaultWorkspaceSyncInterval          = 30 * time.Second
	DefaultHealthPort                     = 19514
	DefaultMaxConcurrentTasks             = 20
	DefaultGCInterval                     = 1 * time.Hour
	DefaultGCTTL                          = 24 * time.Hour // 1 day — AI-coding issues rarely stay open long
	DefaultGCOrphanTTL                    = 72 * time.Hour // 3 days — orphans with no meta (crashes, pre-GC leftovers)
	DefaultGCArtifactTTL                  = 12 * time.Hour // 12h — drop regenerable artifacts on completed but still-open issues
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
	PollInterval                   time.Duration
	HeartbeatInterval              time.Duration
	AgentTimeout                   time.Duration
	CodexSemanticInactivityTimeout time.Duration
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

	// Probe available agent CLIs
	agents := map[string]AgentEntry{}
	claudePath := envOrDefault("MULTICA_CLAUDE_PATH", "claude")
	if _, err := exec.LookPath(claudePath); err == nil {
		agents["claude"] = AgentEntry{
			Path:  claudePath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_CLAUDE_MODEL")),
		}
	}
	codexPath := envOrDefault("MULTICA_CODEX_PATH", "codex")
	if _, err := exec.LookPath(codexPath); err == nil {
		agents["codex"] = AgentEntry{
			Path:  codexPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_CODEX_MODEL")),
		}
	}
	opencodePath := envOrDefault("MULTICA_OPENCODE_PATH", "opencode")
	if _, err := exec.LookPath(opencodePath); err == nil {
		agents["opencode"] = AgentEntry{
			Path:  opencodePath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_OPENCODE_MODEL")),
		}
	}
	openclawPath := envOrDefault("MULTICA_OPENCLAW_PATH", "openclaw")
	if _, err := exec.LookPath(openclawPath); err == nil {
		agents["openclaw"] = AgentEntry{
			Path:  openclawPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_OPENCLAW_MODEL")),
		}
	}
	hermesPath := envOrDefault("MULTICA_HERMES_PATH", "hermes")
	if resolved, err := exec.LookPath(hermesPath); err == nil {
		agents["hermes"] = AgentEntry{
			Path:  resolved,
			Model: strings.TrimSpace(os.Getenv("MULTICA_HERMES_MODEL")),
		}
	} else if runtime.GOOS == "windows" {
		// Windows fallback: check the managed wrapper at %APPDATA%\Multica\bin\hermes.cmd.
		// Hermes is a Python agent typically installed inside WSL on Windows.
		// The daemon process (spawned by the Desktop app or background start)
		// does not load .env files, so MULTICA_HERMES_PATH is usually empty
		// and exec.LookPath("hermes") fails because there is no native
		// Windows binary named "hermes" on PATH.
		// A lightweight .cmd wrapper placed at this well-known location
		// delegates to the WSL-resident Hermes binary transparently.
		if appData := os.Getenv("APPDATA"); appData != "" {
			fallback := filepath.Join(appData, "Multica", "bin", "hermes.cmd")
			if _, statErr := os.Stat(fallback); statErr == nil {
				agents["hermes"] = AgentEntry{
					Path:         fallback,
					Model:        strings.TrimSpace(os.Getenv("MULTICA_HERMES_MODEL")),
					NeedsWSLPath: true, // discovered via WSL bridge — translate Windows paths to /mnt/...
				}
			}
		}
	}
	geminiPath := envOrDefault("MULTICA_GEMINI_PATH", "gemini")
	if _, err := exec.LookPath(geminiPath); err == nil {
		agents["gemini"] = AgentEntry{
			Path:  geminiPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_GEMINI_MODEL")),
		}
	}
	piPath := envOrDefault("MULTICA_PI_PATH", "pi")
	if _, err := exec.LookPath(piPath); err == nil {
		agents["pi"] = AgentEntry{
			Path:  piPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_PI_MODEL")),
		}
	}
	cursorPath := envOrDefault("MULTICA_CURSOR_PATH", "cursor-agent")
	if _, err := exec.LookPath(cursorPath); err == nil {
		agents["cursor"] = AgentEntry{
			Path:  cursorPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_CURSOR_MODEL")),
		}
	}
	copilotPath := envOrDefault("MULTICA_COPILOT_PATH", "copilot")
	if _, err := exec.LookPath(copilotPath); err == nil {
		agents["copilot"] = AgentEntry{
			Path:  copilotPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_COPILOT_MODEL")),
		}
	}
	kimiPath := envOrDefault("MULTICA_KIMI_PATH", "kimi")
	if _, err := exec.LookPath(kimiPath); err == nil {
		agents["kimi"] = AgentEntry{
			Path:  kimiPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_KIMI_MODEL")),
		}
	}
	kiroPath := envOrDefault("MULTICA_KIRO_PATH", "kiro-cli")
	if _, err := exec.LookPath(kiroPath); err == nil {
		agents["kiro"] = AgentEntry{
			Path:  kiroPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_KIRO_MODEL")),
		}
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
	workspacesRoot := strings.TrimSpace(os.Getenv("MULTICA_WORKSPACES_ROOT"))
	if overrides.WorkspacesRoot != "" {
		workspacesRoot = overrides.WorkspacesRoot
	}
	if workspacesRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve home directory: %w (set MULTICA_WORKSPACES_ROOT to override)", err)
		}
		if profile != "" {
			workspacesRoot = filepath.Join(home, "multica_workspaces_"+profile)
		} else {
			workspacesRoot = filepath.Join(home, "multica_workspaces")
		}
	}
	workspacesRoot, err = filepath.Abs(workspacesRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve absolute workspaces root: %w", err)
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
		HealthPort:                     healthPort,
		MaxConcurrentTasks:             maxConcurrentTasks,
		PollInterval:                   pollInterval,
		HeartbeatInterval:              heartbeatInterval,
		AgentTimeout:                   agentTimeout,
		CodexSemanticInactivityTimeout: codexSemanticInactivityTimeout,
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
