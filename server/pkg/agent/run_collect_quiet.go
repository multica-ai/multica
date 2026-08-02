package agent

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultQuietIdleGrace is how long stdout must stay silent before
// RunCollectQuiet treats a one-shot command's output as complete.
//
// Sized for "the CLI already flushed its answer": these commands print a path
// or a short JSON blob, and the writes of one logical response land
// back-to-back. 400ms is orders of magnitude longer than the gap between those
// writes, and short enough that the three calls task setup makes cost ~1.2s in
// the misbehaving case instead of one full openclawCLITimeout each.
const DefaultQuietIdleGrace = 400 * time.Millisecond

// quietWriter buffers output and records when the last write landed, so
// RunCollectQuiet can tell "still producing" from "flushed and now idle".
type quietWriter struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	lastByte atomic.Int64 // unix nanos of the last write; 0 = nothing yet
}

func (w *quietWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buf.Write(p)
	w.mu.Unlock()
	if n > 0 {
		w.lastByte.Store(time.Now().UnixNano())
	}
	return n, err
}

func (w *quietWriter) snapshot() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

// idleFor reports how long since the last write, and whether anything has been
// written at all.
func (w *quietWriter) idleFor() (time.Duration, bool) {
	last := w.lastByte.Load()
	if last == 0 {
		return 0, false
	}
	return time.Since(time.Unix(0, last)), true
}

// RunCollectQuiet runs a one-shot CLI command and returns as soon as either the
// process exits *or* it has produced output that then stayed idle for
// idleGrace. The process group is reaped before returning on every path.
//
// # Why "output is enough" beats "wait for exit"
//
// Measured on an OpenClaw host (openclaw 2026.5.27):
//
//	openclaw --version    258ms  exits cleanly
//	openclaw config file    60s  correct path printed, then never exits
//	openclaw agents list    60s  correct list printed, then never exits
//
// Waiting for exit turns two working commands into a task-fatal error, because
// they sit on the task's critical path (Prepare → prepareOpenclawConfig):
//
//	prepare execution environment: execenv: prepare openclaw config:
//	locate openclaw active config: openclaw config file:
//	context deadline exceeded (process: signal: killed)
//
// The answer was on stdout the whole time. The contract of these commands is
// "print a value"; once the value has arrived and nothing more is coming,
// whether the process tidies itself up is not the caller's business, and it
// certainly should not fail a chat task.
//
// quiet reports that the return came from the idle path rather than a clean
// exit, so callers can log the CLI's misbehaviour without failing on it.
//
// Use this only for commands whose entire output is a short one-shot response.
// Anything that streams incrementally (agent execution) must keep its own
// lifecycle handling, where a pause in output carries meaning.
func RunCollectQuiet(ctx context.Context, env []string, idleGrace time.Duration, execPath string, args ...string) (stdout []byte, stderr string, quiet bool, err error) {
	if idleGrace <= 0 {
		idleGrace = DefaultQuietIdleGrace
	}

	cmd := exec.CommandContext(ctx, execPath, args...)
	if env != nil {
		cmd.Env = env
	}
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)

	outW := &quietWriter{}
	errW := &quietWriter{}
	cmd.Stdout = outW
	cmd.Stderr = errW

	if startErr := cmd.Start(); startErr != nil {
		return nil, "", false, startErr
	}

	// os/exec owns the draining here — we handed it io.Writers, not the
	// *os.File that RunCollect uses — so Wait can be held open by a
	// pipe-holding descendant. That is precisely why Wait does not get to
	// decide when we are done: the loop below decides, and reapProcessTree
	// releases whatever is held. The channel is buffered so this goroutine can
	// always finish even when nobody is left to receive.
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	// finish reaps the tree and gives Wait a bounded moment to observe the
	// kill, so the child is collected instead of left as a zombie.
	finish := func() error {
		reapProcessTree(cmd)
		select {
		case waitErr := <-waited:
			return waitErr
		case <-time.After(collectDrainGrace):
			return nil
		}
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case waitErr := <-waited:
			// Clean exit (or a real non-zero one): the normal path, and it
			// costs no idle wait at all.
			reapProcessTree(cmd)
			return outW.snapshot(), string(errW.snapshot()), false, waitErr

		case <-ctx.Done():
			waitErr := finish()
			out := outW.snapshot()
			// Salvage: a command that printed its answer and then hung has
			// done its job as far as the caller is concerned.
			if len(bytes.TrimSpace(out)) > 0 {
				return out, string(errW.snapshot()), true, nil
			}
			// Nothing to salvage, so the deadline stands. Prefer the process
			// error when we have one — callers attribute ctx themselves and a
			// "signal: killed" detail is worth keeping in the message.
			if waitErr != nil {
				return out, string(errW.snapshot()), true, waitErr
			}
			return out, string(errW.snapshot()), true, ctx.Err()

		case <-ticker.C:
			idle, produced := outW.idleFor()
			if !produced || idle < idleGrace {
				continue
			}
			if len(bytes.TrimSpace(outW.snapshot())) == 0 {
				continue
			}
			// Output flushed and gone quiet: take it and reap the tree.
			_ = finish()
			return outW.snapshot(), string(errW.snapshot()), true, nil
		}
	}
}
