package agent

import (
	"bytes"
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

	if b.handleUser(msg, ch) {
		t.Fatal("did not expect async launch in ordinary tool result")
	}

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
	updatedInput := innerResp["updatedInput"].(map[string]any)
	if _, ok := updatedInput["run_in_background"]; ok {
		t.Fatal("did not expect run_in_background to be injected into ordinary tool input")
	}
}

func TestClaudeHandleControlRequestForcesBackgroundToolsForeground(t *testing.T) {
	t.Parallel()

	for _, toolName := range []string{"Bash", "Agent"} {
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			b := &claudeBackend{cfg: Config{Logger: slog.Default()}}

			var written bytes.Buffer

			msg := claudeSDKMessage{
				Type:      "control_request",
				RequestID: "req-42",
				Request: mustMarshal(t, claudeControlRequestPayload{
					Subtype:  "tool_use",
					ToolName: toolName,
					Input: mustMarshal(t, map[string]any{
						"command":           "sleep 60",
						"run_in_background": true,
					}),
				}),
			}

			b.handleControlRequest(msg, &written)

			var resp map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(written.Bytes()), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			respInner := resp["response"].(map[string]any)
			innerResp := respInner["response"].(map[string]any)
			if innerResp["behavior"] != "allow" {
				t.Fatalf("expected behavior allow, got %v", innerResp["behavior"])
			}
			updatedInput := innerResp["updatedInput"].(map[string]any)
			if updatedInput["run_in_background"] != false {
				t.Fatalf("expected run_in_background=false, got %v", updatedInput["run_in_background"])
			}
			if updatedInput["command"] != "sleep 60" {
				t.Fatalf("expected original command to be preserved, got %v", updatedInput["command"])
			}
		})
	}
}

func TestClaudeHandleUserDetectsAsyncLaunchedToolResult(t *testing.T) {
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
					Content: mustMarshal(t, map[string]any{
						"status":  "async_launched",
						"message": "background task launched",
					}),
				},
			},
		}),
	}

	if !b.handleUser(msg, ch) {
		t.Fatal("expected async launch to be detected")
	}
}

func TestClaudeHandleUserIgnoresAsyncLaunchedTextOutput(t *testing.T) {
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
					Content: mustMarshal(t, map[string]any{
						"stdout": `fixture contained {"status":"async_launched"} as plain text`,
					}),
				},
			},
		}),
	}

	if b.handleUser(msg, ch) {
		t.Fatal("did not expect async launch to be detected in ordinary text output")
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
		"--disallowedTools", "AskUserQuestion",
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

func TestArgsRequestBypassPermissions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "permission mode bypass",
			args: []string{"--permission-mode", "bypassPermissions"},
			want: true,
		},
		{
			name: "dangerous skip permissions",
			args: []string{"--dangerously-skip-permissions"},
			want: true,
		},
		{
			name: "neither",
			args: []string{"--model", "sonnet"},
			want: false,
		},
		{
			name: "permission mode default",
			args: []string{"--permission-mode", "default"},
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := argsRequestBypassPermissions(tc.args); got != tc.want {
				t.Fatalf("argsRequestBypassPermissions(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
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

func TestFilterCustomArgsStripsShellQuotes(t *testing.T) {
	t.Parallel()

	logger := slog.Default()

	// Single-quoted inline value: --deny-tool='write' → --deny-tool=write
	result := filterCustomArgs([]string{"--deny-tool='write'"}, nil, logger)
	if len(result) != 1 || result[0] != "--deny-tool=write" {
		t.Fatalf("expected [--deny-tool=write], got %v", result)
	}

	// Double-quoted inline value: --deny-tool="write" → --deny-tool=write
	result = filterCustomArgs([]string{`--deny-tool="write"`}, nil, logger)
	if len(result) != 1 || result[0] != "--deny-tool=write" {
		t.Fatalf("expected [--deny-tool=write], got %v", result)
	}

	// Standalone quoted value: 'write' → write
	result = filterCustomArgs([]string{"'write'"}, nil, logger)
	if len(result) != 1 || result[0] != "write" {
		t.Fatalf("expected [write], got %v", result)
	}

	// Non-flag assignments may use quotes semantically (for example Codex
	// `-c model="o3"`), so they must survive unchanged.
	result = filterCustomArgs([]string{`model="o3"`}, nil, logger)
	if len(result) != 1 || result[0] != `model="o3"` {
		t.Fatalf("expected [model=\"o3\"], got %v", result)
	}

	// Unquoted arg passes through unchanged
	result = filterCustomArgs([]string{"--deny-tool=write"}, nil, logger)
	if len(result) != 1 || result[0] != "--deny-tool=write" {
		t.Fatalf("expected [--deny-tool=write], got %v", result)
	}

	// Mismatched quotes are not stripped
	result = filterCustomArgs([]string{"--flag='val\""}, nil, logger)
	if len(result) != 1 || result[0] != "--flag='val\"" {
		t.Fatalf("mismatched quotes should not be stripped, got %v", result)
	}

	// Blocked flag with quoted value: the blocking still fires, the unquoted
	// form passes the flag-match, and the arg is dropped.
	blocked := map[string]blockedArgMode{"--output-format": blockedWithValue}
	result = filterCustomArgs([]string{"--output-format='json'", "--verbose"}, blocked, logger)
	if len(result) != 1 || result[0] != "--verbose" {
		t.Fatalf("quoted blocked flag should still be dropped, got %v", result)
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
		"CLAUDE_CODE_EXECPATH=/opt/claude",
		"CLAUDE_CODE_SESSION_ID=abc123",
		"CLAUDE_CODE_SSE_PORT=9999",
		"CLAUDECODEX=keep-me",
		"CLAUDE_CODE_GIT_BASH_PATH=C:\\Program Files\\Git\\bin\\bash.exe",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"CLAUDE_CODE_TMPDIR=/custom/tmp",
	}, map[string]string{"FOO": "bar"})

	// Internal runtime/session markers must be stripped so the child does not
	// inherit the parent's identity or transport.
	filteredOut := []string{
		"CLAUDECODE=1",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_CODE_EXECPATH=/opt/claude",
		"CLAUDE_CODE_SESSION_ID=abc123",
		"CLAUDE_CODE_SSE_PORT=9999",
	}
	for _, entry := range env {
		for _, banned := range filteredOut {
			if entry == banned {
				t.Fatalf("expected internal Claude Code marker %q to be filtered, got %v", banned, env)
			}
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
	// User-facing CLAUDE_CODE_* config must reach the child — stripping
	// CLAUDE_CODE_GIT_BASH_PATH is what broke Claude Code on Windows (#3671).
	if !found["CLAUDE_CODE_GIT_BASH_PATH=C:\\Program Files\\Git\\bin\\bash.exe"] {
		t.Fatalf("expected CLAUDE_CODE_GIT_BASH_PATH to be preserved, got %v", env)
	}
	if !found["CLAUDE_CODE_USE_BEDROCK=1"] {
		t.Fatalf("expected CLAUDE_CODE_USE_BEDROCK to be preserved, got %v", env)
	}
	// CLAUDE_CODE_TMPDIR is a documented user-configurable temp-dir override, not
	// an internal per-session marker, so it must reach the child.
	if !found["CLAUDE_CODE_TMPDIR=/custom/tmp"] {
		t.Fatalf("expected CLAUDE_CODE_TMPDIR to be preserved, got %v", env)
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

func TestEnvHasSandbox(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  []string
		want bool
	}{
		{name: "one", env: []string{"IS_SANDBOX=1"}, want: true},
		{name: "true", env: []string{"IS_SANDBOX=true"}, want: true},
		{name: "yes", env: []string{"IS_SANDBOX=yes"}, want: true},
		{name: "on", env: []string{"IS_SANDBOX=on"}, want: true},
		{name: "zero", env: []string{"IS_SANDBOX=0"}, want: false},
		{name: "false", env: []string{"IS_SANDBOX=false"}, want: false},
		{name: "empty", env: []string{"IS_SANDBOX="}, want: false},
		{name: "absent", env: []string{"PATH=/usr/bin"}, want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := envHasSandbox(tc.env); got != tc.want {
				t.Fatalf("envHasSandbox(%v) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestClaudeRootSudoPreflight(t *testing.T) {
	t.Parallel()

	t.Run("sandbox bypass allowed", func(t *testing.T) {
		t.Parallel()

		err := claudeRootSudoPreflight(
			[]string{"--permission-mode", "bypassPermissions"},
			[]string{"IS_SANDBOX=1"},
		)
		if err != nil {
			t.Fatalf("expected sandboxed bypass to pass preflight, got %v", err)
		}
	})

	t.Run("non bypass allowed", func(t *testing.T) {
		t.Parallel()

		err := claudeRootSudoPreflight(
			[]string{"--permission-mode", "default"},
			nil,
		)
		if err != nil {
			t.Fatalf("expected non-bypass args to pass preflight, got %v", err)
		}
	})

	t.Run("root bypass without sandbox errors", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() != 0 {
			t.Skip("root-only preflight assertion")
		}

		err := claudeRootSudoPreflight(
			[]string{"--permission-mode", "bypassPermissions"},
			nil,
		)
		if err == nil {
			t.Fatal("expected root bypass without sandbox to fail preflight")
		}
		if !strings.Contains(err.Error(), "IS_SANDBOX") || !strings.Contains(err.Error(), "non-root") {
			t.Fatalf("expected actionable root guidance, got %q", err.Error())
		}
	})
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

	// Cleanup should remove the temp directory and every related sidecar file.
	cleanupMcpConfigTemp(path)
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
		stderr    string
		want      string
	}{
		{
			name:      "no resume requested propagates emitted",
			requested: "",
			emitted:   "fresh-abc",
			failed:    false,
			stderr:    "",
			want:      "fresh-abc",
		},
		{
			name:      "resume succeeded keeps matching id",
			requested: "sess-old",
			emitted:   "sess-old",
			failed:    false,
			stderr:    "",
			want:      "sess-old",
		},
		{
			name:      "resume succeeded but run failed mid-turn keeps id for later retry",
			requested: "sess-old",
			emitted:   "sess-old",
			failed:    true,
			stderr:    "",
			want:      "sess-old",
		},
		{
			name:      "resume did not land and run failed clears id so daemon fallback fires",
			requested: "sess-dead",
			emitted:   "fresh-new",
			failed:    true,
			stderr:    "",
			want:      "",
		},
		{
			name:      "resume not found stderr clears matching id so daemon fallback fires",
			requested: "sess-dead",
			emitted:   "sess-dead",
			failed:    true,
			stderr:    "No conversation found with session ID: sess-dead",
			want:      "",
		},
		{
			name:      "resume did not land but run succeeded keeps fresh id (defensive)",
			requested: "sess-dead",
			emitted:   "fresh-new",
			failed:    false,
			stderr:    "No conversation found with session ID: sess-dead",
			want:      "fresh-new",
		},
		{
			name:      "no emitted id leaves result empty",
			requested: "sess-old",
			emitted:   "",
			failed:    true,
			stderr:    "",
			want:      "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveSessionID(tc.requested, tc.emitted, tc.failed, tc.stderr)
			if got != tc.want {
				t.Fatalf("resolveSessionID(%q, %q, %v, %q) = %q, want %q",
					tc.requested, tc.emitted, tc.failed, tc.stderr, got, tc.want)
			}
		})
	}
}

func TestClaudeExecuteSurfacesStderrWhenChildExitsEarly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Fake claude binary: reads the initial stdin frame so writeClaudeInput
	// succeeds, writes a canonical V8-abort line to stderr, then exits
	// non-zero before emitting any stream-json to stdout. This is the exact
	// failure mode that motivated PR #1674 — without sampling stderrBuf.Tail()
	// after cmd.Wait() returns, Result.Error would be a useless
	// "exit status 3".
	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		"echo \"FATAL ERROR: V8 abort: assertion failed\" >&2\n" +
		"exit 3\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Drain message stream so the lifecycle goroutine can progress.
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
		if !strings.Contains(result.Error, "claude exited with error") {
			t.Fatalf("expected error to mention exit, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "V8 abort: assertion failed") {
			t.Fatalf("expected error to include stderr hint, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "claude stderr:") {
			t.Fatalf("expected stderr label in error, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestClaudeExecuteRecordsResultModelUsage(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"session_id\":\"sess-result-usage\"}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"sess-result-usage\",\"result\":\"done\",\"modelUsage\":{\"zhipu/coding-plan\":{\"inputTokens\":123,\"outputTokens\":45,\"cacheReadInputTokens\":7,\"cacheCreationInputTokens\":11,\"costUSD\":0.01}}}'\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
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
		usage, ok := result.Usage["zhipu/coding-plan"]
		if !ok {
			t.Fatalf("expected usage for zhipu/coding-plan, got %#v", result.Usage)
		}
		if usage.InputTokens != 123 || usage.OutputTokens != 45 || usage.CacheReadTokens != 7 || usage.CacheWriteTokens != 11 {
			t.Fatalf("unexpected usage: %+v", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
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

func TestBuildClaudeArgsExtraArgsBeforeCustomArgsAndFiltersBoth(t *testing.T) {
	args := buildClaudeArgs(ExecOptions{
		ExtraArgs:  []string{"--output-format", "text", "--max-budget-usd", "1.00"},
		CustomArgs: []string{"--max-budget-usd", "2.00", "--permission-mode", "plan"},
	}, slog.Default())
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--output-format text") || strings.Contains(joined, "--permission-mode plan") {
		t.Fatalf("blocked args should be filtered from both layers: %v", args)
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
