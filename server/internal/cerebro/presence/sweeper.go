// TECH-3422
package presence

import (
	"context"
	"time"
)

// StartSweeper launches a background goroutine that periodically ages out
// members who have stopped pinging. Every `interval` it calls Tracker.Sweep and
// broadcasts a cerebro:presence {user_id, online:false} event for each user that
// went offline. It exits when ctx is cancelled.
//
// A non-positive interval falls back to 20s. The goroutine owns no state beyond
// the ticker; all presence state lives on the shared Tracker.
func (h *Handler) StartSweeper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, tr := range h.Tracker.Sweep() {
					h.broadcast(tr.WorkspaceID, tr.UserID, false)
				}
			}
		}
	}()
}
