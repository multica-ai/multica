package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestClaudeHandleAssistantText(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	msg := claudeSDKMessage{
		Type: "assistant",
		Message: mustMarshal(t, claudeMessageContent{
			Role: "assistant",
			Content: []claudeContentBlock{
				{Type: "text", Text: "Hello world"},
			},
		}),
	}

	b.handleAssistant(msg, ch, &output, make(map[string]TokenUsage))

	if output.String() != "Hello world" {
		t.Fatalf("expected output 'Hello world', got %q", output.String())
	}
	select {
	case m := <-ch:
		if m.Type != MessageText || m.Content != "Hello world" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestClaudeHandleAssistantToolUse(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	msg := claudeSDKMessage{
		Type: "assistant",
		Message: mustMarshal(t, claudeMessageContent{
			Role: "assistant",
			Content: []claudeContentBlock{
				{
					Type:  "tool_use",
					ID:    "call-1",
					Name:  "Read",
					Input: mustMarshal(t, map[string]any{"path": "/tmp/foo"}),
				},
			},
		}),
	}

	b.handleAssistant(msg, ch, &output, make(map[string]TokenUsage))

	if output.String() != "" {
		t.Fatalf("tool_use should not add to output, got %q", output.String())
	}
	select {
	case m := <-ch:
		if m.Type != MessageToolUse || m.Tool != "Read" || m.CallID != "call-1" {
			t.Fatalf("unexpected message: %+v", m)
		}
		if m.Input["path"] != "/tmp/foo" {
			t.Fatalf("expected input path /tmp/foo, got %v", m.Input["path"])
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestClaudeHandleUserToolResult(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)

	msg := claudeSDKMessage{
		Type: "user",
		Message: mustMarshal(t, claudeMessageContent{
			Role: "user",
			Content: []claudeContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "call-1",
					Content:   mustMarshal(t, "file contents here"),
				},
			},
		}),
	}

	b.handleUser(msg, ch)

	select {
	case m := <-ch:
		if m.Type != MessageToolResult || m.CallID != "call-1" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestClaudeHandleControlRequestAutoApproves(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}

	var written bytes.Buffer

	msg := claudeSDKMessage{
		Type:      "control_request",
		RequestID: "req-42",
		Request: mustMarshal(t, claudeControlRequestPayload{
			Subtype:  "tool_use",
			ToolName: "Bash",
			Input:    mustMarshal(t, map[string]any{"command": "ls"}),
		}),
	}

	b.handleControlRequest(msg, &written)

	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(written.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["type"] != "control_response" {
		t.Fatalf("expected type control_response, got %v", resp["type"])
	}
	respInner := resp["response"].(map[string]any)
	if respInner["request_id"] != "req-42" {
		t.Fatalf("expected request_id req-42, got %v", respInner["request_id"])
	}
	innerResp := respInner["response"].(map[string]any)
	if innerResp["behavior"] != "allow" {
		t.Fatalf("expected behavior allow, got %v", innerResp["behavior"])
	}
}

func TestClaudeHandleAssistantInvalidJSON(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	msg := claudeSDKMessage{
		Type:    "assistant",
		Message: json.RawMessage(`invalid json`),
	}

	// Should not panic
	b.handleAssistant(msg, ch, &output, make(map[string]TokenUsage))

	if output.String() != "" {
		t.Fatalf("expected empty output for invalid JSON, got %q", output.String())
	}
	select {
	case m := <-ch:
		t.Fatalf("expected no message, got %+v", m)
	default:
	}
}

func TestTrySendDropsWhenFull(t *testing.T) {
	t.Parallel()

	ch := make(chan Message, 1)
	// Fill the channel
	trySend(ch, Message{Type: MessageText, Content: "first"})
	// This should not block
	trySend(ch, Message{Type: MessageText, Content: "second"})

	m := <-ch
	if m.Content != "first" {
		t.Fatalf("expected 'first', got %q", m.Content)
	}
	select {
	case m := <-ch:
		t.Fatalf("expected empty channel, got %+v", m)
	default:
	}
}

func TestBuildClaudeArgsIncludesStrictMCPConfig(t *testing.T) {
	t.Parallel()

	args := buildClaudeArgs(ExecOptions{}, slog.Default())
	expected := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
		"--permission-mode", "bypassPermissions",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestFilterCustomArgsBlocksProtocolFlags(t *testing.T) {
	t.Parallel()

	blocked := map[string]blockedArgMode{
		"--output-format":   blockedWithValue,
		"--permission-mode": blockedWithValue,
		"-p":                blockedStandalone,
	}
	logger := slog.Default()

	// Blocks flag with separate value
	result := filterCustomArgs([]string{"--output-format", "text", "--model", "o3"}, blocked, logger)
	if len(result) != 2 || result[0] != "--model" || result[1] != "o3" {
		t.Fatalf("expected [--model o3], got %v", result)
	}

	// Blocks flag=value form
	result = filterCustomArgs([]string{"--permission-mode=plan", "--verbose"}, blocked, logger)
	if len(result) != 1 || result[0] != "--verbose" {
		t.Fatalf("expected [--verbose], got %v", result)
	}

	// Blocks standalone short flags without consuming next arg
	result = filterCustomArgs([]string{"-p", "--max-turns", "10"}, blocked, logger)
	if len(result) != 2 || result[0] != "--max-turns" || result[1] != "10" {
		t.Fatalf("expected [--max-turns 10], got %v", result)
	}

	// Passes through non-blocked args
	result = filterCustomArgs([]string{"--model", "o3", "--max-turns", "50"}, blocked, logger)
	if len(result) != 4 {
		t.Fatalf("expected all 4 args to pass through, got %v", result)
	}

	// Handles nil blocked map
	result = filterCustomArgs([]string{"--anything"}, nil, logger)
	if len(result) != 1 {
		t.Fatalf("expected args to pass through with nil blocked map, got %v", result)
	}

	// Handles empty args
	result = filterCustomArgs(nil, blocked, logger)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
}

func TestBuildClaudeArgsPassesThroughCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildClaudeArgs(ExecOptions{
		CustomArgs: []string{"--max-turns", "50", "--verbose"},
	}, slog.Default())

	// Custom args should appear at the end
	found := 0
	for i, a := range args {
		if a == "--max-turns" && i+1 < len(args) && args[i+1] == "50" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("expected --max-turns 50 in args: %v", args)
	}
}

func TestBuildClaudeArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildClaudeArgs(ExecOptions{
		CustomArgs: []string{"--output-format", "text", "--model", "o3"},
	}, slog.Default())

	// --output-format text should be stripped
	for _, a := range args[len(args)-2:] {
		if a == "text" {
			// "text" should not be in the last args since --output-format was blocked
			// The actual --output-format stream-json is earlier in the list
		}
	}
	// --model o3 should pass through
	foundModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "o3" {
			foundModel = true
		}
		// Verify no duplicate --output-format with value "text"
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "text" {
			t.Fatalf("blocked --output-format text should have been filtered: %v", args)
		}
	}
	if !foundModel {
		t.Fatalf("expected --model o3 in args but it was missing: %v", args)
	}
}

func TestBuildClaudeInputEncodesUserMessage(t *testing.T) {
	t.Parallel()

	data, err := buildClaudeInput("say pong")
	if err != nil {
		t.Fatalf("buildClaudeInput: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected newline-terminated payload, got %q", data)
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["type"] != "user" {
		t.Fatalf("expected type user, got %v", payload["type"])
	}

	message, ok := payload["message"].(map[string]any)
	if !ok {
		t.Fatalf("expected message object, got %T", payload["message"])
	}
	if message["role"] != "user" {
		t.Fatalf("expected role user, got %v", message["role"])
	}

	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected one content block, got %v", message["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected content block object, got %T", content[0])
	}
	if block["type"] != "text" || block["text"] != "say pong" {
		t.Fatalf("unexpected content block: %v", block)
	}
}

func TestMergeEnvFiltersClaudeCodeVars(t *testing.T) {
	t.Parallel()

	env := mergeEnv([]string{
		"PATH=/usr/bin",
		"CLAUDECODE=1",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDECODEX=keep-me",
	}, map[string]string{"FOO": "bar"})

	for _, entry := range env {
		if entry == "CLAUDECODE=1" || entry == "CLAUDE_CODE_ENTRYPOINT=cli" {
			t.Fatalf("expected CLAUDECODE vars to be filtered, got %v", env)
		}
	}

	found := map[string]bool{}
	for _, entry := range env {
		found[entry] = true
	}

	if !found["PATH=/usr/bin"] {
		t.Fatalf("expected PATH to be preserved, got %v", env)
	}
	if !found["CLAUDECODEX=keep-me"] {
		t.Fatalf("expected unrelated env vars to be preserved, got %v", env)
	}
	if !found["FOO=bar"] {
		t.Fatalf("expected extra env var to be appended, got %v", env)
	}
}

func TestBuildEnvAppendsExtras(t *testing.T) {
	t.Parallel()

	env := buildEnv(map[string]string{"FOO": "bar", "BAZ": "qux"})
	found := 0
	for _, e := range env {
		if e == "FOO=bar" || e == "BAZ=qux" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected 2 extra env vars, found %d", found)
	}
}

func TestBuildEnvNilExtras(t *testing.T) {
	t.Parallel()

	env := buildEnv(nil)
	if len(env) == 0 {
		t.Fatal("expected at least system env vars")
	}
}

func TestBuildClaudeArgsBlocksMcpConfig(t *testing.T) {
	t.Parallel()

	// --mcp-config is hardcoded by the daemon — it must not be overridable via custom_args.
	args := buildClaudeArgs(ExecOptions{
		CustomArgs: []string{"--mcp-config", "/tmp/evil.json", "--model", "o3"},
	}, slog.Default())

	for i, a := range args {
		if a == "--mcp-config" {
			t.Fatalf("--mcp-config should be blocked from custom_args, found at index %d: %v", i, args)
		}
		if a == "/tmp/evil.json" {
			t.Fatalf("--mcp-config value should be consumed when blocking, but found it at index %d: %v", i, args)
		}
	}

	// Non-blocked args should still pass through.
	foundModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "o3" {
			foundModel = true
		}
	}
	if !foundModel {
		t.Fatalf("expected --model o3 in args after blocking --mcp-config: %v", args)
	}
}

func TestWriteMcpConfigToTemp(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"test":{"command":"echo","args":["hello"]}}}`)
	path, err := writeMcpConfigToTemp(raw)
	if err != nil {
		t.Fatalf("writeMcpConfigToTemp: %v", err)
	}

	// File should exist and contain exactly the raw JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file %s: %v", path, err)
	}
	if !bytes.Equal(data, []byte(raw)) {
		t.Fatalf("expected %s, got %s", raw, data)
	}

	// Cleanup should remove the file.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove temp file: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed, but it still exists")
	}
}

func TestResolveSessionID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		requested string
		emitted   string
		failed    bool
		want      string
	}{
		{
			name:      "no resume requested propagates emitted",
			requested: "",
			emitted:   "fresh-abc",
			failed:    false,
			want:      "fresh-abc",
		},
		{
			name:      "resume succeeded keeps matching id",
			requested: "sess-old",
			emitted:   "sess-old",
			failed:    false,
			want:      "sess-old",
		},
		{
			name:      "resume succeeded but run failed mid-turn keeps id for later retry",
			requested: "sess-old",
			emitted:   "sess-old",
			failed:    true,
			want:      "sess-old",
		},
		{
			name:      "resume did not land and run failed clears id so daemon fallback fires",
			requested: "sess-dead",
			emitted:   "fresh-new",
			failed:    true,
			want:      "",
		},
		{
			name:      "resume did not land but run succeeded keeps fresh id (defensive)",
			requested: "sess-dead",
			emitted:   "fresh-new",
			failed:    false,
			want:      "fresh-new",
		},
		{
			name:      "no emitted id leaves result empty",
			requested: "sess-old",
			emitted:   "",
			failed:    true,
			want:      "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveSessionID(tc.requested, tc.emitted, tc.failed)
			if got != tc.want {
				t.Fatalf("resolveSessionID(%q, %q, %v) = %q, want %q",
					tc.requested, tc.emitted, tc.failed, got, tc.want)
			}
		})
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
