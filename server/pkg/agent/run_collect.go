package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"context"
)

// collectDrainGrace bounds each of the two waits finish() performs: for the
// direct child to be reaped, and for the reader goroutines to see EOF. Only a
// descendant we failed to kill can consume the second one — on Unix the process
// group kill closes the last write end, so it is not reached. On Windows there
// is no group to signal (see proc_windows.go), so this is what keeps the call
// bounded there, after which the read ends are closed outright.
const collectDrainGrace = 5 * time.Second

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

func startCollector(ctx context.Context, env []string, execPath string, args ...string) (*collector, error) {
	cmd := exec.CommandContext(ctx, execPath, args...)
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

	if startErr := cmd.Start(); startErr != nil {
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

// finish reaps the process tree and guarantees that once it returns, no
// goroutine started by startCollector is still running and nothing can still
// append to the buffers — so the caller may snapshot them without racing.
//
// Safe to call more than once and from any of the caller's exit paths.
func (c *collector) finish() {
	c.finishOnce.Do(func() {
		// Reap whatever the CLI forked, on the success path too: a successful
		// `openclaw --version` still leaves its helper behind, which is how
		// orphans accumulate on a host that probes on a timer. This also
		// releases the last write end so the readers below can see EOF.
		reapProcessTree(c.cmd)

		// Bounded belt-and-braces: because this package owns the pipes, Wait is
		// not being held open by a descendant, so it returns as soon as the
		// direct child is gone — which reapProcessTree has just ensured.
		select {
		case <-c.waitDone:
		case <-time.After(collectDrainGrace):
		}

		select {
		case <-c.readers:
		case <-time.After(collectDrainGrace):
			// A descendant we could not kill still holds a write end, so EOF
			// will not arrive on its own (Windows has no process group to
			// signal). Close the read ends: the in-flight Read returns at once,
			// which is what lets the wait below terminate. Returning here
			// without that wait would leave io.Copy appending to a buffer the
			// caller is about to read.
			c.outR.Close()
			c.errR.Close()
			<-c.readers
		}

		c.outR.Close()
		c.errR.Close()
	})
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
//     collectDrainGrace, whatever the CLI leaves behind.
//  2. On Unix, descendants the CLI forked are killed before returning, so
//     invoking a CLI on a timer cannot accumulate orphans.
//  3. No goroutine outlives the call, on any platform.
//  4. The command's real exit status is reported, which openclawShimDiagnostic
//     depends on (it type-switches on *exec.ExitError).
//
// env, when non-nil, replaces the child's environment (os/exec semantics).
//
// Not for agent execution: those paths stream stdout incrementally and manage
// their own lifecycle. Use this for one-shot, read-only invocations
// (`--version`, `agents list`, `config get`).
func RunCollect(ctx context.Context, env []string, execPath string, args ...string) (stdout []byte, stderr string, err error) {
	c, startErr := startCollector(ctx, env, execPath, args...)
	if startErr != nil {
		return nil, "", startErr
	}
	<-c.waitDone
	c.finish()
	return c.stdout.snapshot(), string(c.stderr.snapshot()), c.waitErr
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
// No-op on Windows, where there is no process-group signalling; finish() closes
// the read ends instead. Owning descendants on Windows needs a Job Object,
// which is the remaining half of MUL-5467.
func reapProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalProcessGroup(cmd.Process, syscall.SIGKILL)
}
