// Package pricing converts model token usage into a cents-denominated cost.
//
// The price table lives in code rather than the database because it is
// provider pricing — it changes when we deploy, not when a workspace owner
// edits a setting. Unknown models fall back to the most expensive entry so
// budget caps over-estimate rather than under-estimate spend (fail-safe).
package pricing

// CEREBRO-PATCH(pricing-pricing): cerebro modification of upstream file

import (
	"log/slog"
	"math"
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

// modelPricing is the lookup table. Keys are normalized (lowercase, version
// suffixes intact). Add new entries here when we onboard a new model.
//
// Numbers below reflect public list pricing as of the deploy that ships this
// table. They will drift; treat updates as a regular code change.
var modelPricing = map[string]Pricing{
	// Anthropic — claude.ai/console list pricing.
	"claude-opus-4-7":   {InputCentsPerMtok: 1500, OutputCentsPerMtok: 7500, CacheReadCentsPerMtok: 150, CacheWriteCentsPerMtok: 1875},
	"claude-opus-4-6":   {InputCentsPerMtok: 1500, OutputCentsPerMtok: 7500, CacheReadCentsPerMtok: 150, CacheWriteCentsPerMtok: 1875},
	"claude-sonnet-4-6": {InputCentsPerMtok: 300, OutputCentsPerMtok: 1500, CacheReadCentsPerMtok: 30, CacheWriteCentsPerMtok: 375},
	"claude-haiku-4-5":  {InputCentsPerMtok: 80, OutputCentsPerMtok: 400, CacheReadCentsPerMtok: 8, CacheWriteCentsPerMtok: 100},

	// OpenAI — gpt-5 family. Cache read priced at 10% of input per OpenAI's
	// public schedule; no separate cache-write line (writes are free).
	"gpt-5":      {InputCentsPerMtok: 125, OutputCentsPerMtok: 1000, CacheReadCentsPerMtok: 12.5, CacheWriteCentsPerMtok: 0},
	"gpt-5-mini": {InputCentsPerMtok: 25, OutputCentsPerMtok: 200, CacheReadCentsPerMtok: 2.5, CacheWriteCentsPerMtok: 0},

	// Google — Gemini 2.5. Cache pricing per Vertex AI's published rates.
	"gemini-2.5-pro":   {InputCentsPerMtok: 125, OutputCentsPerMtok: 1000, CacheReadCentsPerMtok: 31.25, CacheWriteCentsPerMtok: 0},
	"gemini-2.5-flash": {InputCentsPerMtok: 7.5, OutputCentsPerMtok: 30, CacheReadCentsPerMtok: 1.875, CacheWriteCentsPerMtok: 0},
}

// fallbackModel is used when ComputeCents is asked for a model not in the
// table. We pick the priciest entry so unknown models don't accidentally
// undercount and slip past a cap.
const fallbackModel = "claude-opus-4-7"

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
				"model", model, "fallback", fallbackModel)
		}
		p = modelPricing[fallbackModel]
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
func lookup(model string) (Pricing, bool) {
	p, ok := modelPricing[strings.ToLower(strings.TrimSpace(model))]
	return p, ok
}
