package metrics

// ModelPrice is one model's list price in USD per million tokens, used to
// price Prometheus token counters.
type ModelPrice struct {
	Provider       string
	Model          string
	InputPerM      float64
	CacheReadPerM  float64
	CacheWritePerM float64
	OutputPerM     float64
}

// PriceForModelAlias resolves a raw model string to its price row.
//
// CEREBRO-PATCH(metrics-pricing-registry): FIR-2698 — the hardcoded price map
// and alias regex table that lived here moved to the database-backed model
// registry (server/internal/cerebro/modelregistry). Resolution is delegated
// to the hook installed at startup; before wiring, everything is unpriced.
func PriceForModelAlias(model string) (ModelPrice, bool) {
	if fn := registryPriceLookupFn(); fn != nil { // CEREBRO-PATCH(metrics-pricing-registry): delegate to the registry lookup hook.
		return fn(model)
	}
	return ModelPrice{}, false
}

func tokenCostUSD(tokens int64, pricePerM float64) float64 {
	if tokens <= 0 || pricePerM <= 0 {
		return 0
	}
	return float64(tokens) * pricePerM / 1_000_000
}
