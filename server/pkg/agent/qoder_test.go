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

func fakeQoderACPScript() string {
	return `#!/bin/sh
# Fake qodercli — exercises argv (--yolo --acp), blocked custom_args, set_model failure, and prompt success.
if [ -n "$QODER_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$QODER_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
      exit 0
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeQoderACPStaleResumeScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeQoderACPStaleResumeSetModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeQoderACPScriptWithLeakedStdout() string {
	return `#!/bin/sh
# Fake qodercli that returns session/prompt but leaves stdout open via a child
# process, matching qodercli ACP staying alive after the turn completes.
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      sleep 30 &
      wait
      ;;
  esac
done
`
}

func fakeQoderACPScriptWithLateStdoutAfterResult() string {
	return `#!/bin/sh
# Fake qodercli that returns session/prompt, then leaves stdout open via a
# child process that writes one more notification after the bounded drain grace.
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      ( sleep 0.08; printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"late"}}}}\n' ) &
      sleep 30
      ;;
  esac
done
`
}

// fakeQoderACPScriptCapturingRPC records every JSON-RPC line it receives to
// $QODER_RPC_FILE so a test can inspect the session/new mcpServers payload that
// qoder actually sent (e.g. that remote MCP headers survived the conversion).
func fakeQoderACPScriptCapturingRPC() string {
	return `#!/bin/sh
while IFS= read -r line; do
  if [ -n "$QODER_RPC_FILE" ]; then printf '%s\n' "$line" >> "$QODER_RPC_FILE"; fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ "$QODER_INIT_MCP_SSE" = "1" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"sse":true}}}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      fi
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// fakeQoderACPScriptTerminalProviderError streams a normal text chunk (so the
// final output is non-empty) but writes a terminal upstream-LLM error to stderr
// before ending the turn with end_turn. This is the case promoteACPResultOnProviderError
// must catch: a successful-looking end_turn with output, masking a real failure.
func fakeQoderACPScriptTerminalProviderError() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"partial answer"}}}}\n'
      printf '%s\n' '[ERROR] API call failed after 3 retries: HTTP 429 RateLimitError' >&2
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      exit 0
      ;;
  esac
done
	`
}

func fakeQoderACPUsageWithDefaultModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_model","models":{"currentModelId":"qoder:auto","availableModels":[{"modelId":"qoder:auto","name":"Qoder Auto"}]}}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":17,"outputTokens":5,"cachedReadTokens":3}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestQoderBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 30 * time.Second,
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
		if result.SessionID != "ses_fake" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestQoderBackendInvokesACPFlagAndFiltersBlockedArgs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"QODER_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:      "bogus-model",
		Timeout:    30 * time.Second,
		CustomArgs: []string{"--acp", "acp", "--yolo", "--model", "extra"},
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
		t.Fatalf("expected at least 2 argv entries, got %d: %q", len(lines), lines)
	}
	if lines[0] != "--yolo" || lines[1] != "--acp" {
		t.Fatalf("arg[0], arg[1] = %q, %q, want --yolo and --acp (full: %q)", lines[0], lines[1], lines)
	}
	for _, blocked := range []string{"acp"} {
		for _, got := range lines[2:] {
			if got == blocked {
				t.Errorf("custom_args must not inject standalone %q after daemon argv: %q", blocked, lines)
			}
		}
	}
	yoloCount := 0
	for _, got := range lines {
		if got == "--yolo" {
			yoloCount++
		}
	}
	if yoloCount != 1 {
		t.Fatalf("expected exactly one daemon --yolo, got count=%d argv=%q", yoloCount, lines)
	}
	want := []string{"--yolo", "--acp", "--model", "extra"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("unexpected argv after filtering: %q, want %q", lines, want)
	}
}

func TestQoderBackendHappyPath(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
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

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status=%q err=%q", result.Status, result.Error)
	}
	if result.Output != "ok" {
		t.Fatalf("output=%q want ok", result.Output)
	}
	if result.SessionID != "ses_fake" {
		t.Fatalf("session=%q", result.SessionID)
	}
	if u := result.Usage["unknown"]; u.InputTokens != 1 || u.OutputTokens != 2 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestQoderBackendNonPositiveTimeoutDoesNotImposeDeadline(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hi", ExecOptions{Timeout: -1})
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
			t.Fatalf("non-positive timeout should not impose a deadline, got status=%q error=%q", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for qoder result")
	}
}

func TestQoderBackendDoesNotWaitForeverForReaderAfterPromptDone(t *testing.T) {
	oldGrace := qoderReaderDrainGrace
	qoderReaderDrainGrace = 25 * time.Millisecond
	t.Cleanup(func() { qoderReaderDrainGrace = oldGrace })

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScriptWithLeakedStdout()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
			t.Fatalf("status=%q err=%q", result.Status, result.Error)
		}
		if result.Output != "ok" {
			t.Fatalf("output=%q want ok", result.Output)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("qoder result blocked waiting for reader shutdown")
	}
}

func TestQoderMessageStreamDropsSendAfterClose(t *testing.T) {
	stream := newQoderMessageStream(1)
	stream.close()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("send after close panicked: %v", r)
		}
	}()
	stream.send(Message{Type: MessageText, Content: "late"})

	if _, ok := <-stream.ch; ok {
		t.Fatal("message channel should be closed")
	}
}

func TestQoderBackendIgnoresLateReaderOutputAfterGrace(t *testing.T) {
	oldGrace := qoderReaderDrainGrace
	qoderReaderDrainGrace = 25 * time.Millisecond
	t.Cleanup(func() { qoderReaderDrainGrace = oldGrace })

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScriptWithLateStdoutAfterResult()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
			t.Fatalf("status=%q err=%q", result.Status, result.Error)
		}
		if result.Output != "ok" {
			t.Fatalf("output=%q want ok", result.Output)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("qoder result blocked waiting for reader shutdown")
	}

	time.Sleep(150 * time.Millisecond)
}

// TestQoderForwardsMcpAuthHeaderToSessionNew is the end-to-end guard for the
// header-drop bug: qoder must reuse the shared buildACPMcpServers converter so a
// remote MCP server's Authorization header reaches the session/new payload as
// [{name, value}] instead of being silently stripped. We inspect the exact
// JSON-RPC the backend wrote to the (fake) qodercli stdin.
func TestQoderForwardsMcpAuthHeaderToSessionNew(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rpcFile := filepath.Join(tempDir, "rpc.txt")
	fakePath := filepath.Join(tempDir, "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScriptCapturingRPC()))

	backend, err := New("qoder", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"QODER_RPC_FILE":     rpcFile,
			"QODER_INIT_MCP_SSE": "1",
		},
	})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mcpConfig := json.RawMessage(`{"mcpServers":{"fetch":{"type":"sse","url":"https://example.com/sse","headers":{"Authorization":"Bearer tok"}}}}`)
	session, err := backend.Execute(ctx, "hi", ExecOptions{
		Timeout:   30 * time.Second,
		McpConfig: mcpConfig,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(rpcFile)
	if err != nil {
		t.Fatalf("read rpc file: %v", err)
	}
	var sessionNew string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"method":"session/new"`) {
			sessionNew = line
			break
		}
	}
	if sessionNew == "" {
		t.Fatalf("no session/new request captured; rpc log:\n%s", raw)
	}
	if !strings.Contains(sessionNew, "Authorization") || !strings.Contains(sessionNew, "Bearer tok") {
		t.Fatalf("Authorization header did not survive into session/new mcpServers:\n%s", sessionNew)
	}
	// The shared converter emits headers as {name, value} pairs, not the raw
	// Claude object-map; assert that wire shape too.
	if !strings.Contains(sessionNew, `"name":"Authorization"`) || !strings.Contains(sessionNew, `"value":"Bearer tok"`) {
		t.Fatalf("expected headers as [{name,value}] in session/new payload:\n%s", sessionNew)
	}
}

func TestQoderFiltersRemoteMcpWhenInitializeDoesNotAdvertiseCapability(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	rpcFile := filepath.Join(tempDir, "rpc.txt")
	fakePath := filepath.Join(tempDir, "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScriptCapturingRPC()))

	backend, err := New("qoder", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"QODER_RPC_FILE": rpcFile},
	})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mcpConfig := json.RawMessage(`{"mcpServers":{"local-stdio":{"command":"uvx","args":["mcp-local"]},"remote-sse":{"type":"sse","url":"https://example.com/sse","headers":{"Authorization":"Bearer tok"}}}}`)
	session, err := backend.Execute(ctx, "hi", ExecOptions{
		Timeout:   30 * time.Second,
		McpConfig: mcpConfig,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(rpcFile)
	if err != nil {
		t.Fatalf("read rpc file: %v", err)
	}
	var sessionNew string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"method":"session/new"`) {
			sessionNew = line
			break
		}
	}
	if sessionNew == "" {
		t.Fatalf("no session/new request captured; rpc log:\n%s", raw)
	}
	if !strings.Contains(sessionNew, `"name":"local-stdio"`) || !strings.Contains(sessionNew, `"command":"uvx"`) {
		t.Fatalf("stdio MCP server should remain in session/new payload:\n%s", sessionNew)
	}
	if strings.Contains(sessionNew, `"name":"remote-sse"`) || strings.Contains(sessionNew, "https://example.com/sse") || strings.Contains(sessionNew, "Bearer tok") {
		t.Fatalf("remote SSE MCP server should be filtered when initialize omits mcpCapabilities.sse:\n%s", sessionNew)
	}
}

// TestQoderPromotesTerminalProviderErrorWithOutput guards the second bug: a turn
// that ends with stopReason=end_turn AND non-empty output must still be reported
// as failed when stderr carried a terminal upstream-LLM error. Without the
// StderrPipe drain + promoteACPResultOnProviderError, this run reports
// "completed" and hides the failure.
func TestQoderPromotesTerminalProviderErrorWithOutput(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScriptTerminalProviderError()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
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
			t.Fatalf("expected status=failed for a terminal provider error, got %q (output=%q error=%q)", result.Status, result.Output, result.Error)
		}
		if result.Output != "partial answer" {
			t.Errorf("expected the partial output to be preserved, got %q", result.Output)
		}
		if !strings.Contains(result.Error, "provider error") || !strings.Contains(result.Error, "429") {
			t.Errorf("expected error to surface the terminal stderr marker, got %q", result.Error)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestQoderBackendAttributesUsageToACPDefaultModel(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPUsageWithDefaultModelScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 30 * time.Second,
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
		if result.Status != "completed" {
			t.Fatalf("expected completed result, got %q: %s", result.Status, result.Error)
		}
		if _, ok := result.Usage["unknown"]; ok {
			t.Fatalf("usage should not be attributed to unknown: %+v", result.Usage)
		}
		usage, ok := result.Usage["qoder:auto"]
		if !ok {
			t.Fatalf("expected usage under Qoder current model, got %+v", result.Usage)
		}
		if usage.InputTokens != 17 || usage.OutputTokens != 5 || usage.CacheReadTokens != 3 {
			t.Fatalf("usage = %+v, want input=17 output=5 cache_read=3", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestQoderBackendClearsSessionIDWhenResumedSessionNotFoundAtPrompt(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPStaleResumeScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         30 * time.Second,
		ResumeSessionID: "ses_stale",
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
		if !strings.Contains(result.Error, "Session not found") {
			t.Errorf("expected error to surface the session-not-found message, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestQoderBackendClearsSessionIDWhenResumedSessionNotFoundAtSetModel(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPStaleResumeSetModelScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         30 * time.Second,
		ResumeSessionID: "ses_stale",
		Model:           "some-model",
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
		if !strings.Contains(result.Error, `could not switch to model "some-model"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
