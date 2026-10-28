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

// copilotStreamTail is the minimal successful Copilot JSONL stream the fake
// CLIs below emit once they have recorded what the backend handed them.
const copilotStreamTail = `printf '%s\n' '{"type":"assistant.message","data":{"content":"ok"}}'
printf '%s\n' '{"type":"result","sessionId":"sess-fake","exitCode":0}'
`

func runCopilotFake(t *testing.T, script, prompt string, opts ExecOptions) Result {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), "copilot")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("copilot", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(copilot): %v", err)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	session, err := backend.Execute(t.Context(), prompt, opts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("copilot run never produced a result")
		return Result{}
	}
}

// TestCopilotExecuteSendsLargePromptOnStdinNotArgv is the regression for
// #9362: a task prompt above Linux's 131,072-byte MAX_ARG_STRLEN used to be
// inlined as `-p <prompt>` and died at fork/exec with E2BIG. The prompt must
// now reach Copilot byte-for-byte on stdin while argv keeps only the daemon's
// own flags.
func TestCopilotExecuteSendsLargePromptOnStdinNotArgv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")
	script := fmt.Sprintf("#!/bin/sh\n"+
		": > %[1]q\n"+
		"for a in \"$@\"; do printf '%%s\\n' \"$a\" >> %[1]q; done\n"+
		"cat > %[2]q\n", argvPath, stdinPath) + copilotStreamTail

	// ~1.2 MB, far above MAX_ARG_STRLEN and every pipe buffer.
	prompt := "MULTICA_COPILOT_PROMPT\n" + strings.Repeat("pasted daemon log line 0123456789 \"quoted\"\n", 28_000)
	if len(prompt) <= 131_072 {
		t.Fatalf("test prompt must exceed MAX_ARG_STRLEN, got %d bytes", len(prompt))
	}

	result := runCopilotFake(t, script, prompt, ExecOptions{Model: "gpt-5", ResumeSessionID: "sess-prev"})

	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read recorded stdin: %v", err)
	}
	if string(stdinRaw) != prompt {
		t.Errorf("prompt corrupted on stdin: got %d bytes, want %d", len(stdinRaw), len(prompt))
	}
	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	argv := strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n")
	for _, a := range argv {
		if a == "-p" || strings.Contains(a, "MULTICA_COPILOT_PROMPT") {
			t.Fatalf("prompt leaked onto argv: %v", argv)
		}
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{"--output-format json", "--allow-all", "--no-ask-user", "--model gpt-5", "--resume sess-prev"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in argv, got %v", want, argv)
		}
	}
	if result.Status != "completed" || result.Output != "ok" || result.SessionID != "sess-fake" {
		t.Fatalf("result = %+v, want completed/ok/sess-fake", result)
	}
}

// TestCopilotExecuteLargePromptDoesNotDeadlock forces both pipes past
// capacity: the child floods stdout before reading stdin while the parent
// writes a large prompt. Only concurrent writing and scanning can advance.
func TestCopilotExecuteLargePromptDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	script := "#!/bin/sh\n" +
		"yes '{\"type\":\"noise\",\"data\":{\"pad\":\"0123456789012345678901234567890123456789\"}}' | head -n 8000\n" +
		"cat > /dev/null\n" + copilotStreamTail
	prompt := strings.Repeat("copilot stdin payload 0123456789\n", 20_000)

	result := runCopilotFake(t, script, prompt, ExecOptions{})
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}

// A complete, successful stream must not be failed by a prompt write that broke
// on the way out (child exits without draining stdin -> timing-dependent EPIPE).
func TestCopilotExecuteIgnoresBenignPromptWriteEPIPE(t *testing.T) {
	t.Parallel()

	script := "#!/bin/sh\n" + copilotStreamTail
	prompt := strings.Repeat("benign epipe payload 0123456789\n", 40_000)

	result := runCopilotFake(t, script, prompt, ExecOptions{})
	if result.Status != "completed" || result.Output != "ok" {
		t.Fatalf("result = %+v, want completed with output ok", result)
	}
}

// Counterpart to the test above: a child that emits nothing, never reads the
// prompt, and exits 0 must not be reported as a clean success.
func TestCopilotExecuteFailsWhenPromptWriteBreaksWithoutResult(t *testing.T) {
	t.Parallel()

	prompt := strings.Repeat("undelivered prompt payload 0123456789\n", 36_000)
	result := runCopilotFake(t, "#!/bin/sh\nexit 0\n", prompt, ExecOptions{})

	if result.Status == "completed" {
		t.Fatalf("undelivered prompt reported as success: %+v", result)
	}
	if !strings.Contains(result.Error, "prompt write failed") {
		t.Errorf("error = %q, want it to name the failed prompt write", result.Error)
	}
}

// Cancelling must release a prompt writer blocked on a full stdin pipe and
// still settle the run as aborted rather than hang.
func TestCopilotExecuteCancelReleasesBlockedPromptWriter(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "copilot")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+
		"printf '%s\\n' '{\"type\":\"session.start\",\"data\":{\"sessionId\":\"sess-x\"}}'\n"+
		"exec sleep 60\n"))

	backend, err := New("copilot", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(copilot): %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	prompt := strings.Repeat("blocked prompt payload 0123456789\n", 32_768)
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

func TestCopilotExecuteRefusesEmptyPrompt(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "copilot")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))
	backend, err := New("copilot", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(copilot): %v", err)
	}
	if _, err := backend.Execute(t.Context(), "  \n", ExecOptions{}); err == nil {
		t.Fatal("expected an error for an empty prompt")
	}
}
