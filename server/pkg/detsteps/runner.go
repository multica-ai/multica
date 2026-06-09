package detsteps

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/multica-ai/multica/server/pkg/dettools"
)

// runStepEnv is the sentinel that turns any multica-family binary into a
// one-shot step sandbox. When set, MaybeRunStepChild (called first thing in each
// main) reads a request from stdin, runs the step, writes the Result to stdout,
// and exits — the process never reaches normal startup.
const runStepEnv = "MULTICA_DETSTEPS_CHILD"

// killGrace is how long past the step's own deadline the parent waits before
// SIGKILLing the child. The child's cooperative timeout normally wins; the kill
// is the hard backstop for a child that wedges uninterruptibly.
const killGrace = 2 * time.Second

// stepRequest is the stdin payload the parent sends the child.
type stepRequest struct {
	Source    string         `json:"source"`
	Input     map[string]any `json:"input"`
	TimeoutMS int64          `json:"timeout_ms"`
}

// RunSubprocess executes a step in a separate, killable process built from
// selfBin (the current binary, re-exec'd as a step sandbox). This is the
// production entry point: unlike the in-process Run, a runaway step is hard
// SIGKILLed instead of leaking a goroutine into a long-lived server, and the
// child runs with a minimal environment (no inherited secrets) plus OS resource
// limits where the platform supports them.
func RunSubprocess(ctx context.Context, selfBin, source string, input map[string]any, timeout time.Duration) dettools.Result {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if selfBin == "" {
		// No binary to re-exec (should not happen in production); fall back to the
		// in-process path so the feature degrades rather than fails.
		return Run(ctx, source, input, Options{Timeout: timeout})
	}

	reqJSON, err := json.Marshal(stepRequest{Source: source, Input: input, TimeoutMS: timeout.Milliseconds()})
	if err != nil {
		return dettools.Errf(dettools.CodeInternal, "encode step request: %v", err)
	}

	// The parent kill deadline sits past the step deadline so the child's own
	// graceful TIMEOUT result wins normally and the SIGKILL is only a backstop.
	killCtx, cancel := context.WithTimeout(ctx, timeout+killGrace)
	defer cancel()

	cmd := exec.CommandContext(killCtx, selfBin)
	// Minimal environment: the sentinel only. The child needs no PATH (it execs
	// nothing) and must not see the server's secrets (DB creds, tokens, …).
	cmd.Env = []string{runStepEnv + "=1"}
	cmd.Stdin = bytes.NewReader(reqJSON)
	// Ensure Output() doesn't block indefinitely if a killed child leaves a pipe
	// open; give the kernel a moment to tear things down after the deadline.
	cmd.WaitDelay = killGrace

	out, err := cmd.Output()
	if killCtx.Err() == context.DeadlineExceeded {
		return dettools.Errf(dettools.CodeTimeout, "step exceeded the %s time limit (process killed)", timeout)
	}
	if ctx.Err() != nil {
		return dettools.Errf(dettools.CodeTimeout, "step cancelled: %v", ctx.Err())
	}
	if err != nil {
		return dettools.Errf(dettools.CodeInternal, "step runner failed: %v", err)
	}

	var res dettools.Result
	if err := json.Unmarshal(out, &res); err != nil {
		return dettools.Errf(dettools.CodeInternal, "decode step result: %v", err)
	}
	return res
}

// MaybeRunStepChild runs the one-shot step sandbox when the process was
// re-exec'd with runStepEnv set, then exits. Otherwise it returns immediately.
// Every multica-family main must call this first thing so the binary can act as
// its own isolated step runner.
func MaybeRunStepChild() {
	if os.Getenv(runStepEnv) != "1" {
		return
	}
	var req stepRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		emitChildResult(dettools.Errf(dettools.CodeInternal, "decode step request: %v", err))
		os.Exit(0)
	}

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	// Kernel CPU-time backstop (no-op off unix): if the parent's wall-clock kill
	// ever fails, RLIMIT_CPU still terminates a pure CPU spin.
	limitChildCPU(timeout)
	// Run in-process here: this whole process is disposable, so a leaked
	// interpreter goroutine dies with it when we exit.
	res := Run(context.Background(), req.Source, req.Input, Options{Timeout: timeout})
	emitChildResult(res)
	os.Exit(0)
}

func emitChildResult(res dettools.Result) {
	// The Result is the only thing on stdout; anything else corrupts the parent's
	// decode. Marshal errors degrade to a minimal internal-error envelope.
	data, err := json.Marshal(res)
	if err != nil {
		data = []byte(`{"status":"error","error_code":"INTERNAL_ERROR","summary":"failed to encode result","retryable":true}`)
	}
	_, _ = os.Stdout.Write(data)
}
