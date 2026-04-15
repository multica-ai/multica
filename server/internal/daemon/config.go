package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const (
	DefaultServerURL             = "ws://localhost:8080/ws"
	DefaultPollInterval          = 3 * time.Second
	DefaultHeartbeatInterval     = 15 * time.Second
	DefaultAgentTimeout          = 2 * time.Hour
	DefaultRuntimeName           = "Local Agent"
	DefaultConfigReloadInterval  = 5 * time.Second
	DefaultWorkspaceSyncInterval = 30 * time.Second
	DefaultHealthPort            = 19514
	DefaultMaxConcurrentTasks    = 20
	DefaultGCInterval            = 1 * time.Hour
	DefaultGCTTL                 = 5 * 24 * time.Hour  // 5 days
	DefaultGCOrphanTTL           = 30 * 24 * time.Hour // 30 days
	ollamaProbeTimeout           = 2 * time.Second
)

// Config holds all daemon configuration.
type Config struct {
	ServerBaseURL      string
	DaemonID           string
	DeviceName         string
	RuntimeName        string
	CLIVersion         string                // multica CLI version (e.g. "0.1.13")
	LaunchedBy         string                // "desktop" when spawned by the Electron app, empty for standalone
	Profile            string                // profile name (empty = default)
	Agents             map[string]AgentEntry // keyed by provider: claude, codex, opencode, openclaw, hermes, gemini, ollama
	WorkspacesRoot     string                // base path for execution envs (default: ~/multica_workspaces)
	KeepEnvAfterTask   bool                  // preserve env after task for debugging
	HealthPort         int                   // local HTTP port for health checks (default: 19514)
	MaxConcurrentTasks int                   // max tasks running in parallel (default: 20)
	GCEnabled          bool                  // enable periodic workspace garbage collection (default: true)
	GCInterval         time.Duration         // how often the GC loop runs (default: 1h)
	GCTTL              time.Duration         // clean dirs whose issue is done/canceled and updated_at < now()-TTL (default: 5d)
	GCOrphanTTL        time.Duration         // clean orphan dirs (no meta or unknown issue) older than this (default: 30d)
	PollInterval       time.Duration
	HeartbeatInterval  time.Duration
	AgentTimeout       time.Duration
}

// Overrides allows CLI flags to override environment variables and defaults.
// Zero values are ignored and the env/default value is used instead.
type Overrides struct {
	ServerURL          string
	WorkspacesRoot     string
	PollInterval       time.Duration
	HeartbeatInterval  time.Duration
	AgentTimeout       time.Duration
	MaxConcurrentTasks int
	DaemonID           string
	DeviceName         string
	RuntimeName        string
	Profile            string // profile name (empty = default)
	HealthPort         int    // health check port (0 = use default)
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
	if _, err := exec.LookPath(hermesPath); err == nil {
		agents["hermes"] = AgentEntry{
			Path:  hermesPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_HERMES_MODEL")),
		}
	}
	geminiPath := envOrDefault("MULTICA_GEMINI_PATH", "gemini")
	if _, err := exec.LookPath(geminiPath); err == nil {
		agents["gemini"] = AgentEntry{
			Path:  geminiPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_GEMINI_MODEL")),
		}
	}

	// Ollama is HTTP-based, not a CLI — probe the /api/tags endpoint
	// rather than looking for a binary on PATH. The resolved base URL is
	// stored in AgentEntry.Path and read back by the ollama backend.
	ollamaHost := strings.TrimRight(envOrDefault("MULTICA_OLLAMA_HOST", agent.DefaultOllamaHost), "/")
	ollamaModel := strings.TrimSpace(os.Getenv("MULTICA_OLLAMA_MODEL"))
	if ollamaModel != "" && probeOllamaReachable(ollamaHost) {
		agents["ollama"] = AgentEntry{
			Path:  ollamaHost,
			Model: ollamaModel,
		}
		// Best-effort capability check — warn if the model doesn't
		// advertise tool support. Don't block registration: the user
		// may intentionally want a chat-only model, and an /api/show
		// failure shouldn't stop the daemon from starting. Scope the
		// cancel to the narrowest block so it matches the pattern in
		// probeOllamaReachable rather than hanging around for the rest
		// of LoadConfig.
		capCtx, capCancel := context.WithTimeout(context.Background(), ollamaProbeTimeout)
		supports, capErr := agent.OllamaModelSupportsTools(capCtx, ollamaHost, ollamaModel)
		capCancel()
		if capErr == nil && !supports {
			fmt.Fprintf(os.Stderr,
				"WARNING: Ollama model %q does not declare 'tools' capability. "+
					"Issue-assignment tasks will degrade to text-only responses. "+
					"Use a tool-capable model (e.g. gemma4:latest, qwen2.5-coder:1.5b, llama3.1).\n",
				ollamaModel)
		}
	}

	if len(agents) == 0 {
		return Config{}, fmt.Errorf(
			"no agent runtime available: install claude/codex/opencode/openclaw/hermes/gemini on PATH, " +
				"or set MULTICA_OLLAMA_MODEL with a reachable Ollama server " +
				"(MULTICA_OLLAMA_HOST, default " + agent.DefaultOllamaHost + ")",
		)
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

	maxConcurrentTasks, err := intFromEnv("MULTICA_DAEMON_MAX_CONCURRENT_TASKS", DefaultMaxConcurrentTasks)
	if err != nil {
		return Config{}, err
	}
	if overrides.MaxConcurrentTasks > 0 {
		maxConcurrentTasks = overrides.MaxConcurrentTasks
	}

	// Profile
	profile := overrides.Profile

	// String overrides
	daemonID := envOrDefault("MULTICA_DAEMON_ID", host)
	if overrides.DaemonID != "" {
		daemonID = overrides.DaemonID
	}
	// NOTE: daemon_id is intentionally stable (hostname or explicit override).
	// The unique constraint (workspace_id, daemon_id, provider) already prevents
	// collisions within the same workspace. Appending the profile name caused
	// duplicate runtimes when users switched profiles.

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

	return Config{
		ServerBaseURL:      serverBaseURL,
		DaemonID:           daemonID,
		DeviceName:         deviceName,
		RuntimeName:        runtimeName,
		Profile:            profile,
		Agents:             agents,
		WorkspacesRoot:     workspacesRoot,
		KeepEnvAfterTask:   keepEnv,
		GCEnabled:          gcEnabled,
		GCInterval:         gcInterval,
		GCTTL:              gcTTL,
		GCOrphanTTL:        gcOrphanTTL,
		HealthPort:         healthPort,
		MaxConcurrentTasks: maxConcurrentTasks,
		PollInterval:       pollInterval,
		HeartbeatInterval:  heartbeatInterval,
		AgentTimeout:       agentTimeout,
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

// probeOllamaReachable returns true if an Ollama server responds 2xx to
// GET {baseURL}/api/tags within ollamaProbeTimeout. The call is cheap
// (lists local model tags) and exists on every Ollama version.
func probeOllamaReachable(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), ollamaProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
