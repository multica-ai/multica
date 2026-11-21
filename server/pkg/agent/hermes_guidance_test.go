package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
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
		ack, _ := json.Marshal(map[string]any{"content": map[string]string{"text": "⏩ Steer queued for the active turn: " + string([]rune(text)[:80]) + "...\n"}})
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
	if len(s.pendingAcks) != 0 {
		t.Fatal("control presentation state leaked")
	}
}

// A local response timeout does not retract a command already written to ACP.
func TestHermesNativeSteerLateAcknowledgementDoesNotBecomeAssistantText(t *testing.T) {
	s := &hermesGuidanceSession{sessionID: "s", commandsSession: "s", supportsSteer: true, active: true}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	var messages []Message
	var output acpDeliverableTracker
	c := &hermesClient{
		pending:           make(map[int]*pendingRPC),
		primaryPromptOnly: true,
		acceptUpdate:      s.acceptUpdate,
		onMessage:         func(m Message) { messages = append(messages, m); output.observe(m) },
	}
	s.client = c
	c.stdin = hermesGuidanceWriter(func(b []byte) (int, error) {
		// The write succeeded, but the caller stops waiting before the reply.
		return len(b), nil
	})
	if err := s.send(ctx, "literal guidance"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send error = %v", err)
	}
	params, err := json.Marshal(map[string]any{
		"sessionId": "s",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]string{"type": "text", "text": "⏩ Steer queued for the active turn: literal guidance\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.handleNotification(map[string]json.RawMessage{"method": json.RawMessage(`"session/update"`), "params": params})
	if result, full := output.result(); result != "" || full != "" {
		t.Fatalf("late acknowledgement leaked into primary output: %q / %q", result, full)
	}
	if len(s.pendingAcks) != 0 {
		t.Fatal("observed acknowledgement was not consumed")
	}
	if len(messages) != 0 {
		t.Fatalf("late command acknowledgement leaked into assistant transcript: %+v", messages)
	}
}

func TestHermesNativeSteerPreviousAcknowledgementDuringNextCommand(t *testing.T) {
	s := &hermesGuidanceSession{sessionID: "s", commandsSession: "s", supportsSteer: true, active: true}
	var messages []Message
	var output acpDeliverableTracker
	c := &hermesClient{
		pending: make(map[int]*pendingRPC), primaryPromptOnly: true,
		acceptUpdate: s.acceptUpdate,
		onMessage:    func(m Message) { messages = append(messages, m); output.observe(m) },
		onPromptDone: func(hermesPromptResult) { t.Fatal("command completed primary prompt") },
	}
	s.client = c
	const previous = "⏩ Steer queued for the active turn: previous instruction\n"
	const next = "⏩ Steer queued for the active turn: next instruction\n"
	const answer = "⏩ Steer queued for the active turn: a real answer with a similar prefix\n"
	writes := 0
	c.stdin = hermesGuidanceWriter(func(b []byte) (int, error) {
		writes++
		if writes == 1 {
			return len(b), nil // The previous command is written, but its reply is lost.
		}
		var req struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		hermesSteerTextUpdate(t, c, previous)
		hermesSteerTextUpdate(t, c, answer)
		// A normal command reply can also precede its presentation update.
		c.handleResponse(map[string]json.RawMessage{
			"id":     json.RawMessage(strconv.Itoa(req.ID)),
			"result": json.RawMessage(`{"stopReason":"end_turn","usage":{"inputTokens":999,"outputTokens":999}}`),
		})
		return len(b), nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := s.send(ctx, "previous instruction"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first command: %v", err)
	}
	if err := s.send(t.Context(), "next instruction"); err != nil {
		t.Fatal(err)
	}
	hermesSteerTextUpdate(t, c, next)
	if writes != 2 || len(s.pendingAcks) != 0 || !s.ready() {
		t.Fatalf("writes=%d pending=%v ready=%t", writes, s.pendingAcks, s.ready())
	}
	if len(messages) != 1 || messages[0].Content != answer {
		t.Fatalf("transcript=%+v", messages)
	}
	if result, full := output.result(); result != answer || full != answer {
		t.Fatalf("primary output=%q full=%q", result, full)
	}
}

func TestHermesNativeSteerAcknowledgementLifetime(t *testing.T) {
	s := &hermesGuidanceSession{sessionID: "s", commandsSession: "s", supportsSteer: true, active: true}
	c := &hermesClient{pending: make(map[int]*pendingRPC), primaryPromptOnly: true}
	s.client = c
	c.stdin = hermesGuidanceWriter(func(b []byte) (int, error) { return len(b), nil })
	// Different instructions can have identical 80-codepoint native previews.
	text := strings.Repeat("界", 80)
	ack := "⏩ Steer queued for the active turn: " + text + "...\n"
	for _, suffix := range []string{"first", "second"} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		err := s.send(ctx, text+suffix)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("send: %v", err)
		}
	}
	update, err := json.Marshal(map[string]any{"content": map[string]string{"text": ack}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if s.acceptUpdate("agent_message_chunk", update) {
			t.Fatalf("acknowledgement %d escaped", i)
		}
	}
	if len(s.pendingAcks) != 0 || !s.acceptUpdate("agent_message_chunk", update) {
		t.Fatal("consumed signatures still filter ordinary output")
	}
	for _, suffix := range []string{"third", "fourth"} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		err := s.send(ctx, text+suffix)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("send: %v", err)
		}
	}
	if len(s.pendingAcks) != 1 {
		t.Fatal("unobserved signature was not retained")
	}
	s.finish(nil)
	if len(s.pendingAcks) != 1 || s.ready() || s.acceptUpdate("agent_message_chunk", update) {
		t.Fatal("primary completion discarded a late acknowledgement before pipe drain")
	}
	if len(s.pendingAcks) != 1 {
		t.Fatal("identical unobserved signature was lost")
	}
	s.close()
	if len(s.pendingAcks) != 0 || s.ready() || !s.acceptUpdate("agent_message_chunk", update) {
		t.Fatal("session closure retained presentation state")
	}
	if err := s.send(t.Context(), "after closure"); err == nil || len(s.pendingAcks) != 0 {
		t.Fatalf("closed session accepted a command: %v", err)
	}
}

func hermesSteerTextUpdate(t *testing.T, c *hermesClient, text string) {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"sessionId": "s",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]string{"type": "text", "text": text},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.handleNotification(map[string]json.RawMessage{"method": json.RawMessage(`"session/update"`), "params": params})
}
