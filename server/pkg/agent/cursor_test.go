package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewReturnsCursorBackend(t *testing.T) {
	t.Parallel()
	b, err := New("cursor", Config{ExecutablePath: "/nonexistent/cursor-agent"})
	if err != nil {
		t.Fatalf("New(cursor) error: %v", err)
	}
	if _, ok := b.(*cursorBackend); !ok {
		t.Fatalf("expected *cursorBackend, got %T", b)
	}
}

func TestBuildCursorArgs(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("do something", ExecOptions{
		Cwd:   "/tmp/work",
		Model: "composer-1.5",
	}, slog.Default())

	expected := []string{
		"chat",
		"-p", "do something",
		"--output-format", "stream-json",
		"--yolo",
		"--workspace", "/tmp/work",
		"--model", "composer-1.5",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestBuildCursorArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("continue", ExecOptions{
		ResumeSessionID: "sess-123",
	}, slog.Default())

	hasResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "sess-123" {
			hasResume = true
		}
	}
	if !hasResume {
		t.Fatalf("expected --resume sess-123, got %v", args)
	}
}

func TestBuildCursorArgsMinimal(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("hello", ExecOptions{}, slog.Default())
	expected := []string{"chat", "-p", "hello", "--output-format", "stream-json", "--yolo"}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
}

func TestBuildCursorArgsIgnoresSystemPromptAndMaxTurns(t *testing.T) {
	t.Parallel()

	// cursor-agent CLI does not support --system-prompt or --max-turns;
	// verify they are NOT emitted even when set in ExecOptions.
	args := buildCursorArgs("task", ExecOptions{
		SystemPrompt: "You are helpful",
		MaxTurns:     5,
	}, slog.Default())

	for _, a := range args {
		if a == "--system-prompt" {
			t.Fatalf("unexpected --system-prompt in args: %v", args)
		}
		if a == "--max-turns" {
			t.Fatalf("unexpected --max-turns in args: %v", args)
		}
	}
}

func TestBuildCursorArgsCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("task", ExecOptions{
		CustomArgs: []string{"--extra", "val", "--yolo", "--output-format", "text"},
	}, slog.Default())

	// --extra val should be present; --yolo and --output-format should be filtered out
	hasExtra := false
	hasBlockedYolo := false
	hasBlockedFormat := false
	for i, a := range args {
		if a == "--extra" && i+1 < len(args) && args[i+1] == "val" {
			hasExtra = true
		}
	}
	// Count occurrences of --yolo (should be exactly 1 — the hardcoded one)
	yoloCount := 0
	for _, a := range args {
		if a == "--yolo" {
			yoloCount++
		}
		if a == "text" {
			hasBlockedFormat = true
		}
	}
	if yoloCount > 1 {
		hasBlockedYolo = true
	}
	if !hasExtra {
		t.Fatalf("expected --extra val in args, got %v", args)
	}
	if hasBlockedYolo {
		t.Fatalf("--yolo from custom args should be filtered, got %v", args)
	}
	if hasBlockedFormat {
		t.Fatalf("--output-format from custom args should be filtered, got %v", args)
	}
}

func TestNormalizeCursorStreamLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{`stdout: {"type":"init"}`, `{"type":"init"}`},
		{`stderr: {"type":"error"}`, `{"type":"error"}`},
		{`stdout:{"type":"init"}`, `{"type":"init"}`},
		{`  {"type":"assistant"}  `, `{"type":"assistant"}`},
		{``, ``},
		{`  `, ``},
		{`plain text`, `plain text`},
	}

	for _, tc := range tests {
		got := normalizeCursorStreamLine(tc.input)
		if got != tc.want {
			t.Errorf("normalizeCursorStreamLine(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCursorHandleAssistantText(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	evt := &cursorStreamEvent{
		Type: "assistant",
		Message: mustMarshal(t, cursorAssistantMessage{
			Model: "composer-1.5",
			Content: []cursorContentBlock{
				{Type: "output_text", Text: "Hello from Cursor"},
			},
			Usage: &cursorUsage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		}),
	}

	b.handleCursorAssistant(evt, ch, &output)

	if output.String() != "Hello from Cursor" {
		t.Fatalf("expected output 'Hello from Cursor', got %q", output.String())
	}

	select {
	case m := <-ch:
		if m.Type != MessageText || m.Content != "Hello from Cursor" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestCursorHandleAssistantToolUse(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	evt := &cursorStreamEvent{
		Type: "assistant",
		Message: mustMarshal(t, cursorAssistantMessage{
			Content: []cursorContentBlock{
				{
					Type:  "tool_use",
					ID:    "call-42",
					Name:  "file_edit",
					Input: mustMarshal(t, map[string]any{"path": "/tmp/foo.go"}),
				},
			},
		}),
	}

	b.handleCursorAssistant(evt, ch, &output)

	select {
	case m := <-ch:
		if m.Type != MessageToolUse || m.Tool != "file_edit" || m.CallID != "call-42" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestCursorErrorText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		evt  cursorStreamEvent
		want string
	}{
		{"error field", cursorStreamEvent{ErrorMsg: "bad request"}, "bad request"},
		{"detail field", cursorStreamEvent{Detail: "not found"}, "not found"},
		{"result field", cursorStreamEvent{ResultText: "failed"}, "failed"},
		{"empty", cursorStreamEvent{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cursorErrorText(&tc.evt)
			if got != tc.want {
				t.Errorf("cursorErrorText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCursorAccumulateResultUsage(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	usage := make(map[string]TokenUsage)

	evt := &cursorStreamEvent{
		Model: "gpt-5.3",
		Usage: &cursorUsage{
			InputTokens:          200,
			OutputTokens:         100,
			CacheReadInputTokens: 50,
		},
	}

	b.accumulateResultUsage(usage, evt)

	u := usage["gpt-5.3"]
	if u.InputTokens != 200 || u.OutputTokens != 100 || u.CacheReadTokens != 50 {
		t.Fatalf("unexpected usage: %+v", u)
	}
}

func TestCursorUsageOnlyFromResult(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	evt := &cursorStreamEvent{
		Type: "assistant",
		Message: mustMarshal(t, cursorAssistantMessage{
			Model: "gpt-5",
			Content: []cursorContentBlock{
				{Type: "text", Text: "hello"},
			},
			Usage: &cursorUsage{
				InputTokens:  999,
				OutputTokens: 888,
			},
		}),
	}

	b.handleCursorAssistant(evt, ch, &output)

	if output.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", output.String())
	}

	// handleCursorAssistant should NOT have accumulated usage anywhere —
	// usage is only taken from result events to avoid double-counting.
	// (no usage map to check; this test documents the intent)
}

func TestCursorStepFinishParsing(t *testing.T) {
	t.Parallel()

	part := cursorStepFinishPart{}
	data := `{"tokens":{"input":500,"output":200,"cache":{"read":100}},"cost":0.01}`
	if err := json.Unmarshal([]byte(data), &part); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if part.Tokens.Input != 500 || part.Tokens.Output != 200 || part.Tokens.Cache.Read != 100 {
		t.Fatalf("unexpected part: %+v", part)
	}
}
