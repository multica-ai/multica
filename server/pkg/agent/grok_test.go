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

// fakeGrokACPScript exercises the full Grok ACP lifecycle: initialize (which
// advertises authMethods), the authenticate round-trip, session/new, and a
// streamed session/prompt. It records argv to $GROK_ARGS_FILE when set.
func fakeGrokACPScript() string {
	return `#!/bin/sh
if [ -n "$GROK_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$GROK_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token"}],"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_grok"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_grok","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":3,"outputTokens":1}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// fakeGrokACPAuthFailureScript rejects the authenticate call so the backend
// must surface a failed task with a login hint.
func fakeGrokACPAuthFailureScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token"}]}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"not authenticated"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// fakeGrokACPNoAuthMethodsScript advertises no authMethods, so the backend
// must skip authenticate and proceed straight to session/new.
func fakeGrokACPNoAuthMethodsScript() string {
	return `#!/bin/sh
if [ -n "$GROK_RPC_FILE" ]; then :; fi
while IFS= read -r line; do
  if [ -n "$GROK_RPC_FILE" ]; then printf '%s\n' "$line" >> "$GROK_RPC_FILE"; fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unexpected authenticate"}}\n' "$id"
      exit 1
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_grok"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_grok","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestGrokBackendAuthenticatesAndStreamsPrompt(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say hello", ExecOptions{Timeout: 30 * time.Second})
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
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "Hello" {
			t.Errorf("expected output %q, got %q", "Hello", result.Output)
		}
		if result.SessionID != "ses_grok" {
			t.Errorf("expected session id ses_grok, got %q", result.SessionID)
		}
		if len(result.Usage) == 0 {
			t.Errorf("expected usage to be recorded, got none")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestGrokBackendInvokesAgentStdioAndFiltersBlockedArgs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"GROK_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A user-supplied "stdio"/"agent" must be filtered (daemon-owned), while a
	// benign custom flag passes through.
	session, err := backend.Execute(ctx, "hi", ExecOptions{
		Timeout:    30 * time.Second,
		CustomArgs: []string{"agent", "stdio", "--debug"},
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
		t.Fatalf("read argv file: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(argv) < 4 || argv[0] != "--always-approve" || argv[1] != "--no-plan" || argv[2] != "agent" || argv[3] != "stdio" {
		t.Fatalf("expected argv to start with [--always-approve --no-plan agent stdio], got %v", argv)
	}
	// The blocked tokens must appear exactly once (the daemon's own), not
	// duplicated by the custom args.
	agentCount, stdioCount, sawDebug := 0, 0, false
	for _, a := range argv {
		switch a {
		case "agent":
			agentCount++
		case "stdio":
			stdioCount++
		case "--debug":
			sawDebug = true
		}
	}
	if agentCount != 1 || stdioCount != 1 {
		t.Errorf("expected blocked args filtered to one each, got agent=%d stdio=%d (%v)", agentCount, stdioCount, argv)
	}
	if !sawDebug {
		t.Errorf("expected passthrough custom arg --debug in argv, got %v", argv)
	}
}

func TestGrokBackendAuthFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPAuthFailureScript()))

	backend, err := New("grok", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hi", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q", result.Status)
		}
		if !strings.Contains(result.Error, "authenticate failed") {
			t.Errorf("expected authenticate-failure error, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "grok login") {
			t.Errorf("expected error to hint at `grok login`, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestGrokBackendSkipsAuthenticateWhenNoMethodsAdvertised(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rpcFile := filepath.Join(tempDir, "rpc.txt")
	fakePath := filepath.Join(tempDir, "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPNoAuthMethodsScript()))

	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"GROK_RPC_FILE": rpcFile},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hi", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	raw, _ := os.ReadFile(rpcFile)
	if strings.Contains(string(raw), `"method":"authenticate"`) {
		t.Errorf("expected no authenticate call when initialize advertised no authMethods, got RPC log:\n%s", raw)
	}
}

func TestPickGrokAuthMethod(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		methods       []string
		apiKeyPresent bool
		want          string
	}{
		{"prefers api key when present and offered", []string{"cached_token", "xai.api_key"}, true, "xai.api_key"},
		{"falls back to cached token without api key", []string{"cached_token", "xai.api_key"}, false, "cached_token"},
		{"cached token when api key not offered", []string{"cached_token"}, true, "cached_token"},
		{"first method as last resort", []string{"some_other"}, false, "some_other"},
		{"empty when none advertised", nil, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickGrokAuthMethod(tc.methods, tc.apiKeyPresent); got != tc.want {
				t.Errorf("pickGrokAuthMethod(%v, %v) = %q, want %q", tc.methods, tc.apiKeyPresent, got, tc.want)
			}
		})
	}
}

func TestParseGrokModels(t *testing.T) {
	t.Parallel()

	output := `You are logged in with grok.com.

Default model: grok-build

Available models:
  * grok-build (default)
  - grok-composer-2.5-fast
`
	models := parseGrokModels(output)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "grok-build" || !models[0].Default {
		t.Errorf("expected first model grok-build (default), got %+v", models[0])
	}
	if models[1].ID != "grok-composer-2.5-fast" || models[1].Default {
		t.Errorf("expected second model grok-composer-2.5-fast (non-default), got %+v", models[1])
	}
	for _, m := range models {
		if m.Provider != "xai" {
			t.Errorf("expected provider xai, got %q for %q", m.Provider, m.ID)
		}
	}
}

func TestParseGrokModelsIgnoresGarbage(t *testing.T) {
	t.Parallel()

	// Malformed / unexpected output must not panic or coin bogus models.
	for _, in := range []string{"", "not a model list\njust prose\n", "Error: not logged in\n"} {
		if got := parseGrokModels(in); len(got) != 0 {
			t.Errorf("parseGrokModels(%q) = %+v, want empty", in, got)
		}
	}
}
