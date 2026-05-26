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

func TestNewReturnsDeepseekBackend(t *testing.T) {
	t.Parallel()
	b, err := New("DeepSeek-TUI", Config{ExecutablePath: "/nonexistent/deepseek"})
	if err != nil {
		t.Fatalf("New(deepseek) error: %v", err)
	}
	if _, ok := b.(*deepseekBackend); !ok {
		t.Fatalf("expected *deepseekBackend, got %T", b)
	}
}

func TestDeepseekToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"read_file", "read_file"},
		{"read", "read_file"},
		{"write_file", "write_file"},
		{"write", "write_file"},
		{"edit_file", "edit_file"},
		{"edit", "edit_file"},
		{"patch", "edit_file"},
		{"shell", "terminal"},
		{"bash", "terminal"},
		{"terminal", "terminal"},
		{"run_command", "terminal"},
		{"search", "search_files"},
		{"search_files", "search_files"},
		{"grep", "search_files"},
		{"glob", "glob"},
		{"web_search", "web_search"},
		{"web_fetch", "web_fetch"},
		{"fetch", "web_fetch"},
		{"todo_write", "todo_write"},
		// Fallback: pass through as snake_case.
		{"custom_thing", "custom_thing"},
		// Empty input returns empty.
		{"", ""},
	}
	for _, tt := range tests {
		got := deepseekToolName(tt.name)
		if got != tt.want {
			t.Errorf("deepseekToolName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// fakeDeepseekNativeScript returns a POSIX-sh script that emulates
// `deepseek app-server --stdio` using the native thread/* protocol.
// It handles thread/start, thread/message (emitting push events),
// and shutdown. The "error" variant rejects thread/start for testing
// failure propagation.
func fakeDeepseekNativeScript(mode string) string {
	switch mode {
	case "success":
		return `#!/bin/sh
# Fake deepseek binary — native protocol
if [ -n "$DEEPSEEK_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEEPSEEK_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"thread/start"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"thread_id":"thread-fake-123","status":"started"}}\n' "$id"
      ;;
    *'"method":"thread/message"'*)
      printf '{"type":"response_start","response_id":"thread-fake-123:1"}\n'
      printf '{"type":"response_delta","response_id":"thread-fake-123:1","delta":"Hello from DeepSeek!"}\n'
      printf '{"type":"usage_update","usage":{"input_tokens":25,"output_tokens":7,"cached_input_tokens":3}}\n'
      printf '{"type":"response_end","response_id":"thread-fake-123:1"}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"thread_id":"thread-fake-123","status":"accepted","events":[]}}\n' "$id"
      ;;
    *'"method":"shutdown"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      exit 0
      ;;
  esac
done
`
	case "thread_start_error":
		return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"thread/start"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"config error: no API key"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
	default:
		return "#!/bin/sh\nexit 1\n"
	}
}

// TestDeepseekBackendNativeProtocol verifies the happy path: the
// backend creates a thread, sends a message, and collects the text
// output from response_delta push events.
func TestDeepseekBackendNativeProtocol(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "deepseek")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekNativeScript("success")), 0o755); err != nil {
		t.Fatalf("write fake deepseek: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say hello", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Collect messages.
	var messages []Message
	go func() {
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.SessionID != "thread-fake-123" {
			t.Errorf("expected session_id=thread-fake-123, got %q", result.SessionID)
		}
		if !strings.Contains(result.Output, "Hello from DeepSeek!") {
			t.Errorf("expected output to contain 'Hello from DeepSeek!', got %q", result.Output)
		}
		usage := result.Usage["unknown"]
		if usage.InputTokens != 25 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 {
			t.Errorf("usage = %+v, want input=25 output=7 cache_read=3", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestDeepseekBackendThreadStartFailure verifies that a thread/start
// error is properly propagated as a failed task result.
func TestDeepseekBackendThreadStartFailure(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "deepseek")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekNativeScript("thread_start_error")), 0o755); err != nil {
		t.Fatalf("write fake deepseek: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
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
		if !strings.Contains(result.Error, "thread/start") {
			t.Errorf("expected error to mention thread/start, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "no API key") {
			t.Errorf("expected error to surface upstream message, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestDeepseekBackendInvokesAppServerStdio pins the argv: the
// daemon must pass `app-server --stdio` to launch the native protocol.
func TestDeepseekBackendInvokesAppServerStdio(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "deepseek")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekNativeScript("success")), 0o755); err != nil {
		t.Fatalf("write fake deepseek: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEEPSEEK_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say hello", ExecOptions{
		Timeout: 5 * time.Second,
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
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 args (app-server --stdio), got %d: %q", len(lines), lines)
	}
	if lines[0] != "app-server" {
		t.Errorf("expected first arg to be app-server, got %q (full: %q)", lines[0], lines)
	}
	if lines[1] != "--stdio" {
		t.Errorf("expected second arg to be --stdio, got %q (full: %q)", lines[1], lines)
	}
}

func TestExtractDeepseekThreadID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{`{"thread_id":"thread-abc-123","status":"started"}`, "thread-abc-123"},
		{`{"status":"started"}`, ""},
		{`{}`, ""},
	}
	for _, tt := range tests {
		got := extractDeepseekThreadID([]byte(tt.input))
		if got != tt.want {
			t.Errorf("extractDeepseekThreadID(%s) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
