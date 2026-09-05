package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeJunieACPScript() string {
	return `#!/bin/sh
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$JUNIE_ARGS_FILE"
  case "$arg" in
    --test-hang) test_hang=1 ;;
    --test-provider-error) test_provider_error=1 ;;
  esac
done
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$JUNIE_REQUESTS_FILE"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id" ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-new","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"old history"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-new","configOptions":[{"id":"model","category":"model","currentValue":"opaque-default","options":[{"name":"Local","options":[{"value":"opaque-default","name":"Default"},{"value":"opaque-picked","name":"Picked"}]}]},{"id":"effort","category":"thought_level","currentValue":"medium","options":[{"value":"low","name":"Low"},{"value":"medium","name":"Medium"},{"value":"high","name":"High"}]}]}}\n' "$id" ;;
    *'"method":"session/load"'*)
      case "$line" in
        *'"sessionId":"ses-missing"'*) printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"No session found for id"}}\n' "$id"; continue ;;
      esac
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-old","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"old history"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"model","category":"model","currentValue":"opaque-default","options":[{"value":"opaque-default","name":"Default"}]},{"id":"effort","category":"thought_level","currentValue":"medium","options":[{"value":"high","name":"High"}]}]}}\n' "$id" ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/set_model must not be used"}}\n' "$id"; exit 0 ;;
    *'"method":"session/set_config_option"'*'"configId":"model"'*)
      case "$line" in *opaque-bad*) printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"unsupported model"}}\n' "$id"; exit 0;; esac
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"model","category":"model","currentValue":"opaque-picked","options":[{"value":"opaque-picked","name":"Picked"}]},{"id":"effort","category":"thought_level","currentValue":"medium","options":[{"value":"low","name":"Low"},{"value":"medium","name":"Medium"},{"value":"high","name":"High"}]}]}}\n' "$id" ;;
    *'"method":"session/set_config_option"'*'"configId":"effort"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"effort","category":"thought_level","currentValue":"high","options":[{"value":"low","name":"Low"},{"value":"medium","name":"Medium"},{"value":"high","name":"High"}]}]}}\n' "$id" ;;
    *'"method":"session/prompt"'*)
      if [ "$test_hang" = 1 ]; then sleep 30; exit 0; fi
      if [ "$test_provider_error" = 1 ]; then printf '%s\n' '[ERROR] AuthenticationError: Authentication is required before this operation can be performed.' >&2; fi
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-new","update":{"type":"AgentThoughtChunk","content":{"type":"text","text":"thinking"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-new","update":{"type":"ToolCall","toolCallId":"call-1","name":"Shell","status":"pending","parameters":{"command":"pwd"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-new","update":{"type":"ToolCallUpdate","toolCallId":"call-1","status":"completed","output":"ok"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-new","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":12,"outputTokens":4}}}\n' "$id"; exit 0 ;;
  esac
done
`
}

func runJunieFake(t *testing.T, opts ExecOptions) (Result, []Message, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell ACP fixture is Unix-only")
	}
	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "args")
	requestsFile := filepath.Join(tmp, "requests")
	fakePath := filepath.Join(tmp, "junie")
	writeTestExecutable(t, fakePath, []byte(fakeJunieACPScript()))
	backend, err := New("junie", Config{ExecutablePath: fakePath, Logger: slog.Default(), Env: map[string]string{
		"JUNIE_ARGS_FILE": argsFile, "JUNIE_REQUESTS_FILE": requestsFile,
	}})
	if err != nil {
		t.Fatalf("New(junie): %v", err)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	session, err := backend.Execute(context.Background(), "work now", opts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}
	result := <-session.Result
	args, _ := os.ReadFile(argsFile)
	requests, _ := os.ReadFile(requestsFile)
	return result, messages, string(args), string(requests)
}

func TestJunieBackendUsesConfigOptionsForModelAndEffort(t *testing.T) {
	result, messages, args, requests := runJunieFake(t, ExecOptions{
		Model: "opaque-picked", ThinkingLevel: "high",
		CustomArgs: []string{"--acp=false", "--task", "ignored", "--safe-flag"},
		McpConfig:  json.RawMessage(`{"mcpServers":{"docs":{"type":"http","url":"https://example.test/mcp"}}}`),
	})
	if result.Status != "completed" || result.SessionID != "ses-new" || result.Output != "done" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if args != "--acp=true\n--safe-flag\n" {
		t.Fatalf("argv = %q", args)
	}
	if strings.Contains(requests, `"method":"session/set_model"`) {
		t.Fatal("Junie must not receive session/set_model")
	}
	if strings.Contains(requests, `"configId":"brave_mode"`) {
		t.Fatal("Multica must not mutate the user's persistent Junie permission mode")
	}
	wantOrder := []string{`"method":"initialize"`, `"method":"session/new"`, `"configId":"model"`, `"configId":"effort"`, `"method":"session/prompt"`}
	pos := -1
	for _, marker := range wantOrder {
		next := strings.Index(requests[pos+1:], marker)
		if next < 0 {
			t.Fatalf("missing %s in requests:\n%s", marker, requests)
		}
		pos += next + 1
	}
	if !strings.Contains(requests, `"mcpServers":[{"headers":[],"name":"docs","type":"http","url":"https://example.test/mcp"}]`) {
		t.Fatalf("managed MCP server missing from session/new:\n%s", requests)
	}
	if len(messages) != 5 || messages[0].Type != MessageStatus || messages[0].SessionID != "ses-new" || messages[len(messages)-1].Content != "done" {
		t.Fatalf("messages = %#v", messages)
	}
	if _, ok := result.Usage["opaque-picked"]; !ok {
		t.Fatalf("usage not attributed to chosen model: %#v", result.Usage)
	}
}

func TestJunieBackendResumeUsesLoadAndDropsHistoryReplay(t *testing.T) {
	result, messages, _, requests := runJunieFake(t, ExecOptions{ResumeSessionID: "ses-old"})
	if result.Status != "completed" || result.SessionID != "ses-old" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(requests, `"method":"session/load"`) || strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("resume request sequence wrong:\n%s", requests)
	}
	for _, msg := range messages {
		if strings.Contains(msg.Content, "old history") {
			t.Fatalf("history replay leaked into current turn: %#v", messages)
		}
	}
}

func TestJunieBackendRejectsStaleResumeSession(t *testing.T) {
	result, _, _, _ := runJunieFake(t, ExecOptions{ResumeSessionID: "ses-missing"})
	if result.Status != "failed" || !result.ResumeRejected || result.SessionID != "" {
		t.Fatalf("unexpected stale-session result: %#v", result)
	}
}

func TestJunieBackendModelFailureFailsTask(t *testing.T) {
	result, _, _, _ := runJunieFake(t, ExecOptions{Model: "opaque-bad"})
	if result.Status != "failed" || !strings.Contains(result.Error, "unsupported model") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestJunieBackendTimeoutStopsPrompt(t *testing.T) {
	result, _, _, _ := runJunieFake(t, ExecOptions{
		Timeout:    time.Second,
		CustomArgs: []string{"--test-hang"},
	})
	if result.Status != "timeout" || !strings.Contains(result.Error, "timed out") {
		t.Fatalf("unexpected timeout result: %#v", result)
	}
}

func TestJunieBackendPromotesProviderErrorFromStderr(t *testing.T) {
	result, _, _, _ := runJunieFake(t, ExecOptions{CustomArgs: []string{"--test-provider-error"}})
	if result.Status != "failed" || !strings.Contains(strings.ToLower(result.Error), "authentication") {
		t.Fatalf("unexpected provider-error result: %#v", result)
	}
}

func TestParseJunieNestedModelCatalog(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "junie-26.8.31-session-new.json"))
	if err != nil {
		t.Fatal(err)
	}
	models := parseACPSessionNewModels(raw)
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].ID != "v1:6:custom:custom:local-model" || !models[0].Default || models[1].Default {
		t.Fatalf("nested opaque model ids/default not preserved: %#v", models)
	}
	// Junie exposes an effort catalog, but it is intentionally not attached to
	// public model metadata until an authenticated real prompt proves the value
	// reaches model execution rather than merely round-tripping in ACP state.
	if option, ok := parseACPEffortOption(raw); !ok || len(option.Choices) != 3 {
		t.Fatalf("effort option was not parsed from Junie configOptions: %#v", option)
	}
	if models[0].Thinking != nil {
		t.Fatalf("unverified Junie effort must not be publicly advertised: %#v", models[0])
	}
}

func TestJunieRejectsMalformedMCPBeforeLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	fakePath := filepath.Join(t.TempDir(), "junie")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))
	backend, err := New("junie", Config{ExecutablePath: fakePath})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{McpConfig: json.RawMessage(`[]`)})
	if err == nil || !strings.Contains(err.Error(), "invalid mcp_config") {
		t.Fatalf("unexpected error: %v", err)
	}
}
