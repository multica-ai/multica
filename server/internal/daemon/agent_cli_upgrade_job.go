package daemon

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Drain timeout for agent-CLI upgrades: pause new claims, wait for in-flight
// tasks to finish, then upgrade. On timeout the job fails without killing tasks.
var agentCLIUpgradeDrainTimeout = 10 * time.Minute

var (
	errAgentCLIUpgradeBusy    = errors.New("another update or agent CLI upgrade is already in progress")
	errAgentCLIUpgradeDrainTO = errors.New("timed out waiting for running tasks to finish before agent CLI upgrade")
)

// handleAgentCLIUpgrade drains claims/tasks then runs UpgradeManagedAgentCLI.
func (d *Daemon) handleAgentCLIUpgrade(ctx context.Context, runtimeID string, pending *PendingAgentCLIUpgrade) {
	if d == nil || pending == nil {
		return
	}
	cli := strings.ToLower(strings.TrimSpace(pending.CLI))
	jobID := strings.TrimSpace(pending.ID)
	d.logger.Info("agent CLI upgrade requested", "runtime_id", runtimeID, "job_id", jobID, "cli", cli)

	// Soft Busy check: another update already owns the runtime. Keep this job
	// queued for the next heartbeat — never mark failed. A busy runtime is
	// waiting on another update, not rejecting this one.
	if d.updating.Load() {
		d.logger.Info("agent CLI upgrade deferred: runtime busy", "runtime_id", runtimeID, "job_id", jobID, "cli", cli)
		d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
			"status": "queued",
		})
		return
	}

	d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
		"status": "draining",
	})

	if err := d.beginAgentCLIUpgradeDrain(ctx); err != nil {
		if errors.Is(err, errAgentCLIUpgradeBusy) {
			// Lost the CAS race after the soft check — still requeue, not fail.
			d.logger.Info("agent CLI upgrade deferred: runtime busy", "runtime_id", runtimeID, "job_id", jobID, "cli", cli)
			d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
				"status": "queued",
			})
			return
		}
		d.logger.Warn("agent CLI upgrade drain failed", "runtime_id", runtimeID, "job_id", jobID, "error", err)
		d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}
	defer func() {
		d.releaseClaimBarrier()
		d.updating.Store(false)
	}()

	d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
		"status": "upgrading",
	})

	res, err := d.UpgradeManagedAgentCLI(ctx, cli)
	if err != nil {
		msg := err.Error()
		d.logger.Error("agent CLI upgrade failed", "runtime_id", runtimeID, "job_id", jobID, "cli", cli, "error", err)
		d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
			"status": "failed",
			"error":  msg,
		})
		return
	}

	current := strings.TrimSpace(res.Probe.Current)
	d.logger.Info("agent CLI upgrade succeeded", "runtime_id", runtimeID, "job_id", jobID, "cli", cli, "current", current)
	d.reportCLIUpgradeResult(ctx, runtimeID, jobID, map[string]any{
		"status":  "succeeded",
		"current": current,
	})
}

// beginAgentCLIUpgradeDrain pauses new claims and waits for in-flight work to
// finish. Unlike tryBeginServerUpdate it does not refuse when tasks are active
// — it drains them up to agentCLIUpgradeDrainTimeout without killing.
func (d *Daemon) beginAgentCLIUpgradeDrain(ctx context.Context) error {
	if !d.updating.CompareAndSwap(false, true) {
		return errAgentCLIUpgradeBusy
	}

	d.claimMu.Lock()
	d.pauseClaims = true
	d.claimMu.Unlock()

	deadline := time.Now().Add(agentCLIUpgradeDrainTimeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		d.claimMu.Lock()
		idle := d.claimsInFlight == 0 && d.activeTasks.Load() == 0
		d.claimMu.Unlock()
		if idle {
			return nil
		}
		if time.Now().After(deadline) {
			d.releaseClaimBarrier()
			d.updating.Store(false)
			return errAgentCLIUpgradeDrainTO
		}
		select {
		case <-ctx.Done():
			d.releaseClaimBarrier()
			d.updating.Store(false)
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *Daemon) reportCLIUpgradeResult(ctx context.Context, runtimeID, jobID string, payload map[string]any) {
	if d == nil || d.client == nil || jobID == "" {
		return
	}
	d.reportUpdateResultWithRetry(ctx, runtimeID, jobID, func(ctx context.Context) error {
		return d.client.ReportCLIUpgradeResult(ctx, runtimeID, jobID, payload)
	})
}
