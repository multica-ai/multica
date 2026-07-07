package engine

import (
	"context"
	"testing"
)

func busyReplies(h *harness) int {
	n := 0
	for _, r := range h.replier.calls() {
		if r.Outcome == OutcomeAgentBusy {
			n++
		}
	}
	return n
}

func TestRouter_FlushAtCapacity_RepliesAgentBusy(t *testing.T) {
	h := newHarness(t)
	h.reader.maxConcurrent = 1
	h.reader.running = 1

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.tasks.wasCalled() {
		t.Fatalf("the run must still be enqueued")
	}
	// Inline flush (no batcher) emits the busy notice synchronously.
	if busyReplies(h) != 1 {
		t.Fatalf("expected 1 AgentBusy reply, got %d", busyReplies(h))
	}
	// A task WILL run — the typing indicator must stay until it settles.
	if h.typing.settledCalls() != 0 {
		t.Fatalf("busy notice must not clear the typing indicator")
	}
}

func TestRouter_FlushAtCapacity_CooldownSuppressesRepeat(t *testing.T) {
	h := newHarness(t)
	h.reader.maxConcurrent = 1
	h.reader.running = 1

	first := p2pMessage(t)
	if err := h.router.Handle(context.Background(), first); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second := p2pMessage(t)
	second.EventID = "evt-2"
	second.MessageID = "om-2"
	if err := h.router.Handle(context.Background(), second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if busyReplies(h) != 1 {
		t.Fatalf("busy notice must be rate-limited per session, got %d", busyReplies(h))
	}
}

func TestRouter_FlushWithCapacity_NoBusyNotice(t *testing.T) {
	h := newHarness(t)
	h.reader.maxConcurrent = 2
	h.reader.running = 1

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if busyReplies(h) != 0 {
		t.Fatalf("free capacity must not produce a busy notice, got %d", busyReplies(h))
	}
}
