package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAutopilotSchedulerLoopContinuesAfterRecoveredTickPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticks := make(chan time.Time, 2)
	done := make(chan struct{})

	var tickCalls atomic.Int32
	var recoveryCalls atomic.Int32

	go func() {
		runAutopilotSchedulerLoop(ctx, ticks, func() {
			safeSchedulerTick(func() {
				if tickCalls.Add(1) == 1 {
					panic("boom")
				}
				cancel()
			}, func() {
				recoveryCalls.Add(1)
			})
		})
		close(done)
	}()

	ticks <- time.Now()
	ticks <- time.Now()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler loop did not survive a recovered panic")
	}

	if got := tickCalls.Load(); got != 2 {
		t.Fatalf("expected 2 ticks to run, got %d", got)
	}
	if got := recoveryCalls.Load(); got != 1 {
		t.Fatalf("expected 1 recovery callback after panic, got %d", got)
	}
}
