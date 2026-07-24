//go:build !windows

package agent

import (
	"errors"
	"os"
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

func codexInitializeRetrySupported() bool { return true }

// signalProcessGroup sends sig to the whole process group led by p (when the
// command was started with configureProcessGroup), falling back to the single
// process if the group send fails. Targeting the group (negative pid) reaches
// the descendants the agent spawned, not just the leader.
func signalProcessGroup(p *os.Process, sig syscall.Signal) {
	if p == nil {
		return
	}
	if err := syscall.Kill(-p.Pid, sig); err != nil {
		_ = p.Signal(sig)
	}
}

func waitProcessGroupGone(p *os.Process, timeout time.Duration) bool {
	if p == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(-p.Pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
