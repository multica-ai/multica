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

func TestParseDshModelID(t *testing.T) {
	got, err := parseDshModelID("deepseek-official/deepseek-v4%2Fflash")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "deepseek-official" || got.ID != "deepseek-v4/flash" {
		t.Fatalf("unexpected model: %#v", got)
	}
	if _, err := parseDshModelID("deepseek-v4-flash"); err == nil {
		t.Fatal("unqualified DSH model should fail")
	}
}

func TestBuildDshMCPServers(t *testing.T) {
	raw := json.RawMessage(`{"mcpServers":{"files":{"command":"node","args":["server.js"],"env":{"TOKEN":"value"}},"remote":{"type":"streamable-http","url":"https://mcp.example/rpc","headers":{"Authorization":"Bearer test"}}}}`)
	got, err := buildDshMCPServers(raw, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d MCP servers", len(got))
	}
	if got[0].Transport != "stdio" || got[0].Command != "node" || got[0].Env["TOKEN"] != "value" {
		t.Fatalf("bad stdio server: %#v", got[0])
	}
	if got[1].Transport != "streamable-http" || got[1].Headers["Authorization"] != "Bearer test" {
		t.Fatalf("bad HTTP server: %#v", got[1])
	}
}

func TestBuildDshMCPServersRejectsSSE(t *testing.T) {
	raw := json.RawMessage(`{"mcpServers":{"legacy":{"type":"sse","url":"https://mcp.example/sse"}}}`)
	if _, err := buildDshMCPServers(raw, slog.Default()); err == nil || !strings.Contains(err.Error(), "SSE") {
		t.Fatalf("expected SSE error, got %v", err)
	}
}

func TestDshBackendExecuteStreamsProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := writeDshFixture(t, `
if [ "$1" != "--profile" ] || [ "$2" != "multica" ] || [ "$3" != "--stdio" ]; then exit 9; fi
printf '%s\n' '{"v":1,"type":"ready","runtime":"dsh","plugin_version":"test","capabilities":{}}'
IFS= read -r command
case "$command" in *'"type":"execute"'*) ;; *) exit 8 ;; esac
printf '%s\n' '{"v":1,"type":"session","request_id":"task-1","session_id":"session-1","resumed":false}'
printf '%s\n' '{"v":1,"type":"thinking","request_id":"task-1","content":"checking"}'
printf '%s\n' '{"v":1,"type":"tool_call","request_id":"task-1","call_id":"call-1","name":"bash","arguments":"{\"command\":\"pwd\"}"}'
printf '%s\n' '{"v":1,"type":"tool_result","request_id":"task-1","call_id":"call-1","name":"bash","output":"/work","is_error":false}'
printf '%s\n' '{"v":1,"type":"text","request_id":"task-1","content":"done"}'
printf '%s\n' '{"v":1,"type":"usage","request_id":"task-1","provider":"deepseek-official","model":"deepseek-v4-flash","input_tokens":12,"output_tokens":3,"cache_read_tokens":2}'
printf '%s\n' '{"v":1,"type":"result","request_id":"task-1","status":"completed","session_id":"session-1","output":"done","resume_rejected":false}'
`)
	b, err := New("dsh", Config{ExecutablePath: bin, TaskID: "task-1", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := b.Execute(context.Background(), "say done", ExecOptions{Cwd: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "done" || result.SessionID != "session-1" {
		t.Fatalf("bad result: %#v", result)
	}
	usage := result.Usage["deepseek-official/deepseek-v4-flash"]
	if usage.InputTokens != 12 || usage.OutputTokens != 3 || usage.CacheReadTokens != 2 {
		t.Fatalf("bad usage: %#v", usage)
	}
	if len(messages) != 5 || messages[0].SessionID != "session-1" || messages[2].Tool != "bash" {
		t.Fatalf("bad messages: %#v", messages)
	}
}

func TestDshBackendCancellationUsesProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := writeDshFixture(t, `
printf '%s\n' '{"v":1,"type":"ready","runtime":"dsh","plugin_version":"test","capabilities":{}}'
IFS= read -r execute
printf '%s\n' '{"v":1,"type":"session","request_id":"task-cancel","session_id":"session-cancel","resumed":false}'
IFS= read -r cancel
case "$cancel" in *'"type":"cancel"'*) ;; *) exit 8 ;; esac
printf '%s\n' '{"v":1,"type":"result","request_id":"task-cancel","status":"cancelled","session_id":"session-cancel","output":"","resume_rejected":false}'
`)
	b, err := New("dsh", Config{ExecutablePath: bin, TaskID: "task-cancel", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	session, err := b.Execute(ctx, "wait", ExecOptions{Cwd: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	message := <-session.Messages
	if message.SessionID != "session-cancel" {
		t.Fatalf("bad session message: %#v", message)
	}
	cancel()
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "cancelled" || result.SessionID != "session-cancel" {
		t.Fatalf("bad cancellation result: %#v", result)
	}
}

func TestDshBackendRuntimeBudgetStopsBeforeSeventhToolCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := writeDshFixture(t, `
printf '%s\n' '{"v":1,"type":"ready","runtime":"dsh","plugin_version":"test","capabilities":{}}'
IFS= read -r execute
printf '%s\n' '{"v":1,"type":"session","request_id":"task-budget","session_id":"session-budget","resumed":false}'
i=1
while [ "$i" -le 7 ]; do
  printf '{"v":1,"type":"tool_call","request_id":"task-budget","call_id":"call-%s","name":"bash","arguments":"{}"}\n' "$i"
  i=$((i + 1))
done
IFS= read -r cancel
case "$cancel" in *'"type":"cancel"'*) ;; *) exit 8 ;; esac
`)
	b, err := New("dsh", Config{ExecutablePath: bin, TaskID: "task-budget", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	prompt := `MULTICA_RUNTIME_BUDGET {"wall_clock_seconds":5,"max_tool_calls":6,"max_repeated_decisions":3}
review`
	session, err := b.Execute(context.Background(), prompt, ExecOptions{Cwd: t.TempDir(), Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	tools := 0
	for message := range session.Messages {
		if message.Type == MessageToolUse {
			tools++
		}
	}
	result := <-session.Result
	if tools != 6 {
		t.Fatalf("forwarded %d tool calls, want exactly 6", tools)
	}
	if result.Status != "failed" || !strings.Contains(result.Error, "before call 7") {
		t.Fatalf("unexpected budget result: %#v", result)
	}
}

func TestDshBackendDiscoversRuntimeBudgetFromIssueGetResult(t *testing.T) {
	output := `{"description":"MULTICA_RUNTIME_BUDGET {\"wall_clock_seconds\":300,\"max_tool_calls\":6,\"max_repeated_decisions\":3}\ncontract"}`
	budget, err := parseDshRuntimeBudgetResult(output)
	if err != nil {
		t.Fatal(err)
	}
	if budget == nil || budget.MaxToolCalls != 6 || budget.WallClockSeconds != 300 {
		t.Fatalf("unexpected discovered budget: %#v", budget)
	}
}

func TestParseDshRuntimeBudgetRejectsMalformedDirective(t *testing.T) {
	if _, err := parseDshRuntimeBudget("MULTICA_RUNTIME_BUDGET {}\nreview"); err == nil {
		t.Fatal("expected invalid zero budget to fail")
	}
}

func TestDiscoverDshModels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := writeDshFixture(t, `
printf '%s\n' '{"v":1,"type":"models","models":[{"id":"deepseek-official/deepseek-v4-flash","label":"DeepSeek V4 Flash","provider":"DeepSeek","default":true,"thinking":{"supported_levels":[{"value":"high","label":"High"},{"value":"max","label":"Max"}],"default_level":"high"}}]}'
`)
	models, err := discoverDshModels(context.Background(), Command{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || !models[0].Default || models[0].Thinking == nil || models[0].Thinking.DefaultLevel != "high" {
		t.Fatalf("bad models: %#v", models)
	}
}

func writeDshFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
