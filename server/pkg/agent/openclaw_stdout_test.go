//go:build !windows

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reproduces the production failure where a chat reply was generated but never
// delivered. Timeline observed on an OpenClaw host:
//
//	T+0s     openclaw started
//	T+24s    the complete result blob was written to stdout
//	T+8min   process still alive, task slot still held, user saw nothing
//
// processOutput used io.ReadAll, which returns only at EOF, and EOF requires
// every write end of the pipe to be closed. openclaw had printed its complete
// result blob but would not exit, so the read never finished, cmd.Wait was
// never reached, and the finished answer sat in the daemon's buffer.

// completeOpenclawResult is the minimal blob that parses as a final result
// (payloads + meta.durationMs).
const completeOpenclawResult = `{"payloads":[{"text":"the agent reply text"}],` +
	`"meta":{"durationMs":1234,"agentMeta":{"sessionId":"sess-abc","model":"test-model"}}}`

// writeOpenclawStub creates a fake `openclaw` that satisfies the version gate,
// prints body for the `agent` subcommand, and then either exits or hangs.
func writeOpenclawStub(t *testing.T, body string, hangAfter bool) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	tail := "exit 0"
	if hangAfter {
		tail = "sleep 300"
	}
	script := `#!/bin/sh
case "$1" in
  --version) echo "openclaw 2026.5.27"; exit 0 ;;
esac
cat <<'JSON'
` + body + `
JSON
` + tail + `
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write openclaw stub: %v", err)
	}
	return bin
}

func newOpenclawTestBackend(bin string) *openclawBackend {
	return &openclawBackend{cfg: Config{
		ExecutablePath: bin,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}}
}

// TestOpenclawExecuteCompletesWhenCLINeverExits is the assertion that would have
// caught the undelivered-reply incident.
func TestOpenclawExecuteCompletesWhenCLINeverExits(t *testing.T) {
	bin := writeOpenclawStub(t, completeOpenclawResult, true)
	b := newOpenclawTestBackend(bin)

	// No per-run timeout in ExecOptions, matching production since MUL-3064
	// made the run timeout opt-in: completion must come from the protocol
	// boundary, not from a deadline. The ctx bound only keeps the test finite.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	session, err := b.Execute(ctx, "are you there", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
		// drain
	}
	result, ok := <-session.Result
	elapsed := time.Since(start)

	if !ok {
		t.Fatal("result channel closed without a result")
	}
	if result.Status != "completed" {
		t.Errorf("status = %q (error: %q), want completed — a delivered result "+
			"must not be reported as aborted just because we killed the "+
			"lingering process", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "the agent reply text") {
		t.Errorf("output = %q, lost the agent's reply", result.Output)
	}
	if result.SessionID != "sess-abc" {
		t.Errorf("session id = %q, want sess-abc", result.SessionID)
	}
	// Bound only has to be far below the 60s ctx and the stub's 300s sleep: if
	// the boundary mechanism failed, this takes one of those, not 20s.
	if elapsed > 20*time.Second {
		t.Errorf("took %v — waited on a process that never exits", elapsed)
	}
}

// TestOpenclawExecuteStillWorksWhenCLIExits guards the normal path: a
// well-behaved CLI must still be read to EOF and reported correctly.
//
// Deliberately no wall-clock assertion here. "A clean exit does not pay the
// idle grace" is a real property, but this test spawns the stub twice (version
// gate, then the run), so its elapsed time is dominated by process startup and
// an elapsed bound would be a CI flake rather than a check on the mechanism.
// That property is pinned deterministically in
// TestReadOpenclawStdoutDoesNotWaitForIdleGraceAtEOF instead.
func TestOpenclawExecuteStillWorksWhenCLIExits(t *testing.T) {
	bin := writeOpenclawStub(t, completeOpenclawResult, false)
	b := newOpenclawTestBackend(bin)

	session, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result

	if !ok {
		t.Fatal("result channel closed without a result")
	}
	if result.Status != "completed" {
		t.Errorf("status = %q (error %q), want completed", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "the agent reply text") {
		t.Errorf("output = %q, lost the reply on the clean-exit path", result.Output)
	}
	if result.SessionID != "sess-abc" {
		t.Errorf("session id = %q, want sess-abc", result.SessionID)
	}
}

// TestOpenclawExecuteToleratesLingeringStderrHolder is the regression for the
// review finding on #6276: lowering WaitDelay must not turn a delivered reply
// into an error.
//
// WaitDelay's timer starts when Wait observes the child has exited, not only on
// cancellation, so a *clean* exit reaches it whenever a descendant still holds
// one of the pipes os/exec manages — and cmd.Stderr here is a plain io.Writer,
// which is exactly such a pipe. Wait then returns exec.ErrWaitDelay despite the
// process having exited 0, and without the dedicated case that fell through to
// "openclaw exited with error" and discarded a fully parsed reply.
//
// The stub reproduces precisely that shape: stdout reaches EOF when the parent
// exits (the descendant's own stdout goes to /dev/null so it is not a writer on
// that pipe), while the descendant keeps stderr open for ~1s, well past the
// 500ms delay.
func TestOpenclawExecuteToleratesLingeringStderrHolder(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	script := `#!/bin/sh
case "$1" in
  --version) echo "openclaw 2026.5.27"; exit 0 ;;
esac
# Holds ONLY stderr: its stdout is /dev/null, so the stdout pipe's sole writer
# is this parent and EOF arrives as soon as it exits.
( sleep 1 ) >/dev/null &
cat <<'JSON'
` + completeOpenclawResult + `
JSON
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write openclaw stub: %v", err)
	}
	b := newOpenclawTestBackend(bin)

	session, err := b.Execute(context.Background(), "hi", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a result")
	}

	if result.Status != "completed" {
		t.Errorf("status = %q (error: %q), want completed — the process exited 0 "+
			"and the result was parsed; a descendant holding stderr past "+
			"WaitDelay must not discard a deliverable reply", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "the agent reply text") {
		t.Errorf("output = %q, lost the reply", result.Output)
	}
	if result.SessionID != "sess-abc" {
		t.Errorf("session id = %q, want sess-abc", result.SessionID)
	}
}

// TestReadOpenclawStdoutDoesNotWaitForIdleGraceAtEOF pins that the idle grace is
// only ever paid by a CLI that refuses to exit. A reader that reaches EOF must
// return immediately even when the grace is set absurdly high.
//
// No process is involved, so this is a deterministic check on the mechanism
// rather than a timing race: a regression that made the reader poll until the
// grace elapsed would take 10s instead of microseconds.
func TestReadOpenclawStdoutDoesNotWaitForIdleGraceAtEOF(t *testing.T) {
	start := time.Now()
	buf, cutShort, err := readOpenclawStdout(strings.NewReader(completeOpenclawResult), 10*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("readOpenclawStdout: %v", err)
	}
	if cutShort {
		t.Error("cutShort = true for a stream that reached EOF, want false — the " +
			"caller would cancel and log a warning for a well-behaved CLI")
	}
	if string(buf) != completeOpenclawResult {
		t.Errorf("buf = %q, want the full blob", buf)
	}
	if elapsed > time.Second {
		t.Errorf("took %v with a 10s idle grace — EOF must not wait out the "+
			"grace", elapsed)
	}
}

// TestReadOpenclawStdoutWaitsForCompleteResult pins the safety half of the
// shortcut: idle output alone is not enough. Cutting off a partial buffer would
// throw away work the agent has already done, which is worse than the hang this
// change fixes.
func TestReadOpenclawStdoutWaitsForCompleteResult(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pr.Close()

	// A partial blob that cannot parse, followed by silence.
	if _, err := pw.WriteString(`{"payloads":[{"text":"half`); err != nil {
		t.Fatalf("write: %v", err)
	}

	type outcome struct {
		cutShort bool
		buf      string
	}
	done := make(chan outcome, 1)
	go func() {
		buf, cutShort, _ := readOpenclawStdout(pr, 200*time.Millisecond)
		done <- outcome{cutShort: cutShort, buf: string(buf)}
	}()

	// Well past the idle grace: without the parse guard the reader would have
	// returned cutShort by now.
	time.Sleep(700 * time.Millisecond)
	select {
	case got := <-done:
		t.Fatalf("returned early on an unparseable buffer (cutShort=%v, buf=%q) "+
			"— the agent's partial output would be silently discarded",
			got.cutShort, got.buf)
	default:
	}

	// Completing the blob and closing gives the reader a clean EOF.
	if _, err := pw.WriteString(`"}],"meta":{"durationMs":1}}`); err != nil {
		t.Fatalf("write: %v", err)
	}
	pw.Close()

	select {
	case got := <-done:
		if got.cutShort {
			t.Error("cutShort = true after a clean EOF, want false")
		}
		if !strings.Contains(got.buf, `"durationMs":1`) {
			t.Errorf("buf = %q, lost the bytes written after the pause", got.buf)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readOpenclawStdout did not return after EOF")
	}
}
