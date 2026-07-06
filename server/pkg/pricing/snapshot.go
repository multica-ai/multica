// CEREBRO-PATCH(pricing-snapshot-export): FIR-2471 expose the price table +
// version so the registry's hourly pricing pull can treat cerebro as the single
// pricing source — no mirrored copy lives outside this package.
package pricing

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

// Snapshot returns a copy of the injected registry price table. The map is
// freshly allocated so callers cannot mutate the package-level table. The
// version stamp is "registry-<semver>" — it changes on every approved
// registry merge, which is what the hourly pull keys revisions on.
func Snapshot() TableSnapshot {
	return tableSnapshot() // CEREBRO-PATCH(pricing-registry-table): snapshot is built from the injected registry table.
}
