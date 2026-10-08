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
// Nor is that a quirk of the build those timings came from. Upstream has an
// explicit opt-in for "print a one-shot answer and then exit" —
// `requestExitAfterOneShotOutput`, flushed at the end of `runCli` after both
// streams drain (`src/cli/one-shot-exit.ts`, checked at `v2026.7.1`). In the
// shipped 2026.7.1 build only `models list`, `models status` and the hooks CLI
// take it. `config file`, `config get` and `agents list` do not: their success
// paths only print, so termination is whatever the Node event loop happens to
// do, and `runtime.exit` appears on their failure paths alone. So a version
// where they exit promptly is not making a promise a later one has to keep.
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
//     yields the banner instead of the path. The rule carries the whole weight of
//     that distinction: review broke a rule that judged the path by *shape* with a
//     warning line that was itself a path, so a rule has to be one the CLI's
//     pre-answer output cannot satisfy at all.
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
// Guarantees callers depend on:
//
//  1. Returns within roughly the caller's context deadline plus collectDrainGrace,
//     collectReapWindow and collectSettleGrace, whatever the CLI leaves behind.
//  2. Descendants that are still in the leader's process group are signalled
//     before returning, and the signal is repeated across collectReapWindow so one
//     that was mid-fork does not escape. A descendant that left the group is out
//     of reach on Unix — OpenClaw's helper was measured with its own PGID and SID —
//     which is why guarantee 1 does not depend on the kill landing. Windows is
//     different: the Job Object owns the tree irrespective of groups.
//  3. Output is not abandoned while something may still be writing it: after the
//     direct child exits, the call waits for pipe EOF unless `complete` says the
//     answer is in. Leader exit alone is not treated as completion.
//  4. Retention is bounded — collectStdoutLimit for the answer (reported, not
//     silently truncated) and collectStderrTail for the diagnostic sample.
//  5. The command's real exit status is reported, which openclawShimDiagnostic
//     depends on (it type-switches on *exec.ExitError).
//
// Use this only for commands whose entire output is a short one-shot response.
// Anything that streams incrementally (agent execution) must keep its own
// lifecycle handling, where a pause in output carries meaning.
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
