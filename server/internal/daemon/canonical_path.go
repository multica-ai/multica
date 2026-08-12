//go:build !windows

package daemon

import "path/filepath"

func canonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func discoveredExecutablePath(path string) string {
	return canonicalExecutablePath(path)
}

var executablePathForLaunch = executablePathForLaunchDefault

func executablePathForLaunchDefault(string) (string, bool, error) {
	return "", false, nil
}

func canonicalConfiguredExecutablePath(path string) string {
	return path
}
