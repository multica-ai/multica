package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// collectReapWindow bounds how long finish() keeps re-signalling the process
// tree, and collectReapStep is the interval between passes.
//
// One pass is not enough. A descendant whose fork completes between the kill's
// enumeration of the group and the signal's delivery never receives it, and only
// a later pass reaches it — measured 3 misses in 10 runs of the forking stub in
// run_collect_test.go, each leaving a `sleep 300` reparented to init. The window
// only has to cover that fork, so it is deliberately short: retrying for longer
// would widen the pid-reuse window documented on reapProcessTree, since the group
// id is the pid Wait has already reaped.
const (
	collectReapWindow = 100 * time.Millisecond
	collectReapStep   = 10 * time.Millisecond
)

// collectSettleGrace bounds the one cleanup wait on the answer path: after the
// tree has been signalled, how long finish() waits for the reader goroutines to
// see EOF before handing the caller what arrived.
//
// Short on purpose, and it cannot truncate a well-behaved CLI's answer. Once the
// direct child has exited, at most one pipe buffer of its output can still be
// unread — a child that had written more than that would have blocked in write()
// and could not have exited — and that is drained at memory speed. What the cap
// cuts short is a descendant that inherited the pipe and holds it open, whose
// trailing output is not the answer. EOF short-circuits the wait, so the normal
// case pays nothing at all.
const collectSettleGrace = 400 * time.Millisecond

// outputBuffer accumulates one stream and records when the last write landed.
//
// buf and lastWrite are updated inside the same critical section on purpose. If
// the timestamp were published after releasing the lock, a reader could observe
// new bytes together with a stale timestamp and conclude the stream had gone
// quiet at the very moment it was producing — which for RunCollectQuiet means
// truncating an answer mid-write.
type outputBuffer struct {
	mu        sync.Mutex
	buf       []byte
	lastWrite time.Time
}

// absorb drains r into the buffer until EOF or the file is closed. Read errors
// other than those are reported; a truncated stream is surfaced to the caller
// as whatever arrived, matching the previous cmd.Output() behaviour.
func (o *outputBuffer) absorb(r io.Reader) error {
	chunk := make([]byte, 32*1024)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			now := time.Now()
			o.mu.Lock()
			o.buf = append(o.buf, chunk[:n]...)
			o.lastWrite = now
			o.mu.Unlock()
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

func (o *outputBuffer) snapshot() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.buf...)
}

// idleFor reports how long since the last write and whether anything has been
// written at all.
func (o *outputBuffer) idleFor() (time.Duration, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.lastWrite.IsZero() {
		return 0, false
	}
	return time.Since(o.lastWrite), true
}

// collector runs a command with pipes this package owns, rather than handing
// os/exec an io.Writer and letting it do the draining.
//
// That distinction is the whole point. When os/exec owns the draining, Wait
// blocks until the output pipes reach EOF, and EOF requires every write end to
// be closed — so a CLI that forks a helper inheriting stdout (OpenClaw spawns
// `openclaw-config`) keeps Wait open for the helper's lifetime. Cancelling the
// context does not help: it kills the direct child without unblocking os/exec's
// io.Copy. Handing os/exec an *os.File instead means it starts no copy goroutine
// at all, so Wait returns the instant the direct child exits and this package
// decides when reading is over.
type collector struct {
	cmd        *exec.Cmd
	outR, errR *os.File
	stdout     outputBuffer
	stderr     outputBuffer

	readers  chan struct{} // closed once both absorb loops have returned
	waitDone chan struct{} // closed once cmd.Wait has returned
	waitErr  error         // valid after waitDone is closed

	finishOnce sync.Once
}

// startCollector takes a *exec.Cmd the caller has already built rather than an
// executable path, so a launch that carries an argv prefix (Command.exec, which
// applies a custom runtime's fixed_args) keeps it. Building the command here
// would silently drop that prefix.
//
// The caller must not have started it, and must leave Stdout/Stderr unset: this
// package installs its own *os.File pipes, which is the whole reason Wait cannot
// be held hostage by a descendant.
//
// No ctx parameter: the collector's lifetime is bounded by the callers below,
// which select on ctx themselves. Taking one here would suggest this function
// enforces it.
func startCollector(cmd *exec.Cmd, env []string) (*collector, error) {
	if env != nil {
		cmd.Env = env
	}
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)

	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("collect stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("collect stderr pipe: %w", err)
	}

	cmd.Stdout = outW
	cmd.Stderr = errW

	// startOwnedProcessTree rather than cmd.Start: on Windows it creates the
	// child suspended and assigns it to a Job Object before it runs a single
	// instruction, so a .cmd shim cannot spawn the real CLI outside the
	// ownership boundary. On Unix configureProcessGroup above already did the
	// equivalent and this is a plain Start. Either way the tree — not just the
	// direct child — is what finish() gets to reap.
	if startErr := startOwnedProcessTree(cmd, slog.Default()); startErr != nil {
		releaseProcessGroup(cmd)
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		return nil, startErr
	}

	// Drop the parent's write ends immediately: otherwise EOF can never arrive
	// no matter how thoroughly the child tree is reaped.
	outW.Close()
	errW.Close()

	c := &collector{
		cmd:      cmd,
		outR:     outR,
		errR:     errR,
		readers:  make(chan struct{}),
		waitDone: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = c.stdout.absorb(outR) }()
	go func() { defer wg.Done(); _ = c.stderr.absorb(errR) }()
	go func() { wg.Wait(); close(c.readers) }()
	go func() { c.waitErr = cmd.Wait(); close(c.waitDone) }()

	return c, nil
}

// finish reaps the process tree and leaves the buffers in the most complete
// state it can reach within collectSettleGrace, so a caller may snapshot them
// straight afterwards.
//
// It deliberately reports nothing. Cleanup that does not converge means the OS
// refused to kill something; that is worth a log line, but it must not decide
// whether the caller gets the answer. A version probe whose output arrived and
// whose helper the kernel would not kill is exactly the case #6084's cmd.WaitDelay
// got wrong — it failed a probe that had succeeded, and a failed version probe
// skips runtime registration entirely.
//
// Safe to call more than once and from any of the caller's exit paths.
func (c *collector) finish() {
	c.finishOnce.Do(func() {
		// Reap whatever the CLI forked, on the success path too: a successful
		// `openclaw --version` still leaves its helper behind, which is how
		// orphans accumulate on a host that probes on a timer. This also
		// releases the last write end so the readers below can see EOF.
		//
		// Retried across collectReapWindow because a single pass loses a
		// descendant that was mid-fork when the signal went out; see there.
		reapKill(c.cmd)
		treeGone := waitProcessGroupGone(c.cmd, collectReapStep)
		for reapDeadline := time.Now().Add(collectReapWindow); !treeGone && time.Now().Before(reapDeadline); {
			reapKill(c.cmd)
			treeGone = waitProcessGroupGone(c.cmd, collectReapStep)
		}

		// Because this package owns the pipes, Wait is not being held open by a
		// descendant: it returns as soon as the direct child is gone, which the
		// kill above has just ensured.
		settleDeadline := time.Now().Add(collectSettleGrace)
		waitReturned := waitUntil(c.waitDone, settleDeadline)
		drained := waitUntil(c.readers, settleDeadline)

		// Stop reading either way. Nothing is waited on here: on Windows an
		// anonymous pipe is not pollable, so closing the read end does not evict
		// a blocked Read and waiting for the absorb loop would park this call for
		// as long as the surviving descendant felt like living. The loops are
		// harmless — snapshot takes the buffer's mutex, so one still appending
		// cannot race a caller reading it — and they end when the descendant
		// finally does.
		c.outR.Close()
		c.errR.Close()
		// Only now: on Windows closing the job handle is what kills anything
		// still inside it, so it must not run while the tree could still be
		// serving output.
		releaseProcessGroup(c.cmd)

		if !treeGone || !drained || !waitReturned {
			slog.Default().Warn("agent: collect cleanup did not converge",
				"command", c.cmd.Path,
				"tree_gone", treeGone,
				"output_drained", drained,
				"wait_returned", waitReturned,
				"window", collectReapWindow,
				"settle_grace", collectSettleGrace)
		}
	})
}

// reapKill is the process-tree kill, indirected so a test can make one pass miss
// and prove the retry in finish() is what recovers. Production never reassigns it.
var reapKill = reapProcessTree

func waitUntil(done <-chan struct{}, deadline time.Time) bool {
	select {
	case <-done:
		return true
	default:
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// exitErr reports the command's exit error without blocking, and whether the
// command has been reaped at all yet.
func (c *collector) exitErr() (error, bool) {
	select {
	case <-c.waitDone:
		return c.waitErr, true
	default:
		return nil, false
	}
}

// RunCollect runs a short-lived CLI command to completion and returns its
// stdout and stderr. It is the safe replacement for cmd.Output() on any path
// that shells out to an agent CLI — see collector for why cmd.Output() cannot
// be bounded when the CLI leaves a descendant holding stdout.
//
// #6084 measured that shape (a shim whose backgrounded child slept 6s took
// 6.01s against a 150ms deadline) and reverted a cmd.WaitDelay backstop on
// review, because WaitDelay bounds the call only by leaving the descendant
// running and reports exec.ErrWaitDelay — turning a probe whose output arrived
// perfectly fine into a failure. The gap was tracked as MUL-5467; this closes
// it by owning the pipes and the process group instead.
//
// Guarantees callers depend on:
//
//  1. Returns within roughly the caller's context deadline plus
//     collectReapWindow and collectSettleGrace, whatever the CLI leaves behind.
//  2. Descendants the CLI forked are signalled before returning, and the signal
//     is repeated across collectReapWindow so one that was mid-fork does not
//     escape — invoking a CLI on a timer cannot accumulate orphans.
//  3. Whenever the tree is reaped successfully — which is every case the OS lets
//     us have — the reader goroutines and cmd.Wait have all returned before this
//     call does. If the kill does not take, the residue is logged and the answer
//     is still returned: a goroutine can stay parked reading a pipe a surviving
//     descendant holds, and that is not removable from here. It is deliberately
//     not reported as a call failure — see finish().
//  4. The command's real exit status is reported, which openclawShimDiagnostic
//     depends on (it type-switches on *exec.ExitError).
//
// env, when non-nil, replaces the child's environment (os/exec semantics).
//
// Not for agent execution: those paths stream stdout incrementally and manage
// their own lifecycle. Use this for one-shot, read-only invocations
// (`--version`, `agents list`, `config get`).
//
// This path-taking form has no production caller yet — the three call sites this
// change converts already hold a *exec.Cmd and use RunCollectCmd. It is the
// entry point for the remaining cmd.Output() call sites in this package
// (models.go's catalog probes, codex.go, deveco_models.go, thinking.go), which
// carry the same shape and are deliberately out of scope here.
func RunCollect(ctx context.Context, env []string, execPath string, args ...string) (stdout []byte, stderr string, err error) {
	// Command.exec, not exec.CommandContext: launch.go owns process construction
	// so a custom runtime's fixed_args prefix cannot be dropped (GH #7046). A zero
	// Command has no prefix, which is what a bare path argument means here.
	return RunCollectCmd(ctx, Command{Path: execPath}.exec(ctx, args...), env)
}

// RunCollectCmd is RunCollect for a caller that already holds a *exec.Cmd, which
// is what Command.exec returns.
//
// ctx bounds the call on its own: it is not assumed that cmd was built with
// exec.CommandContext. RunCollect's own cmd is, but a caller passing a plain
// exec.Command would otherwise block here for as long as the CLI chose to run,
// with the ctx argument doing nothing.
func RunCollectCmd(ctx context.Context, cmd *exec.Cmd, env []string) (stdout []byte, stderr string, err error) {
	c, startErr := startCollector(cmd, env)
	if startErr != nil {
		return nil, "", startErr
	}
	select {
	case <-c.waitDone:
		c.finish()
		return c.stdout.snapshot(), string(c.stderr.snapshot()), c.waitErr
	case <-ctx.Done():
		// Same ordering as RunCollectQuietCmd's deadline branch, so the two
		// entry points cannot hand callers different error shapes for the same
		// situation: prefer the process error once the tree has been reaped —
		// "signal: killed" is worth keeping — and fall back to ctx.Err().
		c.finish()
		out := c.stdout.snapshot()
		if werr, reaped := c.exitErr(); reaped && werr != nil {
			return out, string(c.stderr.snapshot()), werr
		}
		return out, string(c.stderr.snapshot()), ctx.Err()
	}
}

// reapProcessTree SIGKILLs the process group led by cmd's child, so helpers the
// child forked die with it. Safe to call after the child has already exited:
// configureProcessGroup makes the child the group leader, so its pid doubles as
// the group id and the group outlives the leader for as long as any member runs.
// An empty group yields ESRCH, which signalProcessGroup absorbs.
//
// The group kill is issued just after Wait has reaped the leader, so in
// principle the leader's pid is already free for reuse. Sequential pid
// allocation makes reuse inside that window (microseconds, and only after
// wrapping the whole pid space) not a practical concern, and it is the same
// window the other backends' cancellation paths already live with.
//
// On Windows signalProcessGroup terminates the Job Object that
// startOwnedProcessTree assigned the child to, which reaches the same
// descendants; see proc_windows.go.
func reapProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalProcessGroup(cmd, syscall.SIGKILL)
}
