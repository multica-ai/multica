/**
 * Phase-5 savings dashboard types + a defensive parser for the
 * GET /api/workspaces/{id}/cost-optimization/dashboard response (FIR-2325).
 *
 * The server returns one entry per saving that has at least one measurement:
 *   - estimated would-save  — the per-run hypothetical saving (shadow + on),
 *     in the saving's native metric unit and, where priced, in US cents.
 *   - measured actually-saved — the holdout A/B: control arm (saving withheld)
 *     vs. treatment arm (saving applied). Present only once both arms have runs;
 *     null otherwise.
 *
 * The client returns the body as `unknown` (see api.getCostOptimizationDashboard)
 * so the contract is validated here, at the boundary, instead of trusting the
 * shape. Per the repo's API Response Compatibility rule, every field is parsed
 * with a default and an unknown shape degrades to an empty dashboard rather than
 * throwing into the settings page.
 */

import type { CostSavingKey, CostSavingMetric } from "./registry";

/** The holdout A/B result for one saving, all in US cents. */
export interface DashboardMeasured {
  treatmentAvgCostCents: number;
  controlAvgCostCents: number;
  /** control avg − treatment avg: the cost a run avoids by applying the saving. */
  savedPerRunCents: number;
  /** savedPerRunCents projected across the treatment runs. */
  totalSavedCents: number;
}

/** One saving's dashboard row. */
export interface DashboardSaving {
  savingKey: CostSavingKey;
  metric: CostSavingMetric;
  shadowRunCount: number;
  treatmentRunCount: number;
  controlRunCount: number;
  /** Per-run hypothetical saving summed in the metric's native unit. */
  estimatedSavedUnits: number;
  /** Same in money where the metric is priced; 0 for unpriced (platform_calls). */
  estimatedSavedCents: number;
  /** Holdout A/B; null until both a treatment and a control arm have runs. */
  measured: DashboardMeasured | null;
}

export type CostOptimizationDashboard = DashboardSaving[];

const KNOWN_KEYS: ReadonlySet<string> = new Set<CostSavingKey>([
  "snapshot_prompt",
  "bundled_read",
  "model_routing",
  "prune_tool_results",
]);

/** Coerce an unknown to a finite number, falling back to 0. */
function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/** Parse the measured A/B block; returns null on any shape it doesn't recognize. */
function parseMeasured(raw: unknown): DashboardMeasured | null {
  if (!isRecord(raw)) return null;
  return {
    treatmentAvgCostCents: num(raw.treatment_avg_cost_cents),
    controlAvgCostCents: num(raw.control_avg_cost_cents),
    savedPerRunCents: num(raw.saved_per_run_cents),
    totalSavedCents: num(raw.total_saved_cents),
  };
}

/**
 * Validate the dashboard response. Unknown saving keys (a server newer than this
 * build) are dropped rather than rendered as a blank card; everything else
 * defaults so a missing field never white-screens the settings page.
 */
export function parseDashboardResponse(raw: unknown): CostOptimizationDashboard {
  if (!isRecord(raw) || !Array.isArray(raw.savings)) return [];
  const out: DashboardSaving[] = [];
  for (const entry of raw.savings) {
    if (!isRecord(entry)) continue;
    const key = entry.saving_key;
    if (typeof key !== "string" || !KNOWN_KEYS.has(key)) continue;
    out.push({
      savingKey: key as CostSavingKey,
      metric: String(entry.metric ?? "") as CostSavingMetric,
      shadowRunCount: num(entry.shadow_run_count),
      treatmentRunCount: num(entry.treatment_run_count),
      controlRunCount: num(entry.control_run_count),
      estimatedSavedUnits: num(entry.estimated_saved_units),
      estimatedSavedCents: num(entry.estimated_saved_cents),
      measured: parseMeasured(entry.measured),
    });
  }
  return out;
}

/** Format US cents as a dollar string, e.g. 12345 → "$123.45". */
export function formatUsd(cents: number): string {
  const sign = cents < 0 ? "-" : "";
  const abs = Math.abs(cents);
  return `${sign}$${(abs / 100).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

/** Human label for a metric's native unit, used when there is no dollar figure. */
export function metricUnitLabel(metric: CostSavingMetric, count: number): string {
  switch (metric) {
    case "platform_calls":
      return count === 1 ? "platform call" : "platform calls";
    case "input_tokens":
    case "context_tokens":
      return count === 1 ? "token" : "tokens";
    case "model_cost":
      return "model cost";
    default:
      return "units";
  }
}

/**
 * The estimated would-save figure as a display string: dollars when the saving
 * is money-denominated, otherwise its native units.
 *
 * model_cost is always money and must render as dollars for every sign —
 * including zero and negative (the saving cost more than the baseline). Pricing
 * it as native units printed the raw cents next to a "model cost" label, e.g.
 * "-200 model cost" instead of "-$2.00". Other metrics show dollars only when a
 * positive priced figure exists, else their native units (e.g. tokens).
 */
export function estimatedValue(saving: DashboardSaving): string {
  if (saving.metric === "model_cost" || saving.estimatedSavedCents > 0) {
    return formatUsd(saving.estimatedSavedCents);
  }
  const unit = metricUnitLabel(saving.metric, saving.estimatedSavedUnits);
  return `${saving.estimatedSavedUnits.toLocaleString("en-US")} ${unit}`;
}
