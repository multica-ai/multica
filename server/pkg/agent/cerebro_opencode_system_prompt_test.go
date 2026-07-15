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

// FIR-3212. JEH-1370 (CEREBRO-PATCH daemon-opencode-inline-brief) established
// that OpenCode must receive the daemon workflow brief INLINE, because it does
// not reliably honor file-only task bootstrapping — without it, runs miss the
// required `multica issue status/comment` instructions.
//
// That intent is correct and is preserved. The mechanism was not: opencode.go
// forwarded the brief as `--prompt`, a flag the OpenCode CLI has never had (see
// TestInstalledOpencodeRejectsPromptFlag, verified against the installed
// 1.17.11). OpenCode rejects unknown flags with a usage dump and exit 1, so the
// brief never arrived AND every run failed before the model was contacted.
//
// Every other provider without a native system-prompt channel already does this
// correctly by prepending the brief to the user message:
//   - openclaw.go:197-199  prompt = opts.SystemPrompt + "\n\n" + prompt
//   - kiro.go:272-273      userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
//   - kimi.go:289-290      userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
//
// These tests pin OpenCode to that same contract.

// argDelimiter separates captured argv entries. The shared fakeOpencodeScript()
// writes one arg per line, which cannot represent an argument that itself
// contains newlines — and the prepended brief always does. We use an explicit
// delimiter so a multi-line message stays a single argument.
const argDelimiter = "<<<CEREBRO-ARG>>>"

// fakeOpencodeArgvScript impersonates `opencode`, recording argv to
// $OPENCODE_ARGS_FILE delimiter-separated, and emits a minimal completed step so
// the backend's event loop terminates.
func fakeOpencodeArgvScript() string {
	return `#!/bin/sh
if [ -n "$OPENCODE_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s<<<CEREBRO-ARG>>>' "$arg" >> "$OPENCODE_ARGS_FILE"
  done
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}\n'
`
}

// runFakeOpencode executes the opencode backend against the argv-capturing fake
// and returns the captured argv.
func runFakeOpencode(t *testing.T, prompt string, opts ExecOptions) []string {
	t.Helper()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeArgvScript()))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCODE_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if opts.Cwd == "" {
		opts.Cwd = t.TempDir()
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}

	session, err := backend.Execute(ctx, prompt, opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(string(raw), argDelimiter)
	// The trailing delimiter yields a final empty element; drop it.
	if len(args) > 0 && args[len(args)-1] == "" {
		args = args[:len(args)-1]
	}
	return args
}

// The bug itself: --prompt must never be emitted, because the CLI does not
// support it and rejects the whole invocation.
func TestOpencodeNeverEmitsPromptFlag(t *testing.T) {
	t.Parallel()

	args := runFakeOpencode(t, "do the task", ExecOptions{
		SystemPrompt: "You are a Multica agent. Always run multica issue status.",
	})

	if containsString(args, "--prompt") {
		t.Fatalf("opencode argv contains --prompt, a flag the OpenCode CLI does not support; "+
			"every run exits 1 with a usage dump. argv=%q", args)
	}
}

// JEH-1370's intent, now actually delivered: the brief must reach the model,
// prepended to the user message, ahead of the task.
func TestOpencodePrependsSystemPromptToMessage(t *testing.T) {
	t.Parallel()

	const brief = "You are a Multica agent. Always run multica issue status."
	const task = "do the task"

	args := runFakeOpencode(t, task, ExecOptions{SystemPrompt: brief})

	message := args[len(args)-1]
	if !strings.Contains(message, brief) {
		t.Fatalf("opencode message does not carry the runtime brief; JEH-1370 requires it inline.\nmessage=%q\nargv=%q", message, args)
	}
	if !strings.Contains(message, task) {
		t.Fatalf("opencode message lost the user task.\nmessage=%q", message)
	}
	if strings.Index(message, brief) > strings.Index(message, task) {
		t.Fatalf("runtime brief must precede the task, matching openclaw/kiro/kimi.\nmessage=%q", message)
	}
}

// With no system prompt, the message must be the bare task — no separator, no
// stray empty prefix.
func TestOpencodeWithoutSystemPromptSendsBareMessage(t *testing.T) {
	t.Parallel()

	args := runFakeOpencode(t, "do the task", ExecOptions{})

	if message := args[len(args)-1]; message != "do the task" {
		t.Fatalf("expected bare message %q, got %q", "do the task", message)
	}
}
