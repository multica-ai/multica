// CEREBRO-PATCH(pricing-snapshot-export): FIR-2471 expose the price table +
// version so the registry's hourly pricing pull can treat cerebro as the single
// pricing source — no mirrored copy lives outside this package.
package pricing

// Version stamps every snapshot the pricing endpoint serves. Bump it whenever
// modelPricing changes so the registry can tell costed rows apart by table
// revision. Format: <source>-<date>.<rev>; the date mirrors the public price
// list cerebro's Anthropic rates were verified against (see modelPricing).
const Version = "cerebro-2026-05-11.1"

// ModelRate is one model's list price, USD-cents per million tokens. JSON tags
// are the wire contract the registry pricing client parses.
type ModelRate struct {
	InputCentsPerMtok      float64 `json:"input_cents_per_mtok"`
	OutputCentsPerMtok     float64 `json:"output_cents_per_mtok"`
	CacheReadCentsPerMtok  float64 `json:"cache_read_cents_per_mtok"`
	CacheWriteCentsPerMtok float64 `json:"cache_write_cents_per_mtok"`
}

// TableSnapshot is the full price table plus the fail-safe fallback model and
// the table version. Returned by Snapshot for the pricing endpoint.
type TableSnapshot struct {
	Version       string               `json:"version"`
	FallbackModel string               `json:"fallback_model"`
	Models        map[string]ModelRate `json:"models"`
}

// Snapshot returns a copy of the in-code price table. The map is freshly
// allocated so callers cannot mutate the package-level table.
func Snapshot() TableSnapshot {
	models := make(map[string]ModelRate, len(modelPricing))
	for name, p := range modelPricing {
		models[name] = ModelRate{
			InputCentsPerMtok:      p.InputCentsPerMtok,
			OutputCentsPerMtok:     p.OutputCentsPerMtok,
			CacheReadCentsPerMtok:  p.CacheReadCentsPerMtok,
			CacheWriteCentsPerMtok: p.CacheWriteCentsPerMtok,
		}
	}
	return TableSnapshot{Version: Version, FallbackModel: fallbackModel, Models: models}
}
