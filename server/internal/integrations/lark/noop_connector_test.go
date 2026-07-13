package lark

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestNoopConnectorRunBlocksUntilContextCancel(t *testing.T) {
	t.Parallel()

	c := NewNoopConnector(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, Installation{}, func(context.Context, InboundMessage) (DispatchResult, error) {
			t.Errorf("noop connector must not emit events")
			return DispatchResult{}, nil
		})
	}()

	// The connector must NOT return while the ctx is live.
	select {
	case err := <-done:
		t.Fatalf("Run returned before ctx cancel: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned non-nil error after cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of ctx cancel")
	}
}

func TestNoopConnectorFactoryReturnsSameConnector(t *testing.T) {
	t.Parallel()

	factory := NoopConnectorFactory(slog.New(slog.NewTextHandler(io.Discard, nil)))

	c1, err := factory(Installation{})
	if err != nil {
		t.Fatalf("factory call 1: %v", err)
	}
	c2, err := factory(Installation{})
	if err != nil {
		t.Fatalf("factory call 2: %v", err)
	}
	if c1 != c2 {
		// Sharing the connector is what lets the logger be allocated
		// exactly once; if a future refactor changes this, also revisit
		// whether per-installation state is needed.
		t.Fatalf("expected factory to share one connector; got %p vs %p", c1, c2)
	}
}
