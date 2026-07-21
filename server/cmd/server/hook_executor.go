package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookExecutorTick is how often the executor polls for runnable executions. It
// stays dormant (a cheap flag check) while automation_event_hooks is off.
const hookExecutorTick = 2 * time.Second

// runHookExecutor is the Event Hooks executor loop (MUL-4332 PR3 §7.2). It only runs
// actions when the automation_event_hooks flag is enabled, so with the default-off
// flag it does nothing and production behaviour is unchanged. The matcher produces
// `queued` executions; this loop leases them and runs their actions.
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
			if !featureflags.EventHooksEnabled(ctx, flags) {
				continue
			}
			if _, err := svc.ClaimAndRun(ctx, service.ExecutorBatchSize); err != nil {
				slog.Warn("hook executor tick failed", "error", err)
			}
		}
	}
}
