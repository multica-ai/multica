import type {
  RuntimeUsage,
  RuntimeUsageByAgent,
  RuntimeUsageByHour,
} from "@multica/core/types";

import { PRICING, type ModelCost } from "./pricing.generated";

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

// Internal pricing shape used by the cost-estimation functions. The
// generated `ModelCost` type mirrors `models.dev` exactly (snake_case,
// optional `cache_read` / `cache_write`); we adapt it to non-optional
// camelCase fields here so callers stay simple. Where the upstream omits
// a cache tier we fall back to `input` — conservative (no discount
// applied) and keeps `estimateCacheSavings` returning 0 naturally.
interface ResolvedPricing {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

function adapt(p: ModelCost): ResolvedPricing {
  return {
    input: p.input,
    output: p.output,
    cacheRead: p.cache_read ?? p.input,
    cacheWrite: p.cache_write ?? p.input,
  };
}

// Strip a leading `provider/` segment, e.g. `openai/gpt-4o` → `gpt-4o`.
function stripProviderPrefix(model: string): string {
  const slash = model.indexOf("/");
  return slash > 0 ? model.slice(slash + 1) : model;
}

// Strip a trailing date / `-latest` tag, e.g. `claude-sonnet-4-5-20250929`
// → `claude-sonnet-4-5`. Anthropic, OpenAI and Google all version their
// model snapshots this way; the family is what we price.
function stripDateSuffix(model: string): string {
  return model.replace(/-(20\d{2}-?\d{2}-?\d{2}|latest)$/, "");
}

// Resolve a model string to its pricing tier. PRICING is keyed by
// `provider/model`. Walked in two passes so exact bare matches always
// win over prefix matches — otherwise `gpt-4o-2024-05-13` would resolve
// to `openai/gpt-4o`'s price (alphabetically first; `keyBare.startsWith`
// is true) instead of the dated entry's specific price.
//   1. Exact match on the full string (`provider/model`).
//   2. First pass — exact bare match (`keyBare === bare`). Catches a
//      bare-keyed input (`gpt-4o`, `gpt-4o-mini`, `gpt-4o-2024-05-13`)
//      hitting the corresponding `provider/<id>` entry verbatim.
//   3. Second pass — `keyBare.startsWith(withoutDate)`. Catches a
//      date-suffixed input whose exact dated entry isn't tracked
//      (`claude-opus-4-1-20260105` falls back to `claude-opus-4-1`).
function resolvePricing(model: string): ResolvedPricing | undefined {
  if (!model) return undefined;

  const exact = PRICING[model];
  if (exact) return adapt(exact);

  const bare = stripProviderPrefix(model);
  const withoutDate = stripDateSuffix(bare);

  for (const [key, p] of Object.entries(PRICING)) {
    if (stripProviderPrefix(key) === bare) return adapt(p);
  }

  for (const [key, p] of Object.entries(PRICING)) {
    if (stripProviderPrefix(key).startsWith(withoutDate)) return adapt(p);
  }
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
