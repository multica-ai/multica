// Package pricing converts model token usage into a cents-denominated cost.
//
// The price table is injected from the database-backed model registry
// (server/internal/cerebro/modelregistry) via SetTable — see table_cerebro.go.
// Unknown models fall back to the registry's fallback entry so budget caps
// over-estimate rather than under-estimate spend (fail-safe).
package pricing

// CEREBRO-PATCH(pricing-pricing): cerebro modification of upstream file

import (
	"log/slog"
	"math"
	"regexp"
	"strings"
	"sync"
)

// Pricing is per million tokens, denominated in USD-cents.
//
// Cents-per-mtok keeps the integer math clean: a 1M-token Opus output costs
// 7500 cents (= 75 USD) — no floating-point dollar conversions in the hot
// path, only at the final ComputeCents step.
type Pricing struct {
	InputCentsPerMtok      float64
	OutputCentsPerMtok     float64
	CacheReadCentsPerMtok  float64
	CacheWriteCentsPerMtok float64
}

// CEREBRO-PATCH(pricing-registry-table): FIR-2698 — the price table is no
// longer hardcoded here. It is injected via SetTable (table_cerebro.go) from
// the database-backed model registry, the single source for model prices.

// unknownModelLogged tracks which unknown model names we've already warned
// about to keep the logs from drowning during a long-running rollout where
// hundreds of tasks report the same new model.
var unknownModelLogged sync.Map

// Usage is the per-task token report from the agent runtime. Mirrors the
// fields of task_usage but kept independent of the db package so this
// package has no DB dependency.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// ComputeCents returns the cost in USD-cents for a single (model, usage)
// report. Returned cents are rounded up — partial cents tip toward the
// budget being exceeded sooner, which is the safer direction.
func ComputeCents(model string, u Usage) int64 {
	p, ok := lookup(model)
	if !ok {
		// Log once per distinct unknown model.
		if _, seen := unknownModelLogged.LoadOrStore(model, struct{}{}); !seen {
			slog.Warn("pricing: unknown model, using worst-case fallback",
				"model", model, "fallback", fallbackModelName()) // CEREBRO-PATCH(pricing-registry-table): fallback comes from the injected registry table.
		}
		p = fallbackPricing() // CEREBRO-PATCH(pricing-registry-table): fail-safe rate from the injected registry table.
	}

	const mtok = 1_000_000.0
	cost := (float64(u.InputTokens)*p.InputCentsPerMtok +
		float64(u.OutputTokens)*p.OutputCentsPerMtok +
		float64(u.CacheReadTokens)*p.CacheReadCentsPerMtok +
		float64(u.CacheWriteTokens)*p.CacheWriteCentsPerMtok) / mtok

	if cost <= 0 {
		return 0
	}
	return int64(math.Ceil(cost))
}

// Known reports whether the model has an explicit pricing entry. Useful for
// admin UIs that want to flag tasks costed against the fallback.
func Known(model string) bool {
	_, ok := lookup(model)
	return ok
}

// lookup normalizes the model name before consulting the table. Agent
// runtimes report models with mixed casing and occasional whitespace.
// Anthropic and OpenAI also ship dated snapshots (e.g.
// `claude-opus-4-7-20251030`, `gpt-5-2025-08-07`) that share pricing with
// the family — strip a trailing date / "latest" tag and retry on miss so
// `claude-opus-4-7-20251030` resolves to the `claude-opus-4-7` row.
func lookup(model string) (Pricing, bool) {
	key := strings.ToLower(strings.TrimSpace(model))
	// CEREBRO-PATCH(pricing-local-free): local Ollama models (gemma family, e.g. "gemma4:12b-it-qat") run on our own hardware — price at zero instead of the worst-case Opus fallback that would book free runs as the priciest tier.
	if strings.HasPrefix(key, "gemma") {
		return Pricing{}, true
	}
	if p, ok := tableGet(key); ok { // CEREBRO-PATCH(pricing-registry-table): lookup against the injected registry table.
		return p, true
	}
	if stripped := stripDateSuffix(key); stripped != key {
		if p, ok := tableGet(stripped); ok { // CEREBRO-PATCH(pricing-registry-table): lookup against the injected registry table.
			return p, true
		}
	}
	return Pricing{}, false
}

// dateSuffixPattern matches a trailing dated snapshot or `-latest` tag.
// Compiled once at package init so the hot path stays allocation-free.
var dateSuffixPattern = regexp.MustCompile(`-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$`)

// stripDateSuffix removes a trailing date / latest tag if present, returning
// the model identifier with the family-level suffix retained.
func stripDateSuffix(model string) string {
	return dateSuffixPattern.ReplaceAllString(model, "")
}
