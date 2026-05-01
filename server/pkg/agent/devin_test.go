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

func TestNewReturnsDevinBackend(t *testing.T) {
	t.Parallel()
	b, err := New("devin", Config{ExecutablePath: "/nonexistent/devin"})
	if err != nil {
		t.Fatalf("New(devin) error: %v", err)
	}
	if _, ok := b.(*devinBackend); !ok {
		t.Fatalf("expected *devinBackend, got %T", b)
	}
}

func TestDevinToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
	}{
		{"Read file: /tmp/foo.go", "read_file"},
		{"View: /tmp/foo.go", "read_file"},
		{"Write: /tmp/bar.go", "write_file"},
		{"Edit: /tmp/x", "edit_file"},
		{"Patch: /tmp/x", "edit_file"},
		{"Replace: /tmp/x", "edit_file"},
		{"Exec: ls -la", "terminal"},
		{"Shell: ls -la", "terminal"},
		{"Run command: pwd", "terminal"},
		{"grep", "search_files"},
		{"Find: foo", "search_files"},
		{"Glob: *.go", "glob"},
		{"Find files: pattern", "glob"},
		{"Web fetch: https://example.com", "web_fetch"},
		{"Web search: golang", "web_search"},
		{"Todo Write", "todo_write"},
		{"Todo List", "todo_write"},
		{"Custom Thing", "custom_thing"},
		{"", ""},
	}
	for _, tt := range tests {
		got := devinToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("devinToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

// fakeDevinACPScript mimics `devin acp`. It captures argv (when
// DEVIN_ARGS_FILE is set) and incoming JSON-RPC requests (when
// DEVIN_REQUESTS_FILE is set), then replies with canned ACP responses
// modeled on what the real `devin 2026.4.29-0` server emits — a session
// id slug rather than a UUID, agentCapabilities.loadSession=true, and no
// `models` field on session/new (Devin streams those via
// config_option_update notifications, which Multica ignores).
func fakeDevinACPScript() string {
	return `#!/bin/sh
if [ -n "$DEVIN_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEVIN_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$DEVIN_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$DEVIN_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":false,"embeddedContext":true}},"agentInfo":{"name":"affogato","title":"Affogato Agent","version":"0.0.0-dev"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"tricolor-diver"}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"loaded-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"history should be ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      # Devin does not implement session/set_model. Reply with a JSON-RPC
      # method-not-found error so any accidental call is loud.
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"Method not found"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"tricolor-diver","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello from devin"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":7,"outputTokens":3}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestDevinBackendSpawnsACPSubcommand(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPScript()))

	backend, err := New("devin", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEVIN_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "do something", ExecOptions{
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
	if len(lines) == 0 || lines[0] != "acp" {
		t.Fatalf("expected first arg to be %q, got %q", "acp", lines)
	}
}

func TestDevinBackendFiltersBlockedArgs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPScript()))

	backend, err := New("devin", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEVIN_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "do something", ExecOptions{
		Timeout: 5 * time.Second,
		// User tries to inject a duplicate `acp` subcommand and
		// keeps an unrelated --agent-type flag — only `acp` should
		// be filtered, the agent type must pass through so users
		// can opt into Devin's specialised agents.
		CustomArgs: []string{"acp", "--agent-type", "summarizer"},
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
	got := strings.Join(strings.Split(strings.TrimSpace(string(raw)), "\n"), " ")
	want := "acp --agent-type summarizer"
	if got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestDevinBackendSurfacesPromptOutput(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPScript()))

	backend, err := New("devin", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say hi", ExecOptions{
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
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "hello from devin" {
		t.Fatalf("output = %q, want %q", result.Output, "hello from devin")
	}
	if result.SessionID != "tricolor-diver" {
		t.Fatalf("session id = %q, want %q", result.SessionID, "tricolor-diver")
	}
	if usage := result.Usage["unknown"]; usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v, want input=7 output=3", usage)
	}
	// History notifications fire before streamingCurrentTurn flips to
	// true, so the only text message we should see is the current turn's.
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want exactly 1 (current turn text only)", messages)
	}
	if messages[0].Type != MessageText || messages[0].Content != "hello from devin" {
		t.Fatalf("messages[0] = %+v, want MessageText 'hello from devin'", messages[0])
	}
}

func TestDevinBackendModelOptionLogsWarningWithoutFailing(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPScript()))

	backend, err := New("devin", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEVIN_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say hi", ExecOptions{
		Model:   "claude-opus-4-7-medium",
		Timeout: 5 * time.Second,
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
		t.Fatalf("expected completed (model-not-supported is a warning, not a failure), got %q error=%q", result.Status, result.Error)
	}

	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	if strings.Contains(string(raw), `"method":"session/set_model"`) {
		t.Fatalf("devin backend must not call session/set_model (Devin does not implement it):\n%s", raw)
	}
}

func TestDevinBackendUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPScript()))

	backend, err := New("devin", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEVIN_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
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
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}

	requests := string(mustReadFile(t, requestsFile))
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load request, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("devin backend must not call session/new on resume, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"sessionId":"ses_existing"`) {
		t.Fatalf("session/load must carry the resume id, got:\n%s", requests)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
