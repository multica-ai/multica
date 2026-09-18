//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The two shapes runtimeStream exists for, driven through the real claude and
// pi backends rather than the type in isolation: what matters is that a
// terminal Result still reaches the daemon, which is the part that used to
// hang.
//
// The fake CLI is a shell script, so a backgrounded child inherits stdout
// directly — the most direct form of the descendant-holds-the-pipe shape.

func claudeStreamScript(events []string, tail string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("case \"$1\" in --version) printf 'fake 2026.5.5\\n'; exit 0;; esac\n")
	b.WriteString("cat > /dev/null 2>/dev/null &\n")
	for _, e := range events {
		b.WriteString("printf '%s\\n' '" + e + "'\n")
	}
	b.WriteString(tail)
	return b.String()
}

const claudeFakeResultEvent = `{"type":"result","subtype":"success","session_id":"s1","is_error":false,"result":"final report posted","usage":{"input_tokens":10,"output_tokens":2}}`

// runFakeBackend runs agentType's real backend against a fake CLI, with both
// fallback windows shortened on this Config alone so parallel tests cannot
// shorten each other's.
func runFakeBackend(t *testing.T, agentType, script string, terminalGrace, exitDrain time.Duration) *Session {
	t.Helper()
	return runFakeBackendWithPrompt(t, agentType, script, "prompt-ignored", terminalGrace, exitDrain)
}

func runFakeBackendWithPrompt(t *testing.T, agentType, script, prompt string, terminalGrace, exitDrain time.Duration) *Session {
	t.Helper()
	dir := t.TempDir()
	fakePath := filepath.Join(dir, agentType)
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New(agentType, Config{
		ExecutablePath:      fakePath,
		Logger:              slog.New(slog.DiscardHandler),
		streamTerminalGrace: terminalGrace,
		streamExitDrain:     exitDrain,
	})
	if err != nil {
		t.Fatalf("new %s backend: %v", agentType, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	session, err := backend.Execute(ctx, prompt, ExecOptions{
		Timeout:         0, // mirrors production: daemon.DefaultAgentTimeout = 0
		Cwd:             dir,
		ResumeSessionID: filepath.Join(dir, "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute %s: %v", agentType, err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	return session
}

func awaitResult(t *testing.T, session *Session, budget time.Duration) (Result, time.Duration) {
	t.Helper()
	start := time.Now()
	select {
	case res := <-session.Result:
		return res, time.Since(start)
	case <-time.After(budget):
		t.Fatalf("no terminal Result within %s — the run is stuck", budget)
		return Result{}, 0
	}
}

// A descendant that inherited stdout outlives the CLI. Before runtimeStream the
// scanner blocked on that descendant's write end and no Result was ever
// published: the task stayed "running" until the idle watchdog reported the
// successful run as failed.
func TestRuntimeStreamPublishesResultWhenDescendantHoldsStdout(t *testing.T) {
	t.Parallel()
	// The terminal grace is set out of reach, so only the exit drain can end
	// this run — which is what the shape under test needs to prove. Asserting
	// on wall-clock instead would measure the machine's load, not the fix.
	session := runFakeBackend(t, "claude", claudeStreamScript(
		[]string{claudeFakeResultEvent},
		"(sleep 40) &\nexit 0\n",
	), time.Minute, 300*time.Millisecond)

	res, _ := awaitResult(t, session, 20*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed", res.Status, res.Error)
	}
	if !strings.Contains(res.Output, "final report posted") {
		t.Fatalf("Output: got %q, want the agent's own output", res.Output)
	}
}

// The CLI writes its terminal event and then never exits — openclaw's observed
// production shape. The leader is alive, so the exit drain cannot fire; the
// protocol boundary is the only thing that can end the run.
func TestRuntimeStreamEndsRunWhenCLIDoesNotExitAfterTerminalEvent(t *testing.T) {
	t.Parallel()
	session := runFakeBackend(t, "claude", claudeStreamScript(
		[]string{claudeFakeResultEvent},
		"sleep 30\n",
	), 500*time.Millisecond, 300*time.Millisecond)

	res, _ := awaitResult(t, session, 10*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed — the protocol said the run succeeded", res.Status, res.Error)
	}
	if !strings.Contains(res.Output, "final report posted") {
		t.Fatalf("Output: got %q, want the agent's own output", res.Output)
	}
}

// Pi emits agent_end before it decides whether to retry, so agent_end alone is
// not the end of the run. auto_retry_start withdraws it; the silent backoff
// that follows must not be mistaken for a finished run that failed to exit.
func TestRuntimeStreamDoesNotCutPiRetryAfterAgentEnd(t *testing.T) {
	t.Parallel()
	// A grace far shorter than the backoff: without runContinues() the retry
	// would be cut off and the run would report the first turn's failure.
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[],"model":"test","usage":{"input":0,"output":0},"stopReason":"error","errorMessage":"OpenAI API error (503): no available channel"}}`,
		`{"type":"agent_end","messages":[],"willRetry":true}`,
		`{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":1500}`,
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\ncat > /dev/null\n")
	for _, e := range events {
		b.WriteString("printf '%s\\n' '" + e + "'\n")
	}
	// The backoff: silent, and longer than the terminal grace.
	b.WriteString("sleep 1.5\n")
	for _, e := range []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"recovered"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":2,"output":2}}}`,
		`{"type":"agent_end","messages":[]}`,
	} {
		b.WriteString("printf '%s\\n' '" + e + "'\n")
	}
	b.WriteString("exit 0\n")

	session := runFakeBackend(t, "pi", b.String(), 300*time.Millisecond, 300*time.Millisecond)

	res, _ := awaitResult(t, session, 15*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed — the retry succeeded", res.Status, res.Error)
	}
	if res.Output != "recovered" {
		t.Fatalf("Output: got %q, want %q — the retry's output was cut off", res.Output, "recovered")
	}
}

// The same shape through stderr. os/exec joins its own stderr copy goroutine
// inside cmd.Wait(), so a descendant holding stderr stalls Wait even when
// stdout is perfectly clean — bounded by cmd.WaitDelay, but ending in
// exec.ErrWaitDelay, which the backend reports as a failed run. Owning the
// pipe is what keeps a successful run reported as successful.
func TestRuntimeStreamPublishesResultWhenDescendantHoldsStderr(t *testing.T) {
	t.Parallel()

	// >/dev/null so the child holds stderr only: stdout reaches EOF normally
	// and neither fallback has anything to do.
	session := runFakeBackend(t, "claude", claudeStreamScript(
		[]string{claudeFakeResultEvent},
		"(sleep 40) >/dev/null &\nexit 0\n",
	), time.Minute, time.Minute)

	res, elapsed := awaitResult(t, session, 20*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed", res.Status, res.Error)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("waited %s: stderr must not gate the run's terminal result", elapsed)
	}
}

// Output written immediately before the leader exits is still in the pipe when
// it is reaped. The drain exists to collect it: closing on leader exit alone
// would drop the terminal event and turn a successful run into a failure.
func TestRuntimeStreamKeepsOutputWrittenJustBeforeExit(t *testing.T) {
	t.Parallel()
	session := runFakeBackend(t, "claude", claudeStreamScript(
		[]string{
			`{"type":"system","subtype":"init","session_id":"s1","model":"test"}`,
			claudeFakeResultEvent,
		},
		"(sleep 40) &\nexit 0\n",
	), time.Minute, time.Second)

	res, _ := awaitResult(t, session, 20*time.Second)
	if !strings.Contains(res.Output, "final report posted") {
		t.Fatalf("Output: got %q, want the result written just before exit", res.Output)
	}
}

// The fallbacks must cost nothing when EOF arrives on its own. A run that ends
// normally is the overwhelming majority; if it paid the grace, every task on
// every runtime would get slower.
func TestRuntimeStreamAddsNoLatencyToACleanExit(t *testing.T) {
	t.Parallel()
	// Both windows are set far beyond the test's budget: if a clean exit paid
	// either of them, this cannot pass. That keeps the assertion about the
	// fallbacks rather than about how fast this machine starts a process.
	session := runFakeBackend(t, "claude", claudeStreamScript(
		[]string{claudeFakeResultEvent},
		"exit 0\n",
	), time.Minute, time.Minute)

	res, elapsed := awaitResult(t, session, 20*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed", res.Status, res.Error)
	}
	if elapsed >= time.Minute {
		t.Fatalf("clean exit took %s: a run that reaches EOF must not wait for either fallback", elapsed)
	}
}

// Pi keeps going after agent_end in a second documented way: context
// compaction. The overflow case compacts — an LLM call, silent on stdout for
// as long as it takes — and then continues generating. compaction_start
// withdraws the boundary agent_end armed; compaction_end says in willRetry
// whether more output is coming.
func TestRuntimeStreamDoesNotCutPiCompactionAfterAgentEnd(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("#!/bin/sh\ncat > /dev/null\n")
	for _, e := range []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"error","errorMessage":"context window exceeded"}}`,
		`{"type":"agent_end","messages":[]}`,
		`{"type":"compaction_start","reason":"overflow"}`,
	} {
		b.WriteString("printf '%s\\n' '" + e + "'\n")
	}
	// Compaction itself: silent, and longer than the terminal grace.
	b.WriteString("sleep 1.5\n")
	for _, e := range []string{
		`{"type":"compaction_end","reason":"overflow","aborted":false,"willRetry":true}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"recovered after compaction"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"stop"}}`,
		`{"type":"agent_end","messages":[]}`,
	} {
		b.WriteString("printf '%s\\n' '" + e + "'\n")
	}
	b.WriteString("exit 0\n")

	session := runFakeBackend(t, "pi", b.String(), 300*time.Millisecond, 300*time.Millisecond)

	res, _ := awaitResult(t, session, 15*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed — compaction recovered the run", res.Status, res.Error)
	}
	if res.Output != "recovered after compaction" {
		t.Fatalf("Output: got %q, want the post-compaction output", res.Output)
	}
}

// A launcher exits while the real CLI, its child, has not read the prompt yet.
// os/exec closes the pipes it created inside cmd.Wait(), and this package now
// calls Wait as soon as the leader is reaped — so an os/exec-owned stdin would
// be closed out from under a prompt write that is still in flight.
func TestRuntimeStreamKeepsPromptWritableWhenLauncherExitsFirst(t *testing.T) {
	t.Parallel()

	// The prompt is larger than the pipe buffer, so the write is still in
	// flight when the launcher exits.
	script := `#!/bin/sh
(
  sleep 0.5
  cat > /dev/null
  printf '%s\n' '{"type":"agent_start"}'
  printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"read full prompt"}}'
  printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"stop"}}'
  printf '%s\n' '{"type":"agent_end","messages":[]}'
) <&0 &
exit 0
`
	session := runFakeBackendWithPrompt(t, "pi", script, strings.Repeat("x", 1024*1024), 5*time.Second, 5*time.Second)

	res, _ := awaitResult(t, session, 20*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed — the prompt write must survive the launcher", res.Status, res.Error)
	}
}

// A CLI that closes its own stdout and stderr but keeps running. Both pipes
// reach EOF, so the scanner finishes and the stderr pump finishes — but the
// adapter is then parked in wait() on a process that is still alive, which is
// why supervision cannot stop at EOF.
func TestRuntimeStreamBoundsALiveLeaderThatClosedItsOutput(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
read line
printf '%s\n' '` + claudeFakeResultEvent + `'
exec 1>/dev/null 2>/dev/null
sleep 30
`
	session := runFakeBackend(t, "claude", script, 300*time.Millisecond, 300*time.Millisecond)

	res, _ := awaitResult(t, session, 15*time.Second)
	if res.Status != "completed" {
		t.Fatalf("Status: got %q (error=%q), want completed", res.Status, res.Error)
	}
}
