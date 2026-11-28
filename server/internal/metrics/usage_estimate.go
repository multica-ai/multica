package metrics

import (
	"sort"
	"strings"
)

// Estimated USD for a set of per-(provider, model) usage rows, by
// the same rule the web app's estimateCost applies to a task's usage slices
// (packages/views/runtimes/utils.ts): a row the provider priced itself
// (cost_usd_ticks > 0) counts at that price; any other row is priced from
// the rate table. A row whose model has no rate is reported, not counted as
// zero. The browser's custom per-model rates are not visible here.

// UsageRow is one provider/model usage slice.
type UsageRow struct {
	Provider         string
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	// CostUSDTicks is the provider-reported charge; nil or 0 means unpriced.
	CostUSDTicks *int64
}

// UsageEstimate is the estimated cost of a set of usage rows.
type UsageEstimate struct {
	CostUSD float64
	// UnpricedModels lists models (provider-qualified, sorted, unique) whose
	// tokens needed a rate and had none. Their tokens are not in CostUSD.
	UnpricedModels []string
}

// EstimateUsageCost prices rows with PriceForRuntimeModel.
func EstimateUsageCost(rows []UsageRow) UsageEstimate {
	var total float64
	unpriced := map[string]struct{}{}
	for _, r := range rows {
		if r.CostUSDTicks != nil && *r.CostUSDTicks > 0 {
			total += float64(*r.CostUSDTicks) / CostUSDTicksPerUSD
			continue
		}
		if r.InputTokens <= 0 && r.OutputTokens <= 0 && r.CacheReadTokens <= 0 && r.CacheWriteTokens <= 0 {
			continue
		}
		price, ok := PriceForRuntimeModel(r.Provider, r.Model)
		if !ok {
			unpriced[unpricedKey(r.Provider, r.Model)] = struct{}{}
			continue
		}
		total += tokenCostUSD(r.InputTokens, price.InputPerM) +
			tokenCostUSD(r.OutputTokens, price.OutputPerM) +
			tokenCostUSD(r.CacheReadTokens, price.CacheReadPerM) +
			tokenCostUSD(r.CacheWriteTokens, price.CacheWritePerM)
	}
	models := make([]string, 0, len(unpriced))
	for m := range unpriced {
		models = append(models, m)
	}
	sort.Strings(models)
	return UsageEstimate{CostUSD: total, UnpricedModels: models}
}

func unpricedKey(provider, model string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	m := strings.TrimSpace(model)
	if m == "" {
		m = "unknown"
	}
	if p == "" || strings.HasPrefix(m, p+"/") {
		return m
	}
	return p + "/" + m
}
