package handler

// CEREBRO-PATCH(task-usage-gateway-cost): net-new cerebro helper that prefers
// the gateway-reported spend over the token estimate (FIR-2405).

import "github.com/multica-ai/multica/server/pkg/pricing"

// preferGatewayCost returns the exact spend the Firtal AI gateway reported for a
// (model) usage row when it is present (> 0), and otherwise falls back to the
// pricing-table estimate computed from tokens.
//
// The managed gateway returns the real charge (`firtal.cost_cents`) on every
// call and the runtime now persists it on task_usage.cost_cents. Daemon and
// other non-gateway runtimes report no cost, so their rows store 0 and
// transparently fall back to the token estimate — identical to the pre-patch
// behaviour. This keeps the per-issue / per-subtree / per-session cost shown in
// the UI equal to what the workspace was actually billed whenever the gateway
// supplied it, instead of a recomputed approximation that drifts when gateway
// pricing changes.
func preferGatewayCost(storedCents int64, model string, input, output, cacheRead, cacheWrite int64) int64 {
	if storedCents > 0 {
		return storedCents
	}
	return pricing.ComputeCents(model, pricing.Usage{
		InputTokens:      input,
		OutputTokens:     output,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
	})
}
