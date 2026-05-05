package mcp

// CEREBRO-PATCH(mcp-repo-config): cerebro modification of upstream file

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RepoBinding maps a git repo root to a workspace and project.
type RepoBinding struct {
	WorkspaceID   string `json:"workspace_id"`
	ProjectID     string `json:"project_id,omitempty"`
	ActiveSession *ActiveSession `json:"active_session,omitempty"`
}

// ActiveSession tracks an attached work session that persists across MCP restarts.
type ActiveSession struct {
	WorkSessionID string    `json:"work_session_id"`
	IssueID       string    `json:"issue_id"`
	AttachedAt    time.Time `json:"attached_at"`
}

// MaxSessionAge is the maximum age of a persisted session before it's considered stale.
const MaxSessionAge = 24 * time.Hour

// IsStale returns true if the session is older than MaxSessionAge.
func (s *ActiveSession) IsStale() bool {
	return time.Since(s.AttachedAt) > MaxSessionAge
}

// RepoBindings is the on-disk format for ~/.multica/repos.json.
type RepoBindings map[string]RepoBinding

func repoConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".multica", "repos.json"), nil
}

// LoadRepoBindings reads the repo bindings from disk.
func LoadRepoBindings() (RepoBindings, error) {
	path, err := repoConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RepoBindings{}, nil
		}
		return nil, err
	}
	var bindings RepoBindings
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, fmt.Errorf("parse repos.json: %w", err)
	}
	return bindings, nil
}

// SaveRepoBindings writes the repo bindings to disk atomically.
func SaveRepoBindings(bindings RepoBindings) error {
	path, err := repoConfigPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bindings, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".repos-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// DetectGitRoot returns the root of the current git repository, or empty string if not in a repo.
func DetectGitRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DetectGitRemote returns the normalized remote URL (e.g. "github.com/org/repo") for the origin remote.
func DetectGitRemote(gitRoot string) string {
	out, err := exec.Command("git", "-C", gitRoot, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return NormalizeRepoURL(strings.TrimSpace(string(out)))
}

// NormalizeRepoURL converts various git remote formats to a canonical form: "github.com/org/repo".
func NormalizeRepoURL(raw string) string {
	// SSH: git@github.com:org/repo.git → github.com/org/repo
	if strings.HasPrefix(raw, "git@") {
		raw = strings.TrimPrefix(raw, "git@")
		raw = strings.Replace(raw, ":", "/", 1)
	}
	// HTTPS: https://github.com/org/repo.git → github.com/org/repo
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimSuffix(raw, ".git")
	return raw
}
