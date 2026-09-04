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
// Short on purpose, and by the time it runs the caller has already decided that
// reading is over — either the answer satisfied its completeness rule, or
// collectDrainGrace expired waiting for one. What this cap cuts short is a
// descendant that inherited the pipe and holds it open, whose trailing output is
// not the answer. EOF short-circuits the wait, so the normal case pays nothing.
const collectSettleGrace = 400 * time.Millisecond

// collectDrainGrace bounds the wait for pipe EOF *after* the direct child has
// exited, before the tree is reaped.
//
// This exists because leader exit is not the end of output, and treating it as
// one is a data-loss bug rather than a slow path. A wrapper can exit
// successfully while the real CLI — its descendant, which inherited stdout — has
// not printed yet; npm and PowerShell shims make that the normal shape on
// Windows. Review measured the consequence on an earlier revision of this
// branch: the wrapper exits 0, its child prints the version 500ms later, and
// reaping on leader exit returned an empty answer with a nil error, where
// os/exec's own EOF wait returns the version.
//
// So EOF is the signal, and this is its bound. 2s matches probeWaitDelay, which
// is the equivalent bound os/exec applies for the same reason on the
// launch.go helpers — a descendant that never closes the pipe must not hold the
// call forever.
//
// A caller's completeness rule short-circuits it: once the answer is in the
// buffer there is nothing left to wait for, so the normal case — a CLI that
// prints its answer and exits, with or without a lingering helper — pays
// nothing.
const collectDrainGrace = 2 * time.Second

// collectStdoutLimit caps the answer this package will accumulate, and
// collectStderrTail caps the diagnostic sample kept from stderr.
//
// Both streams were unbounded in an earlier revision, and review measured the
// cost: a CLI writing continuously for the probe window retained 13,107,400
// bytes of stderr where launch.go's outputOwned keeps the last
// probeStderrSampleBytes (32 KiB). A broken local CLI in a log loop could
// exhaust daemon memory before any deadline helped.
//
// The two limits are deliberately different in kind, because the streams are:
//
//   - stderr is a diagnostic sample, so the *tail* is what matters — a CLI's
//     actual failure line is at the end. Keeping the last 32 KiB matches
//     outputOwned exactly, so moving a call site between the two mechanisms
//     cannot change what a failed probe reports.
//   - stdout is the answer, and silently dropping part of an answer is how a
//     truncated catalog becomes a "successful" empty one. So it is capped and
//     the overflow is reported as an error instead: a one-shot response that
//     exceeds this is a malfunction, not a large answer. Where that cap falls is
//     derived from the largest answer these call sites ask for; see
//     collectStdoutLimit below.
const collectStderrTail = probeStderrSampleBytes

// A var, not a const, so a test can shrink it rather than generating megabytes to
// reach it — the same reason detectVersionTimeout is one. Production never
// reassigns it.
//
// 8 MiB is derived rather than picked, because where this falls is a product
// compatibility boundary and not only a memory bound: a legal config past the cap
// stops a task from starting. The derivation, in the order it constrains the
// value:
//
//   - What is actually asked for. Measured on OpenClaw 2026.7.1-2:
//     `config validate --json` answers in 715 bytes and `config file` in 2663.
//     The largest answer any call site asks for is the fully resolved config from
//     `config get --json`, whose size scales with the user's agents and MCP
//     servers.
//   - There is no upstream ceiling on either, so the worst case is constructed:
//     1000 agents and 250 MCP servers, with the fields OpenClaw's resolved config
//     carries, serialises to 547,291 bytes. Real deployments carry single-digit
//     agent counts, so that is already orders of magnitude past the field. This
//     bound leaves 15x on top of *it*, which is what
//     TestCollectStdoutLimitHasHeadroomOverTheLargestAnswer asserts — the
//     construction lives in the test so the derivation cannot drift from the
//     value silently.
//   - It matches what the daemon already holds for a comparable per-task payload
//     it reads whole into memory (internal/daemon's maxLocalSkillBundleSize is the
//     same 8 MiB), so the two limits do not have to be reasoned about separately.
//     Named rather than imported: pkg/agent is the lower layer.
//
// A cap of some size is unavoidable rather than a design choice: every caller
// parses the answer as one document, so an answer that cannot be held in memory
// cannot be used either. What the derivation buys is that the boundary sits far
// outside any answer a real host produces, and TestCollectedStdoutBoundaryIsExact
// pins exactly where it is — at the limit the answer is returned whole, one byte
// past it fails closed with errCollectStdoutTooLarge instead of being silently
// shortened.
var collectStdoutLimit = 8 << 20

// outputBuffer accumulates one stream and records when the last write landed.
//
// buf and lastWrite are updated inside the same critical section on purpose. If
// the timestamp were published after releasing the lock, a reader could observe
// new bytes together with a stale timestamp and conclude the stream had gone
// quiet at the very moment it was producing — which for RunCollectQuiet means
// truncating an answer mid-write.
//
// max bounds retention. keepTail selects which end survives: a tail buffer drops
// from the front and keeps reading (stderr, a sample), while a head-capped
// buffer stops accumulating and records the overflow so the caller can fail
// rather than return a silently shortened answer (stdout).
type outputBuffer struct {
	mu        sync.Mutex
	buf       []byte
	lastWrite time.Time
	max       int
	keepTail  bool
	overflow  bool
}

// absorb drains r into the buffer until EOF or the file is closed. Read errors
// other than those are reported; a truncated stream is surfaced to the caller
// as whatever arrived, matching the previous cmd.Output() behaviour.
//
// Reading continues past max in both modes. Stopping would leave the child
// blocked in write() with a full pipe, which for a head-capped stream converts
// "answer too large" into a hang.
func (o *outputBuffer) absorb(r io.Reader) error {
	chunk := make([]byte, 32*1024)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			now := time.Now()
			o.mu.Lock()
			o.append(chunk[:n], now)
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

// append records p under the caller's lock, honouring max. The timestamp is set
// even when bytes are dropped: the stream was active, and an idle check that
// concluded otherwise would be wrong.
func (o *outputBuffer) append(p []byte, now time.Time) {
	o.lastWrite = now
	if o.max <= 0 {
		o.buf = append(o.buf, p...)
		return
	}
	if o.keepTail {
		o.buf = append(o.buf, p...)
		if len(o.buf) > o.max {
			o.buf = append([]byte(nil), o.buf[len(o.buf)-o.max:]...)
			o.overflow = true
		}
		return
	}
	room := o.max - len(o.buf)
	if room <= 0 {
		o.overflow = true
		return
	}
	if len(p) > room {
		p = p[:room]
		o.overflow = true
	}
	o.buf = append(o.buf, p...)
}

// overflowed reports whether max caused anything to be dropped.
func (o *outputBuffer) overflowed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.overflow
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
// That distinction is the whole point, and it is the one thing launch.go's
// runOwned / outputOwned / combinedOutputOwned cannot do. When os/exec owns the
// draining, Wait blocks until the output pipes reach EOF, and EOF requires every
// write end to be closed — so a CLI that forks a helper inheriting stdout
// (OpenClaw spawns `openclaw-config`) keeps Wait open for the helper's lifetime.
// Cancelling the context does not help: it kills the direct child without
// unblocking os/exec's io.Copy. Handing os/exec an *os.File instead means it
// starts no copy goroutine at all, so Wait returns the instant the direct child
// exits and this package decides when reading is over.
//
// The owned helpers bound that wait with probeWaitDelay instead, which is the
// right trade for the probes they serve — but the bound expires into
// exec.ErrWaitDelay, so a CLI whose answer arrived and whose helper lingered is
// reported as a failed call. On the OpenClaw paths that is not tolerable: a
// failed `--version` skips runtime registration, and a failed `config file`
// fails task preparation. See RunCollectQuiet for the guarantees this buys and
// the one it cannot make.
//
// The corollary, learned the hard way: because this package decides when reading
// is over, it must decide correctly. The direct child's exit is evidence about the
// direct child only — a wrapper that exits while its descendant still owes the
// answer is the normal shape on Windows — so awaitOutputAfterExit waits for pipe
// EOF, bounded, unless the caller's rule says the answer is already in.
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
	// The answer is capped and reports overflow; the diagnostic keeps its tail.
	// See collectStdoutLimit.
	c.stdout.max = collectStdoutLimit
	c.stderr.max = collectStderrTail
	c.stderr.keepTail = true

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
		// Reap whatever the CLI forked and left in the leader's group, on the
		// success path too: a successful `openclaw --version` still leaves its
		// helper behind, which is how orphans accumulate on a host that probes
		// on a timer. A helper that called setsid is out of reach here — see
		// guarantee 2 on RunCollectQuiet — but this is also what releases the last
		// write end so the readers below can see EOF, which does not depend on
		// the kill landing.
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

// awaitOutputAfterExit waits, after the direct child has exited, for the output
// this call is still owed.
//
// The distinction it enforces is the one an earlier revision of this branch got
// wrong: the leader exiting means *the leader* is done, not that output is done.
// A wrapper can exit 0 while the real CLI, which inherited its stdout, has not
// printed yet. Only pipe EOF means every write end is closed and no more output
// is coming, so that is what this waits for — bounded by collectDrainGrace, since
// a descendant that never closes the pipe must not hold the call forever.
//
// complete short-circuits the wait: once the answer is in the buffer there is
// nothing left to owe, so a CLI that prints and exits pays nothing here even when
// it leaves a helper on the pipe. With a nil rule there is no way to recognise the
// answer, so EOF or the bound are the only stopping conditions.
//
// Note it does *not* additionally require the buffer to have gone idle. Idleness
// is a proxy for "the writer stopped", and the leader's exit is direct evidence
// of that; demanding both would charge every well-behaved call an idle grace it
// has already earned.
func (c *collector) awaitOutputAfterExit(ctx context.Context, complete OutputComplete) {
	if complete != nil && complete(c.stdout.snapshot()) {
		return
	}
	deadline := time.Now().Add(collectDrainGrace)
	ticker := time.NewTicker(quietPoll)
	defer ticker.Stop()
	for {
		select {
		case <-c.readers:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if complete != nil && complete(c.stdout.snapshot()) {
				return
			}
			if time.Now().After(deadline) {
				return
			}
		}
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
