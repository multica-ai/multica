package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveAndGuardProviderConfigPath(provider, source, rawPath, workspacesRoot string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("%s: %s is empty", provider, source)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%s: resolve %s: %w", provider, source, err)
	}
	under, err := pathIsUnder(abs, workspacesRoot)
	if err != nil {
		return "", fmt.Errorf("%s: guard %s: %w", provider, source, err)
	}
	if under {
		return "", fmt.Errorf("%s: inherited %s points inside Multica workspaces root (%s); refusing task-scoped provider state as shared config", provider, source, abs)
	}
	return abs, nil
}

func pathIsUnder(path, root string) (bool, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return false, fmt.Errorf("workspaces root is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("resolve workspaces root: %w", err)
	}
	resolvedRoot := filepath.Clean(resolvePathBestEffort(absRoot))
	resolvedPath := filepath.Clean(resolvePathBestEffort(path))
	if isPathUnder(resolvedRoot, resolvedPath) {
		return true, nil
	}
	return pathHasSameFileAncestor(path, absRoot) || pathHasSameFileAncestor(resolvedPath, resolvedRoot), nil
}

func pathHasSameFileAncestor(path, root string) bool {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	dir := path
	for {
		if info, err := os.Stat(dir); err == nil && os.SameFile(info, rootInfo) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
