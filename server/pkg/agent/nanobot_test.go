package agent

import (
	"encoding/json"
	"testing"
)

func TestNanobotProcessEvents(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantType   MessageType
		wantText   string
		wantStatus string
	}{
		{
			name:     "delta event",
			raw:      `{"event":"delta","chat_id":"abc","text":"hello"}`,
			wantType: MessageText,
			wantText: "hello",
		},
		{
			name:     "empty delta ignored",
			raw:      `{"event":"delta","chat_id":"abc","text":""}`,
			wantType: "",
		},
		{
			name:       "message event default kind",
			raw:        `{"event":"message","chat_id":"abc","text":"response"}`,
			wantType:   MessageText,
			wantText:   "response",
			wantStatus: "",
		},
		{
			name:       "message event tool_hint kind",
			raw:        `{"event":"message","chat_id":"abc","text":"running ls","kind":"tool_hint"}`,
			wantType:   MessageToolUse,
			wantText:   "running ls",
			wantStatus: "",
		},
		{
			name:       "message event progress kind",
			raw:        `{"event":"message","chat_id":"abc","text":"thinking...","kind":"progress"}`,
			wantType:   MessageStatus,
			wantText:   "",
			wantStatus: "thinking...",
		},
		{
			name:     "reasoning_delta event",
			raw:      `{"event":"reasoning_delta","chat_id":"abc","text":"hmm"}`,
			wantType: MessageThinking,
			wantText: "hmm",
		},
		{
			name:       "turn_end event",
			raw:        `{"event":"turn_end","chat_id":"abc"}`,
			wantType:   MessageStatus,
			wantStatus: "turn_end",
		},
		{
			name:     "error event",
			raw:      `{"event":"error","detail":"something broke"}`,
			wantType: MessageError,
			wantText: "",
		},
		{
			name:     "stream_end event ignored",
			raw:      `{"event":"stream_end","chat_id":"abc"}`,
			wantType: "",
		},
		{
			name:     "invalid JSON ignored",
			raw:      `not json`,
			wantType: "",
		},
		{
			name:     "unknown event ignored",
			raw:      `{"event":"unknown_type","text":"data"}`,
			wantType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgType, content, status := processNanobotEvent([]byte(tt.raw))
			if msgType != tt.wantType {
				t.Errorf("processNanobotEvent() type = %q, want %q", msgType, tt.wantType)
			}
			if content != tt.wantText {
				t.Errorf("processNanobotEvent() content = %q, want %q", content, tt.wantText)
			}
			if status != tt.wantStatus {
				t.Errorf("processNanobotEvent() status = %q, want %q", status, tt.wantStatus)
			}
		})
	}
}

func TestNanobotNew(t *testing.T) {
	b, err := New("nanobot", Config{ExecutablePath: "nanobot"})
	if err != nil {
		t.Fatalf("New(\"nanobot\") error: %v", err)
	}
	if _, ok := b.(*nanobotBackend); !ok {
		t.Fatalf("expected *nanobotBackend, got %T", b)
	}
}

func TestNanobotLaunchHeader(t *testing.T) {
	got := LaunchHeader("nanobot")
	want := "nanobot gateway (websocket)"
	if got != want {
		t.Errorf("LaunchHeader(\"nanobot\") = %q, want %q", got, want)
	}
}

func TestNanobotGatewayURL(t *testing.T) {
	// Default URL
	b := &nanobotBackend{cfg: Config{}}
	if got := b.gatewayURL(); got != "ws://127.0.0.1:8765/ws" {
		t.Errorf("default gatewayURL = %q, want ws://127.0.0.1:8765/ws", got)
	}

	// Custom URL via env
	b2 := &nanobotBackend{cfg: Config{
		Env: map[string]string{"NANOBOT_GATEWAY_URL": "ws://custom:9999/ws"},
	}}
	if got := b2.gatewayURL(); got != "ws://custom:9999/ws" {
		t.Errorf("custom gatewayURL = %q, want ws://custom:9999/ws", got)
	}
}

func TestNanobotReadyEventParsing(t *testing.T) {
	raw := `{"event":"ready","chat_id":"test-123","client_id":"client"}`
	var ready struct {
		Event  string `json:"event"`
		ChatID string `json:"chat_id"`
	}
	if err := json.Unmarshal([]byte(raw), &ready); err != nil {
		t.Fatalf("unmarshal ready: %v", err)
	}
	if ready.ChatID != "test-123" {
		t.Errorf("chat_id = %q, want test-123", ready.ChatID)
	}
}
