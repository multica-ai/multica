package agent

import (
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
		"--strict-mcp-config",
		"--permission-mode", "bypassPermissions",
		"--disallowedTools", "AskUserQuestion",
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
	var output strings.Builder

	msg := codebuddySDKMessage{
		Type: "assistant",
		Message: mustMarshal(t, codebuddyMessageContent{
			Role: "assistant",
			Content: []codebuddyContentBlock{
				{Type: "text", Text: "codebuddy says hi"},
			},
		}),
	}

	b.handleAssistant(msg, ch, &output, make(map[string]TokenUsage))

	if output.String() != "codebuddy says hi" {
		t.Fatalf("expected output 'codebuddy says hi', got %q", output.String())
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

func TestParseCodebuddyModels_FullHelp(t *testing.T) {
	t.Parallel()
	helpOutput := `Usage: codebuddy [options] [command] [prompt]

Options:
  --model <model>                                  Model for the current session. Please provide the model ID. Currently supported: (claude-sonnet-4.6, claude-opus-4.7, gemini-3.1-pro, gpt-5.5, glm-5.1-ioa, minimax-m2.7-ioa, kimi-k2.6-ioa, hy3-preview-ioa, deepseek-v3-2-volc-ioa)
  --effort <level>                                 Reasoning effort level (low, medium, high, xhigh)
`
	models := parseCodebuddyModels(helpOutput)
	if len(models) != 9 {
		t.Fatalf("expected 9 models, got %d: %+v", len(models), models)
	}
	if !models[0].Default {
		t.Error("first model should be marked as default")
	}
	if models[0].ID != "claude-sonnet-4.6" {
		t.Errorf("first model ID = %q, want claude-sonnet-4.6", models[0].ID)
	}
	if models[0].Provider != "anthropic" {
		t.Errorf("claude model provider = %q, want anthropic", models[0].Provider)
	}
	// Spot check providers
	providers := map[string]string{}
	for _, m := range models {
		providers[m.ID] = m.Provider
	}
	checks := map[string]string{
		"gpt-5.5":                "openai",
		"gemini-3.1-pro":        "google",
		"glm-5.1-ioa":           "zhipu",
		"minimax-m2.7-ioa":      "minimax",
		"kimi-k2.6-ioa":         "kimi",
		"hy3-preview-ioa":       "hunyuan",
		"deepseek-v3-2-volc-ioa": "deepseek",
	}
	for id, want := range checks {
		if got := providers[id]; got != want {
			t.Errorf("provider(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestParseCodebuddyModels_Malformed(t *testing.T) {
	t.Parallel()
	models := parseCodebuddyModels("totally unrelated output\nno model line here")
	if len(models) != 0 {
		t.Fatalf("expected 0 models from malformed output, got %d", len(models))
	}
}

func TestParseCodebuddyEffortHelp(t *testing.T) {
	t.Parallel()
	helpOutput := `  --effort <level>                                 Reasoning effort level (low, medium, high, xhigh)`
	levels := parseCodebuddyEffortHelp(helpOutput)
	expected := []string{"low", "medium", "high", "xhigh"}
	if len(levels) != len(expected) {
		t.Fatalf("expected %d levels, got %d: %v", len(expected), len(levels), levels)
	}
	for i, l := range levels {
		if l != expected[i] {
			t.Errorf("level[%d]: expected %q, got %q", i, expected[i], l)
		}
	}
}

func TestParseCodebuddyEffortHelp_Missing(t *testing.T) {
	t.Parallel()
	levels := parseCodebuddyEffortHelp("no effort line here")
	if len(levels) != 0 {
		t.Fatalf("expected nil for missing effort line, got %v", levels)
	}
}

func TestIsKnownThinkingValue_Codebuddy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value string
		want  bool
	}{
		{"", true},
		{"low", true},
		{"medium", true},
		{"high", true},
		{"xhigh", true},
		{"max", false},
		{"none", false},
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
