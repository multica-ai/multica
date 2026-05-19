package agent

// CEREBRO-PATCH(agent-sandbox-cerebro): cerebro modification of upstream file

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
	// WritablePaths are absolute filesystem paths the sandboxed process
	// needs read+write access to outside the workdir — typically the
	// agent CLI's per-user state files (~/.claude.json, ~/.cursor, …).
	// Caller is responsible for joining $HOME-relative subpaths to the
	// real home directory before passing them in.
	WritablePaths []string
	// CEREBRO-PATCH(agent-sandbox-cerebro): JEH-1774 keychain access fields.
	// Plumbing for per-task keychain deny + audit-only item allowlist; the
	// kernel-level rules live in pkg/agent/sandbox/macos.go.
	//
	// KeychainAccess controls how the generated profile treats macOS
	// keychain access. The zero value (sandbox.KeychainAccessDeny) blocks
	// both file-read on ~/Library/Keychains and mach-lookup of
	// com.apple.SecurityServer — so a sandboxed agent cannot invoke
	// `security find-generic-password` or any other libsecurity call
	// against any keychain item (the agent's own or another app's).
	// Legacy providers whose CLI authenticates through the user's
	// keychain set sandbox.KeychainAccessReadOnly as an opt-in escape
	// hatch.
	KeychainAccess sandbox.KeychainAccess
	// KeychainItemsAllowed is the audit-only list of keychain item names
	// the daemon is authorised to pre-fetch and inject as env vars before
	// spawn. The sandbox profile cannot enforce per-item allowlists at
	// the kernel layer; this list is recorded as a profile comment for
	// forensics and represents the contract the higher-level token
	// injector must honour.
	KeychainItemsAllowed []string
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
		Workdir:              wd,
		Home:                 home,
		AllowedHosts:         sb.NetworkAllowlist,
		WritablePaths:        sb.WritablePaths,
		KeychainAccess:       sb.KeychainAccess,
		KeychainItemsAllowed: sb.KeychainItemsAllowed,
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
		"writable_paths", len(sb.WritablePaths),
	)
	return exec.CommandContext(ctx, sandboxExec, wrappedArgs...), cleanup, nil
}
