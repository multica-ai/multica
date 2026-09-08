package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// channelPushRetention bounds how long a push stays replyable. A reply
	// arriving a week later is not a decision anyone is waiting on, and the
	// app inbox is the durable record either way.
	channelPushRetention = 7 * 24 * time.Hour
	// channelPushSweepInterval is hourly: the table is small and nothing
	// downstream is latency-sensitive.
	channelPushSweepInterval = time.Hour
)

// channelPushExpirer is the slice of Queries this sweeper drives, kept as an
// interface so the loop can be tested without a database.
type channelPushExpirer interface {
	DeleteExpiredChannelPushMessages(ctx context.Context, before pgtype.Timestamptz) (int64, error)
}

// runChannelPushSweeper deletes channel_push_message rows older than
// channelPushRetention. Without this loop the table only grows: nothing else
// ever deletes a row from it.
func runChannelPushSweeper(ctx context.Context, q channelPushExpirer) {
	ticker := time.NewTicker(channelPushSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := pgtype.Timestamptz{Time: time.Now().Add(-channelPushRetention), Valid: true}
			n, err := q.DeleteExpiredChannelPushMessages(ctx, cutoff)
			if err != nil {
				slog.Warn("channel push sweep failed", "error", err)
				continue
			}
			if n > 0 {
				slog.Info("channel push sweep", "deleted", n)
			}
		}
	}
}
