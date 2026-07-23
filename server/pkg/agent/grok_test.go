package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsGrokBackend(t *testing.T) {
	t.Parallel()
	b, err := New("grok", Config{ExecutablePath: "/nonexistent/grok"})
	if err != nil {
		t.Fatalf("New(grok) error: %v", err)
	}
	if _, ok := b.(*grokBackend); !ok {
		t.Fatalf("expected *grokBackend, got %T", b)
	}
}

// fakeGrokACPScript impersonates `grok agent --always-approve stdio` for unit
// tests. Wire format mirrors other Multica ACP fakes (traecli/kimi): method
// "session/update" with update.sessionUpdate discriminators, session/new
// returning sessionId + models, session/prompt returning stopReason=end_turn.
func fakeGrokACPScript() string {
	return `#!/bin/sh
seen_stdio=
for arg in "$@"; do
  if [ -n "$GROK_ARGS_FILE" ]; then
    printf '%s\n' "$arg" >> "$GROK_ARGS_FILE"
  fi
  if [ -n "$seen_stdio" ]; then
    printf 'unexpected argument after stdio: %s\n' "$arg" >&2
    exit 64
  fi
  if [ "$arg" = "stdio" ]; then
    seen_stdio=1
  fi
done
authenticated=
while IFS= read -r line; do
  if [ -n "$GROK_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$GROK_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      case "$GROK_AUTH_METHODS" in
        none)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[],"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
          ;;
        api)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"xai.api_key","name":"API key"}],"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
          ;;
        unknown)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"future_method","name":"Future"}],"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token","name":"Cached login"},{"id":"xai.api_key","name":"API key"}],"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
          ;;
      esac
      ;;
    *'"method":"authenticate"'*)
      if [ -n "$GROK_AUTH_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"authentication required: run grok login"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      authenticated=1
      ;;
    *'"method":"session/new"'*)
      if [ -z "$authenticated" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"authenticate must complete first"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"availableModels":[{"modelId":"grok-4.5","name":"Grok 4.5","description":""},{"modelId":"grok-composer-2.5-fast","name":"Grok Composer 2.5 Fast","description":""}],"currentModelId":"grok-4.5"}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      if [ -z "$authenticated" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"authenticate must complete first"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_loaded","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"history replay ignored"}}}}\n'
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
      if [ -n "$GROK_HANG_PROMPT" ]; then
        while :; do sleep 1; done
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking about it"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed","name":"Shell","output":"hi\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      if [ -n "$GROK_USAGE" ]; then
        # Match live Grok Build ACP (0.2.x): metering lives under result._meta,
        # not a top-level usage field or sessionUpdate=usage_update.
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","_meta":{"sessionId":"ses_new","modelId":"grok-4.5","inputTokens":120,"outputTokens":30,"cachedReadTokens":20,"usage":{"inputTokens":120,"outputTokens":30,"totalTokens":150,"cachedReadTokens":20,"modelCalls":1}}}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      fi
      if [ -n "$GROK_LATE_CHUNK" ]; then
        sleep 0.05
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":" tail"}}}}\n'
      fi
      exit 0
      ;;
  esac
done
`
}

func TestGrokBackendStreamsAndCompletes(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say pong", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range session.Messages {
			messages = append(messages, m)
		}
	}()
	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "pong") {
		t.Fatalf("output = %q, want it to contain the assistant message 'pong'", result.Output)
	}
	if result.SessionID != "ses_new" {
		t.Fatalf("session id = %q, want ses_new", result.SessionID)
	}
	var sawText, sawToolUse bool
	for _, m := range messages {
		if m.Type == MessageText && strings.Contains(m.Content, "pong") {
			sawText = true
		}
		if m.Type == MessageToolUse && m.Tool == "terminal" {
			sawToolUse = true
		}
	}
	if !sawText {
		t.Error("expected a MessageText carrying the assistant 'pong'")
	}
	if !sawToolUse {
		t.Errorf("expected the Shell tool_call to normalize to 'terminal'; messages=%+v", messages)
	}
}

func TestGrokBlockedArgsFiltering(t *testing.T) {
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

	session, err := backend.Execute(ctx, "task", ExecOptions{
		Timeout:       5 * time.Second,
		ThinkingLevel: "high",
		// Users must not strip ACP mode, disable auto-approve, or switch
		// into print/headless transports.
		CustomArgs: []string{"agent", "stdio", "--always-approve", "--yolo", "headless", "-p", "--output-format", "json", "--permission-mode", "default", "--model", "hijack", "--reasoning-effort", "low", "--effort", "low", "--cwd", "/tmp/hijack", "--worktree", "branch-dir", "--ref", "other", "--fork-session", "--rules", "extra"},
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
	wantPrefix := []string{"--no-auto-update", "agent", "--always-approve", "--effort", "high", "--rules", "extra", "stdio"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	joined := strings.Join(lines, " ")
	for _, once := range []string{"--no-auto-update", "agent", "--always-approve", "stdio"} {
		if c := countTokens(lines, once); c != 1 {
			t.Errorf("expected exactly one %q, got %d (full: %q)", once, c, joined)
		}
	}
	for _, blocked := range []string{"headless", "-p", "--output-format", "json", "--permission-mode", "default", "--yolo", "hijack", "--cwd", "/tmp/hijack", "--worktree", "branch-dir", "--ref", "other", "--fork-session"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("blocked custom arg %q survived filtering: %q", blocked, lines)
			}
		}
	}
	// Daemon-owned thinking must win over custom --effort/--reasoning-effort low.
	if strings.Count(joined, "--effort") != 1 || !strings.Contains(joined, "--effort high") {
		t.Errorf("expected single --effort high, got %q", joined)
	}
	if strings.Contains(joined, "--reasoning-effort") {
		t.Errorf("legacy --reasoning-effort must be stripped, got %q", joined)
	}
	// An allowed custom arg must survive (after the fixed prefix).
	if !strings.Contains(joined, "--rules") || !strings.Contains(joined, "extra") {
		t.Errorf("expected allowed custom arg --rules to survive, got %q", joined)
	}
	if lines[len(lines)-1] != "stdio" {
		t.Errorf("stdio must be the final transport subcommand, got %q", lines)
	}
}

func TestGrokSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{Model: "bogus-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed on set_model error, got %q", result.Status)
	}
	if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
		t.Errorf("expected error to name the model, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "model not available") {
		t.Errorf("expected upstream message surfaced, got %q", result.Error)
	}
}

func TestGrokUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"GROK_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
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
		t.Fatalf("expected completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load on resume, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("grok must use session/load when resuming, not session/resume:\n%s", requests)
	}
}

// TestGrokAuthenticatesBeforeSession asserts the ACP auth handshake happens in
// the order the real Grok CLI requires: `authenticate` must be sent after
// `initialize` and before any session operation (session/new or session/load).
// A fake ACP that blindly accepts session/new (as ours does) would otherwise
// hide a missing handshake — the exact gap this guards against.
func TestGrokAuthenticatesBeforeSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		resume      string
		wantSession string
	}{
		{name: "new session", resume: "", wantSession: `"method":"session/new"`},
		{name: "resume", resume: "ses_existing", wantSession: `"method":"session/load"`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			requestsFile := filepath.Join(tempDir, "requests.jsonl")
			fakePath := filepath.Join(tempDir, "grok")
			writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

			backend, err := New("grok", Config{
				ExecutablePath: fakePath,
				Logger:         slog.Default(),
				Env:            map[string]string{"GROK_REQUESTS_FILE": requestsFile},
			})
			if err != nil {
				t.Fatalf("new grok backend: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			session, err := backend.Execute(ctx, "task", ExecOptions{
				ResumeSessionID: tc.resume,
				Timeout:         5 * time.Second,
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			<-session.Result

			raw, err := os.ReadFile(requestsFile)
			if err != nil {
				t.Fatalf("read requests: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			authIdx, sessionIdx, initIdx := -1, -1, -1
			for i, l := range lines {
				switch {
				case strings.Contains(l, `"method":"initialize"`):
					initIdx = i
				case strings.Contains(l, `"method":"authenticate"`):
					authIdx = i
				case strings.Contains(l, tc.wantSession):
					if sessionIdx == -1 {
						sessionIdx = i
					}
				}
			}
			if authIdx == -1 {
				t.Fatalf("expected an authenticate request, got:\n%s", raw)
			}
			if sessionIdx == -1 {
				t.Fatalf("expected a %s request, got:\n%s", tc.wantSession, raw)
			}
			if !(initIdx < authIdx && authIdx < sessionIdx) {
				t.Fatalf("expected order initialize(%d) < authenticate(%d) < session(%d):\n%s",
					initIdx, authIdx, sessionIdx, raw)
			}
			// The daemon has no XAI_API_KEY here, so it must fall back to the
			// cached-token method advertised by the fake.
			if !strings.Contains(lines[authIdx], `"methodId":"cached_token"`) {
				t.Errorf("expected cached_token auth method, got: %s", lines[authIdx])
			}
			if !strings.Contains(lines[authIdx], `"headless":true`) {
				t.Errorf("expected headless meta on authenticate, got: %s", lines[authIdx])
			}
		})
	}
}

// TestGrokAuthFailureFailsTask asserts a rejected authenticate handshake fails
// the task with a clear error instead of falling through to session/new.
func TestGrokAuthFailureFailsTask(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"GROK_AUTH_FAIL": "1"},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed on authenticate error, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "authenticate") {
		t.Errorf("expected error to mention authenticate, got %q", result.Error)
	}
}

func TestGrokNoUsableAuthMethodFailsBeforeSession(t *testing.T) {
	for _, methods := range []string{"none", "unknown", "api"} {
		t.Run(methods, func(t *testing.T) {
			tempDir := t.TempDir()
			requestsFile := filepath.Join(tempDir, "requests.jsonl")
			fakePath := filepath.Join(tempDir, "grok")
			writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

			backend, err := New("grok", Config{
				ExecutablePath: fakePath,
				Logger:         slog.Default(),
				Env: map[string]string{
					"GROK_AUTH_METHODS":  methods,
					"GROK_REQUESTS_FILE": requestsFile,
					"XAI_API_KEY":        "",
				},
			})
			if err != nil {
				t.Fatalf("new grok backend: %v", err)
			}
			session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			result := <-session.Result
			if result.Status != "failed" || !strings.Contains(result.Error, "authentication setup") {
				t.Fatalf("expected auth setup failure, got status=%q error=%q", result.Status, result.Error)
			}
			raw, err := os.ReadFile(requestsFile)
			if err != nil {
				t.Fatalf("read requests: %v", err)
			}
			if strings.Contains(string(raw), `"method":"authenticate"`) || strings.Contains(string(raw), `"method":"session/`) {
				t.Fatalf("must stop before auth/session with unusable methods:\n%s", raw)
			}
		})
	}
}

func TestGrokUsesAdvertisedAPIKeyMethod(t *testing.T) {
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))

	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"GROK_AUTH_METHODS":  "api",
			"GROK_REQUESTS_FILE": requestsFile,
			"XAI_API_KEY":        "test-only-key",
		},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	if !strings.Contains(string(raw), `"methodId":"xai.api_key"`) {
		t.Fatalf("expected advertised API-key method, got:\n%s", raw)
	}
}

func TestGrokDrainsNotificationsAfterPromptResponse(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))
	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"GROK_LATE_CHUNK": "1"},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "pong tail") {
		t.Fatalf("late output was truncated: %q", result.Output)
	}
}

func TestGrokPropagatesMCPAndUsage(t *testing.T) {
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))
	backend, err := New("grok", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"GROK_REQUESTS_FILE": requestsFile,
			"GROK_USAGE":         "1",
		},
	})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "task", ExecOptions{
		Timeout:   5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`),
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
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"name":"fetch"`) || !strings.Contains(requests, `"command":"uvx"`) {
		t.Fatalf("session/new did not receive MCP server:\n%s", raw)
	}
	usage, ok := result.Usage["grok-4.5"]
	if !ok {
		t.Fatalf("usage missing grok-4.5 key: %+v", result.Usage)
	}
	// The fixture's totalTokens (150) equals input + output, so its 20 cached
	// reads sit inside inputTokens and are billed once: input is stored as the
	// uncached remainder 120 - 20 = 100.
	if usage.InputTokens != 100 || usage.OutputTokens != 30 || usage.CacheReadTokens != 20 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestGrokTimeoutAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		timeout    time.Duration
		cancelSoon bool
		wantStatus string
	}{
		{name: "timeout", timeout: time.Second, wantStatus: "timeout"},
		{name: "cancel", timeout: 5 * time.Second, cancelSoon: true, wantStatus: "aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			fakePath := filepath.Join(tempDir, "grok")
			requestsFile := filepath.Join(tempDir, "requests.jsonl")
			writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))
			backend, err := New("grok", Config{
				ExecutablePath: fakePath,
				Logger:         slog.Default(),
				Env: map[string]string{
					"GROK_HANG_PROMPT":   "1",
					"GROK_REQUESTS_FILE": requestsFile,
				},
			})
			if err != nil {
				t.Fatalf("new grok backend: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "task", ExecOptions{Timeout: tc.timeout})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			if tc.cancelSoon {
				deadline := time.Now().Add(3 * time.Second)
				for {
					raw, _ := os.ReadFile(requestsFile)
					if strings.Contains(string(raw), `"method":"session/prompt"`) {
						cancel()
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("fake never reached session/prompt before cancellation")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			select {
			case result := <-session.Result:
				if result.Status != tc.wantStatus {
					t.Fatalf("status=%q error=%q, want %q", result.Status, result.Error, tc.wantStatus)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("grok child was not terminated and reaped")
			}
		})
	}
}

func TestDiscoverGrokModelsWaitsForAdvertisedAuth(t *testing.T) {
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "grok")
	writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))
	t.Setenv("GROK_REQUESTS_FILE", requestsFile)
	t.Setenv("GROK_AUTH_METHODS", "api")
	t.Setenv("XAI_API_KEY", "test-only-key")

	models, err := discoverGrokModels(context.Background(), fakePath)
	if err != nil {
		t.Fatalf("discover grok models: %v", err)
	}
	if len(models) != 2 || models[0].ID != "grok-4.5" {
		t.Fatalf("unexpected models: %+v", models)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	requests := string(raw)
	initAt := strings.Index(requests, `"method":"initialize"`)
	authAt := strings.Index(requests, `"method":"authenticate"`)
	sessionAt := strings.Index(requests, `"method":"session/new"`)
	if !(initAt >= 0 && initAt < authAt && authAt < sessionAt) {
		t.Fatalf("expected response-driven initialize/auth/session order:\n%s", raw)
	}
	if !strings.Contains(requests, `"methodId":"xai.api_key"`) {
		t.Fatalf("expected API-key auth selected from initialize response:\n%s", raw)
	}
}

func TestDiscoverGrokModelsStopsOnAuthFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		methods  string
		authFail string
		wantAuth bool
	}{
		{name: "no methods", methods: "none", wantAuth: false},
		{name: "authenticate rejected", authFail: "1", wantAuth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			requestsFile := filepath.Join(tempDir, "requests.jsonl")
			fakePath := filepath.Join(tempDir, "grok")
			writeTestExecutable(t, fakePath, []byte(fakeGrokACPScript()))
			t.Setenv("GROK_REQUESTS_FILE", requestsFile)
			t.Setenv("GROK_AUTH_METHODS", tc.methods)
			t.Setenv("GROK_AUTH_FAIL", tc.authFail)
			t.Setenv("XAI_API_KEY", "")

			models, err := discoverGrokModels(context.Background(), fakePath)
			if err != nil {
				t.Fatalf("discover grok models: %v", err)
			}
			if len(models) != 2 || models[0].ID != "grok-4.5" {
				t.Fatalf("expected static fallback, got %+v", models)
			}
			raw, err := os.ReadFile(requestsFile)
			if err != nil {
				t.Fatalf("read requests: %v", err)
			}
			requests := string(raw)
			if strings.Contains(requests, `"method":"session/new"`) {
				t.Fatalf("discovery must stop before session/new on auth failure:\n%s", raw)
			}
			if got := strings.Contains(requests, `"method":"authenticate"`); got != tc.wantAuth {
				t.Fatalf("authenticate present=%v, want %v:\n%s", got, tc.wantAuth, raw)
			}
		})
	}
}

func TestGrokThinkingCatalogIsPerModel(t *testing.T) {
	models := grokStaticModels()
	if models[0].Thinking == nil {
		t.Fatal("grok-4.5 should advertise documented effort levels")
	}
	got := make([]string, 0, len(models[0].Thinking.SupportedLevels))
	for _, level := range models[0].Thinking.SupportedLevels {
		got = append(got, level.Value)
	}
	if strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("grok-4.5 levels = %v, want low/medium/high", got)
	}
	if models[1].Thinking != nil {
		t.Fatalf("unverified composer model must hide thinking controls: %+v", models[1].Thinking)
	}
	unknown := []Model{{ID: "future-grok", Label: "Future"}}
	annotateGrokThinking(unknown)
	if unknown[0].Thinking != nil {
		t.Fatalf("unknown models must not inherit grok-4.5 effort levels: %+v", unknown[0].Thinking)
	}
}

func TestGrokValidateThinkingLevelUsesPerModelCatalog(t *testing.T) {
	for _, tc := range []struct {
		model string
		level string
		want  bool
	}{
		{model: "grok-4.5", level: "low", want: true},
		{model: "grok-4.5", level: "none", want: false},
		{model: "grok-4.5", level: "xhigh", want: false},
		{model: "grok-composer-2.5-fast", level: "low", want: false},
		{model: "future-grok", level: "high", want: false},
	} {
		got, err := ValidateThinkingLevel(context.Background(), "grok", "/nonexistent/grok", tc.model, tc.level)
		if err != nil {
			t.Fatalf("ValidateThinkingLevel(%q, %q): %v", tc.model, tc.level, err)
		}
		if got != tc.want {
			t.Errorf("ValidateThinkingLevel(%q, %q) = %v, want %v", tc.model, tc.level, got, tc.want)
		}
	}
}

// TestGrokSelectAuthMethod covers the auth-method selection preference.
func TestGrokSelectAuthMethod(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		methods []string
		haveKey bool
		wantID  string
		wantErr bool
	}{
		{"none advertised", nil, false, "", true},
		{"cached only", []string{"cached_token"}, false, "cached_token", false},
		{"api key preferred when present", []string{"cached_token", "xai.api_key"}, true, "xai.api_key", false},
		{"api key ignored without env", []string{"cached_token", "xai.api_key"}, false, "cached_token", false},
		{"api only requires env", []string{"xai.api_key"}, false, "", true},
		{"unknown fails closed", []string{"future_method"}, true, "", true},
	}
	for _, tc := range cases {
		got, err := selectGrokAuthMethod(tc.methods, tc.haveKey)
		if got != tc.wantID || (err != nil) != tc.wantErr {
			t.Errorf("%s: selectGrokAuthMethod(%v, %v) = (%q, %v), want (%q, err=%v)",
				tc.name, tc.methods, tc.haveKey, got, err, tc.wantID, tc.wantErr)
		}
	}
}

func TestGrokIsKnownThinkingValue(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"", "low", "medium", "high"} {
		if !IsKnownThinkingValue("grok", level) {
			t.Errorf("IsKnownThinkingValue(grok, %q) = false", level)
		}
	}
	for _, level := range []string{"none", "minimal", "xhigh", "bogus", "max"} {
		if IsKnownThinkingValue("grok", level) {
			t.Errorf("IsKnownThinkingValue(grok, %q) = true, want rejected", level)
		}
	}
}
