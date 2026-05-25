package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func runUsageHourlyRollup(ctx context.Context, pool *pgxpool.Pool) {
	const interval = 5 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var rows int64
			err := pool.QueryRow(ctx, "SELECT rollup_task_usage_hourly()").Scan(&rows)
			if err != nil {
				slog.Warn("usage hourly rollup tick failed", "error", err)
			} else if rows > 0 {
				slog.Info("usage hourly rollup tick", "rows", rows)
			}
		}
	}
}
