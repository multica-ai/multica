package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// stalledChannelPushExpirer models a database that stopped answering: the
// delete blocks until its context is done.
type stalledChannelPushExpirer struct{}

func (stalledChannelPushExpirer) DeleteExpiredChannelPushMessages(ctx context.Context, _ pgtype.Timestamptz) (int64, error) {
	<-ctx.Done()
	return 0, nil
}

// TestChannelPushSweeperStopsWithItsContext keeps shutdown clean: the loop
// must exit on context cancellation rather than waiting out a full interval.
// Mirrors TestSourceContextSweeperStopsWithItsContext.
func TestChannelPushSweeperStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		runChannelPushSweeper(ctx, stalledChannelPushExpirer{})
		close(stopped)
	}()
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("channel push sweeper did not stop with its context")
	}
}
