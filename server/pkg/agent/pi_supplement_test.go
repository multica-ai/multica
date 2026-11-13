package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"slices"
	"testing"
	"time"
)

func TestPiSupplementRequiresConsumption(t *testing.T) {
	for _, order := range []string{"reply-first", "message-first", "unconsumed", "rejected", "cancelled"} {
		t.Run(order, func(t *testing.T) {
			r, w := io.Pipe()
			defer r.Close()
			defer w.Close()
			s := newPiSupplementSession(w)
			if s.ready() {
				t.Fatal("ready before agent_start")
			}
			s.observe([]byte(`{"type":"agent_start"}`))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- s.send(ctx, "change course") }()
			line, err := bufio.NewReader(r).ReadBytes('\n')
			if err != nil {
				t.Fatal(err)
			}
			var cmd map[string]string
			if err := json.Unmarshal(line, &cmd); err != nil || cmd["type"] != "steer" || cmd["message"] != "change course" {
				t.Fatalf("command=%s err=%v", line, err)
			}
			reply := []byte(`{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"queued"}}`)
			consumed := []byte(`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"change course"}]}}`)
			switch order {
			case "reply-first":
				s.observe(reply)
				select {
				case err := <-result:
					t.Fatalf("queue acknowledged as delivery: %v", err)
				default:
				}
				s.observe(consumed)
			case "message-first":
				s.observe(consumed)
				s.observe(reply)
			case "unconsumed":
				s.observe(reply)
				s.stop()
			case "rejected":
				s.observe([]byte(`{"type":"response","id":"steer-1","command":"steer","success":false,"error":"rejected"}`))
			case "cancelled":
				cancel()
			}
			select {
			case err := <-result:
				wantSuccess := order == "reply-first" || order == "message-first"
				if (err == nil) != wantSuccess {
					t.Fatalf("result=%v wantSuccess=%v", err, wantSuccess)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("steering blocked")
			}
			s.stop()
			if s.ready() {
				t.Fatal("ready after stop")
			}
			if err := s.send(ctx, "late"); err == nil {
				t.Fatal("accepted after stop")
			}
		})
	}
}

func TestPiSupplementConsumptionWinsLateReply(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.send(ctx, "change course") }()
	if _, err := bufio.NewReader(r).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	s.observe([]byte(`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"change course"}]}}`))
	s.stop()
	if err := <-result; err != nil {
		t.Fatalf("consumed steering reported as failed: %v", err)
	}
}

func TestPiHandledPromptDoesNotCloseActiveRun(t *testing.T) {
	s := newPiSupplementSession(nil)
	s.observe([]byte(`{"type":"agent_start"}`))
	s.observe([]byte(`{"type":"response","id":"initial-prompt","command":"prompt","success":true,"data":{"disposition":"handled"}}`))
	if s.isSettled() || !s.ready() {
		t.Fatal("handled prompt closed an independent active run")
	}
	s.observe([]byte(`{"type":"agent_settled"}`))
	if !s.isSettled() || s.ready() {
		t.Fatal("settlement not observed")
	}
}

func TestPiSupplementTransformedMessageStartAfterQueued(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.send(ctx, "change course") }()
	line, err := bufio.NewReader(r).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var cmd map[string]string
	if err := json.Unmarshal(line, &cmd); err != nil || cmd["type"] != "steer" || cmd["message"] != "change course" {
		t.Fatalf("command=%s err=%v", line, err)
	}
	// Pi queues the input-handler result and announces it before replying.
	transformedText := "change course\nAdditional context from transform"
	s.observe([]byte(`{"type":"queue_update","steering":["change course\nAdditional context from transform"],"followUp":[]}`))
	// queued alone must not complete delivery
	s.observe([]byte(`{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"queued"}}`))
	select {
	case err := <-result:
		t.Fatalf("queue acknowledged as delivery: %v", err)
	default:
	}
	s.observe([]byte(`{"type":"queue_update","steering":[],"followUp":[]}`))
	transformed, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": transformedText}}}})
	s.observe(transformed)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("transformed message_start after queued should deliver: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("steering blocked after transformed message_start")
	}
	s.stop()
}

func TestPiSupplementQueuedWithoutMessageStartFails(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.send(ctx, "change course") }()
	if _, err := bufio.NewReader(r).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	s.observe([]byte(`{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"queued"}}`))
	select {
	case err := <-result:
		t.Fatalf("queue alone must not deliver: %v", err)
	default:
	}
	// Run settles / ends with no user message_start — still not consumed.
	s.observe([]byte(`{"type":"agent_settled"}`))
	s.stop()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected failure without consumption evidence")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("steering blocked")
	}
}

func TestPiSupplementHandledDispositionDelivers(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.send(ctx, "change course") }()
	if _, err := bufio.NewReader(r).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	s.observe([]byte(`{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"handled"}}`))
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("handled disposition should deliver: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("steering blocked")
	}
	s.stop()
}

// startPiTestSteer starts one steer on an active session and returns once the
// command reached stdin, so tests can feed Pi events in any order.
func startPiTestSteer(t *testing.T, s *piSupplementSession, w io.Closer, r *io.PipeReader, text string) <-chan error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	t.Cleanup(func() { r.Close(); w.Close() })
	result := make(chan error, 1)
	go func() { result <- s.send(ctx, text) }()
	if _, err := bufio.NewReader(r).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	return result
}

func piTestUserMessage(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": text}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func piTestQueue(t *testing.T, steering ...string) []byte {
	t.Helper()
	if steering == nil {
		steering = []string{}
	}
	raw, err := json.Marshal(map[string]any{"type": "queue_update", "steering": steering, "followUp": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const piTestQueuedReply = `{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"queued"}}`

func requirePiSteerPending(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("steer resolved without its own consumption: %v", err)
	default:
	}
}

func requirePiSteerResult(t *testing.T, result <-chan error, wantSuccess bool) {
	t.Helper()
	select {
	case err := <-result:
		if (err == nil) != wantSuccess {
			t.Fatalf("result=%v wantSuccess=%v", err, wantSuccess)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("steering blocked")
	}
}

// Review finding: the initial prompt (or any other user message) after the
// queued reply is not a receipt for this steer.
func TestPiSupplementUnrelatedUserMessageIsNotReceipt(t *testing.T) {
	r, w := io.Pipe()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	result := startPiTestSteer(t, s, w, r, "change course")
	s.observe(piTestQueue(t, "change course"))
	s.observe([]byte(piTestQueuedReply))
	s.observe(piTestUserMessage(t, "original prompt, not the pending steer"))
	requirePiSteerPending(t, result)
	s.stop()
	requirePiSteerResult(t, result, false)
}

// Review finding: a transformed consumption that arrives before the queued
// reply must still be correlated once the reply arrives.
func TestPiSupplementTransformedConsumptionBeforeReply(t *testing.T) {
	r, w := io.Pipe()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	result := startPiTestSteer(t, s, w, r, "change course")
	s.observe(piTestQueue(t, "rewritten guidance"))
	s.observe(piTestQueue(t))
	s.observe(piTestUserMessage(t, "rewritten guidance"))
	requirePiSteerPending(t, result)
	s.observe([]byte(piTestQueuedReply))
	requirePiSteerResult(t, result, true)
}

// Without the reply, a transformed entry cannot be attributed: the steer may
// still be inside its input handler while an extension's entry is consumed.
// Fail closed rather than report unproven delivery.
func TestPiSupplementTransformedConsumptionWithoutReplyFailsClosed(t *testing.T) {
	r, w := io.Pipe()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	result := startPiTestSteer(t, s, w, r, "change course")
	s.observe(piTestQueue(t, "rewritten guidance"))
	s.observe(piTestUserMessage(t, "rewritten guidance"))
	s.stop()
	requirePiSteerResult(t, result, false)
}

// An extension can inject its own steering while the input handler for this
// steer runs. Pi queues this steer last, right before replying; consuming the
// injected entry does not deliver this steer.
func TestPiSupplementExtensionInjectedSteerIsNotReceipt(t *testing.T) {
	for _, consumeOwn := range []bool{false, true} {
		t.Run(map[bool]string{false: "own-unconsumed", true: "own-consumed"}[consumeOwn], func(t *testing.T) {
			r, w := io.Pipe()
			s := newPiSupplementSession(w)
			s.observe([]byte(`{"type":"agent_start"}`))
			result := startPiTestSteer(t, s, w, r, "change course")
			s.observe(piTestQueue(t, "UNRELATED_EXTENSION_INPUT"))
			s.observe(piTestQueue(t, "UNRELATED_EXTENSION_INPUT", "change course\nTransformed."))
			s.observe([]byte(piTestQueuedReply))
			s.observe(piTestQueue(t, "change course\nTransformed."))
			s.observe(piTestUserMessage(t, "UNRELATED_EXTENSION_INPUT"))
			requirePiSteerPending(t, result)
			if consumeOwn {
				s.observe(piTestQueue(t))
				s.observe(piTestUserMessage(t, "change course\nTransformed."))
			} else {
				s.observe([]byte(`{"type":"agent_settled","aborted":true}`))
				s.stop()
			}
			requirePiSteerResult(t, result, consumeOwn)
		})
	}
}

// Steering queued before this steer was written belongs to someone else, even
// when it is consumed while this steer is pending.
func TestPiSupplementPreexistingQueueEntryIsNotAttributed(t *testing.T) {
	r, w := io.Pipe()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	s.observe(piTestQueue(t, "earlier extension steer"))
	result := startPiTestSteer(t, s, w, r, "change course")
	s.observe(piTestQueue(t, "earlier extension steer", "change course"))
	s.observe(piTestUserMessage(t, "earlier extension steer"))
	s.observe([]byte(piTestQueuedReply))
	requirePiSteerPending(t, result)
	s.observe(piTestUserMessage(t, "change course"))
	requirePiSteerResult(t, result, true)
}

func TestPiQueueAdditions(t *testing.T) {
	for _, tc := range []struct {
		prev, next, want []string
	}{
		{nil, []string{"a"}, []string{"a"}},
		{[]string{"a"}, []string{"a", "b"}, []string{"b"}},
		{[]string{"a", "b"}, []string{"b"}, nil},
		{[]string{"a"}, []string{"a", "a"}, []string{"a"}},
		{[]string{"a", "b"}, nil, nil},
	} {
		if got := piQueueAdditions(tc.prev, tc.next); !slices.Equal(got, tc.want) {
			t.Fatalf("piQueueAdditions(%q, %q)=%q want %q", tc.prev, tc.next, got, tc.want)
		}
	}
}

func TestPiUserMessageText(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`"plain"`, "plain"},
		{`[{"type":"text","text":"a"},{"type":"image","data":"x"},{"type":"text","text":"b"}]`, "ab"},
		{`{"unexpected":true}`, ""},
	} {
		if got := piUserMessageText(json.RawMessage(tc.raw)); got != tc.want {
			t.Fatalf("piUserMessageText(%s)=%q want %q", tc.raw, got, tc.want)
		}
	}
}

// An unchanged copy of this steer is conclusive even before the reply.
func TestPiSupplementExactConsumptionBeforeReplyResolves(t *testing.T) {
	r, w := io.Pipe()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	result := startPiTestSteer(t, s, w, r, "change course")
	s.observe(piTestQueue(t, "change course"))
	s.observe(piTestQueue(t))
	s.observe(piTestUserMessage(t, "change course"))
	requirePiSteerResult(t, result, true)
}

// Before the reply, the latest queued entry can still be an extension's.
// Its consumption must not be attributed to this steer.
func TestPiSupplementInjectedConsumptionBeforeOwnQueueIsNotReceipt(t *testing.T) {
	r, w := io.Pipe()
	s := newPiSupplementSession(w)
	s.observe([]byte(`{"type":"agent_start"}`))
	result := startPiTestSteer(t, s, w, r, "change course")
	s.observe(piTestQueue(t, "UNRELATED_EXTENSION_INPUT"))
	s.observe(piTestQueue(t))
	s.observe(piTestUserMessage(t, "UNRELATED_EXTENSION_INPUT"))
	s.observe(piTestQueue(t, "change course\nTransformed."))
	s.observe([]byte(piTestQueuedReply))
	requirePiSteerPending(t, result)
	s.stop()
	requirePiSteerResult(t, result, false)
}
