import type {
  RuntimeUsage,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
} from "@multica/core/types";

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

// Pricing per million tokens (USD). Anthropic figures sourced from
// https://platform.claude.com/docs/en/about-claude/pricing; OpenAI figures
// from https://openai.com/api/pricing — keep in sync when providers release
// new models or adjust prices.
//
// Anthropic's cacheWrite reflects the 5-minute cache TTL (1.25× input); the
// daemon reports cache_creation_input_tokens without TTL metadata, so 5m is
// the safest / cheapest assumption (matches the API default). OpenAI does
// not bill cache writes separately (cached input is just discounted on
// subsequent reads), so cacheWrite mirrors input there.
//
// The resolver matches exact keys after stripping a trailing date snapshot
// (see `resolvePricing` below). It deliberately does NOT do startsWith
// fallbacks: every catalog SKU needs its own row. That keeps unfamiliar
// variants (`gpt-5.5-mini`, hypothetical `gpt-5.4-foo`) from silently
// inheriting the price of a near-named relative; they surface in the
// unmapped diagnostic instead. Mirror new entries in
// `server/pkg/agent/models.go` so the catalog and pricing stay in sync.
const MODEL_PRICING: Record<
  string,
  { input: number; output: number; cacheRead: number; cacheWrite: number }
> = {
  // -- Anthropic: current generation (4.5+ — Opus dropped from 15/75 to 5/25 here) --
  "claude-haiku-4-5":   { input: 1,    output: 5,    cacheRead: 0.10, cacheWrite: 1.25 },
  "claude-sonnet-4-5":  { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },
  "claude-sonnet-4-6":  { input: 3,    output: 15,   cacheRead: 0.30, cacheWrite: 3.75 },
  "claude-opus-4-5":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-6":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },
  "claude-opus-4-7":    { input: 5,    output: 25,   cacheRead: 0.50, cacheWrite: 6.25 },

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
};

// Resolve a model string to its pricing tier. Exact match, with one
// tolerance: providers ship dated snapshots (`claude-sonnet-4-5-20250929`,
// `gpt-5-2025-08-07`) where the family is what we price and the date is
// volatile, so we strip a trailing date / "latest" tag and try again.
// Anything still unmapped after that is genuinely unknown; return
// undefined so callers can distinguish "$0 spend" from "spent but model
// not priced". No startsWith fallback: variants like `gpt-5.5-mini` must
// have their own row to be priced (otherwise they'd inherit `gpt-5.5`).
function resolvePricing(model: string) {
  if (!model) return undefined;
  if (MODEL_PRICING[model]) return MODEL_PRICING[model];

  const stripped = model.replace(/-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$/, "");
  if (stripped !== model && MODEL_PRICING[stripped]) return MODEL_PRICING[stripped];

  return undefined;
}

// Cheap predicate for the empty-state diagnostic: which model strings in a
// usage batch failed pricing resolution. Useful when the user is staring at
// "$0.00 / 2M tokens" and wants to know why.
export function isModelPriced(model: string): boolean {
  return resolvePricing(model) !== undefined;
}

// Returns the unique, sorted list of model strings present in `rows` that
// don't resolve to a price. Empty when everything's priced or there are no
// rows.
export function collectUnmappedModels(rows: readonly Priceable[]): string[] {
  const set = new Set<string>();
  for (const r of rows) {
    if (r.model && !isModelPriced(r.model)) set.add(r.model);
  }
  return [...set].sort();
}

// Anything carrying per-model token totals can be priced — RuntimeUsage,
// RuntimeUsageByAgent, RuntimeUsageByHour all share this shape on purpose
// (the back-end keeps the model dimension specifically so the client can
// run this calculation for any aggregation axis).
type Priceable = Pick<
  RuntimeUsage,
  "model" | "input_tokens" | "output_tokens" | "cache_read_tokens" | "cache_write_tokens"
>;

export function estimateCost(usage: Priceable): number {
  const pricing = resolvePricing(usage.model);
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
  const pricing = resolvePricing(usage.model);
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
  const pricing = resolvePricing(usage.model);
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

    const modelName = u.model || u.provider;
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

  const dailyTokens = [...dateMap.values()]
    .sort((a, b) => a.date.localeCompare(b.date))
    .map((d) => ({ ...d, label: formatLabel(d.date) }));

  const dailyCost = [...costMap.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, cost]) => ({
      date,
      label: formatLabel(date),
      cost: Math.round(cost * 100) / 100,
    }));

  const dailyCostStack = [...stackMap.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
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
  return [...map.values()].sort((a, b) => b.cost - a.cost);
}

// Per-(date, model) rows → per-model totals (the "By model" tab reuses the
// daily-grain data we already cache, so no extra request is needed).
export function aggregateCostByModel(rows: RuntimeUsage[]): CostByKey[] {
  const map = new Map<string, CostByKey>();
  for (const r of rows) {
    const key = r.model || r.provider || "unknown";
    const entry = map.get(key) ?? { key, tokens: 0, cost: 0, taskCount: 0 };
    entry.tokens +=
      r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    map.set(key, entry);
  }
  return [...map.values()].sort((a, b) => b.cost - a.cost);
}

// Per-(hour, model) rows → 24 fixed buckets (0..23). Hours with no activity
// stay in the list as empty rows so the bar chart axis stays continuous.
export function aggregateCostByHour(rows: RuntimeUsageByHour[]): CostByKey[] {
  const buckets = new Map<number, CostByKey>();
  for (let h = 0; h < 24; h++) {
    buckets.set(h, { key: String(h), tokens: 0, cost: 0, taskCount: 0 });
  }
  for (const r of rows) {
    const entry = buckets.get(r.hour);
    if (!entry) continue;
    entry.tokens +=
      r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    entry.taskCount += r.task_count;
  }
  return [...buckets.values()];
}

// "Cost · 30D" KPI hint: percentage delta vs. the immediately prior window
// of equal length. Returns null when there's no comparable prior data
// (caller renders nothing rather than a misleading "+∞%").
// Sum of estimated cost over the trailing window
//   [today − offsetDays − daysBack, today − offsetDays).
// `offsetDays = 0, daysBack = 7` → last 7 days.
// `offsetDays = 7, daysBack = 7` → the 7 days *before* the last 7 (the
// "previous" window for the runtime-list ↑/↓ delta).
//
// Walks the same daily-grain `RuntimeUsage` rows that `aggregateByDate` uses,
// so the runtime-list cost stays consistent with the runtime-detail KPIs
// (and crucially, hits the same TanStack Query cache key).
export function computeCostInWindow(
  rows: readonly RuntimeUsage[],
  daysBack: number,
  offsetDays: number = 0,
): number {
  const now = new Date();
  const end = new Date(now);
  end.setDate(now.getDate() - offsetDays);
  const start = new Date(now);
  start.setDate(now.getDate() - offsetDays - daysBack);
  const isoEnd = end.toISOString().slice(0, 10);
  const isoStart = start.toISOString().slice(0, 10);
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
