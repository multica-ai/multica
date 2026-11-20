package daemon

import (
	"context"
	"errors"
	"net/http"
)

// queuedServerUpdate is a deferrable update from the Runtimes page that
// arrived while agent work was running. The daemon holds it — without pausing
// claims — and starts it the moment its last task finishes.
//
// The daemon holds at most one, and refuses a different update while holding
// or installing one. One daemon is one machine, so this is what keeps a
// machine to a single update even when its runtimes span several workspaces.
//
// The server stays the source of truth: it re-offers a waiting update on every
// heartbeat (so a restarted daemon picks it up again) and refuses the
// "scheduled" / "running" reports with 409 once it was cancelled or expired.
// The daemon installs only after the server accepted "running".
type queuedServerUpdate struct {
	runtimeID     string
	id            string
	targetVersion string
	// reportedTasks is the task count last reported as "scheduled", or -1.
	reportedTasks int64
	// installing is set once the server accepted "running". The update stays
	// held while it installs, so a re-offer that was already in flight is
	// recognised as the same update rather than refused as a second one.
	installing bool
}

const errAnotherUpdateQueued = "another runtime update is already queued or in progress on this machine"

func (d *Daemon) handleQueuedUpdate(ctx context.Context, runtimeID string, update *PendingUpdate) {
	d.queuedUpdateMu.Lock()
	defer d.queuedUpdateMu.Unlock()

	held := d.queuedUpdate
	if held != nil && held.installing {
		// A re-offer of the installing update is ignored, not refused.
		if held.id != update.ID {
			d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
				"status": "failed",
				"error":  errAnotherUpdateQueued,
			})
		}
		return
	}
	if held != nil && held.id != update.ID {
		// The held update may have been cancelled or expired since; a
		// refused "scheduled" report says so and frees the slot.
		if !isUpdateWithdrawn(d.reportQueuedUpdateWaiting(ctx, held)) {
			d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
				"status": "failed",
				"error":  errAnotherUpdateQueued,
			})
			return
		}
		d.queuedUpdate, held = nil, nil
	}
	if held == nil {
		if d.updating.Load() {
			d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
				"status": "failed",
				"error":  errAnotherUpdateQueued,
			})
			return
		}
		d.queuedUpdate = &queuedServerUpdate{
			runtimeID:     runtimeID,
			id:            update.ID,
			targetVersion: update.TargetVersion,
			reportedTasks: -1,
		}
	}
	d.tryQueuedUpdateLocked(ctx)
}

// finishActiveTask ends a task's activeTasks slot and, when the daemon just
// became idle, starts a held update.
func (d *Daemon) finishActiveTask() {
	if d.activeTasks.Add(-1) == 0 {
		go func() {
			d.queuedUpdateMu.Lock()
			defer d.queuedUpdateMu.Unlock()
			d.tryQueuedUpdateLocked(d.recoveryContext())
		}()
	}
}

func (d *Daemon) tryQueuedUpdateLocked(ctx context.Context) {
	held := d.queuedUpdate
	if held == nil || held.installing {
		return
	}

	switch d.tryBeginServerUpdate(ctx) {
	case serverUpdateAlreadyRunning:
		// Auto-update owns the machine; the next offer tries again.
		return
	case serverUpdateRuntimeBusy:
		if active := d.activeTasks.Load(); active != held.reportedTasks {
			d.logger.Info("update queued until the daemon is idle", "runtime_id", held.runtimeID, "update_id", held.id, "active_tasks", active)
			err := d.reportQueuedUpdateWaiting(ctx, held)
			switch {
			case err == nil:
				held.reportedTasks = active
			case isUpdateWithdrawn(err):
				d.queuedUpdate = nil
			}
		}
		return
	}

	// Idle, with claims paused. Install only if the server still wants it.
	err := d.reportUpdateResult(ctx, held.runtimeID, held.id, map[string]any{"status": "running"})
	if err != nil {
		d.releaseClaimBarrier()
		d.updating.Store(false)
		if isUpdateWithdrawn(err) {
			d.logger.Info("queued update dropped: cancelled or expired", "runtime_id", held.runtimeID, "update_id", held.id)
			d.queuedUpdate = nil
		}
		return
	}
	held.installing = true
	d.logger.Info("CLI update requested", "runtime_id", held.runtimeID, "update_id", held.id, "target_version", held.targetVersion)
	go func() {
		d.runAcquiredServerUpdate(ctx, held.runtimeID, held.id, held.targetVersion)
		// Reached on failure; a successful install restarts the daemon.
		d.queuedUpdateMu.Lock()
		if d.queuedUpdate == held {
			d.queuedUpdate = nil
		}
		d.queuedUpdateMu.Unlock()
	}()
}

func (d *Daemon) reportQueuedUpdateWaiting(ctx context.Context, held *queuedServerUpdate) error {
	return d.reportUpdateResult(ctx, held.runtimeID, held.id, map[string]any{
		"status":       "scheduled",
		"active_tasks": d.activeTasks.Load(),
	})
}

// isUpdateWithdrawn reports the server refusing a held update: cancelled,
// expired or finished (409), or gone (404).
func isUpdateWithdrawn(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusConflict || reqErr.StatusCode == http.StatusNotFound)
}
