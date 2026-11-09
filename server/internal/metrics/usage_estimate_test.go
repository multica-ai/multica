package metrics

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

func ticks(v int64) *int64 { return &v }

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("cost = %.10f, want %.10f", got, want)
	}
}

func TestEstimateUsageCostPricesUncostedRowsFromTheTable(t *testing.T) {
	got := EstimateUsageCost([]UsageRow{{
		Provider: "claude", Model: "claude-opus-5-5",
		InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000,
	}})
	approx(t, got.CostUSD, 4+20+0.2+5)
	if len(got.UnpricedModels) != 0 {
		t.Fatalf("unpriced = %v", got.UnpricedModels)
	}
}

func TestEstimateUsageCostUsesProviderCharge(t *testing.T) {
	got := EstimateUsageCost([]UsageRow{
		// Priced by the provider: the charge wins over the table.
		{Provider: "claude", Model: "claude-opus-5-5", InputTokens: 1_000_000, CostUSDTicks: ticks(3 * CostUSDTicksPerUSD)},
		// A zero charge means "not priced", same as the web app.
		{Provider: "claude", Model: "claude-opus-5-5", InputTokens: 1_000_000, CostUSDTicks: ticks(0)},
	})
	approx(t, got.CostUSD, 3+4)
}

func TestEstimateUsageCostReportsUnpricedModels(t *testing.T) {
	got := EstimateUsageCost([]UsageRow{
		{Provider: "claude", Model: "claude-opus-5-5", InputTokens: 1_000_000},
		{Provider: "acme", Model: "mystery-1", InputTokens: 5},
		{Provider: "acme", Model: "mystery-1", OutputTokens: 5},
		{Provider: "", Model: "", InputTokens: 1},
		// A provider-priced row needs no rate.
		{Provider: "acme", Model: "billed-1", InputTokens: 7, CostUSDTicks: ticks(CostUSDTicksPerUSD)},
		// No tokens, no charge: nothing to price.
		{Provider: "acme", Model: "idle-1"},
	})
	approx(t, got.CostUSD, 4+1)
	want := []string{"acme/mystery-1", "unknown"}
	if len(got.UnpricedModels) != len(want) {
		t.Fatalf("unpriced = %v, want %v", got.UnpricedModels, want)
	}
	for i := range want {
		if got.UnpricedModels[i] != want[i] {
			t.Fatalf("unpriced = %v, want %v", got.UnpricedModels, want)
		}
	}
}

func TestEstimateUsageCostEmpty(t *testing.T) {
	got := EstimateUsageCost(nil)
	if got.CostUSD != 0 || len(got.UnpricedModels) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// The issue page prices with MODEL_PRICING in packages/views/runtimes/utils.ts
// and the CLI with this package's table. These are commonly used models; if
// one side's rate changes without the other, the CLI and the page disagree.
// Update both tables together.
func TestRatesMatchTheWebAppForModelsInUse(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	src, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "..", "..", "packages", "views", "runtimes", "utils.ts"))
	if err != nil {
		t.Fatalf("web app rate table not readable: %v", err)
	}
	for _, c := range []struct{ provider, model string }{
		{"claude", "claude-opus-5-5"},
		{"claude", "claude-sonnet-5-5"},
		{"codex", "gpt-6-astra"},
		{"codex", "gpt-6.1-terra"},
	} {
		t.Run(c.model, func(t *testing.T) {
			price, ok := PriceForRuntimeModel(c.provider, c.model)
			if !ok {
				t.Fatalf("no backend rate for %s/%s", c.provider, c.model)
			}
			re := regexp.MustCompile(`"` + regexp.QuoteMeta(c.model) + `":\s*\{\s*input:\s*([0-9.]+),\s*output:\s*([0-9.]+),\s*cacheRead:\s*([0-9.]+),\s*cacheWrite:\s*([0-9.]+)\s*\}`)
			m := re.FindSubmatch(src)
			if m == nil {
				t.Fatalf("no web app rate row for %s", c.model)
			}
			web := make([]float64, 4)
			for i := range web {
				web[i], _ = strconv.ParseFloat(string(m[i+1]), 64)
			}
			got := []float64{price.InputPerM, price.OutputPerM, price.CacheReadPerM, price.CacheWritePerM}
			for i, name := range []string{"input", "output", "cacheRead", "cacheWrite"} {
				if got[i] != web[i] {
					t.Errorf("%s %s: backend %v, web app %v", c.model, name, got[i], web[i])
				}
			}
		})
	}
}
