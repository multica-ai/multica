//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// opencodeStreamTail is the minimal successful OpenCode event stream the fake
// CLIs below emit once they have recorded what the backend handed them.
const opencodeStreamTail = `printf '%s\n' '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}'
printf '%s\n' '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}'
printf '%s\n' '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}'
`

// opencodeStdinProbe runs the OpenCode backend against a fake CLI that records
// argv, drains stdin to EOF, and emits a successful OpenCode JSON stream.
func opencodeStdinProbe(t *testing.T, prompt string) ([]string, string, Result) {
	t.Helper()

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	script := fmt.Sprintf("#!/bin/sh\n"+
		": > %[1]q\n"+
		"for a in \"$@\"; do printf '%%s\\n' \"$a\" >> %[1]q; done\n"+
		"cat > %[2]q\n", argvPath, stdinPath) + opencodeStreamTail

	fakePath := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("opencode", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Timeout: 30 * time.Second,
		Model:   "lanz/Lanz-Medium",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	argv := strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n")
	return argv, string(stdinRaw), result
}

// TestOpencodeExecuteSendsPromptOnStdinNotArgv is the regression for #6538.
// The daemon used to inline the whole task prompt as an argv element, which
// Windows CreateProcess rejects once the command line clears 32,767 characters.
// The prompt must now reach OpenCode byte-for-byte on stdin while argv keeps
// only the daemon's own flags.
func TestOpencodeExecuteSendsPromptOnStdinNotArgv(t *testing.T) {
	t.Parallel()

	prompt := "MULTICA_AGENT_BUILDER_INPUT\n" +
		"{\n" +
		`  "user_request": "专注本机 CPA 有关的所有工作",` + "\n" +
		`  "current_draft": {"instructions": "Run go build -ldflags \"-X main.version=foo\""},` + "\n" +
		`  "available_workspace_skills": [{"description": "- inspect local changes"}]` + "\n" +
		"}"

	argv, stdinGot, result := opencodeStdinProbe(t, prompt)

	if stdinGot != prompt {
		t.Errorf("prompt did not arrive on stdin intact:\n got  %q\n want %q", stdinGot, prompt)
	}
	for _, arg := range argv {
		for _, needle := range []string{"MULTICA_AGENT_BUILDER_INPUT", "user_request", "-X", "inspect local"} {
			if strings.Contains(arg, needle) {
				t.Errorf("prompt fragment %q leaked into argv element %q; argv=%v", needle, arg, argv)
			}
		}
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{"run", "--format json", "--dangerously-skip-permissions", "--model lanz/Lanz-Medium"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in argv, got %v", want, argv)
		}
	}
	if result.Status != "completed" || result.Output != "ok" {
		t.Fatalf("result = %+v, want completed with output ok", result)
	}
}

// TestOpencodeExecutePromptExceedingWindowsCommandLineLimit pins the exact
// shape reported in #6538: a prompt well past the 32,767-character Windows
// command-line ceiling. On the old argv path this could not even start the
// process on Windows; on stdin it is just bytes.
func TestOpencodeExecutePromptExceedingWindowsCommandLineLimit(t *testing.T) {
	t.Parallel()

	prompt := strings.Repeat("multica opencode workspace skill catalogue 0123456789\n", 800)
	if len(prompt) <= 32767 {
		t.Fatalf("test prompt must exceed the Windows command-line limit, got %d bytes", len(prompt))
	}

	_, stdinGot, result := opencodeStdinProbe(t, prompt)

	if stdinGot != prompt {
		t.Errorf("oversized prompt corrupted on stdin: got %d bytes, want %d", len(stdinGot), len(prompt))
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}

// TestOpencodeExecuteLargePromptDoesNotDeadlock forces both process pipes past
// capacity: the child floods stdout before reading stdin, while the parent
// writes a large prompt. Only concurrent writing and scanning can advance.
func TestOpencodeExecuteLargePromptDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")
	script := fmt.Sprintf("#!/bin/sh\n"+
		"yes '{\"type\":\"noise\",\"pad\":\"0123456789012345678901234567890123456789\"}' | head -n 8000\n"+
		"cat > %[1]q\n", stdinPath) + opencodeStreamTail

	fakePath := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("multica opencode stdin payload 0123456789\n", 16_384)
	if len(prompt) < 512*1024 {
		t.Fatalf("test prompt too small: %d bytes", len(prompt))
	}
	backend, err := New("opencode", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	if string(stdinRaw) != prompt {
		t.Errorf("large stdin prompt corrupted: got %d bytes, want %d", len(stdinRaw), len(prompt))
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}

// TestOpencodeExecuteIgnoresBenignPromptWriteEPIPE pins that a run which
// produced a complete, successful stream is not failed by a prompt write that
// broke on the way out. A child exiting without draining stdin closes the read
// end under the concurrent write, so the parent sees EPIPE — and whether that
// lands before or after the child exits is pure timing. Reporting it as a
// failure makes the result load-dependent and throws away a good run.
func TestOpencodeExecuteIgnoresBenignPromptWriteEPIPE(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A complete, successful stream from a child that never reads stdin.
	script := "#!/bin/sh\n" + opencodeStreamTail
	fakePath := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	// Far larger than any pipe buffer, so the write cannot slip through before
	// the child exits: EPIPE here is deterministic, not incidental.
	prompt := strings.Repeat("benign epipe payload 0123456789\n", 40_000)

	backend, err := New("opencode", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
	if result.Output != "ok" {
		t.Errorf("Output = %q, want %q", result.Output, "ok")
	}
}

// TestOpencodeExecuteFailsWhenPromptWriteBreaksAndStreamNeverStarted is the
// counterpart to the test above and guards the false-success it could
// otherwise create. processEvents starts at "completed" and only fails closed
// on structural evidence (an open step, or a step awaiting a continuation), so
// a child that emits NOTHING and exits 0 leaves that default untouched. If the
// prompt write also broke, suppressing it on the strength of that default
// would report a clean, empty success for a run whose prompt never landed.
//
// Suppression must therefore key on a positive terminal signal, not on the
// absence of a negative one.
func TestOpencodeExecuteFailsWhenPromptWriteBreaksAndStreamNeverStarted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Emits no events at all and exits 0 without reading stdin.
	fakePath := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))

	// Large enough that the write cannot complete before the child exits.
	prompt := strings.Repeat("undelivered prompt payload 0123456789\n", 36_000)

	backend, err := New("opencode", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	if result.Status == "completed" {
		t.Fatalf("undelivered prompt reported as success: status=%q output=%q error=%q",
			result.Status, result.Output, result.Error)
	}
	if !strings.Contains(result.Error, "prompt write failed") {
		t.Errorf("error = %q, want it to name the failed prompt write", result.Error)
	}
}

// TestOpencodeExecuteCancelReleasesBlockedPromptWriter covers the cancellation
// half of the stdin contract: a child that never reads stdin leaves the prompt
// writer blocked on a full pipe. Cancelling must close stdin so that goroutine
// unwinds, and the run must still settle as aborted rather than hang.
func TestOpencodeExecuteCancelReleasesBlockedPromptWriter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Emit one event so the backend is streaming, then sleep without ever
	// draining stdin — the parent's prompt write wedges on a full pipe.
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"step_start\",\"timestamp\":1,\"sessionID\":\"ses_fake\",\"part\":{\"type\":\"step-start\"}}'\n" +
		"sleep 60\n"
	fakePath := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("blocked prompt payload 0123456789\n", 32_768)
	if len(prompt) < 1024*1024 {
		t.Fatalf("test prompt too small to fill the pipe buffer: %d bytes", len(prompt))
	}

	backend, err := New("opencode", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}
	// The fake CLI does not trap SIGTERM, so the group dies inside the grace
	// window; this test deliberately leaves opencodeTerminateGraceNanos alone so
	// it stays parallel-safe alongside the cancellation tests that do set it.
	ctx, cancel := context.WithCancel(context.Background())
	session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	cancel()

	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Fatalf("status = %q, want aborted; error=%q", result.Status, result.Error)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled run never produced a result; the prompt writer likely deadlocked")
	}
}
