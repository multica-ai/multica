package handler

// CEREBRO-PATCH(daemon-usage-wiring-guard): net-new cerebro tripwire test (FIR-2189).
//
// Twice now an upstream sync has silently deleted cerebro wiring inside the big
// daemon.go usage handlers: commit 1485f43df (MUL-3375) reverted GetIssueUsage to
// the token-only query (issues showed "0 kr") and dropped the context-footprint
// recorder (session context-window measurement went dark). A CEREBRO-PATCH marker
// cannot catch this — once the line is gone, there is no marker left to validate.
// This source-presence tripwire fails loudly the next time the wiring disappears,
// so the regression is caught in CI instead of in production.

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonUsageCerebroWiringPresent(t *testing.T) {
	t.Parallel()

	const path = "daemon.go"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Collapse all whitespace runs to single spaces so the check is immune to
	// gofmt re-alignment of struct-literal columns.
	body := strings.Join(strings.Fields(string(src)), " ")

	// Each entry: a required wiring fragment + why it must stay.
	required := []struct {
		fragment string
		why      string
	}{
		{
			fragment: "h.recordCerebroTaskContextFootprint(r.Context(), taskID, u)",
			why:      "ReportTaskUsage must persist the last-turn footprint, or session context-window measurement reads no data (FIR-1856).",
		},
		{
			fragment: "CostCents: u.CostCents",
			why:      "ReportTaskUsage must pass the gateway-reported spend through to task_usage, or daemon-runtime cost lands as 0 (FIR-2405).",
		},
		{
			fragment: "h.Queries.GetIssueUsageByModel(r.Context(), issue.ID)",
			why:      "GetIssueUsage must read the cost-aware per-model query, or issues show 0 kr (FIR-2405).",
		},
		{
			fragment: `"cost_cents": selfCostCents`,
			why:      "GetIssueUsage must return cost_cents to the frontend, or the issue cost display stays empty.",
		},
		{
			fragment: `"subtree_cost_cents": subtreeCostCents`,
			why:      "GetIssueUsage must return the subtree cost roll-up the frontend reads.",
		},
	}

	for _, r := range required {
		if !strings.Contains(body, r.fragment) {
			t.Errorf("daemon.go is missing required cerebro usage wiring:\n  fragment: %q\n  why: %s\n  (likely deleted by an upstream sync — see FIR-2189 / docs/cerebro-patches.md)", r.fragment, r.why)
		}
	}
}
