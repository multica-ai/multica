// CEREBRO (FIR-2698): the price table is no longer hardcoded in this package —
// it is injected from the model registry (server/internal/cerebro/
// modelregistry), which loads it from the database at startup and republishes
// it on every approved change. This file is net-new cerebro code (cerebro-zone
// carve-out via the _cerebro suffix); pricing.go keeps only the computation
// and normalization logic.
package pricing

import "sync"

var (
	tableMu       sync.RWMutex
	tableModels   map[string]Pricing
	tableFallback string
	tableVersion  string
)

// failSafePricing is the absolute worst-case rate (Claude Opus 4.1 list
// price) used only in the window before the registry has been loaded — e.g.
// during startup or in unit tests that never call SetTable. Fail-expensive:
// budget caps over-estimate rather than under-estimate. This is a safety
// constant, not a price table — the registry is the single source of prices.
var failSafePricing = Pricing{
	InputCentsPerMtok:      1500,
	OutputCentsPerMtok:     7500,
	CacheReadCentsPerMtok:  150,
	CacheWriteCentsPerMtok: 1875,
}

const failSafeVersion = "registry-not-loaded"

// SetTable atomically replaces the process-wide price table. Called by the
// model registry store on load and after every merge/rollback.
func SetTable(models map[string]Pricing, fallbackModel, version string) {
	copied := make(map[string]Pricing, len(models))
	for k, v := range models {
		copied[k] = v
	}
	tableMu.Lock()
	tableModels = copied
	tableFallback = fallbackModel
	tableVersion = version
	tableMu.Unlock()
}

// tableGet looks up a normalized key in the injected table.
func tableGet(key string) (Pricing, bool) {
	tableMu.RLock()
	defer tableMu.RUnlock()
	p, ok := tableModels[key]
	return p, ok
}

// fallbackPricing returns the fail-safe rate for unknown models: the injected
// fallback model's row, or the static worst-case constant before load.
func fallbackPricing() Pricing {
	tableMu.RLock()
	defer tableMu.RUnlock()
	if p, ok := tableModels[tableFallback]; ok {
		return p
	}
	return failSafePricing
}

// fallbackModelName names the fallback row for log lines.
func fallbackModelName() string {
	tableMu.RLock()
	defer tableMu.RUnlock()
	if tableFallback != "" {
		return tableFallback
	}
	return "static-fail-safe"
}

// tableSnapshot builds the wire snapshot for the /api/cerebro/pricing
// endpoint from the injected table. The JSON shape is unchanged from the
// pre-registry contract the firtal-data-registry pulls hourly.
func tableSnapshot() TableSnapshot {
	tableMu.RLock()
	defer tableMu.RUnlock()
	models := make(map[string]ModelRate, len(tableModels))
	for name, p := range tableModels {
		models[name] = ModelRate{
			InputCentsPerMtok:      p.InputCentsPerMtok,
			OutputCentsPerMtok:     p.OutputCentsPerMtok,
			CacheReadCentsPerMtok:  p.CacheReadCentsPerMtok,
			CacheWriteCentsPerMtok: p.CacheWriteCentsPerMtok,
		}
	}
	version := tableVersion
	if version == "" {
		version = failSafeVersion
	}
	return TableSnapshot{Version: version, FallbackModel: tableFallback, Models: models}
}
