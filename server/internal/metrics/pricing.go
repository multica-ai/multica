package metrics

import (
	"regexp"
	"strings"
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

var modelPrices = map[string]ModelPrice{
	// GPT-5.6 series and GPT-6 (Codex `codex` provider): standard short-context
	// rates from developers.openai.com/api/docs/pricing (2026-10-31). GPT-5.6
	// Sol keeps its $5 / $30 list rate; the page's temporary promotional rate is
	// deliberately not used. For 5.6+ (including Astra) cache read is generally
	// the 90%-off cached-input rate (0.1x input). Cache write is billed at 1.25x
	// the uncached input rate. Earlier OpenAI SKUs don't bill cache writes
	// separately, so their written tokens bill as ordinary input and
	// CacheWritePerM mirrors InputPerM. Codex app-server v0.147 reports
	// cache-write input separately while including it in the raw input total;
	// the collector normalizes those into mutually exclusive billing buckets.
	"openai:gpt-6-astra": {Provider: "openai", Model: "gpt-6-astra", InputPerM: 10.00, CacheReadPerM: 1.00, CacheWritePerM: 12.50, OutputPerM: 50.00},
	// Standard short-context rates from developers.openai.com/api/docs/models/.
	// GPT-6.1 Sol and Terra cache at 0.05x input, not GPT-6 Sol's 0.1x.
	"openai:gpt-6.1-sol":   {Provider: "openai", Model: "gpt-6.1-sol", InputPerM: 2.00, CacheReadPerM: 0.10, CacheWritePerM: 2.50, OutputPerM: 10.00},
	"openai:gpt-6.1-terra": {Provider: "openai", Model: "gpt-6.1-terra", InputPerM: 1.00, CacheReadPerM: 0.05, CacheWritePerM: 1.25, OutputPerM: 5.00},
	"openai:gpt-6-sol":     {Provider: "openai", Model: "gpt-6-sol", InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.50, OutputPerM: 10.00},
	"openai:gpt-6-luna":    {Provider: "openai", Model: "gpt-6-luna", InputPerM: 0.10, CacheReadPerM: 0.01, CacheWritePerM: 0.125, OutputPerM: 0.50},
	"openai:gpt-5.6-sol":   {Provider: "openai", Model: "gpt-5.6-sol", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 30.00},
	"openai:gpt-5.6-terra": {Provider: "openai", Model: "gpt-5.6-terra", InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.50, OutputPerM: 12.00},
	"openai:gpt-5.6-luna":  {Provider: "openai", Model: "gpt-5.6-luna", InputPerM: 0.20, CacheReadPerM: 0.02, CacheWritePerM: 0.25, OutputPerM: 1.20},
	"openai:gpt-5.5":       {Provider: "openai", Model: "gpt-5.5", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 5.00, OutputPerM: 30.00},
	"openai:gpt-5.4":       {Provider: "openai", Model: "gpt-5.4", InputPerM: 2.50, CacheReadPerM: 0.25, CacheWritePerM: 2.50, OutputPerM: 15.00},
	"openai:gpt-5.4-mini":  {Provider: "openai", Model: "gpt-5.4-mini", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 4.50},
	"openai:gpt-5.3-codex": {Provider: "openai", Model: "gpt-5.3-codex", InputPerM: 1.75, CacheReadPerM: 0.175, CacheWritePerM: 1.75, OutputPerM: 14.00},
	"openai:gpt-5.2-codex": {Provider: "openai", Model: "gpt-5.2-codex", InputPerM: 1.75, CacheReadPerM: 0.175, CacheWritePerM: 1.75, OutputPerM: 14.00},
	// Anthropic's Sonnet 5 launch price is $2 / $10 through 2026-08-31. This
	// static table cannot schedule the published post-intro $3 / $15 change yet,
	// so keep the intro rate here and update the row when catalog support exists.
	"anthropic:claude-sonnet-5":   {Provider: "anthropic", Model: "claude-sonnet-5", InputPerM: 2.00, CacheReadPerM: 0.20, CacheWritePerM: 2.50, OutputPerM: 10.00},
	"anthropic:claude-sonnet-5-5": {Provider: "anthropic", Model: "claude-sonnet-5-5", InputPerM: 2.00, CacheReadPerM: 0.10, CacheWritePerM: 2.50, OutputPerM: 10.00},
	// Haiku 5.5 uses the <=100K prompt tier, matching the frontend estimate.
	// Above 100K (including cache reads/writes), all four rates are 5x higher.
	// Usage aggregates lack per-request prompt sizes, so long requests are underestimated.
	"anthropic:claude-haiku-5-5":  {Provider: "anthropic", Model: "claude-haiku-5-5", InputPerM: 0.10, CacheReadPerM: 0.01, CacheWritePerM: 0.125, OutputPerM: 0.50},
	"anthropic:claude-fable-5-1":  {Provider: "anthropic", Model: "claude-fable-5-1", InputPerM: 10.00, CacheReadPerM: 0.25, CacheWritePerM: 12.50, OutputPerM: 50.00},
	"anthropic:claude-fable-5":    {Provider: "anthropic", Model: "claude-fable-5", InputPerM: 10.00, CacheReadPerM: 1.00, CacheWritePerM: 12.50, OutputPerM: 50.00},
	"anthropic:claude-opus-5-5":   {Provider: "anthropic", Model: "claude-opus-5-5", InputPerM: 4.00, CacheReadPerM: 0.20, CacheWritePerM: 5.00, OutputPerM: 20.00},
	"anthropic:claude-opus-5":     {Provider: "anthropic", Model: "claude-opus-5", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.8":   {Provider: "anthropic", Model: "claude-opus-4.8", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.7":   {Provider: "anthropic", Model: "claude-opus-4.7", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.6":   {Provider: "anthropic", Model: "claude-opus-4.6", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-opus-4.5":   {Provider: "anthropic", Model: "claude-opus-4.5", InputPerM: 5.00, CacheReadPerM: 0.50, CacheWritePerM: 6.25, OutputPerM: 25.00},
	"anthropic:claude-sonnet-4.6": {Provider: "anthropic", Model: "claude-sonnet-4.6", InputPerM: 3.00, CacheReadPerM: 0.30, CacheWritePerM: 3.75, OutputPerM: 15.00},
	"anthropic:claude-sonnet-4.5": {Provider: "anthropic", Model: "claude-sonnet-4.5", InputPerM: 3.00, CacheReadPerM: 0.30, CacheWritePerM: 3.75, OutputPerM: 15.00},
	"anthropic:claude-haiku-4.5":  {Provider: "anthropic", Model: "claude-haiku-4.5", InputPerM: 1.00, CacheReadPerM: 0.10, CacheWritePerM: 1.25, OutputPerM: 5.00},
	"deepseek:v4-pro":             {Provider: "deepseek", Model: "v4-pro", InputPerM: 1.74, CacheReadPerM: 0.0145, CacheWritePerM: 1.74, OutputPerM: 3.48},
	// DeepSeek Flash: api-docs.deepseek.com/quick_start/pricing/ (2026-10-27).
	// The English sheet supplies USD; the Chinese sheet lists CNY. Use peak
	// USD rates because rollups lack each request's pricing window. Off-peak
	// costs half as much; reseller prices can also differ. Cache writes bill
	// as ordinary input. Mirror packages/views/runtimes/utils.ts.
	"deepseek:v4.1-flash": {Provider: "deepseek", Model: "v4.1-flash", InputPerM: 0.30, CacheReadPerM: 0.006, CacheWritePerM: 0.30, OutputPerM: 1.20},
	// MiniMax-M3 standard-tier estimate (https://platform.minimax.io/docs/pricing/overview).
	// Usage aggregates multiple requests, so it cannot identify individual >512K
	// inputs. As with xAI below, use the standard tier; long requests may be
	// underestimated. Cache writes mirror input for newly cached input tokens,
	// not an additional write fee (https://platform.minimax.io/docs/api-reference/text-prompt-caching).
	"minimax:m3":             {Provider: "minimax", Model: "m3", InputPerM: 0.30, CacheReadPerM: 0.06, CacheWritePerM: 0.30, OutputPerM: 1.20},
	"minimax:m2.7":           {Provider: "minimax", Model: "m2.7", InputPerM: 0.30, CacheReadPerM: 0.06, CacheWritePerM: 0.375, OutputPerM: 1.20},
	"minimax:m2.7-highspeed": {Provider: "minimax", Model: "m2.7-highspeed", InputPerM: 0.60, CacheReadPerM: 0.06, CacheWritePerM: 0.375, OutputPerM: 2.40},
	// Google standard token prices: ai.google.dev/gemini-api/docs/pricing
	// (2026-10-19). Mirror packages/views/runtimes/utils.ts.
	// Argon's intro rate expires 2026-11-30; Flash 3.6/3.8, Robotics and
	// 3.8 TTS promotions expire 2026-12-31. Update these static rows at expiry.
	// As documented at ai.google.dev/gemini-api/docs/deprecations, 3.5 Flash
	// routes to 3.6 and 3.7 routes to 3.8; use the destination's current rate.
	// Pro estimates use the <=200K prompt tier; Flash/Lite use text/image/video
	// rates. Request sizes, audio input and service tiers are not in aggregates.
	// Cache writes estimate input processing, not storage. Unsupported cache
	// buckets are 0. Speech-only SKUs have a fixed rate per input/output bucket.
	// Mixed-modality Live/Image/Omni/Embedding, grounding and hourly cache
	// storage cannot be inferred from these token totals.
	"google:gemini-4-argon":                         {Provider: "google", Model: "gemini-4-argon", InputPerM: 2, CacheReadPerM: 0.1, CacheWritePerM: 2, OutputPerM: 10},
	"google:gemini-3.8-flash":                       {Provider: "google", Model: "gemini-3.8-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75},
	"google:gemini-3.7-flash":                       {Provider: "google", Model: "gemini-3.7-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75},
	"google:gemini-3.6-flash":                       {Provider: "google", Model: "gemini-3.6-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75},
	"google:gemini-3.5-flash":                       {Provider: "google", Model: "gemini-3.5-flash", InputPerM: 0.75, CacheReadPerM: 0.075, CacheWritePerM: 0.75, OutputPerM: 3.75},
	"google:gemini-3.5-flash-lite":                  {Provider: "google", Model: "gemini-3.5-flash-lite", InputPerM: 0.3, CacheReadPerM: 0.03, CacheWritePerM: 0.3, OutputPerM: 2.5},
	"google:gemini-3.1-flash-lite":                  {Provider: "google", Model: "gemini-3.1-flash-lite", InputPerM: 0.25, CacheReadPerM: 0.025, CacheWritePerM: 0.25, OutputPerM: 1.5},
	"google:gemini-3.1-pro":                         {Provider: "google", Model: "gemini-3.1-pro", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12},
	"google:gemini-3.1-pro-preview":                 {Provider: "google", Model: "gemini-3.1-pro-preview", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12},
	"google:gemini-3.1-pro-preview-customtools":     {Provider: "google", Model: "gemini-3.1-pro-preview-customtools", InputPerM: 2, CacheReadPerM: 0.2, CacheWritePerM: 2, OutputPerM: 12},
	"google:gemini-3-flash":                         {Provider: "google", Model: "gemini-3-flash", InputPerM: 0.5, CacheReadPerM: 0.05, CacheWritePerM: 0.5, OutputPerM: 3},
	"google:gemini-3-flash-preview":                 {Provider: "google", Model: "gemini-3-flash-preview", InputPerM: 0.5, CacheReadPerM: 0.05, CacheWritePerM: 0.5, OutputPerM: 3},
	"google:gemini-2.5-pro":                         {Provider: "google", Model: "gemini-2.5-pro", InputPerM: 1.25, CacheReadPerM: 0.125, CacheWritePerM: 1.25, OutputPerM: 10},
	"google:gemini-2.5-flash":                       {Provider: "google", Model: "gemini-2.5-flash", InputPerM: 0.3, CacheReadPerM: 0.03, CacheWritePerM: 0.3, OutputPerM: 2.5},
	"google:gemini-2.5-flash-lite":                  {Provider: "google", Model: "gemini-2.5-flash-lite", InputPerM: 0.1, CacheReadPerM: 0.01, CacheWritePerM: 0.1, OutputPerM: 0.4},
	"google:gemini-robotics-er-2-preview":           {Provider: "google", Model: "gemini-robotics-er-2-preview", InputPerM: 1, CacheReadPerM: 0.1, CacheWritePerM: 1, OutputPerM: 5},
	"google:gemini-robotics-er-2-streaming-preview": {Provider: "google", Model: "gemini-robotics-er-2-streaming-preview", InputPerM: 1, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 5},
	"google:gemini-3.8-flash-tts":                   {Provider: "google", Model: "gemini-3.8-flash-tts", InputPerM: 0.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 9},
	"google:gemini-3.8-flash-lite-tts":              {Provider: "google", Model: "gemini-3.8-flash-lite-tts", InputPerM: 0.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 6},
	"google:gemini-3.1-flash-tts-preview":           {Provider: "google", Model: "gemini-3.1-flash-tts-preview", InputPerM: 1, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 20},
	"google:gemini-2.5-flash-preview-tts":           {Provider: "google", Model: "gemini-2.5-flash-preview-tts", InputPerM: 0.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 10},
	"google:gemini-2.5-pro-preview-tts":             {Provider: "google", Model: "gemini-2.5-pro-preview-tts", InputPerM: 1, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 20},
	"google:gemini-3.5-transcribe":                  {Provider: "google", Model: "gemini-3.5-transcribe", InputPerM: 2, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 12},
	"google:gemini-3.5-transcribe-live":             {Provider: "google", Model: "gemini-3.5-transcribe-live", InputPerM: 3.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 21},
	"google:gemini-3.5-live-translate-preview":      {Provider: "google", Model: "gemini-3.5-live-translate-preview", InputPerM: 3.5, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 21},
	// xAI Grok (docs.x.ai/developers/pricing). Short-context tier: xAI bills
	// a request at 2x once its prompt reaches 200K tokens, but a usage record
	// aggregates every model call in a turn, so it cannot say which tier any
	// individual request hit — pricing the standard tier under-estimates a
	// long-context turn by at most 50% instead of over-estimating a short one
	// by 100%. xAI publishes no separate cache-write rate (writes bill as
	// normal input), so CacheWrite mirrors Input. These rows mirror
	// packages/views/runtimes/utils.ts; keep the two tables in sync.
	// `grok-composer-*` ships in the Grok Build catalog but is absent from the
	// price sheet, so it stays unmapped rather than inheriting a guessed rate.
	"xai:grok-4.6":                     {Provider: "xai", Model: "grok-4.6", InputPerM: 2.00, CacheReadPerM: 0.50, CacheWritePerM: 2.00, OutputPerM: 6.00},
	"xai:grok-4.5":                     {Provider: "xai", Model: "grok-4.5", InputPerM: 2.00, CacheReadPerM: 0.30, CacheWritePerM: 2.00, OutputPerM: 6.00},
	"xai:grok-4.3":                     {Provider: "xai", Model: "grok-4.3", InputPerM: 1.25, CacheReadPerM: 0.20, CacheWritePerM: 1.25, OutputPerM: 2.50},
	"xai:grok-build-0.1":               {Provider: "xai", Model: "grok-build-0.1", InputPerM: 1.00, CacheReadPerM: 0.20, CacheWritePerM: 1.00, OutputPerM: 2.00},
	"xai:grok-4.20-multi-agent-0309":   {Provider: "xai", Model: "grok-4.20-multi-agent-0309", InputPerM: 1.25, CacheReadPerM: 0.20, CacheWritePerM: 1.25, OutputPerM: 2.50},
	"xai:grok-4.20-0309-reasoning":     {Provider: "xai", Model: "grok-4.20-0309-reasoning", InputPerM: 1.25, CacheReadPerM: 0.20, CacheWritePerM: 1.25, OutputPerM: 2.50},
	"xai:grok-4.20-0309-non-reasoning": {Provider: "xai", Model: "grok-4.20-0309-non-reasoning", InputPerM: 1.25, CacheReadPerM: 0.20, CacheWritePerM: 1.25, OutputPerM: 2.50},
	// Alibaba Qwen (models.dev providers/alibaba, accessed 2026-08-13;
	// sourced from alibabacloud.com model-pricing — International ≤256K
	// input tier — and the qwencloud.com model pages). qwen3.7-plus and
	// qwen3.6-flash carry the published International ≤256K rates;
	// CacheWritePerM is the Explicit Cache Creation rate (1.25x input) and
	// CacheReadPerM the Explicit Cache Read rate (0.1x input). qwen3.8-max
	// is priced at the published pay-as-you-go rate from its
	// qwencloud.com/models/qwen3.8-max page (the source models.dev cites);
	// qwen3.8-max-preview is served through the Alibaba Token Plan
	// subscription, which does not bill per token, so it stays at 0 (same
	// convention as the free GLM flash tiers). Mirror
	// packages/views/runtimes/utils.ts.
	"alibaba:qwen3.7-plus":        {Provider: "alibaba", Model: "qwen3.7-plus", InputPerM: 0.40, CacheReadPerM: 0.04, CacheWritePerM: 0.50, OutputPerM: 1.60},
	"alibaba:qwen3.6-flash":       {Provider: "alibaba", Model: "qwen3.6-flash", InputPerM: 0.25, CacheReadPerM: 0.025, CacheWritePerM: 0.3125, OutputPerM: 1.50},
	"alibaba:qwen3.8-max":         {Provider: "alibaba", Model: "qwen3.8-max", InputPerM: 2.00, CacheReadPerM: 0.17, CacheWritePerM: 2.50, OutputPerM: 6.00},
	"alibaba:qwen3.8-max-preview": {Provider: "alibaba", Model: "qwen3.8-max-preview", InputPerM: 0, CacheReadPerM: 0, CacheWritePerM: 0, OutputPerM: 0},
	// Moonshot Kimi K3 (platform.kimi.ai/docs/pricing/chat-k3 via models.dev
	// providers/moonshotai/models/kimi-k3.toml). Moonshot bills no separate
	// cache write, so CacheWritePerM mirrors Input.
	"moonshotai:kimi-k3": {Provider: "moonshotai", Model: "kimi-k3", InputPerM: 3.0, CacheReadPerM: 0.30, CacheWritePerM: 3.0, OutputPerM: 15.0},
	// Volcengine Ark (ark.cn-beijing.volces.com). `ark-code-latest` is a
	// rolling alias whose target can be switched in the Volcengine console
	// (across model families), so it is not a stable model identity; the
	// daemon reports the alias itself, never the resolved model. No rate is
	// published for the alias, so it stays unmapped rather than inheriting
	// a guessed rate (same convention as xAI's `grok-composer-*`).
}

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
	{regexp.MustCompile(`(^|/|:)gpt-6-astra$`), "openai:gpt-6-astra"},
	{regexp.MustCompile(`(^|/|:)gpt-6\.1-sol$`), "openai:gpt-6.1-sol"},
	{regexp.MustCompile(`(^|/|:)gpt-6\.1-terra$`), "openai:gpt-6.1-terra"},
	{regexp.MustCompile(`(^|/|:)gpt-6-sol$`), "openai:gpt-6-sol"},
	{regexp.MustCompile(`(^|/|:)gpt-6-luna$`), "openai:gpt-6-luna"},
	{regexp.MustCompile(`(^|/|:)gpt-5\.6-sol$`), "openai:gpt-5.6-sol"},
	{regexp.MustCompile(`(^|/|:)gpt-5\.6-terra$`), "openai:gpt-5.6-terra"},
	{regexp.MustCompile(`(^|/|:)gpt-5\.6-luna$`), "openai:gpt-5.6-luna"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]5$|^gpt-5-5$`), "openai:gpt-5.5"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4($|-2026-03-05|-xhigh)`), "openai:gpt-5.4"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]4-mini($|[^a-z0-9])`), "openai:gpt-5.4-mini"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]3-codex$`), "openai:gpt-5.3-codex"},
	{regexp.MustCompile(`(^|/|:)gpt-5[.-]2-codex$`), "openai:gpt-5.2-codex"},
	// Sonnet 5.5 halves Sonnet 5's cache-read rate. End both rules at their
	// own version so later SKUs cannot inherit either tier.
	{regexp.MustCompile(`claude-sonnet-5[-.]5` + claudeVersionEnd), "anthropic:claude-sonnet-5-5"},
	{regexp.MustCompile(`(?:claude-sonnet-5|claude-5-sonnet)` + claudeVersionEnd), "anthropic:claude-sonnet-5"},
	{regexp.MustCompile(`claude-haiku-5[-.]5` + claudeVersionEnd), "anthropic:claude-haiku-5-5"},
	// Fable 5.1 shares Fable 5's $10 / $50 and $12.50 cache write but prices
	// cache reads at 0.025x input ($0.25) instead of the standard 0.1x, so it
	// needs its own row, and both rules end at their own version
	// (claudeVersionEnd) so neither can swallow the other's ids.
	{regexp.MustCompile(`claude-fable-5[-.]1` + claudeVersionEnd), "anthropic:claude-fable-5-1"},
	{regexp.MustCompile(`claude-fable-5` + claudeVersionEnd), "anthropic:claude-fable-5"},
	// Opus 5.5 is cheaper than Opus 5 ($4 / $20) and prices cache reads at
	// 0.05x input, so the two need separate rows. Both rules end at their own
	// version (claudeVersionEnd), the same as the Fable pair above, so the
	// Opus 5 rule cannot swallow 5.5 ids and bill them at Opus 5 rates.
	{regexp.MustCompile(`claude-opus-5[-.]5` + claudeVersionEnd), "anthropic:claude-opus-5-5"},
	{regexp.MustCompile(`claude-opus-5` + claudeVersionEnd), "anthropic:claude-opus-5"},
	{regexp.MustCompile(`claude-opus-4[-.]8`), "anthropic:claude-opus-4.8"},
	{regexp.MustCompile(`claude-opus-4[-.]7`), "anthropic:claude-opus-4.7"},
	{regexp.MustCompile(`claude-opus-4[-.]6`), "anthropic:claude-opus-4.6"},
	{regexp.MustCompile(`claude-opus-4[-.]5`), "anthropic:claude-opus-4.5"},
	{regexp.MustCompile(`claude-sonnet-4[-.]6|claude-4[-.]6-sonnet`), "anthropic:claude-sonnet-4.6"},
	{regexp.MustCompile(`claude-sonnet-4[-.]5|claude-4[-.]5-sonnet`), "anthropic:claude-sonnet-4.5"},
	{regexp.MustCompile(`claude-haiku-4[-.]5`), "anthropic:claude-haiku-4.5"},
	// The official Flash name and retired v4 aliases serve V4.1-Flash-1017.
	// Preserve chat/reasoner's existing Flash routing. Only known spellings
	// and the 1017 snapshot resolve, matching the frontend's exact keys.
	{regexp.MustCompile(`(^|/|:)deepseek-(?:flash|v4-flash(?:-vision-exp)?|v4\.1-flash(?:-1017)?|chat|reasoner)$`), "deepseek:v4.1-flash"},
	{regexp.MustCompile(`deepseek-v4-pro`), "deepseek:v4-pro"},
	{regexp.MustCompile(`(^|/|:)minimax-m3$`), "minimax:m3"},
	{regexp.MustCompile(`minimax-m2[.]7.*highspeed|highspeed.*minimax-m2[.]7`), "minimax:m2.7-highspeed"},
	{regexp.MustCompile(`minimax-m2[.]7`), "minimax:m2.7"},
	// Each Gemini SKU is exact: an image/audio/preview variant must never
	// inherit the nearest text model rate. Known efforts normalize below.
	{regexp.MustCompile(`(^|/|:)gemini-4-argon$`), "google:gemini-4-argon"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.8-flash$`), "google:gemini-3.8-flash"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.7-flash$`), "google:gemini-3.7-flash"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.6-flash$`), "google:gemini-3.6-flash"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.5-flash$`), "google:gemini-3.5-flash"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.5-flash-lite$`), "google:gemini-3.5-flash-lite"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.1-flash-lite$`), "google:gemini-3.1-flash-lite"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.1-pro$`), "google:gemini-3.1-pro"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.1-pro-preview$`), "google:gemini-3.1-pro-preview"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.1-pro-preview-customtools$`), "google:gemini-3.1-pro-preview-customtools"},
	{regexp.MustCompile(`(^|/|:)gemini-3-flash$`), "google:gemini-3-flash"},
	{regexp.MustCompile(`(^|/|:)gemini-3-flash-preview$`), "google:gemini-3-flash-preview"},
	{regexp.MustCompile(`(^|/|:)gemini-2\.5-pro$`), "google:gemini-2.5-pro"},
	{regexp.MustCompile(`(^|/|:)gemini-2\.5-flash$`), "google:gemini-2.5-flash"},
	{regexp.MustCompile(`(^|/|:)gemini-2\.5-flash-lite$`), "google:gemini-2.5-flash-lite"},
	{regexp.MustCompile(`(^|/|:)gemini-robotics-er-2-preview$`), "google:gemini-robotics-er-2-preview"},
	{regexp.MustCompile(`(^|/|:)gemini-robotics-er-2-streaming-preview$`), "google:gemini-robotics-er-2-streaming-preview"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.8-flash-tts$`), "google:gemini-3.8-flash-tts"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.8-flash-lite-tts$`), "google:gemini-3.8-flash-lite-tts"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.1-flash-tts-preview$`), "google:gemini-3.1-flash-tts-preview"},
	{regexp.MustCompile(`(^|/|:)gemini-2\.5-flash-preview-tts$`), "google:gemini-2.5-flash-preview-tts"},
	{regexp.MustCompile(`(^|/|:)gemini-2\.5-pro-preview-tts$`), "google:gemini-2.5-pro-preview-tts"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.5-transcribe$`), "google:gemini-3.5-transcribe"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.5-transcribe-live$`), "google:gemini-3.5-transcribe-live"},
	{regexp.MustCompile(`(^|/|:)gemini-3\.5-live-translate-preview$`), "google:gemini-3.5-live-translate-preview"},
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
	{regexp.MustCompile(`(^|/|:)qwen3[.-]8-max-preview(\[[^\]]+\])?$`), "alibaba:qwen3.8-max-preview"},
	// Kimi K3. Anchored so the distinct CodeBuddy SKU `kimi-k3-1` stays
	// unmapped; `kimi-code/k3` (Kimi Code CLI) resolves via the `/k3$` form.
	{regexp.MustCompile(`(^|/|:)kimi-k3$`), "moonshotai:kimi-k3"},
	{regexp.MustCompile(`(^|/|:)k3$`), "moonshotai:kimi-k3"},
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

// Antigravity reports a display label or an effort-suffixed model ID. Effort
// selects reasoning, not a different SKU. Only strip known effort spellings;
// unknown variants must remain unpriced instead of borrowing a family's rate.
var geminiDisplayNameRe = regexp.MustCompile(`(^|/|:)gemini ([0-9]+(?:\.[0-9]+)?) (flash(?:-lite)?|pro|argon)(?: \((?:minimal|low|medium|high)\))?$`)
var geminiEffortSuffixRe = regexp.MustCompile(`((?:^|/|:)gemini-[0-9]+(?:\.[0-9]+)?-(?:flash(?:-lite)?|pro|argon))-(?:minimal|low|medium|high)$`)

func matchModelAlias(model string) (ModelPrice, bool) {
	model = geminiDisplayNameRe.ReplaceAllString(model, "${1}gemini-${2}-${3}")
	model = geminiEffortSuffixRe.ReplaceAllString(model, "${1}")
	for _, rule := range modelAliasRules {
		if rule.re.MatchString(model) {
			price, ok := modelPrices[rule.priceKey]
			return price, ok
		}
	}
	return ModelPrice{}, false
}

// Bedrock Anthropic ids may follow routing prefixes such as `custom:bedrock/`.
// Accept the same routing segments as the frontend's stripProvider, and only
// normalize Claude ids under the recognized Bedrock vendor/region prefixes.
var bedrockAnthropicIDRe = regexp.MustCompile(`^(?:[a-z][a-z0-9_-]*[/:])*(?:(?:us|eu|apac|global)\.)?anthropic\.(claude-)`)

// Preserve a single context tag for the existing matcher while removing the
// Bedrock version (`...-v1:0[1m]`). Multiple tags remain unrecognized.
var bedrockVersionSuffixRe = regexp.MustCompile(`-v\d+:\d+(\[[^\]]+\])?$`)

// normalizeBedrockID rewrites a Bedrock-served Anthropic id into the bare
// catalog id the price rows are keyed by, so `us.anthropic.claude-sonnet-5`
// prices as `claude-sonnet-5` — the same SKU. It is a no-op for every other id,
// including non-Anthropic Bedrock models (`us.amazon.nova-*`), which stay
// unmapped rather than inheriting a Claude rate. Keep in step with
// `stripBedrock` in packages/views/runtimes/utils.ts: the dashboard and this
// metric must agree on what is priced.
func normalizeBedrockID(model string) string {
	if !bedrockAnthropicIDRe.MatchString(model) {
		return model
	}
	model = bedrockAnthropicIDRe.ReplaceAllString(model, "${1}")
	return bedrockVersionSuffixRe.ReplaceAllString(model, "${1}")
}

func PriceForModelAlias(model string) (ModelPrice, bool) {
	model = normalizeBedrockID(strings.ToLower(strings.TrimSpace(model)))
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
