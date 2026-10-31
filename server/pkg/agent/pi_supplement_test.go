package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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
	// queued alone must not complete delivery
	s.observe([]byte(`{"type":"response","id":"steer-1","command":"steer","success":true,"data":{"disposition":"queued"}}`))
	select {
	case err := <-result:
		t.Fatalf("queue acknowledged as delivery: %v", err)
	default:
	}
	// Pi input transform rewrites the user message text after accept.
	transformed := []byte(`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"change course\nAdditional context from transform"}]}}`)
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
