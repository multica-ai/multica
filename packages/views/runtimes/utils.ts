import type {
  AgentRuntime,
  RuntimeUsage,
  RuntimeUsageByAgent,
} from "@multica/core/types";
import { getCustomPricing } from "@multica/core/runtimes/custom-pricing-store";

// A live local daemon re-registers itself within seconds of a server-side
// delete (daemon self-heal, #2404), so deleting an online local runtime from
// the UI has no lasting effect. Both the detail page and the list row menu
// gate their Delete affordance on this same predicate.
export function isSelfHealingRuntime(runtime: AgentRuntime): boolean {
  return runtime.runtime_mode === "local" && runtime.status === "online";
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

// Compound-unit relative timestamp ("2m 14s ago", "1d 4h ago", "6d 19h ago")
// — gives the user enough precision to tell "just lost" from "long lost"
// at a glance without forcing them to mouse-over for a full timestamp.
export function formatLastSeen(lastSeenAt: string | null): string {
  if (!lastSeenAt) return "Never";
  const diffMs = Date.now() - new Date(lastSeenAt).getTime();
  if (diffMs < 5_000) return "Just now";

  const seconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return `${seconds}s ago`;
  if (hours < 1) {
    const s = seconds % 60;
    return s > 0 ? `${minutes}m ${s}s ago` : `${minutes}m ago`;
  }
  if (days < 1) {
    const m = minutes % 60;
    return m > 0 ? `${hours}h ${m}m ago` : `${hours}h ago`;
  }
  const h = hours % 24;
  return h > 0 ? `${days}d ${h}h ago` : `${days}d ago`;
}

// Turns the back-end's `device_info` string ("MacBook-Pro · darwin-amd64",
// "some-host · linux-amd64") into something humans recognise. We don't have
// hardware model or geo data on the wire today, so we settle for an OS-aware
// rewrite of the GOOS/GOARCH suffix while preserving the hostname.
export function formatDeviceInfo(raw: string | null): string | null {
  if (!raw) return null;
  const trimmed = raw.trim();
  if (!trimmed) return null;
  return trimmed
    .split(" · ")
    .map((part) => prettifyOsArch(part))
    .join(" · ");
}

function prettifyOsArch(part: string): string {
  const lower = part.toLowerCase();
  // Pattern: <os>-<arch>; e.g. darwin-amd64, linux-arm64, windows-amd64.
  const match = lower.match(/^(darwin|linux|windows|freebsd|openbsd|netbsd)-(amd64|arm64|386|arm)$/);
  if (!match) return part;
  const os = match[1] ?? "";
  const arch = match[2] ?? "";
  const osLabel = OS_LABEL[os] ?? os;
  const archLabel = ARCH_LABEL[arch] ?? arch;
  return `${osLabel} (${archLabel})`;
}

const OS_LABEL: Record<string, string> = {
  darwin: "macOS",
  linux: "Linux",
  windows: "Windows",
  freebsd: "FreeBSD",
  openbsd: "OpenBSD",
  netbsd: "NetBSD",
};

const ARCH_LABEL: Record<string, string> = {
  amd64: "x86_64",
  arm64: "arm64",
  "386": "x86",
  arm: "arm",
};

// Strip leading "v" from version strings — GitHub releases ship `v0.2.17`,
// daemon metadata reports `0.2.15`; normalising lets us compare both.
function stripVersionPrefix(v: string): string {
  return v.replace(/^v/, "");
}

// True iff `latest` is strictly newer than `current` by dotted-numeric
// comparison. Non-numeric / missing segments compare as 0 ("0.2" < "0.2.1").
// Used by the runtime-list CLI column to decide whether to surface the ↑
// marker; same logic also lives inline in update-section.tsx for now.
export function isVersionNewer(latest: string, current: string): boolean {
  const l = stripVersionPrefix(latest).split(".").map(Number);
  const c = stripVersionPrefix(current).split(".").map(Number);
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = l[i] ?? 0;
    const cv = c[i] ?? 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return m % 1 < 0.05 ? `${Math.round(m)}M` : `${m.toFixed(1)}M`;
  }
  if (n >= 1_000) {
    const k = n / 1_000;
    return k % 1 < 0.05 ? `${Math.round(k)}K` : `${k.toFixed(1)}K`;
  }
  return n.toLocaleString();
}

// ---------------------------------------------------------------------------
// Cost estimation
// ---------------------------------------------------------------------------

// Pricing per million tokens (USD). Sources, each authoritative for the
// rows tagged under it — keep in sync when providers release new models
// or adjust prices.
//
//   Anthropic: https://platform.claude.com/docs/en/about-claude/pricing
//   OpenAI:    https://openai.com/api/pricing
//   DeepSeek:  https://api-docs.deepseek.com/quick_start/pricing
//   Moonshot:  https://www.kimi.com/resources/kimi-k2-6-pricing
//   Zhipu:     https://docs.z.ai/guides/overview/pricing
//
// Anthropic's cacheWrite reflects the 5-minute cache TTL (1.25× input); the
// daemon reports cache_creation_input_tokens without TTL metadata, so 5m is
// the safest / cheapest assumption (matches the API default). DeepSeek,
// Moonshot and Zhipu do not bill cache writes separately (cached input is
// just discounted on subsequent reads), so cacheWrite mirrors input there.
// OpenAI historically did the same, but its GPT-5.6+ generation bills cache
// writes at 1.25× input (cache reads still get the 90% cached-input
// discount), so those rows carry a distinct cacheWrite. Codex usage doesn't
// yet stream cache-write tokens, so that rate isn't exercised today.
//
// The resolver matches exact keys after stripping a trailing date snapshot
// (see `resolvePricing` below). It deliberately does NOT do startsWith
// fallbacks: every catalog SKU needs its own row. That keeps unfamiliar
// variants (`gpt-5.5-mini`, hypothetical `gpt-5.4-foo`) from silently
// inheriting the price of a near-named relative; they surface in the
// unmapped diagnostic instead. Mirror new entries in
// `server/pkg/agent/models.go` so the catalog and pricing stay in sync.
//
// Provider-qualified keys: a model id that is NOT vendor-prefixed
// (`claude-*`, `gpt-*`, `o3*`/`o4*`, `glm-*`, `deepseek-*`, `kimi-*`) and is
// not the provider name itself can collide across providers — more than one
// provider may report the same generic id like `auto`. Such generic ids MUST be keyed as
// `${provider}/${model}` (e.g. `cursor/auto`). `resolvePricing` tries the
// `${provider}/…` form first, then the bare form, so vendor-prefixed SKUs
// stay unqualified and still resolve.
const MODEL_PRICING: Record<
  string,
  { input: number; output: number; cacheRead: number; cacheWrite: number }
> = {
  // -- Anthropic: current generation. Sonnet 5 uses Anthropic's published
  //    intro launch rate ($2 / $10 through 2026-08-31). This static map has
  //    no future-dated pricing support yet, so update the row when the
  //    post-intro $3 / $15 rate takes effect. Fable 5 is a Mythos-class SKU
  //    at 10/50; Opus 4.5+ stays on the lower 5/25 Opus tier. --
  "claude-sonnet-5":     { input: 2,    output: 10,   cacheRead: 0.20, cacheWrite: 2.50 },
  "claude-fable-5":     { input: 10,   output: 50,   cacheRead: 1.00, cacheWrite: 12.50 },
  "claude-haiku-4-5":   { input: 1,    output: 5,    cacheRead: 0.10, cacheWrite: 1.25 },
  "claude-sonnet-4-5":  { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },
  "claude-sonnet-4-6":  { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },
  "claude-opus-4-5":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-6":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-7":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-8":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },

  // -- Anthropic: pre-4.5 Opus (legacy, still served at original price tier) --
  "claude-opus-4-1":    { input: 15,   output: 75,   cacheRead: 1.50, cacheWrite: 18.75 },
  "claude-opus-4":      { input: 15,   output: 75,   cacheRead: 1.50, cacheWrite: 18.75 },

  // -- Anthropic: Sonnet 4.0 (deprecated; same price as the 4.x family) --
  "claude-sonnet-4":    { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },

  // -- Anthropic: older Haiku tier (defensive entry for the rare runtime still on it) --
  "claude-haiku-3-5":   { input: 0.80, output: 4,    cacheRead: 0.08, cacheWrite: 1.00 },

  // -- OpenAI: dotted-minor Codex catalog SKUs. Each generation is priced
  //    independently — no fallback to `gpt-5`. Entries track
  //    `server/pkg/agent/models.go` (Codex provider list).
  //    gpt-5.6 (sol/terra/luna) uses OpenAI's official announcement rates.
  //    5.6+ is the first OpenAI generation to bill cache writes separately:
  //    cacheRead = 0.1x input (90% cached-input discount), cacheWrite = 1.25x
  //    input (see the header note above). Codex usage doesn't yet report
  //    cache-write tokens, so cacheWrite isn't exercised today, but the rate
  //    is kept correct for when it is.
  "gpt-5.6-sol":        { input: 5,    output: 30,   cacheRead: 0.50,  cacheWrite: 6.25 },
  "gpt-5.6-terra":      { input: 2.50, output: 15,   cacheRead: 0.25,  cacheWrite: 3.125 },
  "gpt-5.6-luna":       { input: 1,    output: 6,    cacheRead: 0.10,  cacheWrite: 1.25 },
  "gpt-5.5":            { input: 5,    output: 30,   cacheRead: 0.50,  cacheWrite: 5 },
  "gpt-5.4-mini":       { input: 0.75, output: 4.50, cacheRead: 0.075, cacheWrite: 0.75 },
  "gpt-5.4":            { input: 2.50, output: 15,   cacheRead: 0.25,  cacheWrite: 2.50 },
  "gpt-5.3-codex":      { input: 1.75, output: 14,   cacheRead: 0.175, cacheWrite: 1.75 },

  // -- OpenAI: GPT-5 family (Codex CLI's default is gpt-5-codex; -codex/-mini/-nano variants priced per OpenAI tiers) --
  "gpt-5-codex":        { input: 1.25, output: 10,   cacheRead: 0.125, cacheWrite: 1.25 },
  "gpt-5-mini":         { input: 0.25, output: 2,    cacheRead: 0.025, cacheWrite: 0.25 },
  "gpt-5-nano":         { input: 0.05, output: 0.40, cacheRead: 0.005, cacheWrite: 0.05 },
  "gpt-5":              { input: 1.25, output: 10,   cacheRead: 0.125, cacheWrite: 1.25 },

  // -- OpenAI: o-series reasoning models --
  "o3-mini":            { input: 1.10, output: 4.40, cacheRead: 0.55,  cacheWrite: 1.10 },
  "o3":                 { input: 2,    output: 8,    cacheRead: 0.50,  cacheWrite: 2 },
  "o4-mini":            { input: 1.10, output: 4.40, cacheRead: 0.275, cacheWrite: 1.10 },

  // -- OpenAI: GPT-4o family (legacy, kept for runtimes still configured against it) --
  "gpt-4o-mini":        { input: 0.15, output: 0.60, cacheRead: 0.075, cacheWrite: 0.15 },
  "gpt-4o":             { input: 2.50, output: 10,   cacheRead: 1.25,  cacheWrite: 2.50 },

  // -- DeepSeek (api-docs.deepseek.com/quick_start/pricing).
  //    The official catalog lists exactly two current SKUs; `deepseek-chat`
  //    and `deepseek-reasoner` are aliases that route to `deepseek-v4-flash`
  //    (non-thinking and thinking mode respectively) per the same page.
  //    `deepseek-v4-pro` is currently under a 75%-off promo that ends
  //    2026-05-31 15:59 UTC; we price at the post-promo standard rate
  //    ($1.74/$3.48) so the dashboard does not jump 4× on June 1 — accept
  //    a brief over-estimate during the promo over a sudden cliff after it. --
  "deepseek-v4-flash":  { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },
  "deepseek-v4-pro":    { input: 1.74, output: 3.48, cacheRead: 0.0145, cacheWrite: 1.74 },
  "deepseek-chat":      { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },
  "deepseek-reasoner":  { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },

  // -- Moonshot Kimi (kimi.com/resources/kimi-k2-6-pricing).
  //    Only K2.6 is on the official price sheet today; earlier K2 variants
  //    are intentionally omitted until Moonshot publishes their rates. --
  "kimi-k2.6":          { input: 0.95, output: 4.00, cacheRead: 0.16,   cacheWrite: 0.95 },

  // -- Zhipu z.ai (docs.z.ai/guides/overview/pricing). Free flash tiers
  //    are priced at 0 so they resolve cleanly instead of falling through
  //    to the "unmapped" diagnostic. --
  "glm-5.1":            { input: 1.4,  output: 4.4,  cacheRead: 0.26,   cacheWrite: 1.4 },
  "glm-5":              { input: 1.0,  output: 3.2,  cacheRead: 0.2,    cacheWrite: 1.0 },
  "glm-5-turbo":        { input: 1.2,  output: 4.0,  cacheRead: 0.24,   cacheWrite: 1.2 },
  "glm-4.7":            { input: 0.6,  output: 2.2,  cacheRead: 0.11,   cacheWrite: 0.6 },
  "glm-4.7-flashx":     { input: 0.07, output: 0.4,  cacheRead: 0.01,   cacheWrite: 0.07 },
  "glm-4.7-flash":      { input: 0,    output: 0,    cacheRead: 0,      cacheWrite: 0 },
  "glm-4.6":            { input: 0.6,  output: 2.2,  cacheRead: 0.11,   cacheWrite: 0.6 },
  "glm-4.5":            { input: 0.6,  output: 2.2,  cacheRead: 0.11,   cacheWrite: 0.6 },
  "glm-4.5-x":          { input: 2.2,  output: 8.9,  cacheRead: 0.45,   cacheWrite: 2.2 },
  "glm-4.5-air":        { input: 0.2,  output: 1.1,  cacheRead: 0.03,   cacheWrite: 0.2 },
  "glm-4.5-airx":       { input: 1.1,  output: 4.5,  cacheRead: 0.22,   cacheWrite: 1.1 },
  "glm-4.5-flash":      { input: 0,    output: 0,    cacheRead: 0,      cacheWrite: 0 },

  // -- Cursor Composer / Auto (cursor.com/docs/models-and-pricing,
  //    cursor.com/docs/models/cursor-composer-2,
  //    cursor.com/docs/models/cursor-composer-2-5).
  //    Cursor's model ids are all unprefixed generic names (`auto`,
  //    `composer-*`) that collide with other providers (another provider
  //    could also report `auto`), so they are provider-qualified under `cursor/`.
  //    See the `provider-qualified keys` note above. Cursor result events
  //    often omit `model`, so the daemon falls back to the configured
  //    runtime model or the legacy key `cursor`. Cursor does not publish a
  //    cache-write rate for these rows; keep it at 0 so reported
  //    cache_write_tokens don't invent spend from input pricing.
  "cursor/auto":              { input: 1.25, output: 6,    cacheRead: 0.25,   cacheWrite: 0 },
  "cursor/composer-2.5-fast": { input: 3,    output: 15,   cacheRead: 0.5,    cacheWrite: 0 },
  "cursor/composer-2.5":      { input: 0.5,  output: 2.5,  cacheRead: 0.2,    cacheWrite: 0 },
  "cursor/composer-2-fast":   { input: 1.5,  output: 7.5,  cacheRead: 0.35,   cacheWrite: 0 },
  "cursor/composer-2":        { input: 0.5,  output: 2.5,  cacheRead: 0.2,    cacheWrite: 0 },
  "cursor/composer-1.5":      { input: 3.5,  output: 17.5, cacheRead: 0.35,   cacheWrite: 0 },
  "cursor/composer-1":        { input: 1.25, output: 10,   cacheRead: 0.125,  cacheWrite: 0 },
  // Legacy fallback bucket when neither the result event nor the runtime
  // model is known — the daemon emits the literal `cursor`. This key equals
  // the provider name itself, so it can't collide across providers and stays
  // unqualified. Price at the current Composer 2.5 Fast default.
  "cursor":                   { input: 3,    output: 15,   cacheRead: 0.5,    cacheWrite: 0 },
};

// Resolve a model string to its pricing tier. Exact match, with four
// tolerances applied in order:
//
//  1. Provider-prefixed IDs (`anthropic/claude-opus-4.7` from openclaw /
//     opencode) — the `<provider>/` segment is routing metadata, not part
//     of the SKU, so we strip it before lookup.
//  2. Anthropic dot↔dash normalization — Claude Code reports
//     `claude-opus-4-7`, GitHub Copilot reports `claude-opus-4.7`. Same
//     SKU, two transports. We canonicalize `claude-*` IDs to the dashed
//     form Anthropic itself publishes. Scoped to `claude-*` because for
//     OpenAI the separator IS semantic (`gpt-5.4` ≠ `gpt-5-4`).
//  3. Trailing dated snapshots (`claude-sonnet-4-5-20250929`,
//     `gpt-5-2025-08-07`) — the family is what we price, the date is
//     volatile, so we strip a trailing date / "latest" tag.
//  4. Trailing context-window tag (`claude-opus-4-7[1m]`) — Anthropic's
//     1M-context beta is the same SKU at standard rates for prompts
//     ≤200K input tokens, with a 2× surcharge above that. Aggregated
//     usage rows don't carry per-request prompt sizes, so we price the
//     bracketed variant at the standard tier. Slight under-estimate
//     beats the previous behaviour of dropping the row entirely.
//
// Anything still unmapped falls back to the user-supplied custom pricing
// store. No startsWith fallback: variants like `gpt-5.5-mini` must have
// their own row to be priced (otherwise they'd inherit `gpt-5.5`).
//
// `provider` disambiguates unprefixed generic ids (see the header note):
// every candidate is tried `${provider}/…`-qualified first, then bare, so a
// `cursor/auto` row wins for a Cursor row while an unqualified `auto` (no
// provider) stays unmapped instead of silently borrowing Cursor's price.
function resolvePricing(model: string, provider?: string) {
  if (!model) return undefined;

  const candidates = pricingCandidates(model, provider);
  for (const candidate of candidates) {
    const hit = MODEL_PRICING[candidate];
    if (hit) return hit;
  }
  for (const candidate of candidates) {
    const hit = getCustomPricing(candidate);
    if (hit) return hit;
  }
  return undefined;
}

// Canonical provider token for keying: trimmed + lowercased so lookup keys,
// storage keys, and grouping labels all tolerate case drift in the stored
// value. Returns "" when no provider is known.
function normalizeProvider(provider?: string): string {
  return provider?.trim().toLowerCase() ?? "";
}

// Provider-qualify a key, skipping the prefix when the key already carries
// this provider (an upstream-qualified `cursor/auto` must not become
// `cursor/cursor/auto`). `provider` must already be normalized.
function qualify(provider: string, key: string): string {
  return key.startsWith(`${provider}/`) ? key : `${provider}/${key}`;
}

// Lookup keys for a (model, provider) pair: every canonical candidate
// `${provider}/`-qualified first (when a provider is known), then the bare
// candidates. Qualified-first means a provider-scoped row/override always
// beats an unqualified one.
function pricingCandidates(model: string, provider?: string): string[] {
  const base = canonicalCandidates(model);
  const p = normalizeProvider(provider);
  if (!p) return base;
  return [...base.map((c) => qualify(p, c)), ...base];
}

// The canonical storage/diagnostic key for a (model, provider) pair: the
// provider-qualified form when a provider is known, else the bare model.
// `collectUnmappedModels` returns these, and the custom-pricing dialog keys
// overrides by them, so a user-entered rate for `cursor/auto` resolves only
// for Cursor rows — not for another provider that also reports `auto`.
// Provider is lowercased so lookups tolerate case drift in the stored value.
export function pricingKey(model: string, provider?: string): string {
  const p = normalizeProvider(provider);
  return p ? qualify(p, model) : model;
}

// Display/grouping key for a usage row's model. Self-resolving ids
// (vendor-prefixed SKUs like `claude-opus-4-7`, and the legacy `cursor`
// fallback whose key equals the provider name) stay bare; a generic id that
// only prices under a provider (`auto`, `composer-*`) is provider-qualified
// so two providers reporting the same bare id don't merge into one mislabelled
// row, and the label matches what `collectUnmappedModels` / the pricing dialog
// surface.
export function modelGroupingKey(model: string, provider?: string): string {
  if (!model) return normalizeProvider(provider) || "unknown";
  return isSelfResolvingId(model) ? model : pricingKey(model, provider);
}

// Whether a model id prices on its own without a provider qualifier (a
// vendor-prefixed SKU, or the legacy `cursor` fallback). Such ids keep a bare
// grouping key; generic ids (`auto`, `composer-*`) stay provider-qualified.
//
// Probes the BARE model on purpose: forwarding a provider would let a
// qualified row report as self-resolving and collapse back to a bare key,
// re-merging the cross-provider collision this scheme prevents. Keep the
// argument list provider-free so that stays true.
function isSelfResolvingId(model: string): boolean {
  return isModelPriced(model);
}

// Generate the lookup candidates for a model string, in priority order:
// the raw string first (preserves explicit user / catalog spellings),
// then the canonicalized forms. Deduped so we don't repeat lookups.
//
// Pure in `model`, and the aggregation loops call it 3-4x per row, so the
// result is memoized — the model-string set is small and bounded. Callers
// only read the array (pricingCandidates maps/spreads into a fresh one), so
// sharing the cached reference is safe.
// Intentionally process-lifetime: never evicted (bounded key set, see above).
const canonicalCandidatesCache = new Map<string, string[]>();
function canonicalCandidates(model: string): string[] {
  const cached = canonicalCandidatesCache.get(model);
  if (cached) return cached;
  const seen = new Set<string>();
  const out: string[] = [];
  const push = (s: string) => {
    if (!s || seen.has(s)) return;
    seen.add(s);
    out.push(s);
  };
  const stripDate = (s: string) =>
    s.replace(/-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$/, "");
  const stripProvider = (s: string) => {
    const i = s.indexOf("/");
    return i > 0 && /^[a-z][a-z0-9_-]*$/i.test(s.slice(0, i)) ? s.slice(i + 1) : s;
  };
  // Only Anthropic IDs are dot↔dash equivalent. OpenAI separators are
  // semantic, so we leave `gpt-5.4` etc. alone.
  const canonAnthropic = (s: string) =>
    s.startsWith("claude-") ? s.replace(/\./g, "-") : s;
  // Trailing context-window tag (`claude-opus-4-7[1m]`). Same family,
  // same price tier — see resolver comment above for the 1M-context
  // pricing trade-off.
  const stripContextTag = (s: string) => s.replace(/\[[^\]]+\]$/, "");

  const raw = model;
  const noProvider = stripProvider(raw);
  const dashed = canonAnthropic(noProvider);
  const noTag = stripContextTag(dashed);

  push(raw);
  push(noProvider);
  push(dashed);
  push(noTag);
  push(stripDate(raw));
  push(stripDate(noProvider));
  push(stripDate(dashed));
  push(stripDate(noTag));
  canonicalCandidatesCache.set(model, out);
  return out;
}

// Cheap predicate for the empty-state diagnostic: which model strings in a
// usage batch failed pricing resolution. Useful when the user is staring at
// "$0.00 / 2M tokens" and wants to know why.
export function isModelPriced(model: string, provider?: string): boolean {
  return resolvePricing(model, provider) !== undefined;
}

// Returns the unique, sorted list of pricing keys present in `rows` that
// don't resolve to a price. Keys are provider-qualified (`cursor/auto`) when
// the row carries a provider, so the same bare model id reported by two
// providers surfaces as two distinct entries the user can price separately.
// Empty when everything's priced or there are no rows.
export function collectUnmappedModels(rows: readonly Priceable[]): string[] {
  const set = new Set<string>();
  for (const r of rows) {
    if (r.model && !isModelPriced(r.model, r.provider)) {
      set.add(pricingKey(r.model, r.provider));
    }
  }
  return Array.from(set).toSorted();
}

// Anything carrying per-model token totals can be priced — RuntimeUsage,
// RuntimeUsageByAgent, RuntimeUsageByHour all share this shape on purpose
// (the back-end keeps the model dimension specifically so the client can
// run this calculation for any aggregation axis).
// `provider` is optional so callers with provider-less rows (and existing
// test fixtures) still type-check; when present it disambiguates generic
// model ids during pricing. RuntimeUsage / RuntimeUsageByAgent /
// DashboardUsageDaily / DashboardUsageByAgent all carry it on the wire.
type Priceable = Pick<
  RuntimeUsage,
  "model" | "input_tokens" | "output_tokens" | "cache_read_tokens" | "cache_write_tokens"
> & { provider?: string };

export function estimateCost(usage: Priceable): number {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) return 0;
  return (
    (usage.input_tokens * pricing.input +
      usage.output_tokens * pricing.output +
      usage.cache_read_tokens * pricing.cacheRead +
      usage.cache_write_tokens * pricing.cacheWrite) /
    1_000_000
  );
}

export interface CostBreakdown {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export function estimateCostBreakdown(usage: Priceable): CostBreakdown {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) {
    return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
  }
  return {
    input: (usage.input_tokens * pricing.input) / 1_000_000,
    output: (usage.output_tokens * pricing.output) / 1_000_000,
    cacheRead: (usage.cache_read_tokens * pricing.cacheRead) / 1_000_000,
    cacheWrite: (usage.cache_write_tokens * pricing.cacheWrite) / 1_000_000,
  };
}

// Cache savings: what cache *reads* would have cost at full input pricing
// minus what they actually cost at the discounted cache-hit rate. This is a
// reconstruction of "money the cache saved you", not real-world spend.
export function estimateCacheSavings(usage: Priceable): number {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) return 0;
  const wouldHaveCost = (usage.cache_read_tokens * pricing.input) / 1_000_000;
  const actualCost = (usage.cache_read_tokens * pricing.cacheRead) / 1_000_000;
  return wouldHaveCost - actualCost;
}

// ---------------------------------------------------------------------------
// Data aggregation
// ---------------------------------------------------------------------------

export interface DailyTokenData {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface DailyCostData {
  date: string;
  label: string;
  cost: number;
}

// Stacked variant — splits the daily $ figure into the three components that
// drive billing (cache reads excluded; their cost is tracked separately as
// "savings" since they're typically dominated by the cached-input discount).
export interface DailyCostStackData {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export interface ModelDistribution {
  model: string;
  tokens: number;
  cost: number;
}

export interface WeeklyTokenData {
  weekStart: string;
  weekEnd: string;
  // X-axis tick — Monday of the week, e.g. "May 12".
  label: string;
  // Tooltip header — inclusive range, e.g. "May 12 – May 18".
  rangeLabel: string;
  // True when `weekEnd` is in the future (today is mid-week). Surface this
  // in the chart so the bar can be drawn at reduced opacity / striped to
  // signal "don't read this as a finished week".
  partial: boolean;
  daysCovered: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface WeeklyCostStackData {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export function aggregateByDate(usage: RuntimeUsage[]): {
  dailyTokens: DailyTokenData[];
  dailyCost: DailyCostData[];
  dailyCostStack: DailyCostStackData[];
  modelDist: ModelDistribution[];
} {
  const dateMap = new Map<string, Omit<DailyTokenData, "label">>();
  const costMap = new Map<string, number>();
  const stackMap = new Map<
    string,
    { input: number; output: number; cacheWrite: number }
  >();
  const modelMap = new Map<string, { tokens: number; cost: number }>();

  for (const u of usage) {
    const existing = dateMap.get(u.date) ?? {
      date: u.date,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    };
    existing.input += u.input_tokens;
    existing.output += u.output_tokens;
    existing.cacheRead += u.cache_read_tokens;
    existing.cacheWrite += u.cache_write_tokens;
    dateMap.set(u.date, existing);

    const dayCost = (costMap.get(u.date) ?? 0) + estimateCost(u);
    costMap.set(u.date, dayCost);

    const breakdown = estimateCostBreakdown(u);
    const stack = stackMap.get(u.date) ?? {
      input: 0,
      output: 0,
      cacheWrite: 0,
    };
    stack.input += breakdown.input;
    stack.output += breakdown.output;
    stack.cacheWrite += breakdown.cacheWrite;
    stackMap.set(u.date, stack);

    const modelName = modelGroupingKey(u.model, u.provider);
    const m = modelMap.get(modelName) ?? { tokens: 0, cost: 0 };
    m.tokens +=
      u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens;
    m.cost += estimateCost(u);
    modelMap.set(modelName, m);
  }

  const formatLabel = (d: string) => {
    const date = new Date(d + "T00:00:00");
    return `${date.getMonth() + 1}/${date.getDate()}`;
  };

  const dailyTokens = Array.from(dateMap.values())
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((d) => ({ ...d, label: formatLabel(d.date) }));

  const dailyCost = Array.from(costMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, cost]) => ({
      date,
      label: formatLabel(date),
      cost: Math.round(cost * 100) / 100,
    }));

  const dailyCostStack = Array.from(stackMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, s]) => {
      const round = (n: number) => Math.round(n * 100) / 100;
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return {
        date,
        label: formatLabel(date),
        input,
        output,
        cacheWrite,
        total: round(input + output + cacheWrite),
      };
    });

  const modelDist = [...modelMap.entries()]
    .map(([model, data]) => ({ model, ...data }))
    .sort((a, b) => b.tokens - a.tokens);

  return { dailyTokens, dailyCost, dailyCostStack, modelDist };
}

// Fold daily-grain rows into ISO calendar weeks (Mon–Sun). Reuses the same
// 180-day cache the daily aggregation reads from — no extra request. The
// latest week is flagged `partial` when today (in the runtime's tz) is
// before Sunday, so the chart can render the in-progress bar at half
// opacity instead of letting the user misread "this week" as a dip.
//
// `weekCount` pins the output to exactly that many trailing calendar weeks
// ending at the week that contains today (in `tz`). Buckets are pre-zeroed,
// so sparse data — including weeks with no usage — renders as empty bars
// rather than disappearing. Rows whose week falls outside the window are
// dropped; without this guard `.slice(-weekCount)` on a sparse 180-day
// aggregate would surface old populated weeks instead of the empty
// in-range buckets the user asked for (MUL-2382 weekly window scoping).
// Accepts any row carrying `date` + token counts + the model needed for
// pricing. Both `RuntimeUsage` (runtime detail) and `DashboardUsageDaily`
// (workspace dashboard) match this shape — there's no behavioural difference,
// just slightly different surrounding fields neither aggregator cares about.
type WeeklyAggregable = Pick<
  RuntimeUsage,
  | "date"
  | "model"
  | "input_tokens"
  | "output_tokens"
  | "cache_read_tokens"
  | "cache_write_tokens"
> & { provider?: string };

export function aggregateByWeek(
  usage: readonly WeeklyAggregable[],
  tz: string,
  weekCount: number,
): {
  weeklyTokens: WeeklyTokenData[];
  weeklyCostStack: WeeklyCostStackData[];
} {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);

  type TokenAgg = Omit<WeeklyTokenData, "label" | "rangeLabel" | "partial" | "daysCovered" | "weekEnd">;
  const tokenMap = new Map<string, TokenAgg>();
  const stackMap = new Map<string, { input: number; output: number; cacheWrite: number }>();

  // Pre-seed every trailing calendar week in the window so sparse / empty
  // weeks still render as zero bars instead of being dropped.
  for (let i = 0; i < count; i++) {
    const wkStart = addDaysIso(firstWeekStart, i * 7);
    tokenMap.set(wkStart, {
      weekStart: wkStart,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    });
    stackMap.set(wkStart, { input: 0, output: 0, cacheWrite: 0 });
  }

  for (const u of usage) {
    const wkStart = weekStartIso(u.date);
    if (wkStart < firstWeekStart || wkStart > currentWeekStart) continue;
    const tokens = tokenMap.get(wkStart);
    if (!tokens) continue;
    tokens.input += u.input_tokens;
    tokens.output += u.output_tokens;
    tokens.cacheRead += u.cache_read_tokens;
    tokens.cacheWrite += u.cache_write_tokens;

    const breakdown = estimateCostBreakdown(u);
    const stack = stackMap.get(wkStart);
    if (!stack) continue;
    stack.input += breakdown.input;
    stack.output += breakdown.output;
    stack.cacheWrite += breakdown.cacheWrite;
  }

  const decorate = (weekStart: string) => {
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    // Inclusive count of how many days of this week have actually elapsed.
    // Sits at 7 for closed weeks, 1..6 for the current week.
    const elapsedDays = Math.min(
      7,
      Math.max(
        1,
        // Day index of `today` within [weekStart, weekEnd] + 1.
        diffDaysIso(weekStart, today < weekStart ? weekStart : today < weekEnd ? today : weekEnd) + 1,
      ),
    );
    return {
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} – ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsedDays : 7,
    };
  };

  const weeklyTokens: WeeklyTokenData[] = Array.from(tokenMap.values())
    .toSorted((a, b) => a.weekStart.localeCompare(b.weekStart))
    .map((t) => ({ ...t, ...decorate(t.weekStart) }));

  const weeklyCostStack: WeeklyCostStackData[] = Array.from(stackMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([weekStart, s]) => {
      const round = (n: number) => Math.round(n * 100) / 100;
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return {
        ...decorate(weekStart),
        input,
        output,
        cacheWrite,
        total: round(input + output + cacheWrite),
      };
    });

  return { weeklyTokens, weeklyCostStack };
}

// Slice a daily-grain usage series into the user's selected window AND the
// immediately prior window of equal length. "Today" is read in the runtime's
// timezone so the cutoff lands on the same calendar boundary the backend
// used when bucketing rows — without this the browser/runtime tz gap could
// shift the boundary by a day at the edges (#MUL-2382 sliceWindow tz bug).
export function sliceWindow(
  usage: readonly RuntimeUsage[],
  days: number,
  tz: string,
): { filtered: RuntimeUsage[]; prevFiltered: RuntimeUsage[] } {
  const today = todayIso(tz);
  const isoCurrent = addDaysIso(today, -days);
  const isoPrev = addDaysIso(today, -days * 2);
  return {
    filtered: usage.filter((u) => u.date >= isoCurrent),
    prevFiltered: usage.filter(
      (u) => u.date >= isoPrev && u.date < isoCurrent,
    ),
  };
}

function diffDaysIso(from: string, to: string): number {
  const [y1, m1, d1] = from.split("-").map(Number);
  const [y2, m2, d2] = to.split("-").map(Number);
  const a = Date.UTC(y1 ?? 1970, (m1 ?? 1) - 1, d1 ?? 1);
  const b = Date.UTC(y2 ?? 1970, (m2 ?? 1) - 1, d2 ?? 1);
  return Math.round((b - a) / 86_400_000);
}

// ---------------------------------------------------------------------------
// Calendar helpers — all date math runs on YYYY-MM-DD strings in the
// runtime's IANA timezone. The backend already groups daily usage by
// `start-of-day in runtime tz`, so we keep the entire frontend aggregation
// on the same axis (Daily / Weekly) to avoid one-day drift when the browser
// and runtime sit in different time zones.
// ---------------------------------------------------------------------------

// Today's calendar date (YYYY-MM-DD) in the given IANA timezone. `en-CA`
// gives ISO-shaped output without us having to assemble Intl parts by hand.
export function todayIso(tz: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: tz,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
}

// Pure date arithmetic on a YYYY-MM-DD string. Uses UTC under the hood so
// DST transitions never shift the result by an hour and round to a
// neighbouring day.
export function addDaysIso(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  dt.setUTCDate(dt.getUTCDate() + days);
  return dt.toISOString().slice(0, 10);
}

// Monday-of-week as YYYY-MM-DD. ISO 8601 week-start, matching the heatmap
// and the team's day-to-day "this week" mental model. Pure string math —
// no `new Date()` reads — so it's stable under any host timezone.
export function weekStartIso(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  const day = dt.getUTCDay(); // 0 = Sun, 1 = Mon, ..., 6 = Sat
  const offset = (day + 6) % 7; // distance back to Monday
  dt.setUTCDate(dt.getUTCDate() - offset);
  return dt.toISOString().slice(0, 10);
}

// "May 12" — short, locale-aware month/day for a YYYY-MM-DD string. Parsing
// via UTC keeps the displayed day stable regardless of the browser's tz.
export function formatShortDate(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  return dt.toLocaleString("en", {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

// ---------------------------------------------------------------------------
// Cost-by-X aggregations
//
// All three "Cost by …" tabs share the same shape: a sorted list of rows
// where each row carries a key (agent name, model name, or hour-of-day),
// total tokens and total cost. The chart / list components are oblivious
// to which axis they're rendering — they just see {key, tokens, cost}.
// ---------------------------------------------------------------------------

export interface CostByKey {
  key: string;
  tokens: number;
  cost: number;
  taskCount: number;
}

// Per-(agent, model) rows → per-agent totals. Cost is summed across all
// models for that agent, then the list is sorted by cost desc so the
// heaviest-spending agent appears first.
export function aggregateCostByAgent(rows: RuntimeUsageByAgent[]): CostByKey[] {
  const map = new Map<string, CostByKey>();
  for (const r of rows) {
    const entry = map.get(r.agent_id) ?? {
      key: r.agent_id,
      tokens: 0,
      cost: 0,
      taskCount: 0,
    };
    entry.tokens +=
      r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    entry.taskCount += r.task_count;
    map.set(r.agent_id, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

// Per-(date, model) rows → per-model totals (the "By model" tab reuses the
// daily-grain data we already cache, so no extra request is needed).
export function aggregateCostByModel(rows: RuntimeUsage[]): CostByKey[] {
  const map = new Map<string, CostByKey>();
  for (const r of rows) {
    const key = modelGroupingKey(r.model, r.provider);
    const entry = map.get(key) ?? { key, tokens: 0, cost: 0, taskCount: 0 };
    entry.tokens +=
      r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    map.set(key, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

// Sum of estimated cost over the trailing window
//   [today − offsetDays − daysBack, today − offsetDays).
// `offsetDays = 0, daysBack = 7` → last 7 days.
// `offsetDays = 7, daysBack = 7` → the 7 days *before* the last 7 (the
// "previous" window for the runtime-list ↑/↓ delta).
//
// "Today" is read in `tz` (the viewer's timezone) so the cutoff lands on
// the same calendar boundary the backend used when bucketing rows — the
// rows arrive bucketed in the viewer's tz, so slicing them with the JS
// engine's local tz would shift the window by a day at the edges.
//
// Walks the same daily-grain `RuntimeUsage` rows that `aggregateByDate` uses,
// so the runtime-list cost stays consistent with the runtime-detail KPIs
// (and crucially, hits the same TanStack Query cache key).
export function computeCostInWindow(
  rows: readonly RuntimeUsage[],
  daysBack: number,
  tz: string,
  offsetDays: number = 0,
): number {
  const today = todayIso(tz);
  const isoEnd = addDaysIso(today, -offsetDays);
  const isoStart = addDaysIso(today, -offsetDays - daysBack);
  let total = 0;
  for (const r of rows) {
    if (r.date >= isoStart && r.date < isoEnd) total += estimateCost(r);
  }
  return total;
}

export function pctChange(current: number, previous: number): number | null {
  if (previous <= 0) return null;
  return Math.round(((current - previous) / previous) * 100);
}
