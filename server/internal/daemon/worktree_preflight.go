package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// waitForWorktreeReplay runs at the claim handoff, before environment setup.
// A blocked source is waiting work, not an agent failure: keep the same task
// and its lease so failure notifications cannot recursively wake coordinators.
func (d *Daemon) waitForWorktreeReplay(ctx context.Context, task Task, assignment *localDirectoryAssignment, logger *slog.Logger) bool {
	interval := d.cancelPollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stopLease func()
	var cancelled <-chan struct{}
	waiting := false
	lastReason := ""
	defer func() {
		if stopLease != nil {
			stopLease()
		}
		if waiting {
			d.resourceWaitTasks.Add(-1)
		}
	}()
	for {
		scanCtx, stopScan := context.WithTimeout(waitCtx, 30*time.Second)
		check, err := execenv.InspectUntrackedReplay(scanCtx, assignment.AbsPath)
		stopScan()
		if err != nil && waitCtx.Err() == nil && !waiting {
			// Non-budget errors keep the existing preparation failure path.
			// Do not permanently park a missing repository or a Git error.
			logger.Warn("worktree preflight could not inspect source", "error", err)
			return false
		}
		if waitCtx.Err() != nil {
			// The server owns terminal cancellation; never send a new failure
			// for it. On shutdown the prepare lease expires and reclaim keeps
			// the same task recoverable.
			return true
		}
		if err == nil && check.Err() == nil {
			select {
			case <-cancelled:
				return true
			default:
			}
			return false
		}
		reason := fmt.Sprintf("%s: %s", assignment.DisplayName(), check.Err())
		if err != nil {
			reason = assignment.DisplayName() + ": source inspection unavailable; waiting for a fresh measurement"
		}
		if reason != lastReason {
			if err := d.client.MarkTaskWaitingLocalDirectory(waitCtx, task.ID, reason); err != nil {
				logger.Warn("worktree preflight: could not publish wait reason", "error", err)
				// Retry publishing as well as inspecting. Never prepare a source
				// known to be unreplayable merely because the server is offline.
			} else {
				if !waiting {
					d.resourceWaitTasks.Add(1)
				}
				waiting = true
				lastReason = reason
				logger.Info("worktree preflight: waiting for source cleanup", "reason", reason)
			}
		}
		if stopLease == nil {
			stopLease = d.startTaskPrepareLeaseExtender(waitCtx, task, logger)
			cancelled = d.watchTaskCancellation(waitCtx, task.ID, interval, logger)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return true
		case <-cancelled:
			timer.Stop()
			return true
		case <-timer.C:
		}
	}
}
