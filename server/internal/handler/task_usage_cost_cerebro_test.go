package handler

// CEREBRO-PATCH(task-usage-gateway-cost-test): net-new cerebro test file for
// the gateway-cost preference helper (FIR-2405).

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/pricing"
)

// preferGatewayCost must return the gateway's exact spend when present, and
// fall back to the pricing-table estimate only when no gateway cost was stored.
func TestPreferGatewayCost(t *testing.T) {
	const model = "claude-sonnet-4-5"

	// Gateway reported a real charge — use it verbatim, ignore the estimate.
	if got := preferGatewayCost(4242, model, 1000, 2000, 0, 0); got != 4242 {
		t.Fatalf("stored cost should win: got %d, want 4242", got)
	}

	// No gateway cost (0) — fall back to the token estimate.
	want := pricing.ComputeCents(model, pricing.Usage{InputTokens: 1000, OutputTokens: 2000})
	if got := preferGatewayCost(0, model, 1000, 2000, 0, 0); got != want {
		t.Fatalf("zero stored cost should fall back to estimate: got %d, want %d", got, want)
	}

	// Negative/garbage stored cost is treated as absent (defensive).
	if got := preferGatewayCost(-5, model, 1000, 2000, 0, 0); got != want {
		t.Fatalf("negative stored cost should fall back to estimate: got %d, want %d", got, want)
	}
}
