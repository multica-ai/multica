package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestSessionPoller_ParseResponse_Running(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	// Real format: array of messages with {info, parts}
	resp := `[{"info":{"role":"assistant"},"parts":[{"type":"text","text":"Working on it..."}]}]`

	status, err := p.parseResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionRunning {
		t.Fatalf("expected running, got %s", status.State)
	}
	if len(status.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(status.Messages))
	}
	if status.Messages[0].Type != "text" {
		t.Fatalf("expected text message, got %s", status.Messages[0].Type)
	}
	if status.Messages[0].Content != "Working on it..." {
		t.Fatalf("unexpected content: %s", status.Messages[0].Content)
	}
}

func TestSessionPoller_ParseResponse_Completed(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	// Real format: step-finish with reason "stop" (not "end_turn")
	resp := `[{"info":{"role":"assistant","finish":"stop","tokens":{"input":100,"output":50,"cache":{"read":10,"write":5}}},"parts":[{"type":"text","text":"Done!"},{"type":"step-finish","reason":"stop","tokens":{"input":100,"output":50,"cache":{"read":10,"write":5}}}]}]`

	status, err := p.parseResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionIdle {
		t.Fatalf("expected idle, got %s", status.State)
	}
	if status.Usage.InputTokens != 100 {
		t.Fatalf("expected 100 input tokens, got %d", status.Usage.InputTokens)
	}
	if status.Usage.OutputTokens != 50 {
		t.Fatalf("expected 50 output tokens, got %d", status.Usage.OutputTokens)
	}
	if status.Usage.CacheReadTokens != 10 {
		t.Fatalf("expected 10 cache read tokens, got %d", status.Usage.CacheReadTokens)
	}
}

func TestSessionPoller_ParseResponse_Error(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	resp := `[{"info":{"role":"assistant"},"parts":[{"type":"step-finish","reason":"error"}]}]`

	status, err := p.parseResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionError {
		t.Fatalf("expected error, got %s", status.State)
	}
}

func TestSessionPoller_ParseResponse_Empty(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	status, err := p.parseResponse("[]")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionRunning {
		t.Fatalf("expected running for empty response, got %s", status.State)
	}
}

func TestSessionPoller_ParseResponse_InvalidJSON(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	_, err := p.parseResponse("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSessionPoller_ParseResponse_ToolUse(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	input := map[string]any{"command": "ls -la"}
	inputBytes, _ := json.Marshal(input)
	resp := `[{"info":{"role":"assistant"},"parts":[{"type":"tool-invocation","toolName":"bash","input":` + string(inputBytes) + `}]}]`

	status, err := p.parseResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(status.Messages))
	}
	if status.Messages[0].Type != "tool-use" {
		t.Fatalf("expected tool-use, got %s", status.Messages[0].Type)
	}
	if status.Messages[0].Tool != "bash" {
		t.Fatalf("expected tool bash, got %s", status.Messages[0].Tool)
	}
}

func TestSessionPoller_ParseResponse_MultipleTokenAccumulation(t *testing.T) {
	p := NewSessionPoller(nil, nil)
	resp := `[{"info":{"role":"assistant"},"parts":[
		{"type":"step-finish","reason":"tool-calls","tokens":{"input":100,"output":50}},
		{"type":"step-finish","reason":"tool-calls","tokens":{"input":200,"output":75}},
		{"type":"step-finish","reason":"stop","tokens":{"input":50,"output":25}}
	]}]`

	status, err := p.parseResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if status.Usage.InputTokens != 350 {
		t.Fatalf("expected 350 total input tokens, got %d", status.Usage.InputTokens)
	}
	if status.Usage.OutputTokens != 150 {
		t.Fatalf("expected 150 total output tokens, got %d", status.Usage.OutputTokens)
	}
}

func TestSessionPoller_ParseResponse_RealAPIFormat(t *testing.T) {
	// Test with actual OpenCode v1.2.27 API response format
	p := NewSessionPoller(nil, nil)
	resp := `[{"info":{"role":"user","time":{"created":1775925658042},"id":"msg_1","sessionID":"ses_1"},"parts":[{"type":"text","text":"Say hello world"}]},{"info":{"role":"assistant","time":{"created":1775925658056,"completed":1775925660844},"tokens":{"total":11382,"input":65,"output":22,"cache":{"read":510,"write":10785}},"finish":"stop","id":"msg_2","sessionID":"ses_1"},"parts":[{"type":"text","text":"Hello World"},{"type":"step-finish","reason":"stop","tokens":{"total":11382,"input":65,"output":22,"cache":{"read":510,"write":10785}}}]}]`

	status, err := p.parseResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionIdle {
		t.Fatalf("expected idle, got %s", status.State)
	}
	// Should have 1 text message (user message is skipped)
	if len(status.Messages) != 1 {
		t.Fatalf("expected 1 message (user skipped), got %d", len(status.Messages))
	}
	if status.Messages[0].Content != "Hello World" {
		t.Fatalf("expected 'Hello World', got %s", status.Messages[0].Content)
	}
	if status.Usage.InputTokens != 65 {
		t.Fatalf("expected 65 input tokens, got %d", status.Usage.InputTokens)
	}
}

func TestSessionPoller_WatchSession_Completion(t *testing.T) {
	mock := NewMockProvider()
	sb, _ := mock.CreateOrConnect(context.Background(), "", CreateOpts{})

	callCount := 0
	mock.ExecFunc = func(_ context.Context, _ *Sandbox, cmd []string) (string, error) {
		callCount++
		if callCount >= 2 {
			return `[{"info":{"role":"assistant","finish":"stop"},"parts":[{"type":"text","text":"Done"},{"type":"step-finish","reason":"stop","tokens":{"input":10,"output":5}}]}]`, nil
		}
		return `[{"info":{"role":"assistant"},"parts":[{"type":"text","text":"Working..."}]}]`, nil
	}

	poller := NewSessionPoller(mock, sb)
	poller.PollInterval = 10 * time.Millisecond // fast for tests

	var receivedMessages int
	status, err := poller.WatchSession(context.Background(), "test-session", func(s *SessionStatus) {
		receivedMessages += len(s.Messages)
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionIdle {
		t.Fatalf("expected idle, got %s", status.State)
	}
	if receivedMessages == 0 {
		t.Fatal("expected to receive messages via callback")
	}
}

func TestSessionPoller_WatchSession_ContextCancel(t *testing.T) {
	mock := NewMockProvider()
	sb, _ := mock.CreateOrConnect(context.Background(), "", CreateOpts{})

	mock.ExecFunc = func(_ context.Context, _ *Sandbox, cmd []string) (string, error) {
		return `[{"info":{"role":"assistant"},"parts":[{"type":"text","text":"Still working"}]}]`, nil
	}

	poller := NewSessionPoller(mock, sb)
	poller.PollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	status, _ := poller.WatchSession(ctx, "test-session", nil)
	if status.State != SessionError {
		t.Fatalf("expected error state on cancel, got %s", status.State)
	}
}

func TestSessionPoller_WatchSession_Timeout(t *testing.T) {
	mock := NewMockProvider()
	sb, _ := mock.CreateOrConnect(context.Background(), "", CreateOpts{})

	// Always return error so no messages arrive → triggers timeout
	mock.ExecFunc = func(_ context.Context, _ *Sandbox, cmd []string) (string, error) {
		return "", fmt.Errorf("connection refused")
	}

	poller := NewSessionPoller(mock, sb)
	poller.PollInterval = 10 * time.Millisecond
	poller.Timeout = 50 * time.Millisecond

	status, err := poller.WatchSession(context.Background(), "test-session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionTimeout {
		t.Fatalf("expected timeout, got %s", status.State)
	}
}
