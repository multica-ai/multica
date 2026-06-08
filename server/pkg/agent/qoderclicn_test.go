package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func quietQoderclicnLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewReturnsQoderclicnBackend(t *testing.T) {
	t.Parallel()

	b, err := New("qoderclicn", Config{ExecutablePath: "/nonexistent/qoderclicn"})
	if err != nil {
		t.Fatalf("New(qoderclicn) error: %v", err)
	}
	if _, ok := b.(*qoderclicnBackend); !ok {
		t.Fatalf("expected *qoderclicnBackend, got %T", b)
	}
}

func TestQoderclicnLaunchHeader(t *testing.T) {
	t.Parallel()

	if got := LaunchHeader("qoderclicn"); got != "qoderclicn (stream-json)" {
		t.Errorf("unexpected launch header: %q", got)
	}
}

func TestQoderclicnCapability(t *testing.T) {
	t.Parallel()

	cap := CapabilityOrDefault("qoderclicn")
	if !cap.StreamDisplay || !cap.ToolCallStream || cap.Approval || !cap.ResumeSession || cap.PlanMode || !cap.StructuredOutput {
		t.Fatalf("unexpected qoderclicn capability: %+v", cap)
	}
}

func TestBuildQoderclicnArgsBasic(t *testing.T) {
	t.Parallel()

	args := buildQoderclicnArgs(
		"hello",
		ExecOptions{
			Cwd:             "/work",
			Model:           "GLM-5.1",
			SystemPrompt:    "answer briefly",
			ResumeSessionID: "ses_123",
		},
		"/tmp/mcp.json",
		quietQoderclicnLogger(),
	)

	want := []string{
		"-p",
		"--output-format", "stream-json",
		"--permission-mode", "bypass_permissions",
		"--dangerously-skip-permissions",
		"--model", "GLM-5.1",
		"--cwd", filepath.Clean("/work"),
		"--append-system-prompt", "answer briefly",
		"--resume", "ses_123",
		"--mcp-config", "/tmp/mcp.json",
		"--strict-mcp-config",
		"--", "hello",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildQoderclicnArgs mismatch\n got: %v\nwant: %v", args, want)
	}
}

func TestBuildQoderclicnArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildQoderclicnArgs(
		"go",
		ExecOptions{
			ExtraArgs: []string{
				"--output-format", "text",
				"--allowed-tools", "Read",
			},
			CustomArgs: []string{
				"-p",
				"--resume", "bad-session",
				"--cwd=/tmp/elsewhere",
				"--permission-mode", "default",
				"--session-id", "bad-id",
				"--", "hijacked-prompt",
				"--agent", "coder",
			},
		},
		"",
		quietQoderclicnLogger(),
	)

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "text") {
		t.Errorf("blocked --output-format value leaked through: %v", args)
	}
	if strings.Contains(joined, "bad-session") || strings.Contains(joined, "bad-id") || strings.Contains(joined, "/tmp/elsewhere") {
		t.Errorf("blocked resume/session/cwd args leaked through: %v", args)
	}
	if strings.Contains(joined, "hijacked-prompt") {
		t.Errorf("custom -- separator value leaked through: %v", args)
	}
	if !strings.Contains(joined, "--allowed-tools Read") {
		t.Errorf("non-blocked extra arg should pass through: %v", args)
	}
	if !strings.Contains(joined, "--agent coder") {
		t.Errorf("non-blocked custom arg should pass through: %v", args)
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "go" {
		t.Fatalf("prompt must remain the final positional query behind --, got %v", args)
	}
}

func TestQoderclicnAssistantParsing(t *testing.T) {
	t.Parallel()

	message := json.RawMessage(`{
		"role":"assistant",
		"model":"GLM-5.1",
		"usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":3,"cache_creation_input_tokens":2},
		"content":[
			{"type":"thinking","thinking":"plan"},
			{"type":"text","text":"hello"},
			{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"pwd"}}
		]
	}`)
	evt := qoderclicnStreamEvent{Type: "assistant", Message: message}
	ch := make(chan Message, 8)
	var output strings.Builder
	usage := map[string]TokenUsage{}

	handleQoderclicnAssistant(evt, ch, &output, usage, nil)
	close(ch)

	if output.String() != "hello" {
		t.Fatalf("output = %q, want hello", output.String())
	}
	gotUsage := usage["GLM-5.1"]
	if gotUsage.InputTokens != 10 || gotUsage.OutputTokens != 4 || gotUsage.CacheReadTokens != 3 || gotUsage.CacheWriteTokens != 2 {
		t.Fatalf("usage = %+v", gotUsage)
	}
	var types []MessageType
	for msg := range ch {
		types = append(types, msg.Type)
	}
	wantTypes := []MessageType{MessageThinking, MessageText, MessageToolUse}
	if !slices.Equal(types, wantTypes) {
		t.Fatalf("message types = %v, want %v", types, wantTypes)
	}
}

func TestQoderclicnResultUsagePrefersModelUsage(t *testing.T) {
	t.Parallel()

	evt := qoderclicnStreamEvent{
		Model: "fallback",
		Usage: &qoderclicnUsage{InputTokensSnake: 1, OutputTokensSnake: 2},
		ModelUsage: map[string]qoderclicnResultModelUsage{
			"GLM-5.1": {
				InputTokensCamel:              11,
				OutputTokensCamel:             7,
				CacheReadInputTokensCamel:     5,
				CacheCreationInputTokensCamel: 3,
			},
		},
	}

	usage := qoderclicnResultUsage(evt, "")
	got := usage["GLM-5.1"]
	if got.InputTokens != 11 || got.OutputTokens != 7 || got.CacheReadTokens != 5 || got.CacheWriteTokens != 3 {
		t.Fatalf("usage = %+v", got)
	}
	if _, ok := usage["fallback"]; ok {
		t.Fatalf("modelUsage should override fallback usage: %+v", usage)
	}
}

func TestQoderclicnExecuteWithFakeCLI(t *testing.T) {
	t.Parallel()

	execPath := filepath.Join(t.TempDir(), "qoderclicn")
	writeTestExecutable(t, execPath, []byte(`#!/bin/sh
printf '{"type":"system","subtype":"init","session_id":"ses_1","model":"GLM-5.1"}\n'
printf '{"type":"assistant","message":{"role":"assistant","model":"GLM-5.1","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":3,"output_tokens":2}}}\n'
printf '{"type":"result","subtype":"success","session_id":"ses_1","result":"hi","modelUsage":{"GLM-5.1":{"inputTokens":3,"outputTokens":2}}}\n'
`))
	backend := &qoderclicnBackend{cfg: Config{ExecutablePath: execPath, Logger: quietQoderclicnLogger()}}

	session, err := backend.Execute(context.Background(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var streamed strings.Builder
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed.WriteString(msg.Content)
		}
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "hi" || result.SessionID != "ses_1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if streamed.String() != "hi" {
		t.Fatalf("streamed = %q, want hi", streamed.String())
	}
	if result.Usage["GLM-5.1"].InputTokens != 3 || result.Usage["GLM-5.1"].OutputTokens != 2 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}
