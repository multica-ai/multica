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

func TestNewReturnsKiroBackend(t *testing.T) {
	t.Parallel()
	b, err := New("kiro", Config{ExecutablePath: "/nonexistent/kiro-cli"})
	if err != nil {
		t.Fatalf("New(kiro) error: %v", err)
	}
	if _, ok := b.(*kiroBackend); !ok {
		t.Fatalf("expected *kiroBackend, got %T", b)
	}
}

func TestKiroToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
	}{
		{"Read file: /tmp/foo.go", "read_file"},
		{"Write: /tmp/bar.go", "write_file"},
		{"Patch: /tmp/x", "edit_file"},
		{"Shell: ls -la", "terminal"},
		{"Run command: pwd", "terminal"},
		{"grep", "search_files"},
		{"Glob: *.go", "glob"},
		{"Code", "code"},
		{"Todo List", "todo_write"},
		{"Custom Thing", "custom_thing"},
		{"", ""},
	}
	for _, tt := range tests {
		got := kiroToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("kiroToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func fakeKiroACPScript() string {
	return `#!/bin/sh
if [ -n "$KIRO_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$KIRO_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$KIRO_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$KIRO_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"currentModelId":"auto","availableModels":[{"modelId":"auto","name":"auto"}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"session/resume should not be used for kiro"}}\n' "$id"
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
      case "$line" in
        *'"content":'*)
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/prompt must send content and prompt"}}\n' "$id"
          exit 0
          ;;
      esac
      case "$line" in
        *'"prompt":'*)
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/prompt must send content and prompt"}}\n' "$id"
          exit 0
          ;;
      esac
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"loaded"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":2,"outputTokens":1}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestKiroBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
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
		if !strings.Contains(result.Error, "model not available") {
			t.Errorf("expected error to surface upstream message, got %q", result.Error)
		}
		if result.SessionID != "ses_new" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestKiroBackendInvokesACPWithTrustAllTools(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIRO_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:      "bogus-model",
		Timeout:    5 * time.Second,
		CustomArgs: []string{"acp", "--trust-tools", "shell", "-a", "--agent", "multica"},
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
	wantPrefix := []string{"acp", "--trust-all-tools"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	for _, blocked := range []string{"--trust-tools", "shell", "-a"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("protocol-critical custom arg %q was not filtered: %q", blocked, lines)
			}
		}
	}
	if strings.Join(lines, "\n") != strings.Join([]string{"acp", "--trust-all-tools", "--agent", "multica"}, "\n") {
		t.Errorf("unexpected argv after filtering: %q", lines)
	}
}

func TestKiroBackendUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIRO_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "continue", ExecOptions{
		ResumeSessionID: "ses_existing",
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed result, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "loaded" {
		t.Fatalf("output = %q, want loaded", result.Output)
	}
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}

	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load request, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("kiro backend must not call session/resume, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"mcpServers":[]`) {
		t.Fatalf("session/load must include mcpServers, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"content":[`) {
		t.Fatalf("session/prompt must send Kiro content field, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"prompt":[`) {
		t.Fatalf("session/prompt must send standard ACP prompt field for Kiro 2.1.1 compatibility, got:\n%s", requests)
	}
}
