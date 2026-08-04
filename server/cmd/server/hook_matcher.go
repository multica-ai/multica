package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookMatcherTick is how often the matcher polls the outbox. It stays dormant
// (a cheap flag check) while automation_event_hooks is off.
const hookMatcherTick = 2 * time.Second

// runHookMatcher is the durable Event Hooks matcher loop (MUL-4332 PR3). It only
// claims and matches pending domain events when the automation_event_hooks flag
// is enabled, so with the default-off flag it does nothing and production
// behaviour is unchanged. The matcher only records queued/skipped decisions; it
// runs no actions (the executor is a later slice).
func runHookMatcher(ctx context.Context, svc *service.HookService, flags *featureflag.Service) {
	if svc == nil {
		return
	}
	ticker := time.NewTicker(hookMatcherTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !featureflags.EventHooksEnabled(ctx, flags) {
				continue
			}
			if _, err := svc.ClaimAndMatch(ctx, service.MatcherBatchSize); err != nil {
				slog.Warn("hook matcher tick failed", "error", err)
			}
		}
	}
}
