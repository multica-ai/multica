//go:build windows

package agent

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// createNewConsole allocates a fresh console for the child process. Combined
// with HideWindow=true (STARTF_USESHOWWINDOW + SW_HIDE) the console window
// stays off-screen, and — critically — any grandchildren the agent spawns
// (tool subprocesses like bash, cmd, netstat, findstr) inherit this hidden
// console instead of each allocating their own visible one.
//
// Using CREATE_NO_WINDOW here instead would strip the console entirely,
// which forces Windows to allocate a new visible console per grandchild
// when the grandchild is a console-subsystem program that doesn't itself
// pass CREATE_NO_WINDOW — the exact popup storm reported in #1521.
const createNewConsole = 0x00000010

// hideAgentWindow configures cmd to suppress the console window on Windows
// while still giving descendant processes a hidden console to inherit.
// Stdio pipes set via cmd.StdoutPipe/StdinPipe keep working because
// STARTF_USESTDHANDLES takes precedence over the new console's stdio.
func hideAgentWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNewConsole
}

// configureProcessGroup is a no-op on Windows: there is no Setpgid/process-group
// signalling. Descendant cleanup relies on the hidden console group set up by
// hideAgentWindow plus exec.CommandContext / WaitDelay terminating the child.
func configureProcessGroup(cmd *exec.Cmd) {}

// codexInitializeRetrySupported remains false until Codex children are owned
// by a Job Object and descendant termination can be positively confirmed.
func codexInitializeRetrySupported() bool { return false }

// signalProcessGroup terminates the process on Windows. Windows has no
// SIGTERM/SIGKILL distinction or process-group signalling, so the signal is
// ignored and the process is killed directly (TerminateProcess via Kill). The
// caller's grace window still applies before this is invoked with SIGKILL.
func signalProcessGroup(p *os.Process, _ syscall.Signal) {
	if p == nil {
		return
	}
	_ = p.Kill()
}

func waitProcessGroupGone(_ *os.Process, _ time.Duration) bool { return false }
