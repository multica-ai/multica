package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// taskProjectSpace resolves the server-managed directory for this task only
// when the claim contains the immutable project_space resource. It never
// falls back to a sibling project or an inferred directory.
func (d *Daemon) taskProjectSpace(task Task) (string, error) {
	if task.ProjectID == "" {
		return "", nil
	}
	found := false
	for _, resource := range task.ProjectResources {
		if resource.ResourceType != "project_space" {
			continue
		}
		var ref struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(resource.ResourceRef, &ref) != nil || ref.Version != 1 {
			return "", errors.New("project space resource is invalid")
		}
		found = true
		break
	}
	if !found {
		return "", nil
	}
	if !safeProjectSpaceID(task.WorkspaceID) || !safeProjectSpaceID(task.ProjectID) {
		return "", errors.New("project space identity is invalid")
	}
	root, err := filepath.Abs(d.cfg.ProjectSpaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project space root: %w", err)
	}
	target := filepath.Join(root, "workspaces", task.WorkspaceID, "projects", task.ProjectID)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("project space escapes configured root")
	}
	info, err := os.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("project space has not been initialized by the backend")
	}
	if err != nil {
		return "", fmt.Errorf("inspect project space: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("project space is not a directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve project space root symlinks: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve project space symlinks: %w", err)
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(os.PathSeparator)) {
		return "", errors.New("project space symlink escapes configured root")
	}
	probeDir := filepath.Join(resolvedTarget, ".ai")
	if err := os.MkdirAll(probeDir, 0o750); err != nil {
		return "", fmt.Errorf("prepare project space write probe: %w", err)
	}
	probe, err := os.CreateTemp(probeDir, ".daemon-write-*")
	if err != nil {
		return "", fmt.Errorf("project space is not writable: %w", err)
	}
	name := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close project space write probe: %w", closeErr)
	}
	if removeErr := os.Remove(name); removeErr != nil {
		return "", fmt.Errorf("clean project space write probe: %w", removeErr)
	}
	return resolvedTarget, nil
}

func safeProjectSpaceID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' ||
			r == '_' {
			continue
		}
		return false
	}
	return true
}
