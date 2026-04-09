import type { RuntimeUsage } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

export function formatLastSeen(lastSeenAt: string | null): string {
  if (!lastSeenAt) return "Never";
  const diff = Date.now() - new Date(lastSeenAt).getTime();
  if (diff < 60_000) return "Just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

// ---------------------------------------------------------------------------
// Cost estimation
// ---------------------------------------------------------------------------

// Pricing per million tokens (USD)
const MODEL_PRICING: Record<
  string,
  { input: number; output: number; cacheRead: number; cacheWrite: number }
> = {
  "claude-haiku-4-5": { input: 1, output: 5, cacheRead: 0.1, cacheWrite: 1.25 },
  "claude-sonnet-4-5": { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  "claude-sonnet-4-6": { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  "claude-opus-4-5": { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
  "claude-opus-4-6": { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
};

export function estimateCost(usage: RuntimeUsage): number {
  const model = usage.model;
  let pricing = MODEL_PRICING[model];
  if (!pricing) {
    for (const [key, p] of Object.entries(MODEL_PRICING)) {
      if (model.startsWith(key)) {
        pricing = p;
        break;
      }
    }
  }
  if (!pricing) return 0;

  return (
    (usage.input_tokens * pricing.input +
      usage.output_tokens * pricing.output +
      usage.cache_read_tokens * pricing.cacheRead +
      usage.cache_write_tokens * pricing.cacheWrite) /
    1_000_000
  );
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

export interface ModelDistribution {
  model: string;
  tokens: number;
  cost: number;
}

export function aggregateByDate(usage: RuntimeUsage[]): {
  dailyTokens: DailyTokenData[];
  dailyCost: DailyCostData[];
  modelDist: ModelDistribution[];
} {
  const dateMap = new Map<string, Omit<DailyTokenData, "label">>();
  const costMap = new Map<string, number>();
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

  const modelDist = [...modelMap.entries()]
    .map(([model, data]) => ({ model, ...data }))
    .sort((a, b) => b.tokens - a.tokens);

  return { dailyTokens, dailyCost, modelDist };
}
