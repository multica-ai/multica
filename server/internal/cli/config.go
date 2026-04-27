package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultCLIConfigPath = ".multica/config.json"

// CLIConfig holds persistent CLI settings.
type CLIConfig struct {
	ServerURL         string `json:"server_url,omitempty"`
	AppURL            string `json:"app_url,omitempty"`
	WorkspaceID       string `json:"workspace_id,omitempty"`
	Token             string `json:"token,omitempty"`
	UpdateManifestURL string `json:"update_manifest_url,omitempty"`
}

// CLIConfigPath returns the default path for the CLI config file.
func CLIConfigPath() (string, error) {
	return CLIConfigPathForProfile("")
}

// CLIConfigPathForInstance returns the config file path for the given
// instance selector. Explicit config paths take precedence over profiles.
func CLIConfigPathForInstance(profile, configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		return normalizeConfigPath(configPath)
	}
	return CLIConfigPathForProfile(profile)
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

// StateDirForInstance returns the base directory for an instance's local
// state files. Explicit config paths isolate state in the config file's
// parent directory; otherwise profile-based directories are used.
func StateDirForInstance(profile, configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		path, err := CLIConfigPathForInstance(profile, configPath)
		if err != nil {
			return "", err
		}
		return filepath.Dir(path), nil
	}
	return ProfileDir(profile)
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

// LoadCLIConfigForInstance reads the CLI config for the given instance
// selector. Explicit config paths take precedence over profiles.
func LoadCLIConfigForInstance(profile, configPath string) (CLIConfig, error) {
	path, err := CLIConfigPathForInstance(profile, configPath)
	if err != nil {
		return CLIConfig{}, err
	}
	return loadCLIConfigAtPath(path)
}

// LoadCLIConfigForProfile reads the CLI config for the given profile.
func LoadCLIConfigForProfile(profile string) (CLIConfig, error) {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return CLIConfig{}, err
	}
	return loadCLIConfigAtPath(path)
}

func loadCLIConfigAtPath(path string) (CLIConfig, error) {
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

// SaveCLIConfigForInstance writes the CLI config for the given instance
// selector. Explicit config paths take precedence over profiles.
func SaveCLIConfigForInstance(cfg CLIConfig, profile, configPath string) error {
	path, err := CLIConfigPathForInstance(profile, configPath)
	if err != nil {
		return err
	}
	return saveCLIConfigAtPath(cfg, path)
}

// SaveCLIConfigForProfile writes the CLI config for the given profile.
func SaveCLIConfigForProfile(cfg CLIConfig, profile string) error {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return err
	}
	return saveCLIConfigAtPath(cfg, path)
}

func saveCLIConfigAtPath(cfg CLIConfig, path string) error {
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

func normalizeConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("config path is empty")
	}
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for config path: %w", err)
		}
		switch expanded {
		case "~":
			expanded = home
		case "~/":
			expanded = home + string(filepath.Separator)
		default:
			if strings.HasPrefix(expanded, "~/") {
				expanded = filepath.Join(home, expanded[2:])
			}
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve absolute config path: %w", err)
	}
	return filepath.Clean(abs), nil
}
