package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// heldServerUpdate is a deferrable update from the Runtimes page that arrived
// while agent work was running. The daemon keeps it and starts it the moment
// its last task finishes, so the update lands in the first idle gap instead of
// waiting for an offer to happen to arrive during one.
//
// Holding it never pauses claiming: a busy machine keeps taking work, exactly
// as before the request arrived. The server stays the source of truth — it
// re-offers the request every 30 seconds (a daemon that restarted picks it up
// again), and it answers the "running" report with 409 once the request was
// cancelled or expired, which is how a held request learns to drop itself.
type heldServerUpdate struct {
	runtimeID     string
	id            string
	targetVersion string
	// reportedTasks is the active-task count last sent as "scheduled", or -1
	// before the first report. Re-offers only send a report when it changed.
	reportedTasks int64
}

// holdServerUpdate records an offered deferrable update. A re-offer of the
// held request keeps its report state; a different request replaces it.
func (d *Daemon) holdServerUpdate(runtimeID string, update *PendingUpdate) {
	d.heldUpdateMu.Lock()
	defer d.heldUpdateMu.Unlock()
	if d.heldUpdate != nil && d.heldUpdate.id == update.ID {
		return
	}
	d.heldUpdate = &heldServerUpdate{
		runtimeID:     runtimeID,
		id:            update.ID,
		targetVersion: update.TargetVersion,
		reportedTasks: -1,
	}
}

func (d *Daemon) snapshotHeldUpdate() (heldServerUpdate, bool) {
	d.heldUpdateMu.Lock()
	defer d.heldUpdateMu.Unlock()
	if d.heldUpdate == nil {
		return heldServerUpdate{}, false
	}
	return *d.heldUpdate, true
}

func (d *Daemon) dropHeldUpdate(id string) {
	d.heldUpdateMu.Lock()
	defer d.heldUpdateMu.Unlock()
	if d.heldUpdate != nil && d.heldUpdate.id == id {
		d.heldUpdate = nil
	}
}

func (d *Daemon) setHeldUpdateReported(id string, tasks int64) {
	d.heldUpdateMu.Lock()
	defer d.heldUpdateMu.Unlock()
	if d.heldUpdate != nil && d.heldUpdate.id == id {
		d.heldUpdate.reportedTasks = tasks
	}
}

// finishActiveTask decrements activeTasks and wakes heldServerUpdateLoop when
// the daemon has just become idle.
func (d *Daemon) finishActiveTask() {
	if d.activeTasks.Add(-1) == 0 {
		select {
		case d.updateIdle <- struct{}{}:
		default:
		}
	}
}

func (d *Daemon) heldServerUpdateLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.updateIdle:
			d.tryHeldServerUpdate(ctx)
		}
	}
}

// tryHeldServerUpdate starts the held update if the daemon is idle, and
// otherwise tells the server it is still waiting.
func (d *Daemon) tryHeldServerUpdate(ctx context.Context) {
	held, ok := d.snapshotHeldUpdate()
	if !ok {
		return
	}

	// Something else already moved this daemon to the target — auto-update,
	// or an upgrade before the restart that lost the held request. Only an
	// exact match counts: an older target is a deliberate downgrade, which the
	// direct-download path installs by tag.
	if sameReleaseVersion(held.targetVersion, d.cfg.CLIVersion) {
		d.dropHeldUpdate(held.id)
		d.logger.Info("queued update already satisfied", "runtime_id", held.runtimeID, "update_id", held.id,
			"target_version", held.targetVersion, "current", d.cfg.CLIVersion)
		d.reportUpdateResult(ctx, held.runtimeID, held.id, map[string]any{
			"status": "completed",
			"output": fmt.Sprintf("Already on %s", d.cfg.CLIVersion),
		})
		return
	}

	switch d.tryBeginServerUpdate(ctx) {
	case serverUpdateAlreadyRunning:
		// Another update owns the machine (auto-update, auto-reload, or a
		// concurrent attempt at this same request). The next idle signal or
		// re-offer tries again.
		return
	case serverUpdateRuntimeBusy:
		d.reportHeldUpdateWaiting(ctx, held)
		return
	}

	// The "running" report is the server's go-ahead: it refuses with 409 once
	// the request was cancelled or expired, and accepts it explicitly
	// otherwise. A bare "ok" comes from a server that predates queued updates
	// and did not check for cancellation. Without an acceptance, keep holding
	// and let the next re-offer try again rather than install an update
	// nobody may still want.
	accepted, err := d.reportHeldUpdate(ctx, held, map[string]any{"status": "running"})
	if !accepted {
		if isUpdateWithdrawn(err) {
			d.dropHeldUpdate(held.id)
			d.logger.Info("queued update dropped: cancelled or expired on the server", "runtime_id", held.runtimeID, "update_id", held.id)
		}
		d.releaseClaimBarrier()
		d.updating.Store(false)
		return
	}
	d.dropHeldUpdate(held.id)
	d.runAcquiredServerUpdate(ctx, held.runtimeID, held.id, held.targetVersion)
}

// reportHeldUpdateWaiting reports "scheduled" with the machine-wide task
// count, once per change, so the Runtimes page can show what it waits for.
func (d *Daemon) reportHeldUpdateWaiting(ctx context.Context, held heldServerUpdate) {
	active := d.activeTasks.Load()
	if active == held.reportedTasks {
		return
	}
	d.logger.Info("update queued until the daemon is idle", "runtime_id", held.runtimeID, "update_id", held.id, "active_tasks", active)
	accepted, err := d.reportHeldUpdate(ctx, held, map[string]any{
		"status":       "scheduled",
		"active_tasks": active,
	})
	switch {
	case accepted:
		d.setHeldUpdateReported(held.id, active)
	case isUpdateWithdrawn(err):
		d.dropHeldUpdate(held.id)
		d.logger.Info("queued update dropped: cancelled or expired on the server", "runtime_id", held.runtimeID, "update_id", held.id)
	}
}

// reportHeldUpdate sends a "running" or "scheduled" report and says whether
// the server explicitly accepted it.
func (d *Daemon) reportHeldUpdate(ctx context.Context, held heldServerUpdate, payload map[string]any) (bool, error) {
	status, _ := payload["status"].(string)
	var resp struct {
		Accepted string `json:"accepted"`
	}
	err := d.reportUpdateResultWithRetry(ctx, held.runtimeID, held.id, func(ctx context.Context) error {
		resp.Accepted = ""
		return d.client.ReportUpdateResultInto(ctx, held.runtimeID, held.id, payload, &resp)
	})
	return err == nil && resp.Accepted == status, err
}

// isUpdateWithdrawn is the server saying the held request is gone: cancelled
// or expired (409) or unknown (404). Any other refusal — a 400 from a server
// that does not know "scheduled", say — leaves it held.
func isUpdateWithdrawn(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusConflict || reqErr.StatusCode == http.StatusNotFound)
}

// sameReleaseVersion reports whether a and b name the same release, ignoring
// a "v" prefix. Anything that is not a release version never matches.
func sameReleaseVersion(a, b string) bool {
	if !isReleaseVersion(a) || !isReleaseVersion(b) {
		return false
	}
	return !isNewerVersion(a, b) && !isNewerVersion(b, a)
}
