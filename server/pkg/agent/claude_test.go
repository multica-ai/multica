package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
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

func TestClaudeExecuteSurfacesStderrWhenChildExitsEarly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Fake claude binary: drains stdin so writeClaudeInput succeeds, writes a
	// canonical V8-abort line to stderr, then exits non-zero before emitting
	// any stream-json to stdout. This is the exact failure mode that motivated
	// PR #1674 — without sampling stderrBuf.Tail() after cmd.Wait() returns,
	// Result.Error would be a useless "exit status 3".
	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"FATAL ERROR: V8 abort: assertion failed\" >&2\n" +
		"exit 3\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
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
		"cat >/dev/null\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"session_id\":\"sess-result-usage\"}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"sess-result-usage\",\"result\":\"done\",\"modelUsage\":{\"zhipu/coding-plan\":{\"inputTokens\":123,\"outputTokens\":45,\"cacheReadInputTokens\":7,\"cacheCreationInputTokens\":11,\"costUSD\":0.01}}}'\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
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

func TestClaudeExecuteIsolatesHostSkillsWhenIgnoreOptedIn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Fake claude binary that prints its CLAUDE_CONFIG_DIR to stdout so we
	// can confirm the runtime redirected the CLI off `~/.claude/` when the
	// agent explicitly opted into "ignore" mode (the platform default is
	// "merge", which preserves the host's CLAUDE_CONFIG_DIR).
	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"system\\\",\\\"session_id\\\":\\\"sess\\\"}\"\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"result\\\",\\\"subtype\\\":\\\"success\\\",\\\"is_error\\\":false,\\\"session_id\\\":\\\"sess\\\",\\\"result\\\":\\\"$CLAUDE_CONFIG_DIR\\\"}\"\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cwd := t.TempDir()
	// Explicit SkillsLocal == "ignore" → backend points CLAUDE_CONFIG_DIR at
	// a per-task scratch dir under cwd.
	session, err := backend.Execute(ctx, "ignored", ExecOptions{
		Cwd:         cwd,
		Timeout:     5 * time.Second,
		SkillsLocal: "ignore",
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
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (err=%q)", result.Status, result.Error)
		}
		// The CLI saw a CLAUDE_CONFIG_DIR pointed at a multica-managed scratch
		// dir under our task cwd, not the host user's ~/.claude.
		got := strings.TrimSpace(result.Output)
		if got == "" {
			t.Fatalf("expected CLAUDE_CONFIG_DIR to be non-empty in ignore mode")
		}
		if !strings.Contains(got, "multica-claude-config-") {
			t.Fatalf("expected isolated scratch dir, got %q", got)
		}
		if !strings.HasPrefix(got, cwd) {
			t.Fatalf("expected isolated dir under %q, got %q", cwd, got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestClaudeExecuteDefaultModeKeepsHostConfigDir(t *testing.T) {
	// NOT parallel — t.Setenv mutates global env.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Default ExecOptions (no SkillsLocal) must preserve the host's
	// CLAUDE_CONFIG_DIR — the platform default is "merge", which inherits
	// the host's user-global skill directory (Bohan's product decision on
	// MUL-2603: keep MUL-2603 hardening as an explicit opt-in to avoid
	// regressing personal workflows that rely on locally installed skills).
	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"system\\\",\\\"session_id\\\":\\\"sess\\\"}\"\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"result\\\",\\\"subtype\\\":\\\"success\\\",\\\"is_error\\\":false,\\\"session_id\\\":\\\"sess\\\",\\\"result\\\":\\\"${CLAUDE_CONFIG_DIR:-unset}\\\"}\"\n"
	writeTestExecutable(t, fakePath, []byte(script))

	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/test-host-claude")

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Default ExecOptions → SkillsLocal == "" → backend treats as merge
	// (inherit-from-machine) and the host CLAUDE_CONFIG_DIR is preserved.
	session, err := backend.Execute(ctx, "ignored", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 5 * time.Second,
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
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (err=%q)", result.Status, result.Error)
		}
		got := strings.TrimSpace(result.Output)
		if got != "/tmp/test-host-claude" {
			t.Fatalf("expected host CLAUDE_CONFIG_DIR preserved in default mode, got %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestClaudeExecuteMergeModeKeepsHostConfigDir(t *testing.T) {
	// NOT parallel — t.Setenv mutates global env which conflicts with
	// concurrent tests reading CLAUDE_CONFIG_DIR or running under t.Parallel.
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"system\\\",\\\"session_id\\\":\\\"sess\\\"}\"\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"result\\\",\\\"subtype\\\":\\\"success\\\",\\\"is_error\\\":false,\\\"session_id\\\":\\\"sess\\\",\\\"result\\\":\\\"${CLAUDE_CONFIG_DIR:-unset}\\\"}\"\n"
	writeTestExecutable(t, fakePath, []byte(script))

	// Set a host-style CLAUDE_CONFIG_DIR so we can assert merge mode
	// preserves it. The backend strips this in ignore mode but must leave
	// it alone when the operator explicitly opted into merging.
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/test-host-claude")

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "ignored", ExecOptions{
		Cwd:         t.TempDir(),
		Timeout:     5 * time.Second,
		SkillsLocal: "merge",
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
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (err=%q)", result.Status, result.Error)
		}
		got := strings.TrimSpace(result.Output)
		if got != "/tmp/test-host-claude" {
			t.Fatalf("expected host CLAUDE_CONFIG_DIR preserved in merge mode, got %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestBuildClaudeEnvAppendsIsolatedConfigDir(t *testing.T) {
	t.Parallel()

	env := buildClaudeEnvWith(nil, "/tmp/isolated-claude-config", slog.Default(), noopOAuthTokenReader)

	var last string
	hits := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			hits++
			last = entry
		}
	}
	if hits != 1 {
		t.Fatalf("expected exactly one CLAUDE_CONFIG_DIR entry, got %d (%v)", hits, env)
	}
	if last != "CLAUDE_CONFIG_DIR=/tmp/isolated-claude-config" {
		t.Fatalf("expected isolated CLAUDE_CONFIG_DIR override, got %q", last)
	}
}

func TestBuildClaudeEnvSkipsOverrideWhenEmpty(t *testing.T) {
	t.Parallel()

	// Asking for "merge" mode passes "" through. We should not add a
	// CLAUDE_CONFIG_DIR=… entry; the parent's value (if any) wins.
	env := buildClaudeEnvWith(map[string]string{"FOO": "bar"}, "", slog.Default(), noopOAuthTokenReader)
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			// The parent env may legitimately have one set on a developer
			// machine — only assert we did not *add* one. Find the unfiltered
			// merge case to compare.
			return
		}
	}
}

func TestBuildClaudeEnvOverridesPreviousValue(t *testing.T) {
	t.Parallel()

	// Even if custom_env supplies a CLAUDE_CONFIG_DIR, the isolated dir
	// must take precedence: a stale custom_env entry must never be able
	// to point the child back at `~/.claude/`.
	env := buildClaudeEnvWith(
		map[string]string{"CLAUDE_CONFIG_DIR": "/etc/hostile"},
		"/tmp/safe",
		slog.Default(),
		noopOAuthTokenReader,
	)

	hits := 0
	for _, entry := range env {
		if entry == "CLAUDE_CONFIG_DIR=/etc/hostile" {
			t.Fatalf("hostile custom_env CLAUDE_CONFIG_DIR was not stripped: %v", env)
		}
		if entry == "CLAUDE_CONFIG_DIR=/tmp/safe" {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("expected isolated dir to be present exactly once, got %d (%v)", hits, env)
	}
}

// noopOAuthTokenReader stands in for the production keychain reader in
// tests that do not care about the auth-passthrough branch. Returning
// ("", nil) takes the "no token available" path: buildClaudeEnvWith
// neither appends CLAUDE_CODE_OAUTH_TOKEN nor logs a warning.
func noopOAuthTokenReader() (string, error) { return "", nil }

// countEnvEntries returns the number of `key=…` entries in env. Used by
// the auth-passthrough tests below to assert "exactly one" instead of
// "at least one" — a duplicate CLAUDE_CODE_OAUTH_TOKEN entry would let
// later behaviour depend on which one the kernel hands to the child.
func countEnvEntries(env []string, key string) int {
	prefix := key + "="
	n := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			n++
		}
	}
	return n
}

// envValue returns the first value bound to key in env, or "" if absent.
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// TestBuildClaudeEnvIsolatedInjectsKeychainOAuthToken locks in the
// MUL-2603 follow-up: when isolation strips CLAUDE_CONFIG_DIR, the host
// OAuth token must reach the child through CLAUDE_CODE_OAUTH_TOKEN.
// Without this passthrough Claude Code's per-config-dir keychain suffix
// scheme strands the isolated child at "Not logged in · Please run
// /login" even though the host's OAuth credential is sitting in the
// default keychain entry.
func TestBuildClaudeEnvIsolatedInjectsKeychainOAuthToken(t *testing.T) {
	t.Parallel()

	reader := func() (string, error) { return "sk-ant-oat-test", nil }
	env := buildClaudeEnvWith(nil, "/tmp/isolated", slog.Default(), reader)

	if got := countEnvEntries(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != 1 {
		t.Fatalf("expected exactly one CLAUDE_CODE_OAUTH_TOKEN entry, got %d (%v)", got, env)
	}
	if got := envValue(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != "sk-ant-oat-test" {
		t.Fatalf("expected keychain token to be surfaced verbatim, got %q", got)
	}
}

// TestBuildClaudeEnvMergeModeDoesNotInjectOAuthToken guards the
// merge-mode side: CLAUDE_CODE_OAUTH_TOKEN must never appear when the
// caller opted out of isolation, because in that path the child reads
// the host keychain itself and a stale env-var would shadow a freshly
// refreshed token.
func TestBuildClaudeEnvMergeModeDoesNotInjectOAuthToken(t *testing.T) {
	t.Parallel()

	reader := func() (string, error) {
		t.Fatal("readOAuthToken must not be invoked in merge mode")
		return "", nil
	}
	env := buildClaudeEnvWith(nil, "", slog.Default(), reader)

	if got := countEnvEntries(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != 0 {
		t.Fatalf("expected no CLAUDE_CODE_OAUTH_TOKEN in merge mode, got %d (%v)", got, env)
	}
}

// TestBuildClaudeEnvIsolatedRespectsCustomOAuthToken documents that a
// CLAUDE_CODE_OAUTH_TOKEN pinned via agent custom_env wins over the
// keychain reader. Operators sometimes pin a long-lived setup-token
// (`claude setup-token`) for shared agents that must run after the
// interactive user logs out; calling the keychain in that case would
// either prompt or silently replace the intended token.
func TestBuildClaudeEnvIsolatedRespectsCustomOAuthToken(t *testing.T) {
	t.Parallel()

	reader := func() (string, error) {
		t.Fatal("readOAuthToken must not be invoked when custom_env pinned a token")
		return "", nil
	}
	env := buildClaudeEnvWith(
		map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "sk-ant-oat-pinned"},
		"/tmp/isolated",
		slog.Default(),
		reader,
	)

	if got := envValue(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != "sk-ant-oat-pinned" {
		t.Fatalf("expected pinned custom_env token, got %q", got)
	}
}

// TestBuildClaudeEnvIsolatedSkipsKeychainWhenAnthropicKeyPresent
// documents that a user who explicitly chose API-key auth via
// ANTHROPIC_API_KEY does NOT get an OAuth token shoved on top — the two
// auth modes are mutually exclusive on the CLI side, and silently
// adding the keychain token would surface a confusing
// "ambiguous auth" path on every isolated run.
func TestBuildClaudeEnvIsolatedSkipsKeychainWhenAnthropicKeyPresent(t *testing.T) {
	t.Parallel()

	reader := func() (string, error) {
		t.Fatal("readOAuthToken must not be invoked when ANTHROPIC_API_KEY is set")
		return "", nil
	}
	env := buildClaudeEnvWith(
		map[string]string{"ANTHROPIC_API_KEY": "sk-ant-api-test"},
		"/tmp/isolated",
		slog.Default(),
		reader,
	)

	if got := countEnvEntries(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != 0 {
		t.Fatalf("expected no CLAUDE_CODE_OAUTH_TOKEN alongside ANTHROPIC_API_KEY, got %d (%v)", got, env)
	}
}

// TestBuildClaudeEnvIsolatedReaderErrorIsSoftFailure documents that a
// keychain read error (e.g. /usr/bin/security missing) does NOT abort
// the build — the child still gets a coherent env minus the OAuth
// token, and the warning is surfaced through the injected logger so
// operators can correlate "Not logged in" with the underlying cause.
func TestBuildClaudeEnvIsolatedReaderErrorIsSoftFailure(t *testing.T) {
	t.Parallel()

	reader := func() (string, error) {
		return "", errors.New("security CLI unavailable")
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	env := buildClaudeEnvWith(nil, "/tmp/isolated", logger, reader)

	if got := countEnvEntries(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != 0 {
		t.Fatalf("expected no CLAUDE_CODE_OAUTH_TOKEN when reader errors, got %d (%v)", got, env)
	}
	if got := countEnvEntries(env, "CLAUDE_CONFIG_DIR"); got != 1 {
		t.Fatalf("expected isolation env to still be intact, got %d CLAUDE_CONFIG_DIR entries", got)
	}
	if !strings.Contains(logBuf.String(), "read host oauth token failed") {
		t.Fatalf("expected reader error to be logged, got %q", logBuf.String())
	}
}

// TestBuildClaudeEnvIsolatedReaderNilTokenStaysQuiet documents the
// common "host is not logged in" path: the reader returns ("", nil) —
// no token, no error — and the env builder must neither append a
// CLAUDE_CODE_OAUTH_TOKEN nor log a warning. We assert the silence
// explicitly because a noisy warning every isolated run on every
// API-key-only / unauthenticated host would drown out the real ones.
func TestBuildClaudeEnvIsolatedReaderNilTokenStaysQuiet(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	env := buildClaudeEnvWith(nil, "/tmp/isolated", logger, noopOAuthTokenReader)

	if got := countEnvEntries(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != 0 {
		t.Fatalf("expected no CLAUDE_CODE_OAUTH_TOKEN when reader returns empty, got %d (%v)", got, env)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("expected silence on (\"\", nil) reader return, got log output %q", logBuf.String())
	}
}

// TestBuildClaudeEnvIsolatedTreatsEmptyAnthropicAPIKeyAsUnpinned guards the
// MUL-2603 review fix: a parent-inherited `ANTHROPIC_API_KEY=` (set but
// empty — common on login-shell setups that conditionally export auth)
// must not pose as an "operator pinned API key" and disable the keychain
// reader. Without this gate the isolated child gets neither a usable env
// var nor a keychain token, and strands at "Not logged in" while
// pretending the user opted into API-key auth.
//
// CLAUDE_CODE_OAUTH_TOKEN does not need the same os.Environ test: mergeEnv
// strips every `CLAUDE_CODE_*` key from the parent before assembling the
// child env (isFilteredChildEnvKey), so a parent-set value cannot reach
// the gate at all. The empty-value gate still applies on the custom_env
// path — see TestBuildClaudeEnvIsolatedEmptyOAuthTokenInCustomEnvAsUnpinned
// below.
func TestBuildClaudeEnvIsolatedTreatsEmptyAnthropicAPIKeyAsUnpinned(t *testing.T) {
	// NOT parallel — t.Setenv mutates global env.
	t.Setenv("ANTHROPIC_API_KEY", "")

	reader := func() (string, error) { return "sk-ant-oat-keychain", nil }
	env := buildClaudeEnvWith(nil, "/tmp/isolated", slog.Default(), reader)

	if got := envValue(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != "sk-ant-oat-keychain" {
		t.Fatalf("expected keychain token to be injected when ANTHROPIC_API_KEY is empty, got %q", got)
	}
}

// TestBuildClaudeEnvIsolatedHonorsNonEmptyAnthropicAPIKey is the positive
// counterpart: a non-empty ANTHROPIC_API_KEY in the parent env is a real
// auth pin and must keep the keychain reader off, otherwise an isolated
// run on a hostile/API-key host would silently shove an OAuth token in
// on top.
func TestBuildClaudeEnvIsolatedHonorsNonEmptyAnthropicAPIKey(t *testing.T) {
	// NOT parallel — t.Setenv mutates global env.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api-real")

	reader := func() (string, error) {
		t.Fatal("readOAuthToken must not be invoked when non-empty ANTHROPIC_API_KEY is set")
		return "", nil
	}
	env := buildClaudeEnvWith(nil, "/tmp/isolated", slog.Default(), reader)

	if got := countEnvEntries(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != 0 {
		t.Fatalf("expected no CLAUDE_CODE_OAUTH_TOKEN with non-empty ANTHROPIC_API_KEY, got %d (%v)", got, env)
	}
}

// TestBuildClaudeEnvIsolatedEmptyOAuthTokenInCustomEnvAsUnpinned is the
// custom_env-side mirror of the empty-value gate: an explicit
// CLAUDE_CODE_OAUTH_TOKEN="" passed through custom_env (e.g. an agent
// config field that the operator forgot to remove) must not block the
// keychain reader. Same justification as the ANTHROPIC_API_KEY case —
// silent strand at "Not logged in" while disguised as a pinned token.
func TestBuildClaudeEnvIsolatedEmptyOAuthTokenInCustomEnvAsUnpinned(t *testing.T) {
	t.Parallel()

	reader := func() (string, error) { return "sk-ant-oat-keychain", nil }
	env := buildClaudeEnvWith(
		map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": ""},
		"/tmp/isolated",
		slog.Default(),
		reader,
	)

	// The keychain token must reach the child even though custom_env had
	// the key present with an empty value.
	if got := envValue(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != "sk-ant-oat-keychain" {
		t.Fatalf("expected keychain token when custom_env CLAUDE_CODE_OAUTH_TOKEN is empty, got %q", got)
	}
}

// TestClaudeCLIScrubsOAuthTokenFromBashSubprocess is the safety-boundary
// regression test backing the security comment in buildClaudeEnvWith:
// surfacing the host OAuth token through CLAUDE_CODE_OAUTH_TOKEN is only
// safe because Claude Code itself strips that variable from the env it
// hands to Bash / hook subprocesses, so a model-driven `printenv` cannot
// echo the secret into the agent transcript. The MUL-2603 review (Elon)
// flagged this as the blocking question for #3267, and the empirical
// reproduction below is what the team accepted as proof.
//
// Skipped by default because it requires:
//   - macOS (the keychain-suffix path the passthrough exists for)
//   - the real `claude` CLI in PATH
//   - a working ANTHROPIC_API_KEY (the subprocess auth that lets the model
//     actually invoke the Bash tool — we cannot exercise the scrub
//     boundary without the model running)
//   - MULTICA_E2E_CLAUDE_OAUTH_SCRUB=1 (opt-in so CI never gets billed
//     for the model call)
//
// Run it locally on a macOS box where you are logged into both
// `claude /login` and have an API key handy:
//
//	MULTICA_E2E_CLAUDE_OAUTH_SCRUB=1 go test ./pkg/agent/ \
//	    -run TestClaudeCLIScrubsOAuthTokenFromBashSubprocess -v
//
// A failure here means upstream Claude Code stopped scrubbing
// CLAUDE_CODE_OAUTH_TOKEN before spawning Bash. In that case the
// passthrough in buildClaudeEnvWith must move off env vars (e.g. write a
// short-lived `apiKeyHelper` script that the CLI invokes synchronously)
// before MUL-2603 can ship.
func TestClaudeCLIScrubsOAuthTokenFromBashSubprocess(t *testing.T) {
	if os.Getenv("MULTICA_E2E_CLAUDE_OAUTH_SCRUB") != "1" {
		t.Skip("set MULTICA_E2E_CLAUDE_OAUTH_SCRUB=1 to run the live claude CLI env-scrub check")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("env scrub test is only meaningful for the macOS keychain-suffix path")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("set ANTHROPIC_API_KEY so the model can be invoked to drive the Bash tool")
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Skipf("claude CLI not in PATH: %v", err)
	}

	// Distinctive canary the model has no reason to produce on its own.
	// Long + structured so a substring match cannot false-positive on
	// "sk-ant-…" prefixes that legitimately appear in CLI help text.
	const canary = "sk-ant-oat-leak-canary-CLAUDE_CODE_OAUTH_TOKEN-MUL2603-PROBE"

	// Use the *user's* shell-resolved env minus what we explicitly set so
	// the CLI sees the canary token and the real API key. We do not use
	// t.Setenv because Execute spawns a child of the test binary, not of
	// the test goroutine; explicit env list keeps the boundary obvious.
	env := append(os.Environ(), "CLAUDE_CODE_OAUTH_TOKEN="+canary)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudePath,
		"--print",
		"--output-format", "text",
		"--allow-dangerously-skip-permissions",
		"--allowedTools", "Bash",
	)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(
		"Please run this exact bash command and paste the raw output verbatim: " +
			"`printenv CLAUDE_CODE_OAUTH_TOKEN || echo SCRUB-OK`. " +
			"Do not paraphrase the output, do not redact it.",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("claude --print failed: %v\noutput:\n%s", err, out)
	}
	body := string(out)
	if strings.Contains(body, canary) {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN leaked to Bash tool subprocess — Claude Code stopped scrubbing it.\nProof: canary %q present in transcript:\n%s",
			canary, body)
	}
}

func TestNewIsolatedClaudeConfigDirCreatesAndCleansUp(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	dir, cleanup, err := newIsolatedClaudeConfigDir(parent, "", slog.Default())
	if err != nil {
		t.Fatalf("newIsolatedClaudeConfigDir: %v", err)
	}
	defer cleanup()

	if dir == "" {
		t.Fatal("expected non-empty dir")
	}
	if filepath.Dir(dir) != parent {
		t.Fatalf("expected dir under %q, got %q", parent, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected created dir to exist, stat err=%v", err)
	}
	// The dir may contain symlinks mirrored from the host's ~/.claude/ (auth
	// token, settings, etc. — see mirrorHostClaudeExceptSkills). What it must
	// NOT contain is a `skills/` entry; that is the entire point of the
	// isolation. Mirroring behaviour is exercised in dedicated tests below.
	if _, err := os.Lstat(filepath.Join(dir, "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected no skills/ entry in isolated dir, stat err=%v", err)
	}

	// Cleanup is idempotent (Execute may double-defer in error paths).
	cleanup()
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir removed, stat err=%v", err)
	}
}

// TestMirrorHostClaudeExceptSkills_PassesAuthAndSkipsSkills locks in the
// invariant Elon flagged in review: the isolation must not also isolate the
// Claude login token. Mirror reaches every non-skills entry — including
// `.credentials.json`, the Linux/Windows store for the OAuth token — so a
// host that has only "run `claude` to log in" (no ANTHROPIC_API_KEY) still
// authenticates inside the isolated config dir.
func TestMirrorHostClaudeExceptSkills_PassesAuthAndSkipsSkills(t *testing.T) {
	t.Parallel()

	host := t.TempDir()
	// Populate a realistic-ish ~/.claude/ layout.
	for _, sub := range []string{"skills", "agents", "commands", "plugins"} {
		if err := os.MkdirAll(filepath.Join(host, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	// A broken skill in `skills/`: if the mirror passes it through, the
	// regression Elon's review highlighted is back.
	if err := os.WriteFile(filepath.Join(host, "skills", "broken.md"), []byte("frontmatter-corrupt"), 0o644); err != nil {
		t.Fatalf("write broken skill: %v", err)
	}
	// The OAuth credential file is the asset the reviewer specifically flagged.
	if err := os.WriteFile(filepath.Join(host, ".credentials.json"), []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(host, "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	dest := t.TempDir()
	if err := mirrorHostClaudeExceptSkills(host, dest); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	// skills/ must be absent — claude would otherwise discover the broken
	// host skill through the symlink and crash before reading stdin (#3052).
	if _, err := os.Lstat(filepath.Join(dest, "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected skills/ to be skipped, got stat err=%v", err)
	}

	// Everything else must reach the isolated dir with the same contents as
	// the host source. We resolve through the mirrored entry to confirm the
	// CLI sees real bytes whether the implementation used a symlink (Unix),
	// a junction (Windows), a hardlink (Windows no Developer Mode, same
	// volume), or a content copy (last-resort fallback). os.Stat follows
	// every variant, so the assertion is platform-agnostic.
	for _, expected := range []string{".credentials.json", "settings.json", "agents", "commands", "plugins"} {
		dst := filepath.Join(dest, expected)
		if _, err := os.Stat(dst); err != nil {
			t.Fatalf("expected %s mirrored and reachable, stat err=%v", expected, err)
		}
	}

	// The credential file should round-trip — claude reading
	// $CLAUDE_CONFIG_DIR/.credentials.json must see the live host token.
	got, err := os.ReadFile(filepath.Join(dest, ".credentials.json"))
	if err != nil {
		t.Fatalf("read mirrored credentials: %v", err)
	}
	if string(got) != `{"token":"abc"}` {
		t.Fatalf("mirrored credentials content drifted, got %q", got)
	}
}

// TestMirrorHostClaudeExceptSkills_MissingHostDirIsNoop documents that a host
// with no `~/.claude/` (env-var-auth-only setups) is a supported state, not
// an error. The isolated dir simply stays empty.
func TestMirrorHostClaudeExceptSkills_MissingHostDirIsNoop(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	err := mirrorHostClaudeExceptSkills(filepath.Join(t.TempDir(), "nope"), dest)
	if err == nil {
		t.Fatal("expected a read error for missing host dir")
	}
	// dest must remain untouched (no skills/, no other entries).
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatalf("expected empty dest on missing host, got %d entries", len(entries))
	}
}

func TestNewIsolatedClaudeConfigDirFallsBackToTemp(t *testing.T) {
	t.Parallel()

	// Empty cwd → falls back to OS temp dir without erroring.
	dir, cleanup, err := newIsolatedClaudeConfigDir("", "", slog.Default())
	if err != nil {
		t.Fatalf("expected fallback to OS temp, got err=%v", err)
	}
	defer cleanup()

	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("expected fallback under %q, got %q", os.TempDir(), dir)
	}
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

// TestResolveHostClaudeConfigDir locks in the precedence Elon's review asked
// for: agent custom_env wins over a daemon-host CLAUDE_CONFIG_DIR, which
// wins over the documented default at `~/.claude`. Without this, switching
// default mode to "ignore" would mirror the wrong source dir and overwrite
// a valid CLAUDE_CONFIG_DIR pointing at e.g. a managed shared profile.
func TestResolveHostClaudeConfigDir(t *testing.T) {
	// NOT parallel — t.Setenv mutates global env which conflicts with
	// concurrent tests reading CLAUDE_CONFIG_DIR.

	// 1. Agent custom_env wins over parent env.
	t.Setenv("CLAUDE_CONFIG_DIR", "/from/parent/env")
	got := resolveHostClaudeConfigDir(map[string]string{"CLAUDE_CONFIG_DIR": "/from/custom/env"})
	if got != "/from/custom/env" {
		t.Fatalf("agent custom_env should win, got %q", got)
	}

	// 2. Empty custom_env falls back to parent env.
	got = resolveHostClaudeConfigDir(map[string]string{"CLAUDE_CONFIG_DIR": ""})
	if got != "/from/parent/env" {
		t.Fatalf("parent env should win when custom_env is empty, got %q", got)
	}
	got = resolveHostClaudeConfigDir(nil)
	if got != "/from/parent/env" {
		t.Fatalf("parent env should win when custom_env is nil, got %q", got)
	}

	// 3. With neither set, falls back to `~/.claude`.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	got = resolveHostClaudeConfigDir(nil)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no UserHomeDir on this host: %v", err)
	}
	want := filepath.Join(home, ".claude")
	if got != want {
		t.Fatalf("default should be %q, got %q", want, got)
	}
}

// TestNewIsolatedClaudeConfigDirMirrorsCustomHostDir confirms the scratch
// dir reflects the effective CLAUDE_CONFIG_DIR source, not unconditionally
// `~/.claude/`. Previously the mirror was hardcoded to UserHomeDir, so an
// operator who pinned CLAUDE_CONFIG_DIR at a managed install would get the
// wrong credentials in the scratch dir.
func TestNewIsolatedClaudeConfigDirMirrorsCustomHostDir(t *testing.T) {
	t.Parallel()

	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, ".credentials.json"), []byte(`{"token":"from-custom-host"}`), 0o600); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	dir, cleanup, err := newIsolatedClaudeConfigDir(t.TempDir(), host, slog.Default())
	if err != nil {
		t.Fatalf("newIsolatedClaudeConfigDir: %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		t.Fatalf("read mirrored credentials from custom host: %v", err)
	}
	if string(got) != `{"token":"from-custom-host"}` {
		t.Fatalf("mirror sourced from wrong dir: got %q", got)
	}
}

// TestNewIsolatedClaudeConfigDirEmptyHostIsNoop documents the env-var-auth
// case: with no host config dir (host has no `~/.claude/` and no
// CLAUDE_CONFIG_DIR set anywhere), the scratch dir is created but empty and
// nothing is mirrored. The CLI runs with `ANTHROPIC_API_KEY` only.
func TestNewIsolatedClaudeConfigDirEmptyHostIsNoop(t *testing.T) {
	t.Parallel()

	dir, cleanup, err := newIsolatedClaudeConfigDir(t.TempDir(), "", slog.Default())
	if err != nil {
		t.Fatalf("newIsolatedClaudeConfigDir: %v", err)
	}
	defer cleanup()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read scratch dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty scratch dir with no host source, got %d entries", len(entries))
	}
}

// TestMirrorHostClaudeExceptSkillsWith_FallbackWhenSymlinkFails locks in the
// Windows-no-Developer-Mode behaviour Elon's review asked for: when symlink
// raises a permission error, the mirror still places the entry in the
// scratch dir via a fallback (junction for dirs, hardlink/copy for files).
// Tested via the lower-level seam so the assertion runs on Linux/macOS CI;
// the production createDirLink / createFileLink wrappers encode the same
// "try symlink first, then fall back" chain in their platform builds.
func TestMirrorHostClaudeExceptSkillsWith_FallbackWhenSymlinkFails(t *testing.T) {
	t.Parallel()

	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, ".credentials.json"), []byte(`{"token":"fallback"}`), 0o600); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(host, "agents"), 0o755); err != nil {
		t.Fatalf("seed agents dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(host, "agents", "global.md"), []byte("global agent"), 0o600); err != nil {
		t.Fatalf("seed agent file: %v", err)
	}
	// `skills/` must still be skipped even when the linker reports an
	// error — the broken-skill GitHub #3052 regression must not slip back.
	if err := os.MkdirAll(filepath.Join(host, "skills"), 0o755); err != nil {
		t.Fatalf("seed skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(host, "skills", "broken.md"), []byte("frontmatter-corrupt"), 0o600); err != nil {
		t.Fatalf("seed broken skill: %v", err)
	}

	dest := t.TempDir()

	// Simulated "Windows without Developer Mode": symlink always returns
	// EPERM. The fallback path must still land the entry in dest.
	failedSymlinkAttempts := 0
	fakeSymlinkErr := errors.New("simulated EPERM: symlink not permitted")
	dirLink := func(src, dst string) error {
		if err := os.Symlink(src, dst); err == nil {
			// Forcing failure: if a symlink would have worked, pretend it
			// didn't and engage the junction equivalent. We mimic the
			// junction by using os.MkdirAll (a junction behaves like a
			// directory entry from userspace) and copying the immediate
			// child files into it. For the test we only need the entry to
			// exist and be reachable; we do not need real recursive
			// equivalence.
			_ = os.Remove(dst)
		}
		failedSymlinkAttempts++
		_ = fakeSymlinkErr // referenced so the simulated-EPERM error is documented in the test body
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		// Copy any direct children so the destination is non-empty and
		// the test can read through it.
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				continue
			}
			_ = os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600)
		}
		return nil
	}
	fileLink := func(src, dst string) error {
		if err := os.Symlink(src, dst); err == nil {
			_ = os.Remove(dst)
		}
		failedSymlinkAttempts++
		// Hardlink fallback first (this is what createFileLink does on
		// Windows when symlink is denied but the source/dest share a
		// volume). If hardlink also fails (e.g. cross-volume), fall back
		// to a content copy.
		if err := os.Link(src, dst); err == nil {
			return nil
		}
		return copyFile(src, dst)
	}

	if err := mirrorHostClaudeExceptSkillsWith(host, dest, dirLink, fileLink); err != nil {
		t.Fatalf("mirror with failing symlink: %v", err)
	}

	if failedSymlinkAttempts == 0 {
		t.Fatalf("expected fallback path to engage at least once")
	}

	// `.credentials.json` must round-trip through whatever fallback the
	// file linker used (hardlink or copy). This is the assertion Elon's
	// review pinned to "no `.credentials.json` ⇒ default ignore breaks
	// Claude Code auth on Windows".
	got, err := os.ReadFile(filepath.Join(dest, ".credentials.json"))
	if err != nil {
		t.Fatalf("read mirrored credentials after fallback: %v", err)
	}
	if string(got) != `{"token":"fallback"}` {
		t.Fatalf("mirrored credentials drifted after fallback, got %q", got)
	}

	// Sub-directories must also reach the destination via the dir
	// fallback (junction equivalent). We test reachability + child
	// content rather than the underlying file kind, because junctions,
	// symlinks, and the test's copy-based stand-in all present the same
	// userspace view.
	if _, err := os.Stat(filepath.Join(dest, "agents")); err != nil {
		t.Fatalf("agents/ not mirrored: %v", err)
	}
	gotChild, err := os.ReadFile(filepath.Join(dest, "agents", "global.md"))
	if err != nil {
		t.Fatalf("read agent child file: %v", err)
	}
	if string(gotChild) != "global agent" {
		t.Fatalf("agent child content drifted, got %q", gotChild)
	}

	// `skills/` must be absent regardless of fallback engagement.
	if _, err := os.Lstat(filepath.Join(dest, "skills")); !os.IsNotExist(err) {
		t.Fatalf("skills/ leaked into scratch dir on fallback path, stat err=%v", err)
	}
}

// TestMirrorHostClaudeExceptSkillsWith_PropagatesFirstLinkError makes sure
// callers see a per-entry link failure when even the fallback fails — the
// scratch-dir caller logs the error so operators chasing auth issues on
// Windows can correlate the missing mirror with their permission setup.
func TestMirrorHostClaudeExceptSkillsWith_PropagatesFirstLinkError(t *testing.T) {
	t.Parallel()

	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, ".credentials.json"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	hardFail := errors.New("link refused after fallback")
	fail := func(src, dst string) error { return hardFail }

	err := mirrorHostClaudeExceptSkillsWith(host, t.TempDir(), fail, fail)
	if err == nil {
		t.Fatal("expected an error when every linker fails")
	}
	if !errors.Is(err, hardFail) {
		t.Fatalf("expected wrapped hardFail, got %v", err)
	}
}

// TestCopyFileRoundTrip exercises the last-resort content-copy fallback used
// by createFileLink on Windows when both symlink and hardlink are
// unavailable (e.g. cross-volume scratch dir). The copy must produce a
// byte-for-byte equivalent destination so Claude Code reads the real
// credential bytes.
func TestCopyFileRoundTrip(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "creds")
	if err := os.WriteFile(src, []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "creds-copy")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(got) != `{"token":"abc"}` {
		t.Fatalf("copy drifted, got %q", got)
	}
	// EXCL semantics — copyFile refuses to overwrite an existing file so
	// a stale mirror entry never silently shadows a fresh source.
	if err := copyFile(src, dst); err == nil {
		t.Fatal("expected copyFile to refuse overwriting existing dst")
	}
}

// TestMirrorHostClaudeJSONIfMissing_DefaultLayoutMirrorsParentFile covers
// the MUL-2661 regression: Claude Code's default layout stores `.claude.json`
// at `$HOME/.claude.json`, a sibling of `~/.claude/`. The isolation mirror
// must pull that file into the scratch dir or the CLI exits with
// `Not logged in · Please run /login` on the first turn after the operator
// opts into `ignore` mode.
func TestMirrorHostClaudeJSONIfMissing_DefaultLayoutMirrorsParentFile(t *testing.T) {
	t.Parallel()

	fakeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fakeHome, ".claude"), 0o755); err != nil {
		t.Fatalf("seed fake ~/.claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude.json"), []byte(`{"loggedIn":true}`), 0o600); err != nil {
		t.Fatalf("seed fake $HOME/.claude.json: %v", err)
	}
	dest := t.TempDir()

	homeDir := func() (string, error) { return fakeHome, nil }
	if err := mirrorHostClaudeJSONIfMissingWith(filepath.Join(fakeHome, ".claude"), dest, homeDir, os.Symlink); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, ".claude.json"))
	if err != nil {
		t.Fatalf("read mirrored .claude.json: %v", err)
	}
	if string(got) != `{"loggedIn":true}` {
		t.Fatalf("mirrored content drifted, got %q", got)
	}
}

// TestMirrorHostClaudeJSONIfMissing_AlreadyPresentNoop documents that a
// `.claude.json` already in destDir (mirrored from inside a custom
// CLAUDE_CONFIG_DIR by the main mirror loop) wins over the parent-level
// `$HOME/.claude.json`. Re-linking would silently overwrite the
// operator-pinned credentials with the default-account ones.
func TestMirrorHostClaudeJSONIfMissing_AlreadyPresentNoop(t *testing.T) {
	t.Parallel()

	fakeHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude.json"), []byte(`{"loggedIn":"home"}`), 0o600); err != nil {
		t.Fatalf("seed home .claude.json: %v", err)
	}
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, ".claude.json"), []byte(`{"loggedIn":"custom"}`), 0o600); err != nil {
		t.Fatalf("seed existing dest .claude.json: %v", err)
	}

	called := false
	fileLink := func(src, dst string) error {
		called = true
		return nil
	}
	homeDir := func() (string, error) { return fakeHome, nil }
	if err := mirrorHostClaudeJSONIfMissingWith(filepath.Join(fakeHome, ".claude"), dest, homeDir, fileLink); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	if called {
		t.Fatal("fileLink must not be invoked when dest already has .claude.json")
	}
	got, _ := os.ReadFile(filepath.Join(dest, ".claude.json"))
	if string(got) != `{"loggedIn":"custom"}` {
		t.Fatalf("existing dest .claude.json was overwritten, got %q", got)
	}
}

// TestMirrorHostClaudeJSONIfMissing_CustomHostDirSkipped guards the
// operator-pinned CLAUDE_CONFIG_DIR contract. A custom dir is expected to be
// self-contained; pulling in `$HOME/.claude.json` on top would silently merge
// a different account's login state.
func TestMirrorHostClaudeJSONIfMissing_CustomHostDirSkipped(t *testing.T) {
	t.Parallel()

	fakeHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude.json"), []byte(`{"loggedIn":"home"}`), 0o600); err != nil {
		t.Fatalf("seed home .claude.json: %v", err)
	}
	customHost := t.TempDir()
	dest := t.TempDir()

	called := false
	fileLink := func(src, dst string) error {
		called = true
		return nil
	}
	homeDir := func() (string, error) { return fakeHome, nil }
	if err := mirrorHostClaudeJSONIfMissingWith(customHost, dest, homeDir, fileLink); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	if called {
		t.Fatal("fileLink must not be invoked when host dir is custom (not default $HOME/.claude)")
	}
	if _, err := os.Lstat(filepath.Join(dest, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("dest .claude.json should remain absent, stat err=%v", err)
	}
}

// TestMirrorHostClaudeJSONIfMissing_MissingSourceNoop documents that a host
// with no `$HOME/.claude.json` (fresh install, env-var-auth-only setup) is a
// supported state. The mirror is a no-op and the scratch dir's lack of
// `.claude.json` is left to the CLI to handle (it will surface its own
// "not logged in" error, but the daemon does not pretend a file exists).
func TestMirrorHostClaudeJSONIfMissing_MissingSourceNoop(t *testing.T) {
	t.Parallel()

	fakeHome := t.TempDir()
	dest := t.TempDir()

	called := false
	fileLink := func(src, dst string) error {
		called = true
		return nil
	}
	homeDir := func() (string, error) { return fakeHome, nil }
	if err := mirrorHostClaudeJSONIfMissingWith(filepath.Join(fakeHome, ".claude"), dest, homeDir, fileLink); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	if called {
		t.Fatal("fileLink must not be invoked when $HOME/.claude.json is absent")
	}
}

// TestClaudeExecuteIsolatesProvidesClaudeJSONFromHome is the end-to-end
// MUL-2661 regression: an agent opted into `skills_local=ignore` on a host
// that uses Claude Code's default layout ($HOME/.claude.json sibling of
// ~/.claude/) must still start successfully. Before the fix, the scratch
// CLAUDE_CONFIG_DIR was missing `.claude.json` and the CLI exited with
// `Not logged in · Please run /login` on the first turn.
func TestClaudeExecuteIsolatesProvidesClaudeJSONFromHome(t *testing.T) {
	// NOT parallel — t.Setenv mutates global env (HOME + CLAUDE_CONFIG_DIR).
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Synthesize Claude Code's default split layout under a fake $HOME:
	//   $FAKE_HOME/.claude/         — settings, agents, plugins, etc.
	//   $FAKE_HOME/.claude.json     — main config (login state)
	fakeHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fakeHome, ".claude"), 0o755); err != nil {
		t.Fatalf("seed fake ~/.claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude", "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("seed fake settings.json: %v", err)
	}
	const expectedConfig = "logged-in-default-layout"
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude.json"), []byte(expectedConfig), 0o600); err != nil {
		t.Fatalf("seed fake $HOME/.claude.json: %v", err)
	}

	t.Setenv("HOME", fakeHome)
	// Ensure the host has no CLAUDE_CONFIG_DIR override — the regression
	// only manifests in the default split layout.
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	// Fake claude binary that echoes the .claude.json it reads from the
	// scratch CLAUDE_CONFIG_DIR. Before the fix this echoed "MISSING"
	// because the mirror skipped the sibling file.
	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"cfg=$(cat \"$CLAUDE_CONFIG_DIR/.claude.json\" 2>/dev/null || echo MISSING)\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"system\\\",\\\"session_id\\\":\\\"sess\\\"}\"\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"result\\\",\\\"subtype\\\":\\\"success\\\",\\\"is_error\\\":false,\\\"session_id\\\":\\\"sess\\\",\\\"result\\\":\\\"$cfg\\\"}\"\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "ignored", ExecOptions{
		Cwd:         t.TempDir(),
		Timeout:     5 * time.Second,
		SkillsLocal: "ignore",
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
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (err=%q)", result.Status, result.Error)
		}
		got := strings.TrimSpace(result.Output)
		if got == "MISSING" {
			t.Fatalf("MUL-2661 regression: .claude.json was not mirrored into the isolated dir")
		}
		if got != expectedConfig {
			t.Fatalf("expected $HOME/.claude.json mirrored, got %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestClaudeExecuteIsolatesUsesCustomEnvSource confirms the runtime mirrors
// from the agent's custom_env CLAUDE_CONFIG_DIR — the exact bug Elon's
// review flagged: when an operator pins CLAUDE_CONFIG_DIR via custom_env,
// the scratch dir must mirror *that* source, not `~/.claude`. Otherwise
// default `ignore` mode would silently load the wrong credentials.
func TestClaudeExecuteIsolatesUsesCustomEnvSource(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Build a synthetic "host Claude config dir" the agent will pin via
	// custom_env. The mirror should land its `.credentials.json` in the
	// scratch dir. The token value is a plain quote-free string so the
	// fake-claude shell script can echo it through stream-json's `result`
	// field without escape gymnastics.
	customHost := t.TempDir()
	const expectedToken = "from-custom-host-token-ok"
	if err := os.WriteFile(filepath.Join(customHost, ".credentials.json"), []byte(expectedToken), 0o600); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	// Fake claude binary that prints the mirrored credentials content from
	// the scratch CLAUDE_CONFIG_DIR — we then assert that we see the
	// custom host's token, not whatever lives in real `~/.claude/`.
	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"creds=$(cat \"$CLAUDE_CONFIG_DIR/.credentials.json\" 2>/dev/null || echo MISSING)\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"system\\\",\\\"session_id\\\":\\\"sess\\\"}\"\n" +
		"printf '%s\\n' \"{\\\"type\\\":\\\"result\\\",\\\"subtype\\\":\\\"success\\\",\\\"is_error\\\":false,\\\"session_id\\\":\\\"sess\\\",\\\"result\\\":\\\"$creds\\\"}\"\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"CLAUDE_CONFIG_DIR": customHost},
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Explicit SkillsLocal == "ignore" → backend builds scratch dir mirrored
	// from custom_env CLAUDE_CONFIG_DIR. (The platform default is "merge",
	// which would just preserve the host CLAUDE_CONFIG_DIR untouched and
	// never exercise the mirror path — see MUL-2603 product decision.)
	session, err := backend.Execute(ctx, "ignored", ExecOptions{
		Cwd:         t.TempDir(),
		Timeout:     5 * time.Second,
		SkillsLocal: "ignore",
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
		if result.Status != "completed" {
			t.Fatalf("expected completed, got %q (err=%q)", result.Status, result.Error)
		}
		got := strings.TrimSpace(result.Output)
		if got != expectedToken {
			t.Fatalf("expected credentials mirrored from custom CLAUDE_CONFIG_DIR, got %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
