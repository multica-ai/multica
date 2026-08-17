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

// cursorStdinProbe runs the cursor backend against a fake cursor-agent that
// records the argv it was given and drains its stdin to EOF, then emits a
// terminal stream-json result. It returns (argv, stdin, result).
//
// Draining stdin before answering mirrors the real CLI: with no positional
// prompt and a non-TTY stdin, cursor-agent reads stdin to EOF and uses it as
// the prompt.
func cursorStdinProbe(t *testing.T, prompt string) ([]string, string, Result) {
	t.Helper()

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	// Record argv one element per line, then drain stdin to EOF before
	// answering. Quoting "$@" keeps each argv element intact.
	script := fmt.Sprintf(`#!/bin/sh
: > %[1]q
for a in "$@"; do printf '%%s\n' "$a" >> %[1]q; done
cat > %[2]q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, argvPath, stdinPath)

	fakePath := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
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

// TestCursorExecuteSendsPromptOnStdinNotArgv is the regression test for #5649.
// A prompt carrying CLI-like flags inside embedded double quotes (the exact
// shape from the report) must reach the child intact on stdin, and must not
// appear in argv at all — argv is where Windows launchers re-tokenise it.
func TestCursorExecuteSendsPromptOnStdinNotArgv(t *testing.T) {
	t.Parallel()

	prompt := "Please fix the build.\n" +
		"Log follows:\n" +
		`go build -ldflags "-X main.version=foo -X main.commit=bar" -o bin/server ./cmd/server` + "\n" +
		"Thanks."

	argv, stdinGot, result := cursorStdinProbe(t, prompt)

	if stdinGot != prompt {
		t.Errorf("prompt did not arrive on stdin intact:\n got  %q\n want %q", stdinGot, prompt)
	}

	// The whole point of the fix: no fragment of the prompt is in argv, so no
	// shell or launcher on any platform can re-tokenise it into flags.
	for _, a := range argv {
		for _, needle := range []string{"-X", "ldflags", "main.version", "Please fix"} {
			if strings.Contains(a, needle) {
				t.Errorf("prompt fragment %q leaked into argv element %q; argv=%v", needle, a, argv)
			}
		}
	}

	// The fixed, content-free flags must still be present.
	joined := strings.Join(argv, " ")
	for _, want := range []string{"-p", "--output-format", "stream-json", "--yolo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in argv, got %v", want, argv)
		}
	}

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}

// TestCursorExecuteLargePromptDoesNotDeadlock is the interlock test: it forces
// the exact mutual block the concurrent writer exists to avoid.
//
// The child floods stdout PAST the pipe capacity *before* reading a byte of
// stdin. So the child is blocked writing stdout, and a parent that wrote the
// prompt before starting its stdout reader would be blocked writing stdin —
// neither side can advance and the run only ends at the task timeout. It passes
// only because the writer and the scanner run concurrently.
//
// Ordering matters: a fake that drains stdin first would pass even against a
// synchronous write, which is why this test flooded stdout first.
func TestCursorExecuteLargePromptDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")

	// ~520 KiB of stdout before any read of stdin, several times any plausible
	// pipe buffer on Linux/macOS.
	script := fmt.Sprintf(`#!/bin/sh
yes '{"type":"noise","pad":"0123456789012345678901234567890123456789"}' | head -n 8000
cat > %[1]q
printf '%%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"ok"}'
`, stdinPath)

	fakePath := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("multica cursor stdin payload 0123456789\n", 13_108)
	if len(prompt) < 512*1024 {
		t.Fatalf("test prompt too small: %d bytes", len(prompt))
	}

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	// A deadlock surfaces as this timeout firing instead of a completed run.
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
	if len(stdinRaw) != len(prompt) {
		t.Errorf("stdin truncated: got %d bytes, want %d", len(stdinRaw), len(prompt))
	}
	if string(stdinRaw) != prompt {
		t.Error("large prompt arrived corrupted on stdin")
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (deadlock would show as timeout); error=%q", result.Status, result.Error)
	}
}

// TestCursorExecuteCancelReleasesBlockedPromptWrite pins the other unlock
// point. The child never reads stdin at all, so a prompt past the pipe buffer
// leaves the writer blocked indefinitely; cancelling the parent context must
// close stdin, release that writer, and settle the run rather than hang.
func TestCursorExecuteCancelReleasesBlockedPromptWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Never reads stdin, never exits on its own.
	script := "#!/bin/sh\nsleep 120\n"
	fakePath := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("blocked write payload 0123456789\n", 16_384) // ~512 KiB

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: 120 * time.Second})
	if err != nil {
		cancel()
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Give the writer time to fill the pipe and block, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Fatalf("status = %q, want aborted; error=%q", result.Status, result.Error)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancel did not release the blocked prompt write; Result never arrived")
	}
}

// TestCursorExecuteWritesPromptVerbatim pins our side of the whitespace
// contract: we write the prompt bytes exactly as given, adding no wrapper,
// framing or trailing newline (unlike Claude, which sends a JSON frame).
//
// Note the CLI itself trims the stdin prompt before use, so leading/trailing
// whitespace is not preserved end-to-end. That is cursor-agent's behaviour, not
// ours; task prompts carry no meaning in outer whitespace, so we do not
// compensate for it here.
func TestCursorExecuteWritesPromptVerbatim(t *testing.T) {
	t.Parallel()

	prompt := "\n  leading and trailing whitespace  \n\n"

	_, stdinGot, result := cursorStdinProbe(t, prompt)

	if stdinGot != prompt {
		t.Errorf("prompt was mutated before write:\n got  %q\n want %q", stdinGot, prompt)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}
