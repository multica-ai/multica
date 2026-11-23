package main

import (
	"context"
	"testing"
	"time"
)

type commentDeliveryStartupProbe struct {
	called chan int32
	cancel context.CancelFunc
}

func (p *commentDeliveryStartupProbe) DrainCommentDeliveryObligations(ctx context.Context, batch int32) error {
	p.called <- batch
	p.cancel()
	return ctx.Err()
}

func TestCommentDeliverySweeperDrainsAtStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &commentDeliveryStartupProbe{called: make(chan int32, 1), cancel: cancel}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCommentDeliveryObligationSweeper(ctx, probe)
	}()
	select {
	case batch := <-probe.called:
		if batch <= 0 || batch > 50 {
			t.Fatalf("startup batch=%d, want bounded positive batch", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("startup recovery waited for the periodic tick")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled sweeper did not exit")
	}
}
