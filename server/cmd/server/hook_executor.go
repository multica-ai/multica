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

// runHookExecutor is the Event Hooks executor loop (MUL-4332 PR3 §7.2). It is gated
// on its OWN default-off switch, separate from the one that opens the policy API,
// dry-run and the matcher: enabling the engine for shadow evaluation must never
// start performing real side effects. Only automation_event_hook_execution (on top
// of automation_event_hooks) lets this loop claim queued executions.
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
			if !featureflags.EventHookExecutionEnabled(ctx, flags) {
				continue
			}
			if _, err := svc.ClaimAndRun(ctx, service.ExecutorBatchSize); err != nil {
				slog.Warn("hook executor tick failed", "error", err)
			}
		}
	}
}
