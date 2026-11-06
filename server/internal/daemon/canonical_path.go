//go:build !windows

package daemon

import (
	"path/filepath"
	"strings"
)

func canonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// discoveredExecutablePath canonicalizes ordinary executable symlinks but
// preserves entrypoints backed by a known name-dispatching shim. Volta's
// volta-shim, Vite Plus's vp, and mise select the managed package from the
// invoked name; spawning the final target directly turns `claude --version`
// into `volta-shim --version`, `vp --version`, or `mise --version`, so the
// daemon reads the manager's version instead of the agent's and drops the
// runtime (#6183, #6702).
//
// mise is the worse shape of the three, because its `--version` probe succeeds.
// Every mise shim on Unix is a symlink to the mise binary itself, and mise
// dispatches on argv[0]; collapsing it leaves a path whose basename is `mise`,
// so `mise --version` prints a real version, the runtime registers, and the
// breakage only surfaces at launch when the agent's own flags reach mise's
// argument parser: `error: unexpected argument '-p' found` followed by
// `Usage: mise [FLAGS] [TASK] [SUBCOMMAND]` (#9185).
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

func isNameDispatchingAgentShim(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	// "vp" is intentionally admitted despite being a short, generic token:
	// it is Vite Plus's shared dispatcher and, like Volta, selects the managed
	// package from argv[0]. Keep this exact-match list limited to dispatchers
	// confirmed to require the invoked entrypoint name. Do not strip executable
	// extensions: these managers use wrappers or trampolines rather than
	// name-dispatching symlinks, and that is a Windows shape this file never
	// compiles for.
	return base == "volta-shim" || base == "vp" || base == "mise"
}

var executablePathForLaunch = executablePathForLaunchDefault

func executablePathForLaunchDefault(string) (string, bool, error) {
	return "", false, nil
}

func canonicalConfiguredExecutablePath(path string) string {
	return path
}
