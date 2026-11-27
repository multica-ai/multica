import type { DashboardUsageBySource } from "@multica/core/types";
import { estimateCost, weekStartIso } from "../runtimes/utils";

export interface SourceTotals {
  manualTokens: number;
  autopilotTokens: number;
  manualCost: number;
  autopilotCost: number;
}

export interface SourceBucket {
  /** Day, or the Monday of the week. */
  date: string;
  manual: number;
  autopilot: number;
}

export interface AutopilotUsageRow {
  autopilotId: string;
  tokens: number;
  cost: number;
}

export type SourceMetric = "tokens" | "cost";

function rowTokens(r: DashboardUsageBySource): number {
  return r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
}

function rowValue(r: DashboardUsageBySource, metric: SourceMetric): number {
  return metric === "cost" ? estimateCost(r) : rowTokens(r);
}

export function sourceTotals(rows: readonly DashboardUsageBySource[]): SourceTotals {
  const t: SourceTotals = { manualTokens: 0, autopilotTokens: 0, manualCost: 0, autopilotCost: 0 };
  for (const r of rows) {
    const tokens = rowTokens(r);
    const cost = estimateCost(r);
    if (r.autopilot_id) {
      t.autopilotTokens += tokens;
      t.autopilotCost += cost;
    } else {
      t.manualTokens += tokens;
      t.manualCost += cost;
    }
  }
  return t;
}

/** Manual vs autopilot per day (or per Monday-first week), oldest first. */
export function sourceBuckets(
  rows: readonly DashboardUsageBySource[],
  metric: SourceMetric,
  weekly: boolean,
): SourceBucket[] {
  const map = new Map<string, SourceBucket>();
  for (const r of rows) {
    const date = weekly ? weekStartIso(r.date) : r.date;
    const bucket = map.get(date) ?? { date, manual: 0, autopilot: 0 };
    if (r.autopilot_id) bucket.autopilot += rowValue(r, metric);
    else bucket.manual += rowValue(r, metric);
    map.set(date, bucket);
  }
  return [...map.values()].sort((a, b) => a.date.localeCompare(b.date));
}

/** Autopilots ranked by the chosen metric, largest first. */
export function usageByAutopilot(
  rows: readonly DashboardUsageBySource[],
  metric: SourceMetric,
): AutopilotUsageRow[] {
  const map = new Map<string, AutopilotUsageRow>();
  for (const r of rows) {
    if (!r.autopilot_id) continue;
    const row = map.get(r.autopilot_id) ?? { autopilotId: r.autopilot_id, tokens: 0, cost: 0 };
    row.tokens += rowTokens(r);
    row.cost += estimateCost(r);
    map.set(r.autopilot_id, row);
  }
  const key = metric === "cost" ? "cost" : "tokens";
  return [...map.values()].sort((a, b) => b[key] - a[key] || a.autopilotId.localeCompare(b.autopilotId));
}
