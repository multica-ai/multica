package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultCLIConfigPath = ".multica/config.json"

// CLIConfig holds persistent CLI settings.
type CLIConfig struct {
	ServerURL   string `json:"server_url,omitempty"`
	AppURL      string `json:"app_url,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Token       string `json:"token,omitempty"`

	// DeviceName is the human-readable label shown in the server's Runtimes
	// UI for the daemon started with this profile. When multiple daemons run
	// on the same host under different profiles (typical for shared servers
	// where one Linux user hosts one profile), os.Hostname() collapses them
	// all onto one indistinguishable "VM-…" row. Setting device_name per
	// profile — usually to "<host>-<profile>" or the operator's real name —
	// makes the runtimes list navigable.
	//
	// Resolution precedence (highest wins): --device-name flag,
	// MULTICA_DAEMON_DEVICE_NAME env, this field, os.Hostname().
	DeviceName string `json:"device_name,omitempty"`

	// RuntimeName is the daemon's own runtime label ("Runtime display name"
	// in the UI). Same shape as DeviceName but scopes to the runtime row
	// rather than the host: users who run several distinct daemons per
	// profile (rare, but supported) can pin different runtime names for
	// each. Resolution precedence (highest wins): --runtime-name flag,
	// MULTICA_AGENT_RUNTIME_NAME env, this field, the built-in
	// DefaultRuntimeName.
	RuntimeName string `json:"runtime_name,omitempty"`

	// MaxConcurrentTasks caps the number of task executions the daemon
	// processes in parallel. Persist here to avoid re-passing
	// --max-concurrent-tasks on every daemon start / auto-restart. 0 means
	// "not set — use env / built-in default". Resolution precedence
	// (highest wins): --max-concurrent-tasks flag,
	// MULTICA_DAEMON_MAX_CONCURRENT_TASKS env, this field, default.
	MaxConcurrentTasks int `json:"max_concurrent_tasks,omitempty"`

	// PollInterval is how often the daemon polls the server for new tasks
	// (Go duration string, e.g. "10s", "500ms"). Same persist-once
	// motivation as MaxConcurrentTasks. Empty ("") means "not set — use
	// env / built-in default". Only strictly positive durations are
	// meaningful here: `config set poll_interval` rejects non-positive
	// values (including "0s") and un-parseable durations at write time,
	// so a value that reaches this field is always well-formed. Use
	// `config set poll_interval ""` to clear a previously persisted
	// value. Resolution precedence (highest wins): --poll-interval flag,
	// MULTICA_DAEMON_POLL_INTERVAL env, this field, DefaultPollInterval.
	PollInterval string `json:"poll_interval,omitempty"`

	// HeartbeatInterval is how often the daemon sends heartbeat pings to
	// the server (Go duration string). Same persist-once motivation as
	// PollInterval. Empty ("") means "not set — use env / built-in
	// default"; `config set heartbeat_interval` rejects zero and
	// negative durations. Resolution precedence: --heartbeat-interval
	// flag, MULTICA_DAEMON_HEARTBEAT_INTERVAL env, this field,
	// DefaultHeartbeatInterval.
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`

	// AgentTimeout is the absolute wall-clock cap per agent run (Go
	// duration string). Unlike the other duration knobs, "0s" is a
	// meaningful, non-default value here: it explicitly disables the cap
	// so a run is bounded only by the inactivity watchdogs (see
	// DefaultAgentTimeout). To distinguish "not persisted" from
	// "persisted as disabled", we use a pointer: nil = not set, non-nil
	// = use this string (which may be "0s"). `config set agent_timeout`
	// accepts any non-negative Go duration; "" clears the persisted
	// value. Resolution precedence: --agent-timeout flag (including
	// explicit 0), MULTICA_AGENT_TIMEOUT env, this field,
	// DefaultAgentTimeout.
	AgentTimeout *string `json:"agent_timeout,omitempty"`

	// CodexSemanticInactivityTimeout is the Codex-specific inactivity
	// watchdog window (Go duration string). Persist-once semantics match
	// PollInterval: empty = not set, positive = use this value.
	// Resolution precedence: --codex-semantic-inactivity-timeout flag,
	// MULTICA_CODEX_SEMANTIC_INACTIVITY_TIMEOUT env, this field,
	// DefaultCodexSemanticInactivityTimeout.
	CodexSemanticInactivityTimeout string `json:"codex_semantic_inactivity_timeout,omitempty"`

	// CodexHandshakeTimeout caps the Codex app-server startup RPCs (Go
	// duration string). Persist-once semantics match PollInterval.
	// Resolution precedence: --codex-handshake-timeout flag,
	// MULTICA_CODEX_HANDSHAKE_TIMEOUT env, this field,
	// DefaultCodexHandshakeTimeout.
	CodexHandshakeTimeout string `json:"codex_handshake_timeout,omitempty"`

	// DisableAutoUpdate, when true, turns off the daemon's periodic CLI
	// self-update poll. Only a single direction is persistable — the
	// --no-auto-update flag is likewise one-way — because the env/default
	// already resolves to enabled on Multica Cloud. Absent / false means
	// "let env/default decide". Resolution precedence:
	// --no-auto-update flag, MULTICA_DAEMON_AUTO_UPDATE=false env, this
	// field, cloud/self-host default.
	DisableAutoUpdate bool `json:"disable_auto_update,omitempty"`

	// AutoUpdateCheckInterval is how often the daemon polls GitHub for a
	// newer CLI release (Go duration string). Persist-once semantics
	// match PollInterval. Resolution precedence:
	// --auto-update-interval flag, MULTICA_DAEMON_AUTO_UPDATE_INTERVAL
	// env, this field, DefaultAutoUpdateCheckInterval.
	AutoUpdateCheckInterval string `json:"auto_update_check_interval,omitempty"`

	// Backends contains per-backend overrides for users who want to point
	// the daemon at non-default tool installations (e.g. an OpenClaw bundled
	// inside another desktop app, or multiple isolated profiles on the same
	// machine). Empty / absent means "discover from PATH and use vendor
	// defaults" — the historical behavior. See issue #3875.
	Backends *BackendOverrides `json:"backends,omitempty"`

	// ProfileCommandOverrides is a per-machine map of custom runtime
	// profile_id -> absolute executable path (MUL-3284). A workspace custom
	// runtime profile records the command_name the daemon resolves on PATH,
	// but the same logical profile may live at a different path on each
	// machine (or not be on PATH at all). This map lets an operator pin the
	// exact binary for a profile on this host via
	// `multica runtime profile set-path`; the daemon prefers it over the
	// PATH lookup in appendProfileRuntimes. Empty / absent means "resolve the
	// profile's command_name on PATH" — the default behavior. The mapping is
	// intentionally local-only (it is never sent to the server) because the
	// path is a property of this machine, not of the shared profile.
	ProfileCommandOverrides map[string]string `json:"profile_command_overrides,omitempty"`
}

// BackendOverrides holds per-backend configuration overrides. Each field is
// optional; nil means "no override for this backend". Keep new fields additive
// and tagged with `json:",omitempty"` so empty values do not change the saved
// config shape. Unknown-key preservation is a separate forward-compat concern:
// Go's encoding/json drops fields that are not represented in this struct on
// load/save round-trip (see TestCLIConfig_UnknownFieldsArePreserved).
type BackendOverrides struct {
	OpenClaw *OpenClawOverride `json:"openclaw,omitempty"`
}

// OpenClawOverride configures the OpenClaw backend. All fields are optional;
// empty values fall through to the existing discovery path (PATH lookup for
// BinaryPath, default `~/.openclaw/` for StateDir).
//
// Resolution precedence (env beats config beats default, for back-compat):
//
//	BinaryPath: MULTICA_OPENCLAW_PATH (env)  > backends.openclaw.binary_path > PATH lookup
//	StateDir:   OPENCLAW_STATE_DIR (env)     > backends.openclaw.state_dir   > OpenClaw's built-in default (~/.openclaw)
//
// The StateDir env var here is OpenClaw's own OPENCLAW_STATE_DIR — NOT a new
// MULTICA_OPENCLAW_STATE_DIR. Rationale: OpenClaw already honors its own env
// var, the daemon already forwards inherited env to spawned children via
// `mergeEnv`, and a user who exports OPENCLAW_STATE_DIR in their shell
// already gets the right behavior with zero daemon changes today. This field
// is purely additive: when set, the daemon injects OPENCLAW_STATE_DIR=<value>
// into the spawned child's env unless the user already exported one upstream.
// (If a future use case needs daemon-namespaced isolation distinct from
// OpenClaw's own env, MULTICA_OPENCLAW_STATE_DIR can be layered on top
// without breaking this contract — see #3875 discussion.)
//
// Setting StateDir is the fix for the long-standing usability gap where
// users with non-default OpenClaw installations — multiple isolated
// profiles (dev/staging/prod, multiple accounts), containerized / CI
// deployments where ~/.openclaw isn't writable, or third-party desktop
// apps that bundle their own OpenClaw runtime — had to write a wrapper
// shell script to inject OPENCLAW_STATE_DIR + run `launchctl setenv`
// for GUI-launched daemons. With this field, those workarounds become
// unnecessary.
type OpenClawOverride struct {
	BinaryPath string `json:"binary_path,omitempty"`
	StateDir   string `json:"state_dir,omitempty"`
}

// CLIConfigPath returns the default path for the CLI config file.
func CLIConfigPath() (string, error) {
	return CLIConfigPathForProfile("")
}

// CLIConfigPathForProfile returns the config file path for the given profile.
// An empty profile returns the default path (~/.multica/config.json).
// A named profile returns ~/.multica/profiles/<name>/config.json.
func CLIConfigPathForProfile(profile string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve CLI config path: %w", err)
	}
	if profile == "" {
		return filepath.Join(home, defaultCLIConfigPath), nil
	}
	return filepath.Join(home, ".multica", "profiles", profile, "config.json"), nil
}

// ProfileDir returns the base directory for a profile's state files (pid, log).
// An empty profile returns ~/.multica/. A named profile returns ~/.multica/profiles/<name>/.
func ProfileDir(profile string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve profile dir: %w", err)
	}
	if profile == "" {
		return filepath.Join(home, ".multica"), nil
	}
	return filepath.Join(home, ".multica", "profiles", profile), nil
}

// LoadCLIConfig reads the CLI config from disk (default profile).
func LoadCLIConfig() (CLIConfig, error) {
	return LoadCLIConfigForProfile("")
}

// LoadCLIConfigForProfile reads the CLI config for the given profile.
func LoadCLIConfigForProfile(profile string) (CLIConfig, error) {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return CLIConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CLIConfig{}, nil
		}
		return CLIConfig{}, fmt.Errorf("read CLI config: %w", err)
	}
	var cfg CLIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CLIConfig{}, fmt.Errorf("parse CLI config: %w", err)
	}
	return cfg, nil
}

// SaveCLIConfig writes the CLI config to disk atomically (default profile).
func SaveCLIConfig(cfg CLIConfig) error {
	return SaveCLIConfigForProfile(cfg, "")
}

// SaveCLIConfigForProfile writes the CLI config for the given profile.
func SaveCLIConfigForProfile(cfg CLIConfig, profile string) error {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create CLI config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI config: %w", err)
	}

	// Write to a temp file in the same directory, then rename for atomicity.
	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config file: %w", err)
	}
	return nil
}
