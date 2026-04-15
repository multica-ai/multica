package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultCLIConfigPath = ".multica/config.json"

var cliConfigMu sync.Mutex

const (
	cliConfigLockTimeout    = 2 * time.Minute
	cliConfigLockRetryDelay = 50 * time.Millisecond
	cliConfigLockStaleAfter = 10 * time.Minute
)

// WatchedWorkspace represents a workspace the daemon should monitor for tasks.
type WatchedWorkspace struct {
	ID        string              `json:"id"`
	Name      string              `json:"name,omitempty"`
	SkillSync *WorkspaceSkillSync `json:"skill_sync,omitempty"`
}

// WorkspaceSkillSync stores the persistent state for workspace skill syncing.
type WorkspaceSkillSync struct {
	Dir           string `json:"dir"`
	Enabled       bool   `json:"enabled"`
	DeleteManaged bool   `json:"delete_managed,omitempty"`
	LastSyncAt    string `json:"last_sync_at,omitempty"`
	LastSyncError string `json:"last_sync_error,omitempty"`
}

// CLIConfig holds persistent CLI settings.
type CLIConfig struct {
	ServerURL         string             `json:"server_url,omitempty"`
	AppURL            string             `json:"app_url,omitempty"`
	WorkspaceID       string             `json:"workspace_id,omitempty"`
	Token             string             `json:"token,omitempty"`
	WatchedWorkspaces []WatchedWorkspace `json:"watched_workspaces,omitempty"`
}

// AddWatchedWorkspace adds a workspace to the watch list. Returns true if added.
func (c *CLIConfig) AddWatchedWorkspace(id, name string) bool {
	for _, w := range c.WatchedWorkspaces {
		if w.ID == id {
			return false
		}
	}
	c.WatchedWorkspaces = append(c.WatchedWorkspaces, WatchedWorkspace{ID: id, Name: name})
	return true
}

// RemoveWatchedWorkspace removes a workspace from the watch list. Returns true if found.
func (c *CLIConfig) RemoveWatchedWorkspace(id string) bool {
	for i, w := range c.WatchedWorkspaces {
		if w.ID == id {
			c.WatchedWorkspaces = append(c.WatchedWorkspaces[:i], c.WatchedWorkspaces[i+1:]...)
			return true
		}
	}
	return false
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

// UpdateCLIConfigForProfile serializes in-process config mutations by taking a
// process-local lock plus a cross-process lock file around load-modify-save of
// the full CLI config file.
func UpdateCLIConfigForProfile(profile string, mutate func(*CLIConfig) error) error {
	cliConfigMu.Lock()
	defer cliConfigMu.Unlock()

	unlock, err := LockCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	defer unlock()

	cfg, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	if err := mutate(&cfg); err != nil {
		return err
	}
	return SaveCLIConfigForProfile(cfg, profile)
}

// UpdateWorkspaceSkillSyncStatus updates the persisted sync status for one watched workspace.
// On success, it records last_sync_at and clears last_sync_error.
// On failure, it records last_sync_error and preserves the existing last_sync_at.
func UpdateWorkspaceSkillSyncStatus(profile, workspaceID string, syncedAt time.Time, syncErr error) error {
	return UpdateCLIConfigForProfile(profile, func(cfg *CLIConfig) error {
		for i := range cfg.WatchedWorkspaces {
			if cfg.WatchedWorkspaces[i].ID != workspaceID {
				continue
			}

			if cfg.WatchedWorkspaces[i].SkillSync == nil {
				cfg.WatchedWorkspaces[i].SkillSync = &WorkspaceSkillSync{}
			}

			if syncErr != nil {
				cfg.WatchedWorkspaces[i].SkillSync.LastSyncError = syncErr.Error()
			} else {
				cfg.WatchedWorkspaces[i].SkillSync.LastSyncAt = syncedAt.UTC().Format(time.RFC3339)
				cfg.WatchedWorkspaces[i].SkillSync.LastSyncError = ""
			}

			return nil
		}

		return fmt.Errorf("workspace %s is not being watched", workspaceID)
	})
}

// LockCLIConfigForProfile acquires the cross-process lock used to serialize CLI
// config mutation cycles for the given profile.
func LockCLIConfigForProfile(profile string) (func(), error) {
	lockPath, err := cliConfigLockPathForProfile(profile)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create CLI config lock directory: %w", err)
	}

	deadline := time.Now().Add(cliConfigLockTimeout)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "pid=%d time=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = file.Close()
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create CLI config lock file: %w", err)
		}

		info, statErr := os.Stat(lockPath)
		if statErr == nil && time.Since(info.ModTime()) > cliConfigLockStaleAfter {
			if removeErr := os.Remove(lockPath); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				continue
			}
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for CLI config lock")
		}
		time.Sleep(cliConfigLockRetryDelay)
	}
}

func cliConfigLockPathForProfile(profile string) (string, error) {
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}
