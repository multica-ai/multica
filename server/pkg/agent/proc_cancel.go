package agent

import (
	"context"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// configureAgentProcessGroup puts cmd in its own process group and disables
// os/exec's default SIGKILL-on-cancel, so the caller can drive a graceful
// group-wide teardown via startAgentProcessGroupCancel instead. WaitDelay
// remains the hard backstop.
//
// This is the shared setup half of the cancellation pattern used by the
// streaming backends (claude/opencode/zcode) and the zcode --json fallback: it
// guarantees cancellation reaches the whole process tree — the agent CLI plus
// any tool subprocess it spawns — not just the direct child. A grandchild
// holding the stdout pipe would otherwise survive cancellation, keeping the
// read (and thus Result) pending and orphaning the descendant.
//
// On Windows configureProcessGroup is a no-op (no process-group signalling),
// so descendant cleanup there relies on the hidden console group from
// hideAgentWindow plus WaitDelay terminating the leader; see proc_windows.go.
func configureAgentProcessGroup(cmd *exec.Cmd) {
	configureProcessGroup(cmd)
	// Returning nil keeps os/exec from racing the graceful teardown below with
	// its own immediate SIGKILL of the leader.
	cmd.Cancel = func() error { return nil }
}

// startAgentProcessGroupCancel spawns a goroutine that, on runCtx cancellation
// or timeout, gracefully tears down the whole process group led by
// cmd.Process: SIGTERM the group, wait grace, SIGKILL any survivors, then close
// stdout (the last-resort unblock for a reader a wedged descendant still holds
// open). procDone is closed by the caller once cmd.Wait() returns, so this
// handler skips an already-exited process and avoids signalling a dead/reused
// pid. stdout may be nil when the backend does not read stdout via a pipe.
//
// cmd must have been started with configureAgentProcessGroup. SIGKILL is
// uncatchable, so once delivered no group member can write again — only then
// is it safe to close the stdout read end. The grace window lets a
// SIGTERM-respecting tree shut down cleanly; survivors (a CLI/child that
// ignores SIGTERM) get the SIGKILL.
func startAgentProcessGroupCancel(cmd *exec.Cmd, runCtx context.Context, procDone <-chan struct{}, stdout io.Closer, grace time.Duration) {
	go func() {
		select {
		case <-procDone:
			return // finished on its own; nothing to terminate
		case <-runCtx.Done():
		}
		if cmd.Process != nil {
			signalProcessGroup(cmd.Process, syscall.SIGTERM)
			// Escalate to a group SIGKILL unless the WHOLE process group has
			// exited within the grace window. This must key off the process
			// group, not procDone: procDone only means cmd.Wait() returned for
			// the leader, so a SIGTERM-ignoring descendant that does not hold
			// the leader's stdout would let the leader exit, close procDone,
			// and skip the SIGKILL — leaking exactly the orphan this targets.
			if !waitProcessGroupGone(cmd.Process, grace) {
				signalProcessGroup(cmd.Process, syscall.SIGKILL)
			}
		}
		if stdout != nil {
			_ = stdout.Close()
		}
	}()
}
