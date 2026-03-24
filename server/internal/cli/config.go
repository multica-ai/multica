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
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// CLIConfigPath returns the default path for the CLI config file.
func CLIConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve CLI config path: %w", err)
	}
	return filepath.Join(home, defaultCLIConfigPath), nil
}

// LoadCLIConfig reads the CLI config from disk.
func LoadCLIConfig() (CLIConfig, error) {
	path, err := CLIConfigPath()
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

// LoadWorkspaceIDFromDaemonConfig reads workspace_id from ~/.multica/daemon.json
// as a fallback when it's not set in the CLI config.
func LoadWorkspaceIDFromDaemonConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".multica/daemon.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	return cfg.WorkspaceID
}

// SaveCLIConfig writes the CLI config to disk.
func SaveCLIConfig(cfg CLIConfig) error {
	path, err := CLIConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create CLI config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI config: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write CLI config: %w", err)
	}
	return nil
}
