package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// collectDrainGrace bounds how long RunCollect waits for the command's output
// pipes to reach EOF *after* the direct child exited and its process group was
// signalled. Only a descendant we failed to kill can consume this — on Unix
// the group kill closes the last write end, so in practice it is not reached.
// On Windows there is no group to kill (see proc_windows.go), so this is what
// keeps the call bounded there.
const collectDrainGrace = 5 * time.Second

// RunCollect runs a short-lived CLI command to completion and returns its
// stdout and stderr. It is the safe replacement for cmd.Output() on any path
// that shells out to an agent CLI.
//
// # Why not cmd.Output()
//
// Some agent CLIs fork a long-lived helper that inherits stdout/stderr —
// OpenClaw spawns `openclaw-config` — and that helper keeps the pipe's write
// end open long after the direct child has exited. cmd.Output() (and any Cmd
// whose Stdout is an io.Writer) makes os/exec own the draining: Wait blocks
// until those pipes reach EOF, which never happens while the helper lives.
//
// exec.CommandContext does not save you. Cancelling the context kills the
// direct child; it does not unblock os/exec's io.Copy, so the call runs for
// the *descendant's* lifetime and the deadline is decorative. Measured on
// linux/dash: a shim whose backgrounded child slept 6s took 6.01s against a
// 150ms deadline.
//
// cmd.WaitDelay bounds the call but leaves the descendant running, trading a
// hang for a process leak — and on Unix nothing reaps it. It also reports
// exec.ErrWaitDelay, which turns "the CLI printed its answer, then a helper
// lingered" into a failed probe. That trade is why the WaitDelay backstop was
// reverted from execOpenclawCLI in #6084, leaving the gap tracked as MUL-5467.
//
// RunCollect closes it by owning the pipes. Handing os/exec an *os.File means
// it starts no copy goroutine, so Wait returns the instant the direct child
// exits. The child's process group is then signalled — reaping the helper,
// which closes the last write end — so our own readers see EOF and we return
// the complete output with an accurate error.
//
// Guarantees callers depend on:
//
//  1. Returns within roughly the caller's context deadline plus
//     collectDrainGrace, whatever the CLI leaves behind.
//  2. On Unix, descendants the CLI forked are killed before returning, so
//     probing on a timer cannot accumulate orphans.
//  3. A command that printed its output and exited 0 is reported as success
//     even if a descendant was still holding the pipe.
//
// env, when non-nil, replaces the child's environment (os/exec semantics).
//
// Not for agent execution: those paths stream stdout incrementally and manage
// their own lifecycle. Use this for one-shot, read-only invocations
// (`--version`, `agents list`, `config get`).
func RunCollect(ctx context.Context, env []string, execPath string, args ...string) (stdout []byte, stderr string, err error) {
	cmd := exec.CommandContext(ctx, execPath, args...)
	if env != nil {
		cmd.Env = env
	}
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)

	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, "", fmt.Errorf("collect stdout pipe: %w", err)
	}
	defer outR.Close()

	errR, errW, err := os.Pipe()
	if err != nil {
		outW.Close()
		return nil, "", fmt.Errorf("collect stderr pipe: %w", err)
	}
	defer errR.Close()

	// *os.File, not a buffer: this is what keeps os/exec out of the draining
	// business and lets Wait return on the child's exit alone.
	cmd.Stdout = outW
	cmd.Stderr = errW

	if startErr := cmd.Start(); startErr != nil {
		outW.Close()
		errW.Close()
		return nil, "", startErr
	}

	// Drop the parent's write ends immediately. Otherwise EOF can never
	// arrive no matter how thoroughly the child tree is reaped.
	outW.Close()
	errW.Close()

	var (
		outBuf, errBuf bytes.Buffer
		wg             sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, outR) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, errR) }()

	waitErr := cmd.Wait()

	// Reap whatever the CLI forked, on the success path too: a successful
	// `openclaw --version` still leaves its helper behind, which is how the
	// orphans accumulate on a host that probes on a timer. This also releases
	// the last write end so the readers below can finish.
	reapProcessTree(cmd)

	drained := make(chan struct{})
	go func() { wg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(collectDrainGrace):
		// A surviving descendant still holds a write end (Windows, or a kill
		// we could not deliver). Return what we have rather than block; the
		// deferred Close on the read ends unblocks the copy goroutines.
	}

	return outBuf.Bytes(), errBuf.String(), waitErr
}

// reapProcessTree SIGKILLs the process group led by cmd's child, so helpers
// the child forked die with it. Safe to call after the child has already
// exited: configureProcessGroup makes the child the group leader, so its pid
// doubles as the group id and the group outlives the leader for as long as any
// member runs. An empty group yields ESRCH, which signalProcessGroup absorbs.
//
// The group kill is issued just after Wait has reaped the leader, so in
// principle the leader's pid is already free for reuse. Sequential pid
// allocation makes reuse inside that window (microseconds, and only after
// wrapping the whole pid space) not a practical concern, and it is the same
// window the other backends' cancellation paths already live with.
//
// No-op on Windows, where there is no process-group signalling —
// collectDrainGrace is what bounds the call there. Owning descendants on
// Windows needs a Job Object, which is the remaining half of MUL-5467.
func reapProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalProcessGroup(cmd.Process, syscall.SIGKILL)
}
