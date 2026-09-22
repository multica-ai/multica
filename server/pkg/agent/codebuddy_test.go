package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildCodebuddyArgs_Basic(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{
		Model:        "claude-sonnet-4-20250514",
		MaxTurns:     25,
		SystemPrompt: "You are an agent.",
	}, slog.Default())

	expected := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
		"--disallowedTools", "AskUserQuestion", "EnterPlanMode", "ExitPlanMode",
		"--model", "claude-sonnet-4-20250514",
		"--max-turns", "25",
		"--append-system-prompt", "You are an agent.",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q\nfull args: %v", i, args[i], want, args)
		}
	}
}

// --strict-mcp-config must never be passed, with or without a managed config.
// It means "only use servers from --mcp-config" and drops CodeBuddy's user,
// project and local scopes; measured against the real CLI, strict + a managed
// config loaded ONLY the managed server, and strict alone loaded nothing at
// all. The union is what mergeRuntimeAndAgentMcpConfig promises (MUL-5846).
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
			if arg == "--strict-mcp-config" {
				t.Fatalf("mcp_config %q must not disable CodeBuddy's own MCP scopes, got %v", string(mcpConfig), args)
			}
		}
	}
}

func TestBuildCodebuddyArgs_DisallowsInteractiveTools(t *testing.T) {
	t.Parallel()

	// The daemon runs CodeBuddy headless, so every tool that waits on a human
	// confirmation stalls the turn instead of ending it (GitHub #6012).
	// CodeBuddy matches each --disallowedTools entry against the tool name
	// exactly, so each tool must arrive as its own argv value.
	args := buildCodebuddyArgs(ExecOptions{}, slog.Default())

	idx := -1
	for i, a := range args {
		if a == "--disallowedTools" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("expected --disallowedTools in args: %v", args)
	}

	for offset, want := range []string{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode"} {
		got := ""
		if idx+1+offset < len(args) {
			got = args[idx+1+offset]
		}
		if got != want {
			t.Fatalf("disallowed tool %d = %q, want %q\nfull args: %v", offset, got, want, args)
		}
	}
}

func TestBuildCodebuddyArgs_InjectsEffort(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{
		ThinkingLevel: "high",
	}, slog.Default())

	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--effort" && args[i+1] == "high" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --effort high in args: %v", args)
	}
}

func TestBuildCodebuddyArgs_OmitsEffortWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{}, slog.Default())

	for _, a := range args {
		if a == "--effort" {
			t.Fatalf("--effort should not appear when ThinkingLevel is empty: %v", args)
		}
	}
}

func TestBuildCodebuddyArgs_BlocksUserEffortOverride(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{
		ThinkingLevel: "medium",
		CustomArgs:    []string{"--effort", "max"},
	}, slog.Default())

	// Should have exactly one --effort (the daemon-injected one).
	count := 0
	for i, a := range args {
		if a == "--effort" {
			count++
			if i+1 < len(args) && args[i+1] != "medium" {
				t.Fatalf("expected --effort medium, got --effort %s", args[i+1])
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 --effort, got %d in: %v", count, args)
	}
}

func TestBuildCodebuddyArgs_ExtraArgsBeforeCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{
		ExtraArgs:  []string{"--output-format", "text", "--max-budget-usd", "1.00"},
		CustomArgs: []string{"--max-budget-usd", "2.00", "--permission-mode", "plan"},
	}, slog.Default())

	joined := strings.Join(args, " ")
	// Blocked flags should be filtered from both layers.
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

func TestBuildCodebuddyArgs_Resume(t *testing.T) {
	t.Parallel()

	args := buildCodebuddyArgs(ExecOptions{
		ResumeSessionID: "sess-abc123",
	}, slog.Default())

	found := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--resume" && args[i+1] == "sess-abc123" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --resume sess-abc123 in args: %v", args)
	}
}

func TestCodebuddyExecute_Success(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		`printf '%s\n' '{"type":"system","session_id":"sess-cb-001"}'` + "\n" +
		`printf '%s\n' '{"type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hello from codebuddy"}]}}'` + "\n" +
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-cb-001","result":"Hello from codebuddy","modelUsage":{"claude-sonnet-4-20250514":{"inputTokens":100,"outputTokens":50,"cacheReadInputTokens":10,"cacheCreationInputTokens":5}}}'` + "\n"
	writeTestExecutable(t, fakePath, []byte(script))

	b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.Default()}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "say hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain messages.
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
		if result.SessionID != "sess-cb-001" {
			t.Fatalf("expected session_id=sess-cb-001, got %q", result.SessionID)
		}
		usage, ok := result.Usage["claude-sonnet-4-20250514"]
		if !ok {
			t.Fatalf("expected usage for claude-sonnet-4-20250514, got %#v", result.Usage)
		}
		if usage.InputTokens != 100 || usage.OutputTokens != 50 || usage.CacheReadTokens != 10 || usage.CacheWriteTokens != 5 {
			t.Fatalf("unexpected usage: %+v", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
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

func TestCodebuddyExecuteSurfacesStderr(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		"echo \"FATAL ERROR: segfault in codebuddy runtime\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.Default()}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain messages.
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
		if !strings.Contains(result.Error, "codebuddy exited with error") {
			t.Fatalf("expected error to mention exit, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "segfault in codebuddy runtime") {
			t.Fatalf("expected error to include stderr content, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "codebuddy stderr:") {
			t.Fatalf("expected stderr label in error, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestWriteCodebuddyInput(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	err := writeCodebuddyInput(&buf, "hello world")
	if err != nil {
		t.Fatalf("writeCodebuddyInput: %v", err)
	}

	data := buf.String()
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected newline-terminated payload, got %q", data)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &payload); err != nil {
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
	if block["type"] != "text" || block["text"] != "hello world" {
		t.Fatalf("unexpected content block: %v", block)
	}
}

func TestCodebuddyHandleAssistantText(t *testing.T) {
	t.Parallel()

	b := &codebuddyBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)

	msg := codebuddySDKMessage{
		Type: "assistant",
		Message: mustMarshal(t, codebuddyMessageContent{
			Role: "assistant",
			Content: []codebuddyContentBlock{
				{Type: "text", Text: "codebuddy says hi"},
			},
		}),
	}

	turn := b.handleAssistant(msg, ch, make(map[string]TokenUsage), make(map[string]struct{}))
	output, tools := turn.text, turn.toolUses

	if output != "codebuddy says hi" {
		t.Fatalf("expected output 'codebuddy says hi', got %q", output)
	}
	if tools != 0 {
		t.Fatalf("expected no tool uses, got %d", tools)
	}
	select {
	case m := <-ch:
		if m.Type != MessageText || m.Content != "codebuddy says hi" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
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
		// CodeBuddy 2.130.0 advertises `max`; the gate used to reject it.
		{"max", true},
		{"none", false},
		// ACP advertises `enabled` as a session toggle, but `--effort enabled`
		// is not a valid command line, so the gate must not accept it.
		{"enabled", false},
	}
	for _, tc := range cases {
		got := IsKnownThinkingValue("codebuddy", tc.value)
		if got != tc.want {
			t.Errorf("IsKnownThinkingValue(codebuddy, %q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestCodebuddyHandleUserToolResult(t *testing.T) {
	t.Parallel()

	b := &codebuddyBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)

	msg := codebuddySDKMessage{
		Type: "user",
		Message: mustMarshal(t, codebuddyMessageContent{
			Role: "user",
			Content: []codebuddyContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "call-cb-1",
					Content:   mustMarshal(t, "tool output here"),
				},
			},
		}),
	}

	b.handleUser(msg, ch)

	select {
	case m := <-ch:
		if m.Type != MessageToolResult || m.CallID != "call-cb-1" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestCodebuddyHandleControlRequestApprovesInCodebuddyShape(t *testing.T) {
	t.Parallel()

	b := &codebuddyBackend{cfg: Config{Logger: slog.Default()}}

	var written bytes.Buffer

	msg := codebuddySDKMessage{
		Type:      "control_request",
		RequestID: "perm_1730000000000_1",
		Request: mustMarshal(t, codebuddyControlRequestPayload{
			Subtype:  "can_use_tool",
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
	respInner, ok := resp["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected response object, got %v", resp["response"])
	}
	if respInner["subtype"] != "success" {
		t.Fatalf("expected subtype success, got %v", respInner["subtype"])
	}
	if respInner["request_id"] != "perm_1730000000000_1" {
		t.Fatalf("expected the request_id to be echoed back, got %v", respInner["request_id"])
	}

	innerResp, ok := respInner["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected inner response object, got %v", respInner["response"])
	}
	// CodeBuddy reads `allowed`; a missing key is read as a denial, which
	// leaves the CLI waiting on a confirmation the daemon can never deliver.
	if innerResp["allowed"] != true {
		t.Fatalf("expected allowed=true, got %v", innerResp["allowed"])
	}
	if innerResp["behavior"] != "allow" {
		t.Fatalf("expected behavior allow, got %v", innerResp["behavior"])
	}
	updatedInput, ok := innerResp["updatedInput"].(map[string]any)
	if !ok {
		t.Fatalf("expected updatedInput object, got %v", innerResp["updatedInput"])
	}
	if updatedInput["command"] != "ls" {
		t.Fatalf("expected the original tool input to be preserved, got %v", updatedInput["command"])
	}
}

func TestBuildCodebuddyEnvDisablesBackgroundTasks(t *testing.T) {
	t.Parallel()

	env := buildCodebuddyEnv(map[string]string{"FOO": "bar"})
	if got := lastEnvValueFold(env, codebuddyDisableBackgroundTasksEnv); got != "1" {
		t.Fatalf("expected %s=1 in child env, got %q (env=%v)", codebuddyDisableBackgroundTasksEnv, got, env)
	}

	// Exact-key override in caller env must still lose to the appended force.
	env = buildCodebuddyEnv(map[string]string{codebuddyDisableBackgroundTasksEnv: "0"})
	if got := lastEnvValueFold(env, codebuddyDisableBackgroundTasksEnv); got != "1" {
		t.Fatalf("expected forced %s=1 even when caller passes 0, got %q", codebuddyDisableBackgroundTasksEnv, got)
	}

	// Windows os/exec dedups case-insensitively and keeps the last entry. A
	// lowercase custom_env key must not outvote the forced disable.
	env = buildCodebuddyEnv(map[string]string{
		strings.ToLower(codebuddyDisableBackgroundTasksEnv): "0",
	})
	if got := lastEnvValueFold(env, codebuddyDisableBackgroundTasksEnv); got != "1" {
		t.Fatalf("expected forced %s=1 to win over lowercase override, got %q (env=%v)",
			codebuddyDisableBackgroundTasksEnv, got, env)
	}
	lastExact := ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == codebuddyDisableBackgroundTasksEnv {
			lastExact = value
		}
	}
	if lastExact != "1" {
		t.Fatalf("forced entry must be last exact key so os/exec Windows dedup keeps it, got last=%q env=%v", lastExact, env)
	}
}

// lastEnvValueFold mirrors os/exec's Windows dedup: case-insensitive key match,
// last occurrence wins.
func lastEnvValueFold(env []string, key string) string {
	got := ""
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(k, key) {
			got = v
		}
	}
	return got
}

func TestCodebuddySystemIsBackgroundTask(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{"task_started", "task_progress", "task_updated", "task_notification"} {
		if !codebuddySystemIsBackgroundTask(subtype) {
			t.Fatalf("expected %q to be a background-task system subtype", subtype)
		}
	}
	for _, subtype := range []string{"init", "status", "", "session_started"} {
		if codebuddySystemIsBackgroundTask(subtype) {
			t.Fatalf("did not expect %q to be treated as background-task", subtype)
		}
	}
}

func TestCodebuddyExecuteResumeRejectedFromStderr(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		"echo \"No conversation found with session ID: sess-dead\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.Default()}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "prompt", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "sess-dead",
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
			t.Fatalf("expected failed, got %q (%q)", result.Status, result.Error)
		}
		if !result.ResumeRejected {
			t.Fatalf("expected ResumeRejected when stderr reports missing session, got %+v", result)
		}
		if result.SessionID != "" {
			t.Fatalf("expected empty SessionID after resume rejection, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCodebuddyExecuteFailsOnBackgroundTaskStarted(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Replay the CodeBuddy 2.150.0 background-task shape from the headless
	// docs: Bash tool_use → system/task_started → text tool_result → success
	// result. Without a completion guard this used to report completed.
	fakePath := filepath.Join(t.TempDir(), "codebuddy")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		`printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-bg"}'` + "\n" +
		`printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"sleep 60","run_in_background":true}}]}}'` + "\n" +
		`printf '%s\n' '{"type":"system","subtype":"task_started","task_id":"bash-1","tool_use_id":"toolu_01","description":"sleep 60","task_type":"Bash","session_id":"sess-bg"}'` + "\n" +
		`printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"Started the build in the background."}]}}'` + "\n" +
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-bg","result":"Started the build in the background."}'` + "\n" +
		"exit 0\n"
	writeTestExecutable(t, fakePath, []byte(script))

	b := &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.Default()}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
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
			t.Fatalf("expected failed when system/task_started appears, got %q (%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "background task") {
			t.Fatalf("expected background-task error, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
