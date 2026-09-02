package metrics

import (
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/pricing"
)

// CostUSDTicksPerUSD is the scale of provider-reported costs: xAI reports
// whole ticks of 1e-10 USD. Declared here rather than imported from pkg/agent
// (which owns the wire-format parsing) so this package keeps no dependency on
// the agent runtime for a physical unit; the two must stay equal.
const CostUSDTicksPerUSD = 10_000_000_000

type ModelPrice struct {
	Provider       string
	Model          string
	InputPerM      float64
	CacheReadPerM  float64
	CacheWritePerM float64
	OutputPerM     float64
}

var bundledPrices = pricing.Bundled()

// claudeVersionEnd terminates a Claude family rule: at most one suffix that
// the frontend resolver normalizes away, and then the END of the id. Appending
// it keeps a rule from swallowing a later SKU in the same family — without it
// `claude-fable-5` also matches `claude-fable-5-1`, whose cache reads are a
// quarter of Fable 5's, so those reads bill at 4x.
//
// The admitted suffixes are exactly what `stripContextTag` and `stripDate`
// remove in packages/views/runtimes/utils.ts before its exact-key lookup, so
// both sides accept the same suffix forms. (Only the suffixes: the Claude
// rules are still substring matches, so a malformed PREFIX is out of scope
// here.) The trailing `$` is what makes that true and is not optional: these rules are substring matches, so an
// alternative that merely starts a suffix still matches when arbitrary text
// follows it (`claude-fable-5-1-latest-preview`, `claude-fable-5-1[1m]junk`),
// which is the silent tier-borrowing this constant exists to prevent. The
// bracket form requires a complete tag for the same reason.
//
// A date snapshot carrying a context tag (`claude-fable-5-20260401[1m]`) is
// covered by the tag-stripping retry in PriceForModelAlias, so it does not
// need a combined alternative here.
//
// Anything else — another version digit, a `-preview`-style qualifier — is a
// distinct SKU at an unknown rate and stays unmapped until it gets a row of
// its own, the same "every catalog SKU needs its own row" rule the frontend
// table states.
const claudeVersionEnd = `(?:-20\d{6}|-20\d{2}-\d{2}-\d{2}|-latest|\[[^\]]+\])?$`

var modelAliasRules = []struct {
	re       *regexp.Regexp
	priceKey string
}{
	// Anchored exact-match: the effort is carried in a separate field, so the
	// model id is the bare slug. Anchoring to `$` keeps unknown variants
	// (`gpt-5.6-luna-pro`, `gpt-5.6-luna/x`) out of these rows. The `.` is a
	// LITERAL dot, not the `[.-]` class the older rows use — the real Codex
	// slug is always dotted (`gpt-5.6-luna`), and the frontend resolver in
	// utils.ts does NOT dash-normalize, so a dashed `gpt-5-6-luna` must surface
	// as unmapped on both sides rather than silently borrowing a tier here.
	{regexp.MustCompile(`(^|/|:)gpt-5\.6-sol$`), "openai:gpt-5.6-sol"},
	{regexp.MustCompile(`(^|/|:)gpt-5\.6-terra$`), "openai:gpt-5.6-terra"},
	{regexp.MustCompile(`(^|/|:)gpt-5\.6-luna$`), "openai:gpt-5.6-luna"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]5$|^gpt-5-5$`), "openai:gpt-5.5"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4($|-2026-03-05|-xhigh)`), "openai:gpt-5.4"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4-mini($|[^a-z0-9])`), "openai:gpt-5.4-mini"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]3-codex$`), "openai:gpt-5.3-codex"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]2-codex$`), "openai:gpt-5.2-codex"},
	{regexp.MustCompile(`claude-sonnet-5|claude-5-sonnet`), "anthropic:claude-sonnet-5"},
	// Fable 5.1 shares Fable 5's $10 / $50 and $12.50 cache write but prices
	// cache reads at 0.025x input ($0.25) instead of the standard 0.1x, so it
	// needs its own row, and both rules end at their own version
	// (claudeVersionEnd) so neither can swallow the other's ids.
	{regexp.MustCompile(`claude-fable-5[-.]1` + claudeVersionEnd), "anthropic:claude-fable-5-1"},
	{regexp.MustCompile(`claude-fable-5` + claudeVersionEnd), "anthropic:claude-fable-5"},
	{regexp.MustCompile(`claude-opus-5`), "anthropic:claude-opus-5"},
	{regexp.MustCompile(`claude-opus-4[-.]8`), "anthropic:claude-opus-4.8"},
	{regexp.MustCompile(`claude-opus-4[-.]7`), "anthropic:claude-opus-4.7"},
	{regexp.MustCompile(`claude-opus-4[-.]6`), "anthropic:claude-opus-4.6"},
	{regexp.MustCompile(`claude-opus-4[-.]5`), "anthropic:claude-opus-4.5"},
	{regexp.MustCompile(`claude-sonnet-4[-.]6|claude-4[-.]6-sonnet`), "anthropic:claude-sonnet-4.6"},
	{regexp.MustCompile(`claude-sonnet-4[-.]5|claude-4[-.]5-sonnet`), "anthropic:claude-sonnet-4.5"},
	{regexp.MustCompile(`claude-haiku-4[-.]5`), "anthropic:claude-haiku-4.5"},
	{regexp.MustCompile(`deepseek-v4-pro`), "deepseek:v4-pro"},
	{regexp.MustCompile(`deepseek-v4-flash|^deepseek-chat$|^deepseek-reasoner$`), "deepseek:v4-flash"},
	{regexp.MustCompile(`minimax-m2[.]7.*highspeed|highspeed.*minimax-m2[.]7`), "minimax:m2.7-highspeed"},
	{regexp.MustCompile(`minimax-m2[.]7`), "minimax:m2.7"},
	{regexp.MustCompile(`gemini-3-flash`), "google:gemini-3-flash"},
	{regexp.MustCompile(`gemini-3[.]1-pro`), "google:gemini-3.1-pro"},
	{regexp.MustCompile(`gemini-2[.]5-pro`), "google:gemini-2.5-pro"},
	{regexp.MustCompile(`gemini-2[.]5-flash`), "google:gemini-2.5-flash"},
	// Anchored exact-match, dotted spelling only — same rule as the gpt-5.6
	// rows above. The frontend resolver does not dash-normalize non-Anthropic
	// ids, so a dashed `grok-4-5` must surface as unmapped on both sides
	// rather than silently borrowing a tier here.
	{regexp.MustCompile(`(^|/|:)grok-4\.6$`), "xai:grok-4.6"},
	{regexp.MustCompile(`(^|/|:)grok-4\.5$`), "xai:grok-4.5"},
	{regexp.MustCompile(`(^|/|:)grok-4\.3$`), "xai:grok-4.3"},
	{regexp.MustCompile(`(^|/|:)grok-build-0\.1$`), "xai:grok-build-0.1"},
	{regexp.MustCompile(`(^|/|:)grok-4\.20-multi-agent-0309$`), "xai:grok-4.20-multi-agent-0309"},
	{regexp.MustCompile(`(^|/|:)grok-4\.20-0309-reasoning$`), "xai:grok-4.20-0309-reasoning"},
	{regexp.MustCompile(`(^|/|:)grok-4\.20-0309-non-reasoning$`), "xai:grok-4.20-0309-non-reasoning"},
	// Alibaba Qwen. All rules are anchored so unknown suffixed variants
	// (`qwen3.7-plus-extra`, `qwen3.8-max-preview-extra`) stay unmapped;
	// an optional complete bracket tag `[…]` with at least one character
	// inside is admitted to match the frontend's behavior of stripping the
	// context tag (`\[[^\]]+\]$` in packages/views/runtimes/utils.ts), so
	// empty tags like `qwen3.7-plus[]` stay unmapped on both sides.
	// qwen3.8-max stays anchored so `qwen3.8-max-preview` (and its `[1m]`
	// variant) never borrows the GA tier.
	{regexp.MustCompile(`(^|/|:)qwen3[.-]7-plus(\[[^\]]+\])?$`), "alibaba:qwen3.7-plus"},
	{regexp.MustCompile(`(^|/|:)qwen3[.-]6-flash(\[[^\]]+\])?$`), "alibaba:qwen3.6-flash"},
	{regexp.MustCompile(`(^|/|:)qwen3[.-]8-max(\[[^\]]+\])?$`), "alibaba:qwen3.8-max"},

	// Kimi K3. Anchored so the distinct CodeBuddy SKU `kimi-k3-1` stays
	// unmapped; `kimi-code/k3` (Kimi Code CLI) resolves via the `/k3$` form.
	{regexp.MustCompile(`(^|/|:)kimi-k3$`), "moonshotai:kimi-k3"},
	{regexp.MustCompile(`(^|/|:)k3$`), "moonshotai:kimi-k3"},
	{regexp.MustCompile(`(^|/|:)k3(-256k)?$`), "moonshotai:kimi-k3"},
	// Volcengine Ark `ark-code-latest` is deliberately absent: it is a
	// console-switchable rolling alias across model families, not a stable
	// model identity, so it stays unmapped.
}

// contextTagRe matches a trailing context-window variant tag such as the
// `[1m]` Claude Code appends to the model id. A complete bracket tag with at
// least one character inside, anchored at the end — the same shape the
// frontend's `stripContextTag` strips (`\[[^\]]+\]$` in
// packages/views/runtimes/utils.ts), so empty tags (`model[]`) and non-tag
// trailing brackets (`model[`) stay unmapped on both sides.
var contextTagRe = regexp.MustCompile(`\[[^\]]+\]$`)

func matchModelAlias(model string) (ModelPrice, bool) {
	for _, rule := range modelAliasRules {
		if !rule.re.MatchString(model) {
			continue
		}
		parts := strings.SplitN(rule.priceKey, ":", 2)
		id := parts[1]
		if parts[0] == "deepseek" || parts[0] == "minimax" {
			id = parts[0] + "-" + id
		}
		row, ok := pricing.Resolve(bundledPrices, nil, id, parts[0])
		if !ok {
			continue
		}
		return ModelPrice{Provider: parts[0], Model: parts[1], InputPerM: row.Input, OutputPerM: row.Output, CacheReadPerM: row.CacheRead, CacheWritePerM: row.CacheWrite}, true
	}
	return ModelPrice{}, false
}

func PriceForModelAlias(model string) (ModelPrice, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if price, ok := matchModelAlias(model); ok {
		return price, true
	}
	// The raw id did not resolve: a harness-appended context-window tag
	// (`kimi-k3[1m]`, `grok-4.5[1m]`) is the same SKU at the same tier, so
	// retry against the bare id. The anchored Codex / Grok / Kimi rules end at
	// `$`, so without this a bracketed variant would take the unpriced branch
	// in RecordLLMUsage. Only ever turns a miss into a hit — the raw form is
	// tried first, so an explicit bracketed rule still wins.
	//
	// Exactly ONE tag, matching the frontend: `canonicalCandidates` in
	// packages/views/runtimes/utils.ts strips a single trailing tag and does
	// not re-strip the result. Retrying a doubly-tagged id would peel `[2m]`
	// off `claude-fable-5[1m][2m]` and let the leftover `[1m]` satisfy a rule
	// that already, correctly, rejected the raw form — the dashboard leaves
	// that id unmapped, so pricing it here would put two different costs on
	// one usage row. A second tag means the id is not a shape we recognise.
	if stripped := contextTagRe.ReplaceAllString(model, ""); stripped != model {
		if contextTagRe.MatchString(stripped) {
			return ModelPrice{}, false
		}
		return matchModelAlias(stripped)
	}
	return ModelPrice{}, false
}

func tokenCostUSD(tokens int64, pricePerM float64) float64 {
	if tokens <= 0 || pricePerM <= 0 {
		return 0
	}
	return float64(tokens) * pricePerM / 1_000_000
}
