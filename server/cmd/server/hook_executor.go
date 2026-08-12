package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookExecutorTick is how often the executor polls for runnable executions. With
// execution off for every workspace the matcher records no `queued` rows at all, so
// the poll finds nothing and the tick costs one indexed lookup.
const hookExecutorTick = 2 * time.Second

// runHookExecutor is the Event Hooks executor loop (MUL-4332 PR3 §7.2).
//
// Like the matcher, the tick body is NOT gated here: `automation_event_hook_execution`
// is evaluated PER WORKSPACE against each claimed row, since the claim queue is
// global and one process-wide answer cannot be right for every workspace at once.
// A row outside the enabled set is handed straight back unrun (review: workspace
// rollout).
func runHookExecutor(ctx context.Context, svc *service.HookService, flags *featureflag.Service) {
	if svc == nil {
		return
	}
	ticker := time.NewTicker(hookExecutorTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := svc.ClaimAndRun(ctx, service.ExecutorBatchSize); err != nil {
				slog.Warn("hook executor tick failed", "error", err)
			}
		}
	}
}
