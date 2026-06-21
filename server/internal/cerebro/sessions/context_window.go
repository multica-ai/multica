package sessions

// Context-window sizes per model — the denominator for "how full is the
// context window". This is the EXACT per-model spec, not a prefix guess: the
// earlier `strings.Contains(model,"claude") → 200k` was wrong for the Claude
// models that ship a 1M window (Sonnet 4.x), which is exactly the inaccuracy
// Jesper flagged. Keys mirror server/pkg/pricing.modelPricing so the two
// curated tables stay aligned; values are the model's documented max input
// window.
//
// Source of truth note: the Firtal gateway's /v1/models discovery does NOT
// return a context window today (it sends only id/owned_by/display_name), so we
// cannot read it from the provider yet. Until the runtime reports the live
// window per run, this curated table is the authority — maintained on deploy
// like the pricing table next to it.

import (
	"regexp"
	"strings"
)

// defaultContextWindow is the conservative fallback for a model we have not
// curated yet: the standard 200k Claude/OpenAI window. Better to under-state
// "fullness" against a smaller window than to over-promise headroom.
const defaultContextWindow int64 = 200_000

var modelContextWindows = map[string]int64{
	// Anthropic — Opus: 200k.
	"claude-opus-4-8": 200_000,
	"claude-opus-4-7": 200_000,
	"claude-opus-4-6": 200_000,
	"claude-opus-4-5": 200_000,
	"claude-opus-4-1": 200_000,
	"claude-opus-4":   200_000,

	// Anthropic — Sonnet 4.x ships the 1M long-context window.
	"claude-sonnet-4-6": 1_000_000,
	"claude-sonnet-4-5": 1_000_000,
	"claude-sonnet-4":   1_000_000,

	// Anthropic — Haiku: 200k.
	"claude-haiku-4-5": 200_000,
	"claude-haiku-3-5": 200_000,

	// OpenAI — gpt-5 family: 272k input window.
	"gpt-5.5":    272_000,
	"gpt-5":      272_000,
	"gpt-5-mini": 272_000,

	// Google — Gemini 2.5: 1M.
	"gemini-2.5-pro":   1_000_000,
	"gemini-2.5-flash": 1_000_000,
}

// dateSuffix matches a trailing dated snapshot or "-latest" tag, mirroring the
// pricing package so `claude-sonnet-4-5-20251101` resolves to the family row.
var dateSuffix = regexp.MustCompile(`-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$`)

// contextWindowForModel returns the model's documented context window, or the
// conservative default when the model is not curated. Normalization matches the
// pricing lookup (lowercase, trim, strip a trailing date/latest tag).
func contextWindowForModel(model string) int64 {
	key := strings.ToLower(strings.TrimSpace(model))
	if w, ok := modelContextWindows[key]; ok {
		return w
	}
	if stripped := dateSuffix.ReplaceAllString(key, ""); stripped != key {
		if w, ok := modelContextWindows[stripped]; ok {
			return w
		}
	}
	return defaultContextWindow
}
