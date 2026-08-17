package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestNewReturnsMcodeBackend(t *testing.T) {
	t.Parallel()
	backend, err := New("mcode", Config{ExecutablePath: "/nonexistent/mcode"})
	if err != nil {
		t.Fatalf("New(mcode) error: %v", err)
	}
	if _, ok := backend.(*mcodeBackend); !ok {
		t.Fatalf("New(mcode) = %T, want *mcodeBackend", backend)
	}
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
