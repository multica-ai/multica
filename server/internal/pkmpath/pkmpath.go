// Package pkmpath validates and normalizes the per-workspace pkm_path
// setting. The path is user-supplied through the workspace settings API and
// is later joined with a server-side allowlist root (MULTICA_PKM_ROOT) to
// access markdown files on the host. Every value that lands on the host
// filesystem must go through Normalize first.
package pkmpath

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// EnvAllowlistRoot is the env var that pins the on-host directory under
// which all workspace pkm_path values must resolve.
const EnvAllowlistRoot = "MULTICA_PKM_ROOT"

// AllowlistRoot returns the configured allowlist root. An empty string means
// no root is configured; the filesystem-serving handlers (PR3) treat that as
// "feature disabled" and refuse to read.
func AllowlistRoot() string {
	return strings.TrimSpace(os.Getenv(EnvAllowlistRoot))
}

// Normalize validates and cleans a workspace pkm_path against an allowlist
// root.
//
// The user-facing pkm_path is a slash-rooted path interpreted relative to
// allowlistRoot — e.g. pkmPath="/PKM-CUONG/GROWTH/PROJECTS" with
// allowlistRoot="/root" resolves to "/root/PKM-CUONG/GROWTH/PROJECTS" on
// disk. Normalize returns the cleaned slash-rooted form (the value to store
// in workspace.settings.pkm_path), not the resolved on-disk path.
//
// Validation rules:
//   - pkmPath must be non-empty and start with "/".
//   - No segment may equal "..".
//   - cleaned form must not be "/" (callers may not point PKM at the root).
//   - When allowlistRoot is non-empty, the resolved path must remain
//     strictly inside it (so symlink-free traversal cannot escape).
//
// When allowlistRoot is empty, only the shape checks apply; the filesystem
// layer is responsible for refusing access in that case.
func Normalize(pkmPath, allowlistRoot string) (string, error) {
	if pkmPath == "" {
		return "", errors.New("pkm_path must not be empty")
	}
	if !strings.HasPrefix(pkmPath, "/") {
		return "", errors.New("pkm_path must start with '/'")
	}
	for _, seg := range strings.Split(pkmPath, "/") {
		if seg == ".." {
			return "", errors.New("pkm_path must not contain '..'")
		}
	}
	cleaned := path.Clean(pkmPath)
	if cleaned == "/" {
		return "", errors.New("pkm_path must not be '/'")
	}
	if allowlistRoot == "" {
		return cleaned, nil
	}
	root := path.Clean(allowlistRoot)
	resolved := path.Clean(path.Join(root, cleaned))
	if resolved == root || !strings.HasPrefix(resolved, root+"/") {
		return "", fmt.Errorf("pkm_path resolves outside allowlist root %q", root)
	}
	return cleaned, nil
}
