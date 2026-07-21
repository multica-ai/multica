//go:build unix

package agent

import (
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

// TestCursorExecuteLargePromptDoesNotDeadlock guards the reason the prompt
// write lives in its own goroutine. A prompt well past the OS pipe buffer
// (~64 KiB) blocks mid-write until the child drains it; if we wrote it before
// starting the stdout reader, neither side could make progress and the run
// would fail only at the task timeout.
func TestCursorExecuteLargePromptDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	// 512 KiB — several times any plausible pipe buffer on Linux/macOS.
	prompt := strings.Repeat("multica cursor stdin payload 0123456789\n", 13_108)
	if len(prompt) < 512*1024 {
		t.Fatalf("test prompt too small: %d bytes", len(prompt))
	}

	_, stdinGot, result := cursorStdinProbe(t, prompt)

	if len(stdinGot) != len(prompt) {
		t.Errorf("stdin truncated: got %d bytes, want %d", len(stdinGot), len(prompt))
	}
	if stdinGot != prompt {
		t.Error("large prompt arrived corrupted on stdin")
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
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
