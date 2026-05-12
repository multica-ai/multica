package daemon

import (
	"context"
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
				d.logger.Error("watchdog: heartbeat stalled - exiting for KeepAlive to respawn",
					"gap", time.Since(time.Unix(0, d.lastHeartbeatAt.Load())), "threshold", threshold)
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
