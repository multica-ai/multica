package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

const (
	autopilotQuotaReconcileInterval   = time.Minute
	autopilotQuotaTerminalRecoveryAge = 10 * time.Minute
	// Manual/API dispatches have no durable retry owner. Six hours is far
	// beyond normal dispatch latency while still releasing a genuinely
	// abandoned slot before the entitlement period rolls over.
	autopilotQuotaPartialRecoveryAge = 6 * time.Hour
	autopilotQuotaReconcileBatch     = 100
	autopilotRunReconcileInterval    = time.Minute
	autopilotRunReconcileBatch       = 100
)

// runAutopilotRunReconciler is intentionally independent of quota being
// enabled: self-hosted deployments still need stale scheduled runs to release
// the single-flight guard and surface a terminal audit state.
func runAutopilotRunReconciler(ctx context.Context, svc *service.AutopilotService) {
	reconcile := func() {
		createdBefore := time.Now().UTC().Add(-service.AutopilotRunStaleAfter)
		reclaimed, err := svc.ReconcileStaleScheduledAutopilotRuns(
			ctx,
			createdBefore,
			time.Duration(service.RuntimeClaimFreshnessSeconds*float64(time.Second)),
			autopilotRunReconcileBatch,
		)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("autopilot run reconciler failed", "error", err)
			}
		} else if reclaimed > 0 {
			slog.Info("autopilot run reconciler reclaimed stale scheduled runs", "count", reclaimed)
		}
	}

	reconcile()
	ticker := time.NewTicker(autopilotRunReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

func runAutopilotQuotaReconciler(ctx context.Context, svc *service.AutopilotService) {
	ticker := time.NewTicker(autopilotQuotaReconcileInterval)
	defer ticker.Stop()
	for {
		now := time.Now()
		if settled, err := svc.ReconcileAutopilotQuotaReservations(
			ctx,
			now.Add(-autopilotQuotaTerminalRecoveryAge),
			now.Add(-autopilotQuotaPartialRecoveryAge),
			autopilotQuotaReconcileBatch,
		); err != nil {
			if ctx.Err() == nil {
				slog.Warn("autopilot quota reconciler failed", "error", err)
			}
		} else if settled > 0 {
			slog.Info("autopilot quota reconciler settled reservations", "count", settled)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
