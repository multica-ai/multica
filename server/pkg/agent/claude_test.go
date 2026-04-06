package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func containsFlag(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func TestBuildClaudeArgsEffortIncluded(t *testing.T) {
	t.Parallel()

	for _, effort := range []string{"low", "medium", "high", "max"} {
		args := buildClaudeArgs(ExecOptions{Effort: effort}, "do something")
		if !containsFlag(args, "--effort", effort) {
			t.Errorf("effort=%q: expected --effort %s in args %v", effort, effort, args)
		}
	}
}

func TestBuildClaudeArgsEffortOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildClaudeArgs(ExecOptions{}, "do something")
	if containsArg(args, "--effort") {
		t.Errorf("expected no --effort flag when Effort is empty, got %v", args)
	}
}

func TestBuildClaudeArgsModelAndEffort(t *testing.T) {
	t.Parallel()

	args := buildClaudeArgs(ExecOptions{Model: "claude-opus-4-6", Effort: "high"}, "prompt")
	if !containsFlag(args, "--model", "claude-opus-4-6") {
		t.Errorf("expected --model flag in args %v", args)
	}
	if !containsFlag(args, "--effort", "high") {
		t.Errorf("expected --effort high in args %v", args)
	}
}

func TestBuildClaudeArgsPromptAlwaysLast(t *testing.T) {
	t.Parallel()

	prompt := "hello world"
	args := buildClaudeArgs(ExecOptions{Effort: "max", Model: "claude-sonnet-4-6"}, prompt)
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args, got %d", len(args))
	}
	if args[len(args)-1] != prompt {
		t.Errorf("expected prompt as last arg, got %q", args[len(args)-1])
	}
	if args[len(args)-2] != "-p" {
		t.Errorf("expected -p before prompt, got %q", args[len(args)-2])
	}
}

func TestBuildClaudeArgsBaseFlags(t *testing.T) {
	t.Parallel()

	args := buildClaudeArgs(ExecOptions{}, "test")
	for _, flag := range []string{"--output-format", "--verbose", "--permission-mode"} {
		if !containsArg(args, flag) {
			t.Errorf("expected base flag %q in args %v", flag, args)
		}
	}
}

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

	b.handleAssistant(msg, ch, &output)

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

	b.handleAssistant(msg, ch, &output)

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
	b.handleAssistant(msg, ch, &output)

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


func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
