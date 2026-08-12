//go:build !windows

package daemon

import "path/filepath"

func canonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func canonicalConfiguredExecutablePath(path string) string {
	return path
}
