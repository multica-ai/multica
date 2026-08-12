package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// hookMatcherTick is how often the matcher polls the outbox. It polls on every tick
// regardless of the flags — see runHookMatcher for why the gate cannot live here —
// so with Event Hooks off the work per tick is one bounded, indexed candidate read
// plus the claim/dispatch of whatever it finds, and no decisions.
const hookMatcherTick = 2 * time.Second

// runHookMatcher is the durable Event Hooks matcher loop (MUL-4332 PR3).
//
// The tick body is NOT gated here. The gate lives per candidate row, inside
// ClaimAndMatch, because `automation_event_hooks` is evaluated PER WORKSPACE: asking
// once here with the process root context has no workspace attached, so a
// workspace-targeted rule (allow_by: workspace_id) would match nothing and only a
// global override could enable the engine — for every workspace at once, which is
// exactly the canary this loop is supposed to support (review: workspace rollout).
//
// An event whose workspace is not enabled is claimed and dispatched with no
// decisions, so a disabled workspace can neither accumulate a backlog that starves
// the enabled one in the ordered candidate window, nor replay that backlog the
// moment it is switched on.
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
			if _, err := svc.ClaimAndMatch(ctx, service.MatcherBatchSize); err != nil {
				slog.Warn("hook matcher tick failed", "error", err)
			}
		}
	}
}
