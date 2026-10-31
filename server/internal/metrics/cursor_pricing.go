package metrics

import (
	"regexp"
	"strings"
)

// cursorModelPrices is what Cursor bills for each model in its catalog
// (cursor.com/docs/models-and-pricing), keyed by the base id cursorBaseModel
// leaves. Cursor's sheet is the bill even where it differs from the vendor
// rows in modelPrices, except that temporary promotional rates are not used:
// GPT-5.6 Sol keeps its $5 / $30 list rate (Fast at 2x). Where Cursor offers
// a Fast variant its sheet doesn't price, the `-fast` row carries the vendor's
// published Fast rate, since Cursor bills third-party models at API list
// price; other Fast variants stay unmapped rather than borrowing the cheaper
// standard rate. Where the sheet lists no cache-write rate, CacheWritePerM
// is 0.
//
// Columns are input, output, cache read, cache write — the same order as the
// `cursor/` rows in packages/views/runtimes/utils.ts, which this table mirrors;
// keep the two in sync.
var cursorModelPrices = buildCursorPrices([]struct {
	model                                string
	input, output, cacheRead, cacheWrite float64
}{
	{"auto", 1.25, 6, 0.25, 0},
	{"composer-2.5-fast", 3, 15, 0.5, 0},
	{"composer-2.5", 0.5, 2.5, 0.2, 0},
	{"composer-2-fast", 1.5, 7.5, 0.35, 0},
	{"composer-2", 0.5, 2.5, 0.2, 0},
	{"composer-1.5", 3.5, 17.5, 0.35, 0},
	{"composer-1", 1.25, 10, 0.125, 0},
	{"grok-4.7", 2, 6, 0.5, 0},
	{"grok-4.7-fast", 4, 12, 1, 0},
	{"grok-4.6", 2, 6, 0.5, 0},
	{"grok-4.6-fast", 4, 12, 1, 0},
	{"grok-4.5", 2, 6, 0.5, 0},
	{"grok-4.5-fast", 4, 18, 1, 0},

	{"claude-opus-5-5", 4, 20, 0.2, 5},
	{"claude-opus-5-5-fast", 8, 40, 0.4, 10},
	{"claude-opus-5", 5, 25, 0.5, 6.25},
	{"claude-opus-5-fast", 10, 50, 1, 12.5},
	{"claude-opus-4-8", 5, 25, 0.5, 6.25},
	{"claude-opus-4-8-fast", 10, 50, 1, 12.5},
	{"claude-opus-4-7", 5, 25, 0.5, 6.25},
	{"claude-opus-4-7-fast", 30, 150, 3, 37.5},
	{"claude-4.6-opus", 5, 25, 0.5, 6.25},
	{"claude-4.5-opus", 5, 25, 0.5, 6.25},
	{"claude-fable-5-1", 10, 50, 0.25, 12.5},
	{"claude-fable-5", 10, 50, 1, 12.5},
	{"claude-sonnet-5-5", 2, 10, 0.1, 2.5},
	{"claude-sonnet-5", 2, 10, 0.2, 2.5},
	{"claude-4.6-sonnet", 3, 15, 0.3, 3.75},
	{"claude-4.5-sonnet", 3, 15, 0.3, 3.75},
	{"claude-4-sonnet", 3, 15, 0.3, 3.75},
	{"claude-haiku-5-5", 0.1, 0.5, 0.01, 0.125},
	{"gpt-5.6-sol", 5, 30, 0.5, 6.25},
	{"gpt-5.6-sol-fast", 10, 60, 1, 12.5},
	{"gpt-5.6-terra", 2, 12, 0.2, 2.5},
	{"gpt-5.6-terra-fast", 4, 24, 0.4, 5},
	{"gpt-5.6-luna", 0.2, 1.2, 0.02, 0.25},
	{"gpt-5.6-luna-fast", 0.4, 2.4, 0.04, 0.5},
	{"gpt-5.5", 5, 30, 0.5, 0},
	{"gpt-5.5-fast", 12.5, 75, 1.25, 0},
	{"gpt-5.4", 2.5, 15, 0.25, 0},
	{"gpt-5.4-fast", 5, 30, 0.5, 0},
	{"gpt-5.4-mini", 0.75, 4.5, 0.075, 0},
	{"gpt-5.4-nano", 0.2, 1.25, 0.02, 0},
	{"gpt-5.3-codex", 1.75, 14, 0.175, 0},
	{"gpt-5.3-codex-fast", 3.5, 28, 0.35, 0},
	{"gpt-5.2", 1.75, 14, 0.175, 0},
	{"gpt-5.2-fast", 3.5, 28, 0.35, 0},
	{"gpt-5.1", 1.25, 10, 0.125, 0},
	{"gpt-5", 1.25, 10, 0.125, 0},
	{"gpt-5-fast", 2.5, 20, 0.25, 0},
	{"gpt-5-mini", 0.25, 2, 0.025, 0},
	{"gemini-4-argon", 2, 10, 0.1, 0},
	{"gemini-3.8-flash", 0.75, 3.75, 0.075, 0},
	{"gemini-3.7-flash", 0.75, 3.75, 0.075, 0},
	{"gemini-3.6-flash", 1.5, 7.5, 0.15, 0},
	{"gemini-3.5-flash", 1.5, 9, 0.15, 0},
	{"gemini-3.1-pro", 2, 12, 0.2, 0},
	{"gemini-3-flash", 0.5, 3, 0.05, 0},
	{"kimi-k3", 3, 15, 0.3, 0},
	{"kimi-k2.7-code", 0.95, 4, 0.19, 0},
	{"glm-5.2", 1.4, 4.4, 0.26, 0},
	{"deepseek-v41-flash-1017", 0.3, 1.2, 0.006, 0},
	{"muse-wonder-1.0", 4, 20, 0.2, 0},
	{"muse-spark-1.3", 1.25, 4.25, 0.15, 0},

	// The daemon reports the literal `cursor` when neither the result event
	// nor the runtime config names a model; Composer 2.5 Fast is the default.
	{"cursor", 3, 15, 0.5, 0},
})

func buildCursorPrices(rows []struct {
	model                                string
	input, output, cacheRead, cacheWrite float64
}) map[string]ModelPrice {
	prices := make(map[string]ModelPrice, len(rows))
	for _, r := range rows {
		prices[r.model] = ModelPrice{
			Provider:       "cursor",
			Model:          r.model,
			InputPerM:      r.input,
			OutputPerM:     r.output,
			CacheReadPerM:  r.cacheRead,
			CacheWritePerM: r.cacheWrite,
		}
	}
	return prices
}

var (
	cursorParamsRe        = regexp.MustCompile(`\[([^\]]*)\]$`)
	cursorFastParamRe     = regexp.MustCompile(`(?:^|,)\s*fast\s*=\s*true\s*(?:,|$)`)
	cursorVariantSuffixRe = regexp.MustCompile(`-(?:fast|thinking|none|minimal|low|medium|high|xhigh|extra-high|max)$`)
)

// cursorBaseModel splits a Cursor catalog id into its base model and whether
// it selects Fast mode. Variant parts come in no fixed order
// (`claude-opus-4-7-thinking-high-fast`, `claude-4.6-sonnet-medium-thinking`,
// `cursor-grok-4.6-high`) or as parameters
// (`claude-opus-4-8[context=1m,effort=high,fast=true]`). Effort and thinking
// don't change per-token rates; Fast does.
func cursorBaseModel(model string) (base string, fast bool) {
	id := strings.ToLower(strings.TrimSpace(model))
	if m := cursorParamsRe.FindStringSubmatchIndex(id); m != nil {
		fast = cursorFastParamRe.MatchString(id[m[2]:m[3]])
		id = id[:m[0]]
	}
	for {
		loc := cursorVariantSuffixRe.FindStringIndex(id)
		if loc == nil {
			break
		}
		if id[loc[0]:] == "-fast" {
			fast = true
		}
		id = id[:loc[0]]
	}
	return strings.TrimPrefix(id, "cursor-"), fast
}

// priceForCursorModel prices usage reported by the Cursor runtime. A Fast
// variant resolves only through its own `<base>-fast` row, never the base
// model's cheaper rate. A standard variant uses Cursor's row for the base
// model, then the vendor row for the same SKU.
func priceForCursorModel(model string) (ModelPrice, bool) {
	base, fast := cursorBaseModel(model)
	if fast {
		price, ok := cursorModelPrices[base+"-fast"]
		return price, ok
	}
	if price, ok := cursorModelPrices[base]; ok {
		return price, true
	}
	return PriceForModelAlias(base)
}

// PriceForRuntimeModel prices usage by the runtime provider that reported it.
// Cursor bills its catalog at its own rates, so Cursor rows resolve through
// cursorModelPrices; every other provider uses the vendor table.
func PriceForRuntimeModel(provider, model string) (ModelPrice, bool) {
	if strings.EqualFold(strings.TrimSpace(provider), "cursor") {
		return priceForCursorModel(model)
	}
	return PriceForModelAlias(model)
}
