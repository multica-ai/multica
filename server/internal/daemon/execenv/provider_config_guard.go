package execenv

import (
	"fmt"
	"path/filepath"
	"strings"
)

type providerConfigCheck func(path string) error

func resolveAndGuardProviderConfigPath(provider, source, rawPath, workspacesRoot string, check providerConfigCheck) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("%s: %s is empty", provider, source)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%s: resolve %s: %w", provider, source, err)
	}
	if pathIsUnder(abs, workspacesRoot) {
		return "", fmt.Errorf("%s: inherited %s points inside Multica workspaces root (%s); refusing task-scoped provider state as shared config", provider, source, abs)
	}
	if check != nil {
		if err := check(abs); err != nil {
			return "", fmt.Errorf("%s: inherited %s is not usable (%s): %w", provider, source, abs, err)
		}
	}
	return abs, nil
}

func pathIsUnder(path, root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
