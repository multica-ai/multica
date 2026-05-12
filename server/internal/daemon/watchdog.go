package daemon

import (
	"context"
	"fmt"
	"os"
	"time"
)

func (d *Daemon) watchdogLoop(ctx context.Context) {
	interval := d.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	threshold := 2 * interval
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if d.stalledGap(threshold) {
				gap := time.Since(time.Unix(0, d.lastHeartbeatAt.Load()))
				// Best-effort: bypass slog/tint because the logger may be stuck on
				// a blocked stderr write. Cap the attempt so exit always happens.
				done := make(chan struct{})
				go func() {
					fmt.Fprintf(os.Stderr, "watchdog: heartbeat stalled (gap=%s threshold=%s) - exiting for KeepAlive to respawn\n",
						gap.Round(time.Second), threshold)
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(500 * time.Millisecond):
				}
				os.Exit(2)
			}
		}
	}
}

// stalledGap returns true when the recorded last-heartbeat timestamp is older
// than threshold. It returns false before the first heartbeat has been recorded.
func (d *Daemon) stalledGap(threshold time.Duration) bool {
	last := d.lastHeartbeatAt.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) > threshold
}
