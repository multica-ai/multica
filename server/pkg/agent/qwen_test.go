package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildQwenArgs(t *testing.T) {
	t.Parallel()

	args := buildQwenArgs("prompt must stay daemon-owned", ExecOptions{
		Model:           "qwen3.8-max-preview",
		MaxTurns:        12,
		SystemPrompt:    "Follow the project rules.",
		ResumeSessionID: "session-1",
		ExtraArgs:       []string{"--prompt", "replace", "--max-steps", "5"},
		CustomArgs:      []string{"--yolo", "--model", "replace", "--foo", "bar"},
	}, slog.Default())

	wantPrefix := []string{
		"-p", "prompt must stay daemon-owned", "--output-format", "stream-json", "--yolo",
		"--model", "qwen3.8-max-preview", "--max-session-turns", "12",
		"--append-system-prompt", "Follow the project rules.", "--resume", "session-1",
	}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args = %v, want prefix %v", args, wantPrefix)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; all: %v", i, args[i], want, args)
		}
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "replace") || !strings.Contains(joined, "--max-steps 5") || !strings.Contains(joined, "--foo bar") {
		t.Fatalf("unexpected filtered args: %v", args)
	}
}

func TestQwenExecuteStreamJSON(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qwen")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","session_id":"qwen-session-1","model":"qwen3.8-max-preview"}'
printf '%s\n' '{"type":"assistant","message":{"model":"qwen3.8-max-preview","content":[{"type":"thinking","thinking":"checking"},{"type":"tool_use","id":"call-1","name":"run_shell_command","input":{"command":"pwd"}}]}}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-1","content":"/workspace"}]}}'
printf '%s\n' '{"type":"assistant","message":{"model":"qwen3.8-max-preview","content":[{"type":"text","text":"QWEN_OK"}]}}'
printf '%s\n' '{"type":"result","session_id":"qwen-session-1","result":"QWEN_OK","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":10}}'
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("qwen", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(qwen): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say QWEN_OK", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "QWEN_OK" || result.SessionID != "qwen-session-1" {
		t.Fatalf("result = %#v", result)
	}
	usage := result.Usage["qwen3.8-max-preview"]
	if usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.CacheReadTokens != 10 {
		t.Fatalf("usage = %#v", usage)
	}
	if len(messages) != 5 || messages[0].Type != MessageStatus || messages[1].Type != MessageThinking || messages[2].Type != MessageToolUse || messages[3].Type != MessageToolResult || messages[4].Type != MessageText {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[2].Tool != "run_shell_command" || messages[2].CallID != "call-1" || messages[3].Output != "/workspace" {
		t.Fatalf("tool messages = %#v", messages[2:])
	}
}

func TestQwenRejectsManagedMCPConfig(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qwen")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))
	backend, err := New("qwen", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(qwen): %v", err)
	}
	_, err = backend.Execute(context.Background(), "hello", ExecOptions{McpConfig: []byte(`{"mcpServers":{}}`)})
	if err == nil || !strings.Contains(err.Error(), "does not support Multica-managed MCP") {
		t.Fatalf("Execute managed MCP error = %v", err)
	}
}
