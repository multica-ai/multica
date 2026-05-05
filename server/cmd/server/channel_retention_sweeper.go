package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/multica-ai/multica/server/internal/service/channel"
)

// Phase 2 channel-retention sweep — daily background job that soft-deletes
// channel_message rows older than the effective retention (per-channel
// override, falling back to workspace.channel_retention_days). Workspaces
// with NULL retention at both levels are skipped — that's "retain forever".
//
// Schedule: fires once per UTC day at the hour configured via the
// CHANNEL_RETENTION_RUN_HOUR env var (default 03:00 UTC). The default
// matches the spec; production deployments can shift to a quieter window.
//
// We deliberately do NOT use a 5-minute ticker here even though that's the
// pattern used for runtime sweeping — retention is heavy work (potentially
// thousands of soft-deletes) and the run-once-a-day cadence makes the
// extra implementation complexity worth it. Drift from clock skew or DST
// is tolerated because each run is idempotent (the inner WHERE clause
// filters already-deleted rows).
//
// Restart semantics: a server restart at the boundary minute could cause
// the sweep to run twice in one day. The sweep is idempotent so that's
// safe, just slightly wasteful — same trade-off the runtime sweeper makes.

const (
	// defaultRetentionRunHour is the UTC hour for the daily sweep. 03:00 UTC
	// matches the channels spec; sites with US-pacific business hours often
	// shift this to ~10:00 UTC ("quiet pre-dawn for west coast").
	defaultRetentionRunHour = 3
	// retentionBatchSize bounds each individual delete query. Smaller =
	// more queries but shorter transactions; larger = fewer queries but
	// longer per-query lock duration. 1000 matches the spec.
	retentionBatchSize = 1000
)

// runChannelRetentionSweeper schedules and dispatches the daily sweep.
// Blocks until ctx is cancelled.
func runChannelRetentionSweeper(ctx context.Context, msgSvc *channel.MessageService) {
	runHour := defaultRetentionRunHour
	if v := os.Getenv("CHANNEL_RETENTION_RUN_HOUR"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h >= 0 && h < 24 {
			runHour = h
		} else {
			slog.Warn("CHANNEL_RETENTION_RUN_HOUR ignored", "value", v, "default", runHour)
		}
	}
	slog.Info("channel retention sweeper started", "run_hour_utc", runHour)

	for {
		now := time.Now().UTC()
		next := nextRetentionRunAt(now, runHour)
		wait := time.Until(next)
		select {
		case <-ctx.Done():
			slog.Info("channel retention sweeper stopped")
			return
		case <-time.After(wait):
			runStart := time.Now().UTC()
			stats, err := msgSvc.RunRetentionSweep(ctx, runStart, retentionBatchSize)
			elapsed := time.Since(runStart)
			if err != nil {
				slog.Error("channel retention sweep failed",
					"error", err,
					"channels_scanned", stats.ChannelsScanned,
					"messages_deleted", stats.MessagesDeleted,
					"elapsed_ms", elapsed.Milliseconds(),
				)
			} else {
				slog.Info("channel retention sweep complete",
					"channels_scanned", stats.ChannelsScanned,
					"messages_deleted", stats.MessagesDeleted,
					"elapsed_ms", elapsed.Milliseconds(),
				)
			}
		}
	}
}

// nextRetentionRunAt returns the next time-of-day-at-hour fire time after
// `now`, in `now`'s timezone. Pure function so tests can pin time.
//
//	nextRetentionRunAt(2026-01-01 02:30, hour=3) → 2026-01-01 03:00
//	nextRetentionRunAt(2026-01-01 03:30, hour=3) → 2026-01-02 03:00
//	nextRetentionRunAt(2026-01-01 03:00, hour=3) → 2026-01-02 03:00
//
// The "exactly at the hour mark" boundary is treated as already-fired so a
// scheduler that drifts a millisecond past doesn't double-fire.
func nextRetentionRunAt(now time.Time, hour int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
