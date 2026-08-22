package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// DefaultQuietIdleGrace is how long stdout must stay silent, on top of already
// carrying a complete answer, before RunCollectQuiet stops waiting for a CLI to
// exit.
//
// Sized for "the CLI already flushed its answer": these commands print a path or
// a short JSON document, and the writes of one logical response land
// back-to-back. 400ms is orders of magnitude longer than the gap between those
// writes, and short enough that the calls task setup makes cost about a second
// in the misbehaving case instead of a full openclawCLITimeout each.
//
// It is also the window in which a *late* failure can still be observed: a CLI
// that prints a complete answer and then exits non-zero is reported as the
// failure it is, as long as it does so within the grace. Beyond that it is
// indistinguishable from one that prints an answer and hangs forever, which is
// the case this whole helper exists to survive.
const DefaultQuietIdleGrace = 400 * time.Millisecond

// quietPoll is how often RunCollectQuiet re-evaluates its exit conditions.
const quietPoll = 50 * time.Millisecond

// OutputComplete reports whether the bytes captured so far are a *finished*
// answer for the command that produced them.
//
// This is the caller's judgement, not something the runner can infer: only the
// caller knows whether `{"agents":[{"id":"a"},` is a truncated document or a
// legitimate one. Without it, "we have some output" gets mistaken for "we have
// the output" and a response interrupted mid-flight is reported as success.
type OutputComplete func(stdout []byte) bool

// JSONOutputComplete is the rule for commands whose entire answer is one JSON
// document. Empty output is never complete; a lone `null` is, since that is how
// the CLI reports an unset key.
//
// Note this requires the whole buffer to parse, so a CLI that prints banner text
// before its JSON never satisfies it. That matches how the callers parse the
// result anyway (a whole-buffer json.Unmarshal), so such output was never usable
// — it now fails on the deadline instead of on the parse.
func JSONOutputComplete(stdout []byte) bool {
	trimmed := bytes.TrimSpace(stdout)
	return len(trimmed) > 0 && json.Valid(trimmed)
}

// RunCollectQuiet runs a one-shot CLI command and returns when the process
// exits, or — for a CLI that will not exit — as soon as it has produced output
// that `complete` accepts and that has then stayed idle for idleGrace.
//
// # Why "output is enough" beats "wait for exit"
//
// Measured on a host running openclaw 2026.5.27:
//
//	openclaw --version    258ms  exits cleanly
//	openclaw config file    60s  correct path printed, then never exits
//	openclaw agents list    60s  correct list printed, then never exits
//
// Waiting for exit turns two working commands into a task-fatal error, and they
// sit on the task's critical path (Prepare -> prepareOpenclawConfig). The
// contract of those commands is "print a value"; once the value has arrived,
// whether the process tidies itself up is not the caller's business.
//
// # Why `complete` is required rather than "any output"
//
// An earlier revision returned success from the deadline branch whenever stdout
// was non-empty. That salvaged partial answers: a CLI still streaming when the
// deadline arrived had its truncated output reported as success (measured 9 runs
// in 10 against a 250ms deadline). Two conditions now gate the early return, and
// neither is sufficient alone:
//
//   - `complete` accepts the buffer. Idle alone is not enough, because the CLI
//     may be pausing between a banner and the real answer — `openclaw config
//     file` prints Doctor warning UI first (see MUL-3136), and cutting off there
//     yields the banner instead of the path.
//   - The buffer has then been idle for idleGrace. Complete alone is not enough,
//     because more output may still follow and change the answer.
//
// Reaching the context deadline is never success. Whatever was captured is
// returned alongside the error so a caller with a different rule can inspect it,
// but this helper does not decide on the caller's behalf.
//
// A nil `complete` disables the early return entirely: the call then waits for
// exit, bounded only by ctx. That is the right default for a command whose
// output shape has no completeness rule.
//
// quiet reports that the return did not come from a clean exit, so callers can
// log the CLI's misbehaviour without failing on it.
//
// Use this only for commands whose entire output is a short one-shot response.
// Anything that streams incrementally (agent execution) must keep its own
// lifecycle handling, where a pause in output carries meaning.
func RunCollectQuiet(ctx context.Context, env []string, idleGrace time.Duration, complete OutputComplete, execPath string, args ...string) (stdout []byte, stderr string, quiet bool, err error) {
	// Command.exec for the same reason as RunCollect; see the note there.
	return RunCollectQuietCmd(ctx, Command{Path: execPath}.exec(ctx, args...), env, idleGrace, complete)
}

// RunCollectQuietCmd is RunCollectQuiet for a caller that already holds a
// *exec.Cmd, which is what Command.exec returns.
func RunCollectQuietCmd(ctx context.Context, cmd *exec.Cmd, env []string, idleGrace time.Duration, complete OutputComplete) (stdout []byte, stderr string, quiet bool, err error) {
	if idleGrace <= 0 {
		idleGrace = DefaultQuietIdleGrace
	}

	c, startErr := startCollector(cmd, env)
	if startErr != nil {
		return nil, "", false, startErr
	}

	ticker := time.NewTicker(quietPoll)
	defer ticker.Stop()

	for {
		select {
		case <-c.waitDone:
			// Clean exit, or a real non-zero one: the normal path, and it pays
			// no idle wait at all.
			c.finish()
			return c.stdout.snapshot(), string(c.stderr.snapshot()), false, c.waitErr

		case <-ctx.Done():
			c.finish()
			out := c.stdout.snapshot()
			// Deliberately no salvage. Reaching the deadline means we never saw
			// output that `complete` accepted, so by the caller's own rule the
			// buffer is unfinished. Prefer the process error when we have one:
			// callers attribute ctx themselves and a "signal: killed" detail is
			// worth keeping in the message.
			if werr, reaped := c.exitErr(); reaped && werr != nil {
				return out, string(c.stderr.snapshot()), true, werr
			}
			return out, string(c.stderr.snapshot()), true, ctx.Err()

		case <-ticker.C:
			if complete == nil {
				continue
			}
			idle, produced := c.stdout.idleFor()
			if !produced || idle < idleGrace {
				continue
			}
			out := c.stdout.snapshot()
			if !complete(out) {
				continue
			}
			// A finished answer that has gone quiet: take it and reap the tree.
			// `out` is the buffer `complete` accepted, deliberately not a fresh
			// snapshot — bytes a lingering descendant appends after that verdict
			// were never part of the answer and could only break the parse.
			c.finish()
			return out, string(c.stderr.snapshot()), true, nil
		}
	}
}
