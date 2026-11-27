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
)

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
