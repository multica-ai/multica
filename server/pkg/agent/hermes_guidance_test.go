package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

type hermesGuidanceWriter func([]byte) (int, error)

func (f hermesGuidanceWriter) Write(b []byte) (int, error) { return f(b) }

func TestHermesNativeSteerReadiness(t *testing.T) {
	s := &hermesGuidanceSession{sessionID: "s"}
	notify := func(sid, update string) {
		s.notification("session/update", json.RawMessage(`{"sessionId":"`+sid+`","update":`+update+`}`))
	}
	const commands = `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"steer"}]}`
	notify("other", commands)
	notify("s", `{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"working"}}`)
	if s.ready() {
		t.Fatal("another session enabled steering")
	}
	notify("s", commands)
	if !s.ready() {
		t.Fatal("advertised native steer did not become ready")
	}
	notify("s", `{"sessionUpdate":"available_commands_update","availableCommands":[]}`)
	if s.ready() {
		t.Fatal("withdrawn command still ready")
	}
	notify("s", commands)
	s.finish(nil)
	notify("s", commands)
	if s.ready() {
		t.Fatal("late command event revived finished turn")
	}
}

func TestHermesNativeSteerRequiresPromptActivity(t *testing.T) {
	s := &hermesGuidanceSession{sessionID: "s"}
	s.notification("session/update", json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"steer"}]}}`))
	if s.ready() {
		t.Fatal("command advertisement alone enabled injection")
	}
	if err := s.send(context.Background(), "input"); err == nil {
		t.Fatal("unready steer accepted")
	}
}

func TestHermesNativeSteerWireAndPrimaryIsolation(t *testing.T) {
	s := &hermesGuidanceSession{sessionID: "s", commandsSession: "s", supportsSteer: true, active: true}
	c := &hermesClient{pending: make(map[int]*pendingRPC), primaryPromptOnly: true, onPromptDone: func(hermesPromptResult) { t.Fatal("control reply completed primary") }}
	s.client = c
	text := "literal /stop\n" + strings.Repeat("界", 100)
	c.stdin = hermesGuidanceWriter(func(b []byte) (int, error) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Session string `json:"sessionId"`
				Prompt  []struct {
					Text string `json:"text"`
				} `json:"prompt"`
				Meta any `json:"_meta"`
			} `json:"params"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if req.Method != "session/prompt" || req.Params.Session != "s" || len(req.Params.Prompt) != 1 || req.Params.Prompt[0].Text != "/steer "+text || req.Params.Meta != nil {
			t.Fatalf("unexpected native request: %s", b)
		}
		ack, _ := json.Marshal(map[string]any{"content": map[string]string{"text": s.pendingAck + "\n"}})
		if s.acceptUpdate("agent_message_chunk", ack) {
			t.Fatal("slash acknowledgement polluted transcript")
		}
		if !s.acceptUpdate("agent_message_chunk", json.RawMessage(`{"content":{"text":"real model output"}}`)) {
			t.Fatal("primary text swallowed")
		}
		raw := map[string]json.RawMessage{"id": json.RawMessage(strconv.Itoa(req.ID)), "result": json.RawMessage(`{"stopReason":"end_turn","usage":{"inputTokens":999,"outputTokens":999}}`)}
		c.handleResponse(raw)
		return len(b), nil
	})
	if err := s.send(t.Context(), text); err != nil {
		t.Fatal(err)
	}
	if s.pendingAck != "" {
		t.Fatal("control presentation state leaked")
	}
}
