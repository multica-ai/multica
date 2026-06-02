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

/**
 * The real saving accrued on runs where the saving was applied (mode=on, not
 * held out). Available even at 100% rollout with no control arm, so it is what
 * fills the dashboard's measured column when there is no holdout A/B.
 */
export interface DashboardApplied {
  /** Real saving summed in the metric's native unit (tokens, platform calls). */
  savedUnits: number;
  /** Same in money where the metric is priced; 0 for unpriced (platform_calls). */
  savedCents: number;
  /** Number of applied (treatment) runs behind the figure. */
  runCount: number;
}

/** One saving's dashboard row. */
export interface DashboardSaving {
  savingKey: CostSavingKey;
  metric: CostSavingMetric;
  shadowRunCount: number;
  treatmentRunCount: number;
  controlRunCount: number;
  /** Hypothetical would-save (shadow + held-out control) in the metric's unit. */
  estimatedSavedUnits: number;
  /** Same in money where the metric is priced; 0 for unpriced (platform_calls). */
  estimatedSavedCents: number;
  /** Real measured saving on applied runs; null until a treatment run exists. */
  applied: DashboardApplied | null;
  /** Holdout A/B; null until both a treatment and a control arm have runs. */
  measured: DashboardMeasured | null;
}

export type CostOptimizationDashboard = DashboardSaving[];

const KNOWN_KEYS: ReadonlySet<string> = new Set<CostSavingKey>([
  "snapshot_prompt",
  "bundled_read",
  "model_routing",
  "prune_tool_results",
  "context_duplication",
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

/** Parse the applied saving block; returns null on any shape it doesn't recognize. */
function parseApplied(raw: unknown): DashboardApplied | null {
  if (!isRecord(raw)) return null;
  return {
    savedUnits: num(raw.saved_units),
    savedCents: num(raw.saved_cents),
    runCount: num(raw.run_count),
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
      applied: parseApplied(entry.applied),
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
    case "tokens":
      return count === 1 ? "token" : "tokens";
    case "model_cost":
      return "model cost";
    default:
      return "units";
  }
}

/**
 * The estimated would-save figure as a display string, chosen by metric:
 *
 * - model_cost is always money and must render as dollars for every sign —
 *   including zero and negative (the saving cost more than the baseline).
 * - Token metrics (context_tokens, input_tokens) ALWAYS lead with the token
 *   count, because the saved context IS a token quantity — that is the headline
 *   number for the prune card. The dollar value rides alongside in parentheses
 *   when the tokens are priced, so the card never hides the token figure behind
 *   a bare dollar amount (FIR-2639).
 * - Other metrics show dollars when a positive priced figure exists, else their
 *   native units (e.g. platform calls).
 */
function savingValue(
  metric: CostSavingMetric,
  units: number,
  cents: number,
): string {
  if (metric === "model_cost") {
    return formatUsd(cents);
  }
  if (
    metric === "context_tokens" ||
    metric === "input_tokens" ||
    metric === "tokens"
  ) {
    const tokens = `${units.toLocaleString("en-US")} ${metricUnitLabel(metric, units)}`;
    return cents > 0 ? `${tokens} (${formatUsd(cents)})` : tokens;
  }
  if (cents > 0) {
    return formatUsd(cents);
  }
  const unit = metricUnitLabel(metric, units);
  return `${units.toLocaleString("en-US")} ${unit}`;
}

export function estimatedValue(saving: DashboardSaving): string {
  return savingValue(
    saving.metric,
    saving.estimatedSavedUnits,
    saving.estimatedSavedCents,
  );
}

/**
 * The applied (real, measured) saving as a display string, or "—" when no run
 * has applied the saving yet. Same money-vs-units rule as estimatedValue, so the
 * dashboard's measured column reads consistently. This is the figure that works
 * at 100% rollout, where the holdout A/B is unavailable.
 */
export function appliedValue(saving: DashboardSaving): string {
  if (!saving.applied) return "—";
  return savingValue(
    saving.metric,
    saving.applied.savedUnits,
    saving.applied.savedCents,
  );
}
