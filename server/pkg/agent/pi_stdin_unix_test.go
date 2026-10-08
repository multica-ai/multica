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

// piStdinProbe runs the Pi backend against a fake CLI that records argv,
// drains stdin to EOF, and emits a minimal successful Pi JSON stream.
func piStdinProbe(t *testing.T, prompt string) ([]string, string, Result) {
	t.Helper()

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")
	sessionPath := filepath.Join(dir, "session.jsonl")

	script := fmt.Sprintf(`#!/bin/sh
: > %[1]q
for a in "$@"; do printf '%%s\n' "$a" >> %[1]q; done
cat > %[2]q
printf '%%s\n' '{"type":"agent_start"}'
printf '%%s\n' '{"type":"turn_start"}'
printf '%%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"ok"}}'
printf '%%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2}}}'
`, argvPath, stdinPath)

	fakePath := filepath.Join(dir, "pi")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Timeout:         30 * time.Second,
		ResumeSessionID: sessionPath,
		Model:           "cpa/grok-4.5-high",
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

// TestPiExecuteSendsBuilderPromptOnStdinNotArgv is the regression for #6457.
// The Agent Builder wire envelope contains JSON quotes and Markdown list
// markers that Windows PowerShell 5.1 can re-tokenise into Pi CLI flags.
func TestPiExecuteSendsBuilderPromptOnStdinNotArgv(t *testing.T) {
	t.Parallel()

	prompt := "MULTICA_AGENT_BUILDER_INPUT\n" +
		"{\n" +
		`  "user_request": "专注本机 CPA 有关的所有工作",` + "\n" +
		`  "current_draft": {"instructions": "Run go build -ldflags \"-X main.version=foo\""},` + "\n" +
		`  "available_workspace_skills": [{"description": "- inspect local changes"}]` + "\n" +
		"}"

	argv, stdinGot, result := piStdinProbe(t, prompt)

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
	for _, want := range []string{"-p", "--mode json", "--session", "--model cpa/grok-4.5-high"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in argv, got %v", want, argv)
		}
	}
	if result.Status != "completed" || result.Output != "ok" {
		t.Fatalf("result = %+v, want completed with output ok", result)
	}
}

// TestPiExecuteLargePromptDoesNotDeadlock forces both process pipes past
// capacity: the child floods stdout before reading stdin, while the parent
// writes a large prompt. Only concurrent writing and scanning can advance.
func TestPiExecuteLargePromptDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdinPath := filepath.Join(dir, "stdin.txt")
	sessionPath := filepath.Join(dir, "session.jsonl")
	script := fmt.Sprintf(`#!/bin/sh
yes '{"type":"noise","pad":"0123456789012345678901234567890123456789"}' | head -n 8000
cat > %[1]q
printf '%%s\n' '{"type":"agent_start"}'
printf '%%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2}}}'
`, stdinPath)
	fakePath := filepath.Join(dir, "pi")
	writeTestExecutable(t, fakePath, []byte(script))

	prompt := strings.Repeat("multica pi stdin payload 0123456789\n", 16_384)
	if len(prompt) < 512*1024 {
		t.Fatalf("test prompt too small: %d bytes", len(prompt))
	}
	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Timeout:         30 * time.Second,
		ResumeSessionID: sessionPath,
	})
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

// TestPiExecuteCancelReleasesBlockedPromptWrite pins cancellation cleanup. The
// child never reads stdin, so cancelling must close the pipe, release the
// blocked writer, and return an aborted result instead of hanging.
func TestPiExecuteCancelReleasesBlockedPromptWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fakePath := filepath.Join(dir, "pi")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nsleep 120\n"))
	prompt := strings.Repeat("blocked pi write payload 0123456789\n", 16_384)

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	session, err := backend.Execute(ctx, prompt, ExecOptions{
		Timeout:         120 * time.Second,
		ResumeSessionID: filepath.Join(dir, "session.jsonl"),
	})
	if err != nil {
		cancel()
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Fatalf("status = %q, want aborted; error=%q", result.Status, result.Error)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancel did not release blocked Pi prompt write")
	}
}
