//go:build !windows

package daemon

import "path/filepath"

func canonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func discoveredExecutablePath(path string) string {
	return canonicalExecutablePath(path)
}

func executablePathForLaunch(string) (string, bool, error) {
	return "", false, nil
}

func canonicalConfiguredExecutablePath(path string) string {
	return path
}
