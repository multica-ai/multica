package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// DefaultQuietIdleGrace is how long stdout must stay silent, on top of already
// carrying a complete answer, before RunCollectQuiet stops waiting for a CLI to
// exit.
//
// Sized for "the CLI already flushed its answer": these commands print a path or
// a short JSON document, and the writes of one logical response land
// back-to-back — measured at 0.012–0.033ms apart on a real host. 400ms is orders
// of magnitude longer than that, and short enough that the calls task setup
// makes cost about a second in the misbehaving case instead of a full
// openclawCLITimeout each.
//
// What that sizing does *not* cover, and what the caller's `complete` rule has
// to: a gap between output that is not the answer and the answer itself. On the
// same host, a config that produces Doctor and plugin warnings put a single
// 3285ms silence between the end of the warning block and the config path. Any
// rule that accepts something printed before the answer therefore has 3.2s in
// which to fire on it, and no value here can fix that — review demonstrated it
// against a 5s grace with a warning line that was itself a path.
//
// So the requirement lands on the rules rather than on this constant: a rule must
// be unsatisfiable by anything the CLI prints before its answer. The only rule in
// this package that qualifies is JSONOutputComplete, because a document has to
// parse as a whole and because upstream keeps incidental output off a `--json`
// stdout entirely. Judging human-readable output by shape does not qualify, and
// the `config file` rule that tried was removed rather than retuned — see
// openclawOutputComplete in internal/daemon/execenv.
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

// RunCollectQuiet collects a short one-shot response until process exit, or
// until complete accepts the buffer and no output arrives for idleGrace.
// Both checks are required: banners can be idle, and a complete-looking answer
// may still be followed by output. A nil complete disables early return.
//
// Context expiry is always an error, with captured output returned for inspection.
// quiet distinguishes accepted output from a clean process exit.
//
// The collector owns pipes and process cleanup so descendants holding stdout do
// not extend the deadline beyond the drain/reap/settle grace windows. Unix signals
// reach the original process group; Windows uses a Job Object. The return bound
// does not depend on every descendant being reachable.
//
// Leader exit alone is insufficient: wait for pipe EOF unless complete accepts
// the answer. Retention is bounded and overflow is reported; preserve the real
// exit status for callers inspecting exec.ExitError. Streaming agent execution
// needs its own lifecycle handling and must not use this helper.
func RunCollectQuiet(ctx context.Context, env []string, idleGrace time.Duration, complete OutputComplete, execPath string, args ...string) (stdout []byte, stderr string, quiet bool, err error) {
	// Command.exec, not exec.CommandContext: launch.go owns process construction so
	// a custom runtime's fixed_args prefix cannot be dropped (GH #7046). A zero
	// Command has no prefix, which is what a bare path argument means here.
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
			// The direct child is gone, which is not the same as "no more output
			// is coming" — a wrapper may have exited while the real CLI still
			// owes us the answer. Wait for pipe EOF, bounded, unless the answer
			// is already in; see awaitOutputAfterExit.
			c.awaitOutputAfterExit(ctx, complete)
			c.finish()
			out := c.stdout.snapshot()
			if c.stdout.overflowed() {
				return out, string(c.stderr.snapshot()), false, collectStdoutOverflowErr()
			}
			return out, string(c.stderr.snapshot()), false, c.waitErr

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
			if c.stdout.overflowed() {
				return out, string(c.stderr.snapshot()), true, collectStdoutOverflowErr()
			}
			return out, string(c.stderr.snapshot()), true, nil
		}
	}
}

// errCollectStdoutTooLarge reports that the answer exceeded collectStdoutLimit.
//
// Reported rather than returned quietly: these helpers run one-shot commands
// whose response is a path or a short document, so overflow means the CLI is
// malfunctioning, and a caller that parsed a head-truncated answer would be
// parsing something the CLI never said. stderr is capped by keeping its tail
// instead, because it is a diagnostic sample and not an answer — dropping its
// front loses nothing a caller relies on.
var errCollectStdoutTooLarge = errors.New("collected stdout exceeded its limit")

// collectStdoutOverflowErr names the limit in the message while keeping
// errCollectStdoutTooLarge as the sentinel callers match on.
func collectStdoutOverflowErr() error {
	return fmt.Errorf("%w (%d bytes)", errCollectStdoutTooLarge, collectStdoutLimit)
}
