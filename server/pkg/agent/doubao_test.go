package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDoubaoACPScript impersonates `doubao acp serve` for unit tests. The wire
// format mirrors the other Multica ACP fakes: session/update notifications with
// a sessionUpdate discriminator, session/new returning sessionId + models, and
// session/prompt returning stopReason=end_turn. It also records argv and every
// request line so tests can assert the launch contract.
func fakeDoubaoACPScript() string {
	return `#!/bin/sh
if [ -n "$DOUBAO_ARGS_FILE" ]; then
  printf '%s\n' "$@" > "$DOUBAO_ARGS_FILE"
fi
while IFS= read -r line; do
  if [ -n "$DOUBAO_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$DOUBAO_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentInfo":{"name":"doubao","version":"2.38.2"},"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"list":{}},"promptCapabilities":{},"mcpCapabilities":{}},"authMethods":[]}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_doubao","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"history replay ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_doubao","models":{"availableModels":[{"modelId":"doubao-seed-code","name":"Doubao Seed Code"}],"currentModelId":"doubao-seed-code"}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_loaded","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"history replay ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_doubao","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_doubao","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_doubao","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed","name":"Shell","output":"hi\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_doubao","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestDoubaoBackendLaunchesACPAndCompletes(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "doubao")
	writeTestExecutable(t, fakePath, []byte(fakeDoubaoACPScript()))
	argsPath := filepath.Join(t.TempDir(), "args")
	requestsPath := filepath.Join(t.TempDir(), "requests")

	backend, err := New("doubao", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"DOUBAO_ARGS_FILE":     argsPath,
			"DOUBAO_REQUESTS_FILE": requestsPath,
		},
	})
	if err != nil {
		t.Fatalf("new doubao backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testFallbackTimeout)
	defer cancel()

	session, err := backend.Execute(ctx, "say pong", ExecOptions{
		Timeout:       testFallbackTimeout,
		Model:         "doubao-seed-code",
		ThinkingLevel: "high",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	go func() {
		for m := range session.Messages {
			messages = append(messages, m)
		}
	}()
	result := <-session.Result

	if result.Status != "completed" {
		t.Fatalf("status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "pong") {
		t.Fatalf("output=%q, want the assistant text 'pong'", result.Output)
	}
	if result.SessionID != "ses_doubao" {
		t.Fatalf("session id=%q, want ses_doubao", result.SessionID)
	}

	args := strings.Join(readLines(t, argsPath), " ")
	for _, want := range []string{"acp serve", "--permission-mode bypass_permissions", "--effort high", "--model doubao-seed-code"} {
		if !strings.Contains(args, want) {
			t.Errorf("launch args %q missing %q", args, want)
		}
	}

	requests := strings.Join(readLines(t, requestsPath), "\n")
	if !strings.Contains(requests, `"method":"session/new"`) {
		t.Error("expected a session/new request")
	}
	if strings.Contains(requests, `"method":"authenticate"`) {
		t.Error("doubao advertises authMethods: [] and must not be authenticated")
	}
}

func TestDoubaoUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "doubao")
	writeTestExecutable(t, fakePath, []byte(fakeDoubaoACPScript()))
	requestsPath := filepath.Join(t.TempDir(), "requests")

	backend, err := New("doubao", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DOUBAO_REQUESTS_FILE": requestsPath},
	})
	if err != nil {
		t.Fatalf("new doubao backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testFallbackTimeout)
	defer cancel()

	session, err := backend.Execute(ctx, "continue", ExecOptions{
		Timeout:         testFallbackTimeout,
		ResumeSessionID: "ses_previous",
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
		t.Fatalf("status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "ses_previous" {
		t.Fatalf("session id=%q, want the requested ses_previous", result.SessionID)
	}
	requests := strings.Join(readLines(t, requestsPath), "\n")
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Error("expected resume to use session/load")
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Error("doubao advertises loadSession:true; session/resume must not be used")
	}
}

func TestDoubaoBlockedArgsFiltering(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"acp", "serve", "--acp", "--permission-mode", "--effort", "--model", "-C"} {
		if err := ValidateCustomArgs("doubao", []string{arg}); err == nil {
			t.Errorf("ValidateCustomArgs(doubao, %q) = nil, want a managed-arg error", arg)
		}
	}
	if err := ValidateCustomArgs("doubao", []string{"--foo"}); err != nil {
		t.Errorf("ValidateCustomArgs(doubao, --foo) = %v, want nil", err)
	}
}
