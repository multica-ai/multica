package dashboard

import (
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestSumDashboardUsageCostTracksRuntimeTokens(t *testing.T) {
	rows := []cerebrodb.DashboardUsageCostRowsInPeriodRow{{
		Model: "claude-sonnet-4-6", InputTokens: 1_000_000,
	}}
	if got := sumDashboardUsageCost(rows); got <= 0 {
		t.Fatalf("sumDashboardUsageCost() = %d, want positive calculated cost", got)
	}

	rows[0].CostCents = 42
	if got := sumDashboardUsageCost(rows); got != 42 {
		t.Fatalf("sumDashboardUsageCost() = %d, want exact gateway cost 42", got)
	}
}
