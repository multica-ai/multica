package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCopilotHandleMessage(t *testing.T) {
	b := &copilotBackend{}
	ch := make(chan Message, 10)

	data := json.RawMessage(`{
		"messageId": "msg-1",
		"content": "Hello world",
		"toolRequests": []
	}`)

	var out strings.Builder
	b.handleMessage(data, ch, &out)

	if out.String() != "Hello world" {
		t.Errorf("output = %q, want %q", out.String(), "Hello world")
	}
	msg := <-ch
	if msg.Type != MessageText || msg.Content != "Hello world" {
		t.Errorf("got %+v, want MessageText with 'Hello world'", msg)
	}
}

func TestCopilotHandleToolStart(t *testing.T) {
	b := &copilotBackend{}
	ch := make(chan Message, 10)

	// Function-type tool with JSON object arguments
	data := json.RawMessage(`{
		"toolCallId": "call_123",
		"toolName": "bash",
		"arguments": {"command": "ls -la"}
	}`)
	b.handleToolStart(data, ch)

	msg := <-ch
	if msg.Type != MessageToolUse {
		t.Fatalf("type = %v, want MessageToolUse", msg.Type)
	}
	if msg.Tool != "bash" || msg.CallID != "call_123" {
		t.Errorf("got tool=%q callID=%q", msg.Tool, msg.CallID)
	}
	if msg.Input["command"] != "ls -la" {
		t.Errorf("input = %v, want command=ls -la", msg.Input)
	}
}

func TestCopilotHandleToolStartStringArgs(t *testing.T) {
	b := &copilotBackend{}
	ch := make(chan Message, 10)

	// Custom-type tool with string arguments (e.g. apply_patch)
	data := json.RawMessage(`{
		"toolCallId": "custom_call_456",
		"toolName": "apply_patch",
		"arguments": "*** Begin Patch\n+hello\n*** End Patch"
	}`)
	b.handleToolStart(data, ch)

	msg := <-ch
	if msg.Type != MessageToolUse || msg.Tool != "apply_patch" {
		t.Fatalf("got %+v", msg)
	}
	if msg.Input["input"] != "*** Begin Patch\n+hello\n*** End Patch" {
		t.Errorf("input = %v", msg.Input)
	}
}

func TestCopilotHandleToolComplete(t *testing.T) {
	b := &copilotBackend{}
	ch := make(chan Message, 10)

	data := json.RawMessage(`{
		"toolCallId": "call_123",
		"success": true,
		"result": {"content": "file1\nfile2\n"}
	}`)
	b.handleToolComplete(data, ch)

	msg := <-ch
	if msg.Type != MessageToolResult || msg.CallID != "call_123" {
		t.Fatalf("got %+v", msg)
	}
	if msg.Output != "file1\nfile2\n" {
		t.Errorf("output = %q", msg.Output)
	}
}

func TestCopilotEventParsing(t *testing.T) {
	// Test that result event parses sessionId from top level
	line := `{"type":"result","timestamp":"2026-04-08T10:06:03.997Z","sessionId":"abc-123","exitCode":0}`
	var evt copilotEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		t.Fatal(err)
	}
	if evt.Type != "result" || evt.SessionID != "abc-123" || evt.ExitCode != 0 {
		t.Errorf("got %+v", evt)
	}
}

func TestCopilotEventParsingNonZeroExit(t *testing.T) {
	line := `{"type":"result","sessionId":"abc-123","exitCode":1}`
	var evt copilotEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		t.Fatal(err)
	}
	if evt.ExitCode != 1 {
		t.Errorf("exitCode = %d, want 1", evt.ExitCode)
	}
}

func TestParseRawArgs(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want map[string]any
	}{
		{"nil", nil, nil},
		{"object", json.RawMessage(`{"key":"val"}`), map[string]any{"key": "val"}},
		{"string", json.RawMessage(`"hello"`), map[string]any{"input": "hello"}},
		{"empty string", json.RawMessage(`""`), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRawArgs(tt.raw)
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: got %v, want %v", k, got[k], v)
				}
			}
		})
	}
}
