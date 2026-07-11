package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
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
if [ -n "$GROK_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$GROK_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$GROK_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$GROK_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"availableModels":[{"modelId":"grok-4.5","name":"Grok 4.5","description":""},{"modelId":"grok-composer-2.5-fast","name":"Grok Composer 2.5 Fast","description":""}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
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
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking about it"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed","name":"Shell","output":"hi\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
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
		CustomArgs: []string{"agent", "stdio", "--always-approve", "--yolo", "headless", "-p", "--output-format", "json", "--permission-mode", "default", "--model", "hijack", "--reasoning-effort", "low", "--rules", "extra"},
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
	wantPrefix := []string{"agent", "--always-approve", "--reasoning-effort", "high", "stdio"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	joined := strings.Join(lines, " ")
	for _, once := range []string{"agent", "--always-approve", "stdio"} {
		if c := countTokens(lines, once); c != 1 {
			t.Errorf("expected exactly one %q, got %d (full: %q)", once, c, joined)
		}
	}
	for _, blocked := range []string{"headless", "-p", "--output-format", "json", "--permission-mode", "default", "--yolo", "hijack"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("blocked custom arg %q survived filtering: %q", blocked, lines)
			}
		}
	}
	// Daemon-owned thinking must win over custom --reasoning-effort low.
	if strings.Count(joined, "--reasoning-effort") != 1 || !strings.Contains(joined, "--reasoning-effort high") {
		t.Errorf("expected single --reasoning-effort high, got %q", joined)
	}
	// An allowed custom arg must survive (after the fixed prefix).
	if !strings.Contains(joined, "--rules") || !strings.Contains(joined, "extra") {
		t.Errorf("expected allowed custom arg --rules to survive, got %q", joined)
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

func TestGrokIsKnownThinkingValue(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if !IsKnownThinkingValue("grok", level) {
			t.Errorf("IsKnownThinkingValue(grok, %q) = false", level)
		}
	}
	if IsKnownThinkingValue("grok", "bogus") {
		t.Error("bogus should be rejected")
	}
}

// TestGrokRealACPSmoke drives the REAL `grok agent stdio` binary end-to-end
// when it is installed and authenticated. Skipped automatically when grok is
// not on PATH or the session cannot be created, so CI stays green.
func TestGrokRealACPSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("grok")
	if err != nil {
		t.Skip("grok not on PATH; skipping real-binary smoke test")
	}

	backend, err := New("grok", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new grok backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "Reply with exactly one word: pong. Do not use any tools.", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 80 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status == "failed" && (strings.Contains(result.Error, "session/new") || strings.Contains(result.Error, "initialize")) {
			t.Skipf("grok not authenticated or ACP unavailable: %v", result.Error)
		}
		if result.Status != "completed" {
			t.Fatalf("real grok run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real grok output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real grok")
		}
		t.Logf("real grok smoke OK: session=%s output=%q", result.SessionID, result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real grok result")
	}
}
