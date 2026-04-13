package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// RepoTypeGitHub and RepoTypeLocal are the recognized values of WorkspaceRepo.Type.
const (
	RepoTypeGitHub = "github"
	RepoTypeLocal  = "local"
)

// WorkspaceRepo is the canonical representation of a single entry in
// workspace.repos (JSONB).
type WorkspaceRepo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
	Description string `json:"description"`
}

// parseWorkspaceRepos decodes the JSONB array stored in workspace.repos.
// It accepts both the v1 ({url, description}) and v2 shapes, upgrading v1
// entries in memory so callers always receive the canonical v2 shape.
func parseWorkspaceRepos(raw []byte) ([]WorkspaceRepo, error) {
	if len(raw) == 0 {
		return []WorkspaceRepo{}, nil
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make([]WorkspaceRepo, 0, len(entries))
	for _, e := range entries {
		repo := WorkspaceRepo{
			ID:          strFromMap(e, "id"),
			Name:        strFromMap(e, "name"),
			Type:        strFromMap(e, "type"),
			URL:         strFromMap(e, "url"),
			LocalPath:   strFromMap(e, "local_path"),
			Description: strFromMap(e, "description"),
		}
		if repo.Type == "" {
			repo.Type = RepoTypeGitHub
		}
		if repo.ID == "" {
			repo.ID = uuid.NewString()
		}
		if repo.Name == "" {
			repo.Name = nameFromRepo(repo)
		}
		out = append(out, repo)
	}
	return out, nil
}

// validateAndNormalizeRepos validates a client-supplied repos payload and
// returns the canonical form. It auto-generates missing ids, normalizes local
// paths (expands ~, makes absolute), rejects duplicates, and enforces a
// minimal schema per repo type.
func validateAndNormalizeRepos(raw any) ([]WorkspaceRepo, error) {
	// Round-trip through JSON so we accept both struct and map shapes.
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("repos: invalid payload: %w", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(buf, &entries); err != nil {
		return nil, fmt.Errorf("repos: expected an array, got %T", raw)
	}

	seen := make(map[string]struct{}, len(entries))
	out := make([]WorkspaceRepo, 0, len(entries))
	for i, e := range entries {
		repo := WorkspaceRepo{
			ID:          strings.TrimSpace(strFromMap(e, "id")),
			Name:        strings.TrimSpace(strFromMap(e, "name")),
			Type:        strings.TrimSpace(strFromMap(e, "type")),
			URL:         strings.TrimSpace(strFromMap(e, "url")),
			LocalPath:   strings.TrimSpace(strFromMap(e, "local_path")),
			Description: strFromMap(e, "description"),
		}
		if repo.Type == "" {
			repo.Type = RepoTypeGitHub
		}
		if repo.Type != RepoTypeGitHub && repo.Type != RepoTypeLocal {
			return nil, fmt.Errorf("repos[%d]: invalid type %q (expected %q or %q)", i, repo.Type, RepoTypeGitHub, RepoTypeLocal)
		}

		switch repo.Type {
		case RepoTypeGitHub:
			if repo.URL == "" {
				return nil, fmt.Errorf("repos[%d]: url is required for github repos", i)
			}
			repo.LocalPath = ""
		case RepoTypeLocal:
			if repo.LocalPath == "" {
				return nil, fmt.Errorf("repos[%d]: local_path is required for local repos", i)
			}
			expanded, err := expandLocalPath(repo.LocalPath)
			if err != nil {
				return nil, fmt.Errorf("repos[%d]: %w", i, err)
			}
			repo.LocalPath = expanded
			repo.URL = ""
		}

		if repo.Name == "" {
			repo.Name = nameFromRepo(repo)
		}
		if repo.Name == "" {
			return nil, fmt.Errorf("repos[%d]: name is required", i)
		}
		if repo.ID == "" {
			repo.ID = uuid.NewString()
		}
		if _, dup := seen[repo.ID]; dup {
			return nil, fmt.Errorf("repos[%d]: duplicate id %q", i, repo.ID)
		}
		seen[repo.ID] = struct{}{}

		out = append(out, repo)
	}
	return out, nil
}

// expandLocalPath expands a leading ~ to the current user's home directory
// and converts the result to an absolute path. The path is NOT required to
// exist on the server's filesystem — local repos are resolved on the daemon
// host, not the API server — but it MUST be absolute so each daemon can
// interpret it unambiguously.
func expandLocalPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("local_path is empty")
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if p == "~" {
				p = home
			} else if strings.HasPrefix(p, "~/") {
				p = filepath.Join(home, p[2:])
			}
		}
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("local_path must be an absolute path, got %q", p)
	}
	return filepath.Clean(p), nil
}

// nameFromRepo derives a short display name from a repo entry.
func nameFromRepo(r WorkspaceRepo) string {
	switch r.Type {
	case RepoTypeLocal:
		if r.LocalPath != "" {
			return filepath.Base(r.LocalPath)
		}
	default:
		if r.URL != "" {
			name := strings.TrimSuffix(r.URL, "/")
			name = strings.TrimSuffix(name, ".git")
			// Trim to last path segment (works for https and ssh forms).
			if i := strings.LastIndexAny(name, "/:"); i >= 0 {
				name = name[i+1:]
			}
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
