package engine

import (
	"context"
	"testing"
)

func TestRouterAlreadyConsumedNewKeepsLiteralRemainderRegression(t *testing.T) {
	h := newHarness(t)
	h.media.noMedia = true
	msg := p2pMessage(t)
	msg.Text = "/new literal body"
	msg.CommandText = "/new /new literal body"
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if h.binder.startCalls != 1 || h.binder.lastStart.Message.Text != "/new literal body" {
		t.Fatalf("one /new must rotate once and keep literal remainder: starts=%d body=%q", h.binder.startCalls, h.binder.lastStart.Message.Text)
	}
}
