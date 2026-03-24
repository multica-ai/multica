package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultServerURL         = "ws://localhost:8080/ws"
	DefaultDaemonConfigPath  = ".multica/daemon.json"
	DefaultPollInterval      = 3 * time.Second
	DefaultHeartbeatInterval = 15 * time.Second
	DefaultAgentTimeout      = 20 * time.Minute
	DefaultRuntimeName       = "Local Agent"
)

// Config holds all daemon configuration.
type Config struct {
	ServerBaseURL     string
	ConfigPath        string
	WorkspaceID       string
	DaemonID          string
	DeviceName        string
	RuntimeName       string
	Agents            map[string]AgentEntry // "claude" -> entry, "codex" -> entry
	ReposRoot         string                // parent directory containing all repos
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	AgentTimeout      time.Duration
}

// Overrides allows CLI flags to override environment variables and defaults.
// Zero values are ignored and the env/default value is used instead.
type Overrides struct {
	ServerURL         string
	WorkspaceID       string
	ReposRoot         string
	ConfigPath        string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	AgentTimeout      time.Duration
	DaemonID          string
	DeviceName        string
	RuntimeName       string
}

// LoadConfig builds the daemon configuration from environment variables,
// persisted config, and optional CLI flag overrides.
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

	// Config path
	rawConfigPath := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_CONFIG"))
	if overrides.ConfigPath != "" {
		rawConfigPath = overrides.ConfigPath
	}
	configPath, err := resolveDaemonConfigPath(rawConfigPath)
	if err != nil {
		return Config{}, err
	}

	// Load persisted config
	persisted, err := LoadPersistedConfig(configPath)
	if err != nil {
		return Config{}, err
	}

	// Workspace ID: override > env > persisted
	workspaceID := strings.TrimSpace(os.Getenv("MULTICA_WORKSPACE_ID"))
	if workspaceID == "" {
		workspaceID = persisted.WorkspaceID
	}
	if overrides.WorkspaceID != "" {
		workspaceID = overrides.WorkspaceID
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
	if len(agents) == 0 {
		return Config{}, fmt.Errorf("no agent CLI found: install claude or codex and ensure it is on PATH")
	}

	// Host info
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "local-machine"
	}

	// Repos root: override > env > cwd
	reposRoot := strings.TrimSpace(os.Getenv("MULTICA_REPOS_ROOT"))
	if overrides.ReposRoot != "" {
		reposRoot = overrides.ReposRoot
	}
	if reposRoot == "" {
		reposRoot, err = os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}
	reposRoot, err = filepath.Abs(reposRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve absolute repos root: %w", err)
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

	// String overrides
	daemonID := envOrDefault("MULTICA_DAEMON_ID", host)
	if overrides.DaemonID != "" {
		daemonID = overrides.DaemonID
	}

	deviceName := envOrDefault("MULTICA_DAEMON_DEVICE_NAME", host)
	if overrides.DeviceName != "" {
		deviceName = overrides.DeviceName
	}

	runtimeName := envOrDefault("MULTICA_AGENT_RUNTIME_NAME", DefaultRuntimeName)
	if overrides.RuntimeName != "" {
		runtimeName = overrides.RuntimeName
	}

	return Config{
		ServerBaseURL:     serverBaseURL,
		ConfigPath:        configPath,
		WorkspaceID:       workspaceID,
		DaemonID:          daemonID,
		DeviceName:        deviceName,
		RuntimeName:       runtimeName,
		Agents:            agents,
		ReposRoot:         reposRoot,
		PollInterval:      pollInterval,
		HeartbeatInterval: heartbeatInterval,
		AgentTimeout:      agentTimeout,
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

func resolveDaemonConfigPath(raw string) (string, error) {
	if raw != "" {
		return filepath.Abs(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve daemon config path: %w", err)
	}
	return filepath.Join(home, DefaultDaemonConfigPath), nil
}

// LoadPersistedConfig reads the daemon config from disk.
func LoadPersistedConfig(path string) (PersistedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PersistedConfig{}, nil
		}
		return PersistedConfig{}, fmt.Errorf("read daemon config: %w", err)
	}
	var cfg PersistedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return PersistedConfig{}, fmt.Errorf("parse daemon config: %w", err)
	}
	return cfg, nil
}

// SavePersistedConfig writes the daemon config to disk.
func SavePersistedConfig(path string, cfg PersistedConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create daemon config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode daemon config: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write daemon config: %w", err)
	}
	return nil
}
