package agent

import (
	"strings"
	"testing"
)

func TestNewReturnsGeminiBackend(t *testing.T) {
	t.Parallel()
	b, err := New("gemini", Config{ExecutablePath: "/nonexistent/gemini"})
	if err != nil {
		t.Fatalf("New(gemini) error: %v", err)
	}
	if _, ok := b.(*geminiBackend); !ok {
		t.Fatalf("expected *geminiBackend, got %T", b)
	}
}

// ── processEvents ──

func TestGeminiProcessEventsInit(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)
	result := b.processEvents(strings.NewReader(
		`{"type":"init","session_id":"ses-123","model":"gemini-2.5-pro"}`+"\n",
	), ch)

	if result.sessionID != "ses-123" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses-123")
	}
	if result.model != "gemini-2.5-pro" {
		t.Errorf("model: got %q, want %q", result.model, "gemini-2.5-pro")
	}
	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}

	// Should have a status message.
	select {
	case msg := <-ch:
		if msg.Type != MessageStatus {
			t.Errorf("message type: got %v, want MessageStatus", msg.Type)
		}
	default:
		t.Error("expected a status message from init event")
	}
}

func TestGeminiProcessEventsAssistantMessage(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(strings.Join([]string{
		`{"type":"message","role":"assistant","content":"Hello ","delta":true}`,
		`{"type":"message","role":"assistant","content":"world","delta":true}`,
	}, "\n")), ch)

	if result.output != "Hello world" {
		t.Errorf("output: got %q, want %q", result.output, "Hello world")
	}
	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}

	// Should have 2 text messages.
	count := 0
	for len(ch) > 0 {
		msg := <-ch
		if msg.Type != MessageText {
			t.Errorf("message type: got %v, want MessageText", msg.Type)
		}
		count++
	}
	if count != 2 {
		t.Errorf("message count: got %d, want 2", count)
	}
}

func TestGeminiProcessEventsToolUse(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(strings.Join([]string{
		`{"type":"tool_use","tool_name":"read_file","tool_id":"tu-1","parameters":{"path":"/tmp/foo.go"}}`,
		`{"type":"tool_result","tool_id":"tu-1","status":"success","output":"package main"}`,
	}, "\n")), ch)

	_ = result

	// First message: tool use.
	msg1 := <-ch
	if msg1.Type != MessageToolUse {
		t.Errorf("msg1 type: got %v, want MessageToolUse", msg1.Type)
	}
	if msg1.Tool != "read_file" {
		t.Errorf("msg1 tool: got %q, want %q", msg1.Tool, "read_file")
	}
	if msg1.CallID != "tu-1" {
		t.Errorf("msg1 callID: got %q, want %q", msg1.CallID, "tu-1")
	}

	// Second message: tool result.
	msg2 := <-ch
	if msg2.Type != MessageToolResult {
		t.Errorf("msg2 type: got %v, want MessageToolResult", msg2.Type)
	}
	if msg2.CallID != "tu-1" {
		t.Errorf("msg2 callID: got %q, want %q", msg2.CallID, "tu-1")
	}
	if msg2.Output != "package main" {
		t.Errorf("msg2 output: got %q, want %q", msg2.Output, "package main")
	}
}

func TestGeminiProcessEventsError(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(
		`{"type":"error","content":"model not found"}`+"\n",
	), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if result.errMsg != "model not found" {
		t.Errorf("errMsg: got %q, want %q", result.errMsg, "model not found")
	}

	msg := <-ch
	if msg.Type != MessageError {
		t.Errorf("message type: got %v, want MessageError", msg.Type)
	}
}

func TestGeminiProcessEventsUsage(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(strings.Join([]string{
		`{"type":"init","session_id":"ses-1","model":"gemini-2.5-pro"}`,
		`{"type":"usage","usage":{"input_tokens":100,"output_tokens":50}}`,
		`{"type":"usage","usage":{"input_tokens":200,"output_tokens":100}}`,
	}, "\n")), ch)

	if result.usage.InputTokens != 300 {
		t.Errorf("inputTokens: got %d, want 300", result.usage.InputTokens)
	}
	if result.usage.OutputTokens != 150 {
		t.Errorf("outputTokens: got %d, want 150", result.usage.OutputTokens)
	}
}

func TestGeminiProcessEventsIgnoresInvalidJSON(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(strings.Join([]string{
		"not json at all",
		"",
		`{"type":"message","role":"assistant","content":"ok","delta":true}`,
		"{}",
	}, "\n")), ch)

	if result.output != "ok" {
		t.Errorf("output: got %q, want %q", result.output, "ok")
	}
	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
}

func TestGeminiProcessEventsIgnoresUserMessages(t *testing.T) {
	t.Parallel()
	b := &geminiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(strings.Join([]string{
		`{"type":"message","role":"user","content":"echo","delta":true}`,
		`{"type":"message","role":"assistant","content":"pong","delta":true}`,
	}, "\n")), ch)

	if result.output != "pong" {
		t.Errorf("output: got %q, want %q", result.output, "pong")
	}

	// Only 1 message (the assistant one).
	count := 0
	for len(ch) > 0 {
		<-ch
		count++
	}
	if count != 1 {
		t.Errorf("message count: got %d, want 1", count)
	}
}
