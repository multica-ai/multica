//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// mise resolution runs during runtime discovery. Root prevents a task
	// worktree's config from influencing the selected binary and toolset
	// environment, and WaitDelay keeps inherited output pipes from extending
	// the context deadline.
	miseWhichTimeout   = 2 * time.Second
	miseWhichWaitDelay = 250 * time.Millisecond

	trustedExecutableResolutionDir = "/"
)

func canonicalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// discoveredExecutableResolution canonicalizes ordinary executable symlinks
// while retaining the launch contract of known version-manager entrypoints.
// Volta's volta-shim and Vite Plus's vp select the managed package from argv[0], so
// their invoked basename must be preserved (#6183, #6702). mise shims also
// dispatch by name, but preserving them would let a task worktree's mise.toml
// select a different version at launch. Resolve those shims to the concrete
// binary and toolset environment selected from a fixed trusted working
// directory instead (#7764).
//
// For argv[0] dispatchers, the entrypoint's parent is still canonicalized. This
// keeps paths stable when their containing directory is a symlink or an
// ephemeral version-manager prefix while retaining the basename the dispatcher
// needs — the same semantics buildLoginShellResolveScript applies via `pwd -P`.
func discoveredExecutableResolution(path, commandName string) (executableResolution, error) {
	real := canonicalExecutablePath(path)
	if isMiseExecutable(real) {
		return resolveMiseManagedExecutableResolutionWithTimeout(real, commandName)
	}
	if !isNameDispatchingAgentShim(real) {
		return executableResolution{Path: real}, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return executableResolution{Path: real}, nil
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return executableResolution{Path: abs}, nil
	}
	return executableResolution{Path: filepath.Join(realDir, filepath.Base(abs))}, nil
}

func isMiseExecutable(path string) bool {
	return strings.EqualFold(filepath.Base(path), "mise")
}

func resolveMiseDiscoveredExecutable(path, commandName string) (executableResolution, bool, error) {
	real := canonicalExecutablePath(path)
	if !isMiseExecutable(real) {
		return executableResolution{Path: path}, false, nil
	}
	resolved, err := resolveMiseManagedExecutableResolutionWithTimeout(real, commandName)
	return resolved, true, err
}

func resolveMiseManagedExecutableResolutionWithTimeout(misePath, commandName string) (executableResolution, error) {
	ctx, cancel := context.WithTimeout(context.Background(), miseWhichTimeout)
	defer cancel()
	return resolveMiseManagedExecutableResolution(ctx, misePath, commandName)
}

func resolveMiseManagedExecutable(ctx context.Context, misePath, commandName string) (string, error) {
	resolved, err := resolveMiseManagedExecutableResolution(ctx, misePath, commandName)
	return resolved.Path, err
}

func resolveMiseManagedExecutableResolution(ctx context.Context, misePath, commandName string) (executableResolution, error) {
	raw, err := runMiseResolutionCommand(ctx, misePath, "which", commandName)
	if err != nil {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s: %w", commandName, err)
	}

	target := strings.TrimSpace(string(raw))
	if target == "" || strings.ContainsAny(target, "\r\n") {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s: mise which returned an invalid path", commandName)
	}
	if !filepath.IsAbs(target) {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s: mise which returned non-absolute path %q", commandName, target)
	}
	if _, err := exec.LookPath(target); err != nil {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s target %q: %w", commandName, target, err)
	}
	target = canonicalExecutablePath(target)
	if isMiseExecutable(target) {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s: mise which returned the manager executable", commandName)
	}

	rawEnv, err := runMiseResolutionCommand(ctx, misePath, "env", "--json")
	if err != nil {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s environment: %w", commandName, err)
	}
	var env map[string]string
	if err := json.Unmarshal(rawEnv, &env); err != nil {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s environment: parse mise env --json: %w", commandName, err)
	}
	env, err = sanitizeMiseResolvedEnv(env)
	if err != nil {
		return executableResolution{}, fmt.Errorf("resolve mise-managed %s environment: %w", commandName, err)
	}
	return executableResolution{Path: target, Env: env}, nil
}

func runMiseResolutionCommand(ctx context.Context, misePath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, misePath, args...)
	cmd.Dir = trustedExecutableResolutionDir
	cmd.WaitDelay = miseWhichWaitDelay
	// MISE_CWD overrides the process working directory. Drop an inherited value
	// so Dir=/ remains a real trust boundary rather than a cosmetic setting.
	cmd.Env = make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "MISE_CWD") || strings.EqualFold(key, "PWD") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "PWD="+trustedExecutableResolutionDir)
	raw, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func sanitizeMiseResolvedEnv(env map[string]string) (map[string]string, error) {
	pathValue := env["PATH"]
	if strings.TrimSpace(pathValue) == "" {
		return nil, fmt.Errorf("mise env --json returned no PATH")
	}
	clean := make(map[string]string, len(env))
	for key, value := range env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("mise env --json returned an invalid environment entry")
		}
		// PATH is the launch-critical value. Other daemon-owned variables must
		// retain their normal precedence even if a global mise config defines
		// the same name.
		if !strings.EqualFold(key, "PATH") && isBlockedEnvKey(key) {
			continue
		}
		clean[key] = value
	}
	return clean, nil
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
	return base == "volta-shim" || base == "vp"
}

var executablePathForLaunch = executablePathForLaunchDefault

func executablePathForLaunchDefault(string) (string, bool, error) {
	return "", false, nil
}

func canonicalConfiguredExecutablePath(path string) string {
	return path
}
