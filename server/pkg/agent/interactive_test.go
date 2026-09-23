package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLiveControlIdempotencyAndTerminalFence(t *testing.T) {
	c := newLiveControl()
	c.setState(InteractionWorking)
	command := InteractionCommand{ID: "one", Kind: "interrupt", Activity: 1}
	done := make(chan error, 1)
	go func() { _, err := c.Submit(context.Background(), command); done <- err }()
	r := <-c.queue
	if c.finish() {
		t.Fatal("finished with accepted command pending")
	}
	c.setState(InteractionAwaitingInput)
	c.acknowledge(r, InteractionReceipt{Outcome: "applied"}, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	receipt, err := c.Submit(context.Background(), command)
	if err != nil || receipt.Outcome != "applied" {
		t.Fatalf("retry: %+v %v", receipt, err)
	}
	select {
	case <-c.queue:
		t.Fatal("retry redelivered command")
	default:
	}
	c.setState(InteractionWorking)
	if c.Snapshot().Activity != 2 {
		t.Fatal("continuation did not advance activity")
	}
	_, err = c.Submit(context.Background(), InteractionCommand{ID: "stale", Kind: "interrupt", Activity: 1})
	if !errors.Is(err, ErrInteractionStale) {
		t.Fatalf("stale: %v", err)
	}
	if !c.finish() {
		t.Fatal("finish")
	}
	_, err = c.Submit(context.Background(), InteractionCommand{ID: "new", Kind: "input", Text: "hello", Activity: 2})
	if !errors.Is(err, ErrInteractionClosed) {
		t.Fatalf("closed: %v", err)
	}
}

func TestLiveControlTimeoutDoesNotRedeliver(t *testing.T) {
	c := newLiveControl()
	c.setState(InteractionWorking)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	command := InteractionCommand{ID: "one", Kind: "input", Text: "hello", Activity: 1}
	_, err := c.Submit(ctx, command)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	r := <-c.queue
	c.acknowledge(r, InteractionReceipt{Outcome: "applied"}, nil)
	if _, err := c.Submit(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.queue:
		t.Fatal("retry redelivered")
	default:
	}
	command.Text = "different"
	if _, err := c.Submit(context.Background(), command); err == nil {
		t.Fatal("accepted mismatched idempotency key")
	}
}

func TestLiveControlCloseResolvesPending(t *testing.T) {
	c := newLiveControl()
	c.setState(InteractionWorking)
	done := make(chan error, 1)
	go func() {
		_, err := c.Submit(context.Background(), InteractionCommand{ID: "one", Kind: "interrupt", Activity: 1})
		done <- err
	}()
	<-c.queue
	c.close()
	c.close()
	if err := <-done; !errors.Is(err, ErrInteractionClosed) {
		t.Fatal(err)
	}
}
