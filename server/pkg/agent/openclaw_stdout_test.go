//go:build !windows

package agent

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	b, _ := newOpenclawTestBackendWithLog(bin)
	return b
}

// newOpenclawTestBackendWithLog also captures what the backend logs, so a test
// can assert that a specific branch was taken instead of inferring it from
// timing. Warnings still reach stderr so a failure stays readable.
func newOpenclawTestBackendWithLog(bin string) (*openclawBackend, *syncBuffer) {
	buf := &syncBuffer{}
	return &openclawBackend{cfg: Config{
		ExecutablePath: bin,
		Logger: slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, buf),
			&slog.HandlerOptions{Level: slog.LevelWarn})),
	}}, buf
}

// syncBuffer is a bytes.Buffer safe for the backend's logging goroutine to write
// to while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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
// that pipe), while the descendant keeps stderr open for 5s, well past the
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
#
# 5s rather than something nearer WaitDelay: at ~1s a loaded runner could let
# this descendant exit before Wait's 500ms timer elapses, so ErrWaitDelay would
# never fire and the test would pass without exercising the branch it exists
# for. The margin plus the log assertion below turn that vacuous pass into a
# failure.
( sleep 5 ) >/dev/null &
cat <<'JSON'
` + completeOpenclawResult + `
JSON
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write openclaw stub: %v", err)
	}
	b, logs := newOpenclawTestBackendWithLog(bin)

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
	// Without this the test could pass on a run where the descendant happened to
	// exit first, leaving the ErrWaitDelay branch untested and this regression
	// silently uncovered.
	if !strings.Contains(logs.String(), "held a pipe past WaitDelay") {
		t.Errorf("the ErrWaitDelay branch was never taken, so this test proved "+
			"nothing about it; logged warnings were: %q", logs.String())
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

// stagedOpenclawEOFReader returns an incomplete result first, then waits until
// the test releases the final bytes. The final Read returns data and io.EOF
// together, so completion and clean EOF are one deterministic observation
// rather than two independently scheduled pipe operations.
type stagedOpenclawEOFReader struct {
	prefix        string
	suffix        string
	suffixRead    chan struct{}
	releaseSuffix chan struct{}
	readCount     int
}

func (r *stagedOpenclawEOFReader) Read(p []byte) (int, error) {
	switch r.readCount {
	case 0:
		r.readCount++
		return copy(p, r.prefix), nil
	case 1:
		r.readCount++
		close(r.suffixRead)
		<-r.releaseSuffix
		return copy(p, r.suffix), io.EOF
	default:
		return 0, io.EOF
	}
}

// TestReadOpenclawStdoutWaitsForCompleteResult pins the safety half of the
// shortcut: idle output alone is not enough. Cutting off a partial buffer would
// throw away work the agent has already done, which is worse than the hang this
// change fixes.
func TestReadOpenclawStdoutWaitsForCompleteResult(t *testing.T) {
	r := &stagedOpenclawEOFReader{
		prefix:        `{"payloads":[{"text":"half`,
		suffix:        `"}],"meta":{"durationMs":1}}`,
		suffixRead:    make(chan struct{}),
		releaseSuffix: make(chan struct{}),
	}
	released := false
	t.Cleanup(func() {
		if !released {
			close(r.releaseSuffix)
		}
	})

	type outcome struct {
		cutShort bool
		buf      string
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		buf, cutShort, err := readOpenclawStdout(r, 200*time.Millisecond)
		done <- outcome{cutShort: cutShort, buf: string(buf), err: err}
	}()

	select {
	case <-r.suffixRead:
		// The incomplete prefix has been consumed and the reader is now
		// deliberately silent until the test releases the suffix.
	case <-time.After(5 * time.Second):
		t.Fatal("readOpenclawStdout did not request the final bytes")
	}

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

	// Complete the blob and report EOF in the same Read. The old os.Pipe test
	// performed a write and close separately, allowing the idle ticker to win
	// after the bytes became parseable but before the read goroutine observed
	// EOF on a loaded CI runner.
	released = true
	close(r.releaseSuffix)

	select {
	case got := <-done:
		if got.err != nil {
			t.Errorf("readOpenclawStdout: %v", got.err)
		}
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
