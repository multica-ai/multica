//go:build !windows

package agent

import (
	"errors"
	"log/slog"
	"os/exec"
	"syscall"
	"time"
)

// hideAgentWindow is a no-op on non-Windows platforms.
func hideAgentWindow(cmd *exec.Cmd) {}

// configureProcessGroup puts the child into its own process group (it becomes
// the group leader, so the group id equals the child pid). This lets the
// daemon signal the entire tree — the agent CLI plus any tool subprocess it
// spawns — in one call, instead of killing only the direct child and leaking
// grandchildren that keep running (and, for opencode, spinning on EPIPE) after
// a task is cancelled or the daemon restarts. See signalProcessGroup.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// startOwnedProcessTree is a plain Start on non-Windows platforms:
// configureProcessGroup already put the child in its own process group before
// it existed, so there is nothing left to claim once it is running. The logger
// is unused here; Windows needs it to report degraded ownership.
func startOwnedProcessTree(cmd *exec.Cmd, _ *slog.Logger) error { return cmd.Start() }

// releaseProcessGroup is a no-op on non-Windows platforms: a process group needs
// no handle and is gone once its members are.
func releaseProcessGroup(cmd *exec.Cmd) {}

func codexInitializeRetrySupported() bool { return true }

// signalProcessGroup sends sig to the whole process group led by the command
// (when it was started with configureProcessGroup), falling back to the single
// process if the group send fails. Targeting the group (negative pid) reaches
// the descendants the agent spawned, not just the leader.
func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		_ = cmd.Process.Signal(sig)
	}
}

func waitProcessGroupGone(cmd *exec.Cmd, timeout time.Duration) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(-cmd.Process.Pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
