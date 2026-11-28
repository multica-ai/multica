//go:build !windows

package daemon

import "path/filepath"

func canonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// discoveredExecutablePath canonicalizes ordinary executable symlinks but
// preserves entrypoints backed by a known name-dispatching shim. Volta's
// volta-shim, Vite Plus's vp, and mise select the managed command from the
// invoked name. Spawning the final target directly probes the manager's
// version instead of the agent's; `mise app-server` also treats app-server
// as a mise task instead of starting Codex (#6183, #6702, #10451).
//
// The entrypoint's parent is still canonicalized. This keeps paths stable when
// their containing directory is a symlink or an ephemeral version-manager
// prefix, while retaining the basename the dispatcher needs — the same
// semantics buildLoginShellResolveScript has always applied via `pwd -P`.
func discoveredExecutablePath(path string) string {
	real := canonicalExecutablePath(path)
	if !isNameDispatchingAgentShim(real) {
		return real
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return real
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return abs
	}
	return filepath.Join(realDir, filepath.Base(abs))
}

var executablePathForLaunch = executablePathForLaunchDefault

func executablePathForLaunchDefault(string) (string, bool, error) {
	return "", false, nil
}

func canonicalConfiguredExecutablePath(path string) string {
	return path
}
