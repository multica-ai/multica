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

func TestNewReturnsTraecliBackend(t *testing.T) {
	t.Parallel()
	b, err := New("traecli", Config{ExecutablePath: "/nonexistent/trae-cli"})
	if err != nil {
		t.Fatalf("New(traecli) error: %v", err)
	}
	if _, ok := b.(*traecliBackend); !ok {
		t.Fatalf("expected *traecliBackend, got %T", b)
	}
}

func TestTraecliBlockedArgsFiltering(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript(argsFile)))

	backend, err := New("traecli", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"TRAECLI_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:    5 * time.Second,
		CustomArgs: []string{"acp", "--yolo", "--agent", "multica", "-y"},
	})
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
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")

	// The daemon hardcodes "acp", "serve", "--yolo" so those must appear
	// exactly once. User-supplied duplicates must be filtered.
	wantPrefix := []string{"acp", "serve", "--yolo"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	for _, blocked := range []string{"-y"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("blocked custom arg %q was not filtered: %q", blocked, lines)
			}
		}
	}
	// The allowed custom arg must survive.
	found := false
	for _, arg := range lines {
		if arg == "--agent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --agent to survive filtering, got %q", lines)
	}
}

func TestTraecliBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript(argsFile)))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestTraecliBackendSuccessfulExecution(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "trae-cli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript(argsFile)))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hello from test", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	result := <-session.Result
	<-messagesDone
	if result.Status != "completed" {
		t.Fatalf("expected completed result, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "Hello from Trae CLI") {
		t.Fatalf("output = %q, want text containing 'Hello from Trae CLI'", result.Output)
	}
	if result.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

// fakeTraecliACPScript impersonates trae-cli for unit tests. It logs
// received argv to TRAECLI_ARGS_FILE (when set) and speaks the ACP
// JSON-RPC 2.0 protocol over stdin/stdout.
func fakeTraecliACPScript(argsFile string) string {
	argsLogCode := ""
	if argsFile != "" {
		argsLogCode = `
if [ -n "$TRAECLI_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$TRAECLI_ARGS_FILE"
  done
fi`
	}
	return `#!/bin/sh` + argsLogCode + `
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_traecli","models":{"currentModelId":"default","availableModels":[{"modelId":"default","name":"Default"},{"modelId":"doubao-pro","name":"Doubao Pro"}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"history replay"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      case "$line" in
        *bogus-model*)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
          exit 0
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
          ;;
      esac
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_traecli","update":{"type":"ToolCall","toolCallId":"tc-1","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_traecli","update":{"type":"ToolCallUpdate","toolCallId":"tc-1","status":"completed","name":"Shell","parameters":{"command":"echo hi"},"output":"hi\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_traecli","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"Hello from Trae CLI!"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":5}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}
