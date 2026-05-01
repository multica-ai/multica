package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"

	"github.com/multica-ai/multica/server/pkg/agent/sandbox"
)

// SandboxConfig configures the macOS Seatbelt wrapper applied to spawned
// agent processes. It is set by the daemon at backend-construction time
// (see internal/daemon/daemon.go).
//
// On non-darwin platforms the configuration is silently ignored — there is
// no equivalent kernel-level sandbox, and spawning unwrapped is the same
// behaviour the daemon had before this change.
type SandboxConfig struct {
	// Enabled toggles the wrapper. When false, commands run as-is.
	Enabled bool
	// NetworkAllowlist is a list of host:port pairs the sandboxed process
	// may reach over TCP. Loopback to the daemon health port should be
	// included by the caller. An empty list means all outbound network is
	// denied.
	NetworkAllowlist []string
}

// errSandboxRequiredButUnavailable is returned when SandboxConfig.Enabled is
// true but sandbox-exec cannot be located on PATH. Callers must treat this
// as a fail-closed condition: spawning the agent unsandboxed when the
// operator asked for sandboxing would defeat the purpose.
var errSandboxRequiredButUnavailable = errors.New("agent: sandbox enabled but sandbox-exec not found on PATH")

// prepareCommand constructs an exec.Cmd for the given agent invocation,
// transparently wrapping it with sandbox-exec when the sandbox is enabled
// on macOS.
//
// The returned cleanup function removes the temporary profile file. It is
// safe to call multiple times. Callers should defer cleanup() until the
// spawned process has exited.
func prepareCommand(
	ctx context.Context,
	execPath string,
	args []string,
	sb *SandboxConfig,
	workdir string,
	logger *slog.Logger,
) (*exec.Cmd, func(), error) {
	noopCleanup := func() {}

	if sb == nil || !sb.Enabled || runtime.GOOS != "darwin" {
		return exec.CommandContext(ctx, execPath, args...), noopCleanup, nil
	}

	sandboxExec, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return nil, noopCleanup, errSandboxRequiredButUnavailable
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, noopCleanup, fmt.Errorf("agent: resolve home for sandbox: %w", err)
	}

	wd := workdir
	if wd == "" {
		// Fall back to the daemon's CWD; the spec requires Workdir to be
		// non-empty, and a missing workdir would force the agent to write
		// into the daemon's directory anyway.
		wd, err = os.Getwd()
		if err != nil {
			return nil, noopCleanup, fmt.Errorf("agent: resolve cwd for sandbox: %w", err)
		}
	}

	profilePath, err := sandbox.WriteToTemp(sandbox.Profile{
		Workdir:      wd,
		Home:         home,
		AllowedHosts: sb.NetworkAllowlist,
	})
	if err != nil {
		return nil, noopCleanup, fmt.Errorf("agent: write sandbox profile: %w", err)
	}
	cleaned := false
	cleanup := func() {
		if cleaned {
			return
		}
		cleaned = true
		_ = os.Remove(profilePath)
	}

	wrappedArgs := append([]string{"-f", profilePath, execPath}, args...)
	logger.Debug("agent sandbox: wrapping command",
		"profile", profilePath,
		"exec", execPath,
		"workdir", wd,
		"allowlist_size", len(sb.NetworkAllowlist),
	)
	return exec.CommandContext(ctx, sandboxExec, wrappedArgs...), cleanup, nil
}
