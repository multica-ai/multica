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

func fakeCodebuddyACPScript() string {
	return `#!/bin/sh
# Fake codebuddy — ACP over stdin/stdout. Records argv and JSON-RPC.
if [ -n "$CODEBUDDY_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$CODEBUDDY_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$CODEBUDDY_RPC_FILE" ]; then printf '%s\n' "$line" >> "$CODEBUDDY_RPC_FILE"; fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}},"authMethods":[{"id":"external","name":"Login with Google/Github"}]}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"authenticate must not be called for a logged-in CLI"}}\n' "$id"
      exit 0
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-cb-001","models":{"currentModelId":"hy3"},"configOptions":[{"id":"thought_level","currentValue":"enabled","options":[{"value":"enabled","name":"On (default)"},{"value":"high","name":"High"}]}]}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-resume","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"OLD HISTORY"}}}}'
      if [ -n "$CODEBUDDY_STALE_LOAD" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
        exit 0
      fi
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s","models":{"currentModelId":"hy3"},"configOptions":[{"id":"thought_level","currentValue":"enabled","options":[{"value":"high","name":"High"}]}]}}\n' "$id" "$sid"
      ;;
    *'"method":"session/set_model"'*)
      if [ -n "$CODEBUDDY_SET_MODEL_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
        exit 0
      fi
      if [ -n "$CODEBUDDY_STALE_SET_MODEL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"thought_level","currentValue":"high","options":[{"value":"high","name":"High"}]}]}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      if [ -n "$CODEBUDDY_STALE_PROMPT" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session not found"}}\n' "$id"
        exit 0
      fi
      if [ -n "$CODEBUDDY_TOOL_EVENTS" ]; then
        printf '%s\n' \
          '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-cb-001","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"Checking a file"}}}}' \
          '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-cb-001","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Interim narration"}}}}' \
          '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-cb-001","update":{"sessionUpdate":"tool_call","toolCallId":"call-1","title":"read_file","kind":"read","status":"in_progress","rawInput":{"path":"test.txt"}}}}' \
          '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-cb-001","update":{"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"file contents"}}]}}}'
      fi
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses-cb-001","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"Hello from codebuddy"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":100,"outputTokens":50,"cachedReadTokens":10,"cachedWriteTokens":5}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestBuildCodebuddyArgs_ACP(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{
		Model:           "hy3",
		ThinkingLevel:   "high",
		ResumeSessionID: "sess-abc123",
		MaxTurns:        25,
		SystemPrompt:    "You are an agent.",
		ExtraArgs:       []string{"--output-format", "text", "--max-budget-usd", "1.00"},
		CustomArgs:      []string{"--max-budget-usd", "2.00", "--acp", "--multitask", "--permission-mode", "plan"},
	}, slog.Default())

	if len(args) < 1 || args[0] != "--acp" {
		t.Fatalf("expected --acp first, got %v", args)
	}
	joined := strings.Join(args, " ")
	for _, blocked := range []string{"--output-format text", "--permission-mode plan", "--multitask"} {
		if strings.Contains(joined, blocked) {
			t.Fatalf("blocked %q should be filtered: %v", blocked, args)
		}
	}
	acpCount := 0
	for _, a := range args {
		if a == "--acp" {
			acpCount++
		}
	}
	if !strings.Contains(joined, "--max-turns 25") {
		t.Fatalf("max-turns must survive the ACP migration: %v", args)
	}
	if acpCount != 1 {
		t.Fatalf("expected exactly one --acp, got %d in %v", acpCount, args)
	}

	extraIdx, customIdx := -1, -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--max-budget-usd" && args[i+1] == "1.00" {
			extraIdx = i
		}
		if args[i] == "--max-budget-usd" && args[i+1] == "2.00" {
			customIdx = i
		}
	}
	if extraIdx == -1 || customIdx == -1 || extraIdx > customIdx {
		t.Fatalf("expected extra args before custom args, got %v", args)
	}
}

func TestBuildCodebuddyArgsNeverPassesStrictMCP(t *testing.T) {
	t.Parallel()

	for _, mcpConfig := range []json.RawMessage{
		nil,
		json.RawMessage("null"),
		json.RawMessage(`{}`),
		json.RawMessage(`{"mcpServers":{"paper":{"command":"paper"}}}`),
	} {
		args := buildCodebuddyArgs(ExecOptions{McpConfig: mcpConfig}, slog.Default())
		for _, arg := range args {
			if arg == "--strict-mcp-config" || arg == "--mcp-config" {
				t.Fatalf("ACP launch must not pass MCP via argv, mcp_config %q got %v", string(mcpConfig), args)
			}
		}
	}
}

func TestCodebuddyExecute_NotFound(t *testing.T) {
	t.Parallel()

	b := &codebuddyBackend{cfg: Config{ExecutablePath: "/nonexistent/path/codebuddy", Logger: slog.Default()}}

	ctx := context.Background()
	_, err := b.Execute(ctx, "prompt", ExecOptions{})
	if err == nil {
		t.Fatal("expected error for missing executable")
	}
	if !strings.Contains(err.Error(), "codebuddy executable not found") {
		t.Fatalf("expected 'codebuddy executable not found' in error, got %q", err.Error())
	}
}

func TestCodebuddyExecute_Success(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))

	b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.Default()}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "say hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var gotText bool
	for msg := range session.Messages {
		if msg.Type == MessageText && msg.Content == "Hello from codebuddy" {
			gotText = true
		}
	}
	if !gotText {
		t.Fatal("expected text message 'Hello from codebuddy'")
	}

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "Hello from codebuddy" {
			t.Fatalf("expected output 'Hello from codebuddy', got %q", result.Output)
		}
		if result.SessionID != "ses-cb-001" {
			t.Fatalf("expected session_id=ses-cb-001, got %q", result.SessionID)
		}
		usage, ok := result.Usage["hy3"]
		if !ok {
			t.Fatalf("expected usage for hy3, got %#v", result.Usage)
		}
		if usage.InputTokens != 100 || usage.OutputTokens != 50 || usage.CacheReadTokens != 10 || usage.CacheWriteTokens != 5 {
			t.Fatalf("unexpected usage: %+v", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCodebuddyACPInvokesFlagAndSkipsAuthenticate(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	rpcFile := filepath.Join(tempDir, "rpc.jsonl")
	fakePath := filepath.Join(tempDir, "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))

	b := &codebuddyBackend{cfg: Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"CODEBUDDY_ARGS_FILE": argsFile,
			"CODEBUDDY_RPC_FILE":  rpcFile,
		},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "hi", ExecOptions{
		Timeout:    5 * time.Second,
		CustomArgs: []string{"--acp", "--multitask", "--model", "extra"},
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
		t.Fatalf("status=%q err=%q", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 1 || lines[0] != "--acp" {
		t.Fatalf("expected --acp first, got %q", lines)
	}
	for _, got := range lines[1:] {
		if got == "--acp" || got == "--multitask" {
			t.Fatalf("blocked arg leaked: %q", lines)
		}
	}

	rpc, err := os.ReadFile(rpcFile)
	if err != nil {
		t.Fatalf("read rpc: %v", err)
	}
	body := string(rpc)
	if strings.Contains(body, `"method":"authenticate"`) {
		t.Fatalf("logged-in CodeBuddy must skip authenticate, got:\n%s", body)
	}
	if !strings.Contains(body, `"method":"session/new"`) {
		t.Fatalf("expected session/new, got:\n%s", body)
	}
	initFrame := findRecordedFrame(t, rpcFile, "initialize")
	params, _ := initFrame["params"].(map[string]any)
	caps, _ := params["clientCapabilities"].(map[string]any)
	if len(caps) != 0 {
		t.Fatalf("clientCapabilities must be empty so fs/terminal stay on the agent, got %#v", caps)
	}
}

func TestCodebuddyACPSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CODEBUDDY_SET_MODEL_FAIL": "1"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "hi", ExecOptions{Model: "bogus-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("status=%q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "bogus-model") {
		t.Fatalf("error=%q, want model failure", result.Error)
	}
}

func TestCodebuddyACPResumeUsesSessionLoad(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	tempDir := t.TempDir()
	rpcFile := filepath.Join(tempDir, "rpc.jsonl")
	fakePath := filepath.Join(tempDir, "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CODEBUDDY_RPC_FILE": rpcFile},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "continue", ExecOptions{
		ResumeSessionID: "ses-resume",
		ThinkingLevel:   "high",
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
		t.Fatalf("status=%q err=%q", result.Status, result.Error)
	}
	if strings.Contains(result.Output, "OLD HISTORY") {
		t.Fatal("resume replay leaked into this turn")
	}
	if result.SessionID != "ses-resume" {
		t.Fatalf("session=%q", result.SessionID)
	}

	rpc, err := os.ReadFile(rpcFile)
	if err != nil {
		t.Fatalf("read rpc: %v", err)
	}
	body := string(rpc)
	if !strings.Contains(body, `"method":"session/load"`) {
		t.Fatalf("expected session/load on resume, got:\n%s", body)
	}
	if strings.Contains(body, `"method":"session/resume"`) {
		t.Fatalf("codebuddy must use session/load, not session/resume:\n%s", body)
	}
	if !strings.Contains(body, `"method":"session/set_config_option"`) {
		t.Fatalf("expected thought_level via set_config_option, got:\n%s", body)
	}
}

func TestCodebuddyACPStaleResumeAtPrompt(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CODEBUDDY_STALE_PROMPT": "1"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "hi", ExecOptions{
		ResumeSessionID: "dead-session",
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
	if result.Status != "failed" || !result.ResumeRejected {
		t.Fatalf("status=%q resumeRejected=%v err=%q", result.Status, result.ResumeRejected, result.Error)
	}
	if result.SessionID != "" {
		t.Fatalf("stale resume must clear session id, got %q", result.SessionID)
	}
}

func TestCodebuddyACPStaleResumeAtSetModel(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CODEBUDDY_STALE_SET_MODEL": "1"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "hi", ExecOptions{
		ResumeSessionID: "dead-session",
		Model:           "hy3",
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
	if result.Status != "failed" || !result.ResumeRejected {
		t.Fatalf("status=%q resumeRejected=%v err=%q", result.Status, result.ResumeRejected, result.Error)
	}
}

func TestCodebuddyACPForwardsMCPOnSessionNew(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	tempDir := t.TempDir()
	rpcFile := filepath.Join(tempDir, "rpc.jsonl")
	fakePath := filepath.Join(tempDir, "codebuddy")
	writeTestExecutable(t, fakePath, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CODEBUDDY_RPC_FILE": rpcFile},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "hi", ExecOptions{
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
	<-session.Result

	frame := findRecordedFrame(t, rpcFile, "session/new")
	params, _ := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("session/new.mcpServers: got %#v", params["mcpServers"])
	}
	entry, _ := servers[0].(map[string]any)
	if entry["name"] != "fetch" {
		t.Fatalf("mcp server name=%v want fetch", entry["name"])
	}
}

func TestIsKnownThinkingValue_Codebuddy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value string
		want  bool
	}{
		{"", true},
		{"minimal", true},
		{"low", true},
		{"medium", true},
		{"high", true},
		{"xhigh", true},
		{"max", true},
		{"none", false},
		{"enabled", false},
	}
	for _, tc := range cases {
		got := IsKnownThinkingValue("codebuddy", tc.value)
		if got != tc.want {
			t.Errorf("IsKnownThinkingValue(codebuddy, %q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestCodebuddyACPStaleResumeAtLoad(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	path := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, path, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"CODEBUDDY_STALE_LOAD": "1"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "continue", ExecOptions{ResumeSessionID: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || !result.ResumeRejected || result.SessionID != "" {
		t.Fatalf("stale load must allow a fresh-session retry: %+v", result)
	}
}

func TestCodebuddyACPInitializeFailureCleansUpProcess(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	path := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, path, []byte(`#!/bin/sh
IFS= read -r line
printf '%s\n' '{"jsonrpc":"2.0","id":0,"error":{"code":-32603,"message":"initialization failed"}}'
# Simulate a child that ignores stdin EOF after a failed handshake.
exec sleep 30
`))
	b := &codebuddyBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default()}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "hello", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected handshake failure: %+v", result)
	}
	select {
	case _, ok := <-session.Messages:
		if ok {
			t.Fatal("expected closed message stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handshake failure left the execution waiting on the child")
	}
}

func TestCodebuddyACPStreamsToolsAndKeepsFinalAnswer(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	path := filepath.Join(t.TempDir(), "codebuddy")
	writeTestExecutable(t, path, []byte(fakeCodebuddyACPScript()))
	b := &codebuddyBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"CODEBUDDY_TOOL_EVENTS": "1"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "read a file", ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var thought, narration, tool, output bool
	for msg := range session.Messages {
		switch msg.Type {
		case MessageThinking:
			thought = msg.Content == "Checking a file"
		case MessageText:
			narration = narration || msg.Content == "Interim narration"
		case MessageToolUse:
			tool = msg.CallID == "call-1" && msg.Input["path"] == "test.txt"
		case MessageToolResult:
			output = msg.CallID == "call-1" && strings.Contains(msg.Output, "file contents")
		}
	}
	result := <-session.Result
	if !thought || !narration || !tool || !output {
		t.Fatalf("missing streamed events: thought=%v narration=%v tool=%v output=%v", thought, narration, tool, output)
	}
	if result.Status != "completed" || result.Output != "Hello from codebuddy" {
		t.Fatalf("expected final answer without pre-tool narration: %+v", result)
	}
}
