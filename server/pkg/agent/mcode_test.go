package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeFakeMcodeACP(t *testing.T, loadSession bool) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcode")
	requests := filepath.Join(dir, "requests.jsonl")
	args := filepath.Join(dir, "args.txt")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" > %q
while IFS= read -r line; do
  printf '%%s\n' "$line" >> %q
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":%t,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"mcode-session-new"}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      sleep 0.05
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"mcode-session-new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"MCode completed the task"}}}}\n'
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`, args, requests, loadSession)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mcode: %v", err)
	}
	return bin, requests, args
}

func writeFakeMcodeACPReadyAfterInitialize(t *testing.T, readyAfter time.Duration) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcode")
	requests := filepath.Join(dir, "requests.jsonl")
	ready := filepath.Join(dir, "ready")
	script := fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
  printf '%%s\n' "$line" >> %q
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ( sleep %.3f; : > %q ) &
      ;;
    *'"method":"session/new"'*)
      if [ ! -f %q ]; then
        exit 42
      fi
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"mcode-ready-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"mcode-ready-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ready"}}}}\n'
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`, requests, readyAfter.Seconds(), ready, ready)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mcode: %v", err)
	}
	return bin, requests
}

func writeFakeMcodeACPExitsOnSessionNew(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcode")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf 'mcode exited while starting a session\n' >&2
      exit 42
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mcode: %v", err)
	}
	return bin
}

func TestMcodeModelSelectionIsRuntimeManaged(t *testing.T) {
	t.Parallel()
	if ModelSelectionSupported("mcode") {
		t.Fatal("MCode ACP does not expose session-scoped model selection")
	}
}

func TestMcodeFreshSessionUsesACPAndForwardsMCP(t *testing.T) {
	t.Parallel()
	bin, requestsFile, argsFile := writeFakeMcodeACP(t, false)
	backend, err := New("mcode", Config{
		ExecutablePath: bin,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("New(mcode): %v", err)
	}
	session, err := backend.Execute(context.Background(), "ship it", ExecOptions{
		Cwd:       t.TempDir(),
		McpConfig: json.RawMessage(`{"mcpServers":{"docs":{"command":"docs-server","args":["--stdio"]},"remote":{"type":"http","url":"https://example.test/mcp"},"events":{"type":"sse","url":"https://example.test/events"}}}`),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var text strings.Builder
	for message := range session.Messages {
		if message.Type == MessageText {
			text.WriteString(message.Content)
		}
	}
	result := <-session.Result
	if result.Status != "completed" || result.SessionID != "mcode-session-new" {
		t.Fatalf("result = %+v", result)
	}
	if result.Output != "MCode completed the task" {
		t.Fatalf("result output = %q, want final MCode message", result.Output)
	}
	if !strings.Contains(text.String(), "MCode completed the task") {
		t.Fatalf("streamed text = %q", text.String())
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if strings.TrimSpace(string(args)) != "acp" {
		t.Fatalf("mcode args = %q, want %q", strings.TrimSpace(string(args)), "acp")
	}
	requests, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	raw := string(requests)
	for _, want := range []string{`"method":"session/new"`, `"mcpServers"`, `"docs-server"`, `"https://example.test/mcp"`, `"https://example.test/events"`, `"type":"sse"`, `"method":"session/prompt"`, `"text":"ship it"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("requests missing %s:\n%s", want, raw)
		}
	}
}

func TestMcodeWaitsForSessionNewReadinessAfterInitialize(t *testing.T) {
	t.Parallel()
	bin, requestsFile := writeFakeMcodeACPReadyAfterInitialize(t, 50*time.Millisecond)
	backend, err := New("mcode", Config{ExecutablePath: bin, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(mcode): %v", err)
	}
	session, err := backend.Execute(context.Background(), "ship it", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.SessionID != "mcode-ready-session" {
		t.Fatalf("result = %+v", result)
	}
	requests, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	if !strings.Contains(string(requests), `"method":"session/new"`) {
		t.Fatalf("session/new was not sent:\n%s", requests)
	}
}

func TestMcodeSessionNewExitAfterInitializeReportsStartupFailure(t *testing.T) {
	t.Parallel()
	bin := writeFakeMcodeACPExitsOnSessionNew(t)
	backend, err := New("mcode", Config{ExecutablePath: bin, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(mcode): %v", err)
	}
	session, err := backend.Execute(context.Background(), "ship it", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("result = %+v, want failed", result)
	}
	if !strings.Contains(result.Error, "session/new failed after initialize") {
		t.Fatalf("error = %q, want session/new startup failure", result.Error)
	}
	if strings.Contains(result.Error, "initialize failed") {
		t.Fatalf("error = %q, must not blame initialize", result.Error)
	}
}

func TestMcodeUnsupportedResumeRequestsFreshRetry(t *testing.T) {
	t.Parallel()
	bin, requestsFile, _ := writeFakeMcodeACP(t, false)
	backend, err := New("mcode", Config{ExecutablePath: bin, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(mcode): %v", err)
	}
	session, err := backend.Execute(context.Background(), "continue", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "old-mcode-session",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || !result.ResumeRejected {
		t.Fatalf("result = %+v, want failed ResumeRejected", result)
	}
	if !strings.Contains(result.Error, "does not support session loading") {
		t.Fatalf("error = %q", result.Error)
	}
	requests, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	raw := string(requests)
	if strings.Contains(raw, `"method":"session/load"`) || strings.Contains(raw, `"method":"session/prompt"`) {
		t.Fatalf("unsupported resume must stop after initialize:\n%s", raw)
	}
}

func TestMcodeLoadSessionWhenCapabilityAppears(t *testing.T) {
	t.Parallel()
	bin, requestsFile, _ := writeFakeMcodeACP(t, true)
	backend, err := New("mcode", Config{ExecutablePath: bin, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(mcode): %v", err)
	}
	session, err := backend.Execute(context.Background(), "continue", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "old-mcode-session",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.SessionID != "old-mcode-session" {
		t.Fatalf("result = %+v", result)
	}
	requests, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	if !strings.Contains(string(requests), `"method":"session/load"`) {
		t.Fatalf("loadSession capability was not honored:\n%s", requests)
	}
}

func TestMcodeBlockedArgsKeepACPTransportStable(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"acp", "login", "--region", "-h", "--help"} {
		if _, ok := mcodeBlockedArgs[arg]; !ok {
			t.Errorf("mcodeBlockedArgs missing %q", arg)
		}
	}
}

// A still-running MCode reports session/new failures as a JSON-RPC error, and
// session/new is the call that launches mcpServers. Only transport-level exit
// signals may be reported as "MiniMax Code ACP exited" — anything else must
// reach the member with its root cause intact.
func TestMcodeSessionStartupExitedOnlyTrustsTransportSignals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		err       error
		want      bool
	}{
		{
			name:      "reader saw the process exit",
			operation: "session/new",
			err:       errMcodeProcessExited,
			want:      true,
		},
		{
			name:      "stdin write raced the exit",
			operation: "session/new",
			err:       fmt.Errorf("write session/new: %w", syscall.EPIPE),
			want:      true,
		},
		{
			name:      "closed pipe",
			operation: "session/new",
			err:       fmt.Errorf("write session/new: %w", io.ErrClosedPipe),
			want:      true,
		},
		{
			name:      "live mcode reporting a failed MCP server is not an mcode exit",
			operation: "session/new",
			err:       &acpRPCError{Method: "session/new", Code: -32603, Message: "Internal error", Data: `failed to start MCP server "filesystem": process exited with code 1`},
			want:      false,
		},
		{
			name:      "live mcode reporting a broken pipe to an MCP child is not an mcode exit",
			operation: "session/new",
			err:       &acpRPCError{Method: "session/new", Code: -32603, Message: "Internal error", Data: "write to mcp stdio: broken pipe"},
			want:      false,
		},
		{
			name:      "unrelated live error",
			operation: "session/new",
			err:       &acpRPCError{Method: "session/new", Code: -32602, Message: "Invalid params", Data: "cwd must be absolute"},
			want:      false,
		},
		{
			name:      "wrong operation does not match",
			operation: "session/load",
			err:       errMcodeProcessExited,
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcodeSessionStartupExited(tt.operation, tt.err); got != tt.want {
				t.Fatalf("mcodeSessionStartupExited(%q, %v) = %v, want %v", tt.operation, tt.err, got, tt.want)
			}
		})
	}
}

// The rewritten startup message carries no %v, so a false positive would not
// just mislabel the failure — it would drop the original error entirely.
func TestMcodeRequestFailureKeepsLiveSessionErrorRootCause(t *testing.T) {
	t.Parallel()

	err := &acpRPCError{Method: "session/new", Code: -32603, Message: "Internal error", Data: `failed to start MCP server "filesystem": process exited with code 1`}
	status, msg := mcodeRequestFailure(context.Background(), 0, "session/new", err)
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
	if !strings.Contains(msg, "failed to start MCP server") {
		t.Fatalf("error = %q, want the original MCP failure preserved", msg)
	}
	if strings.Contains(msg, "MiniMax Code ACP exited") {
		t.Fatalf("error = %q, must not blame an mcode exit while mcode is still running", msg)
	}
}
