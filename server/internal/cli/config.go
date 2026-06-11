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

	// Backends contains per-backend overrides for users who want to point
	// the daemon at non-default tool installations (e.g. an OpenClaw bundled
	// inside another desktop app, or multiple isolated profiles on the same
	// machine). Empty / absent means "discover from PATH and use vendor
	// defaults" — the historical behavior. See issue #3875.
	Backends *BackendOverrides `json:"backends,omitempty"`
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
