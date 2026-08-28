//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOmniRouteLive exercises the complete non-mutating path against the
// configured OmniRoute endpoint: streamed completion, MCP discovery, a real
// model-selected tool call, and the second turn containing the tool result.
// The fixture tool is local and returns a fixed value; it performs no writes.
func TestOmniRouteLive(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv(omniRouteBaseURLKey))
	apiKey := strings.TrimSpace(os.Getenv(omniRouteAPIKeyKey))
	if baseURL == "" || apiKey == "" {
		t.Skip("OMNIROUTE_BASE_URL and OMNIROUTE_API_KEY are required")
	}

	probe := filepath.Join(t.TempDir(), "mcp-probe.sh")
	const script = `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"method":"notifications/initialized"'*) : ;;
    *'"method":"initialize"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
      printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"protocolVersion\":\"2025-03-26\",\"capabilities\":{}}}"
      ;;
    *'"method":"tools/list"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
      printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"tools\":[{\"name\":\"probe_echo\",\"description\":\"Return a fixed non-mutating probe value\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}]}}"
      ;;
    *'"method":"tools/call"'*)
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
      printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"non-mutating probe ok\"}]}}"
      ;;
  esac
done
`
	if err := os.WriteFile(probe, []byte(script), 0o700); err != nil {
		t.Fatalf("write MCP probe: %v", err)
	}

	backend, err := New("omniroute", Config{Env: map[string]string{
		omniRouteBaseURLKey: baseURL,
		omniRouteAPIKeyKey:  apiKey,
	}})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	mcpConfig, err := json.Marshal(map[string]interface{}{"mcpServers": map[string]interface{}{
		"probe": map[string]interface{}{"command": probe},
	}})
	if err != nil {
		t.Fatalf("encode MCP config: %v", err)
	}
	model := strings.TrimSpace(os.Getenv("MULTICA_OMNIROUTE_MODEL"))
	if model == "" {
		model = "auto/best-coding"
	}
	session, err := backend.Execute(context.Background(), "You must call the mcp__probe__probe_echo tool exactly once before answering. After the tool returns, answer with a short confirmation that includes the exact phrase non-mutating probe ok.", ExecOptions{
		Cwd:       t.TempDir(),
		Model:     model,
		McpConfig: mcpConfig,
		MaxTurns:  4,
		Timeout:   90 * time.Second,
		AllowedTools: []string{
			"mcp__probe__probe_echo",
		},
		AllowedToolsConfigured: true,
	})
	if err != nil {
		t.Fatalf("start execution: %v", err)
	}
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("live execution status=%q error=%q output=%q messages=%d", result.Status, result.Error, result.Output, len(messages))
	}
	var sawToolUse, sawToolResult bool
	for _, message := range messages {
		if message.Type == MessageToolUse && message.Tool == "mcp__probe__probe_echo" {
			sawToolUse = true
		}
		if message.Type == MessageToolResult && message.Tool == "mcp__probe__probe_echo" && strings.Contains(message.Output, "non-mutating probe ok") {
			sawToolResult = true
		}
	}
	if !sawToolUse || !sawToolResult || !strings.Contains(strings.ToLower(result.Output), "non-mutating probe ok") {
		t.Fatalf("live execution did not prove tool round trip: tool_use=%v tool_result=%v output=%q messages=%s", sawToolUse, sawToolResult, result.Output, fmt.Sprint(messages))
	}
}
