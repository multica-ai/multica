import type { CostSavingKey, CostSavingMetric } from "./registry";
import { formatUsd } from "./dashboard";

export interface AnalyticsSavingSummary {
  savingKey: CostSavingKey;
  metric: CostSavingMetric;
  runsToday: number;
  savedUnitsToday: number;
  savedCentsToday: number;
  runs7d: number;
  savedUnits7d: number;
  savedCents7d: number;
  runs30d: number;
  savedUnits30d: number;
  savedCents30d: number;
}

export interface AnalyticsIssueRow {
  issueId: string;
  issueNumber: number;
  issueTitle: string;
  runCount: number;
  savedUnits: number;
  savedCents: number;
}

export interface AnalyticsRunRow {
  taskId: string;
  createdAt: string;
  mode: string;
  applied: boolean;
  heldOut: boolean;
  metric: CostSavingMetric;
  baselineValue: number;
  effectiveValue: number;
  savedUnits: number;
  savedCents: number;
  detailJson?: unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function parseSavingSummary(row: unknown): AnalyticsSavingSummary | null {
  if (!isRecord(row)) return null;
  const savingKey = str(row.saving_key);
  if (!savingKey) return null;
  return {
    savingKey: savingKey as CostSavingKey,
    metric: (str(row.metric) || "platform_calls") as CostSavingMetric,
    runsToday: num(row.runs_today),
    savedUnitsToday: num(row.saved_units_today),
    savedCentsToday: num(row.saved_cents_today),
    runs7d: num(row.runs_7d),
    savedUnits7d: num(row.saved_units_7d),
    savedCents7d: num(row.saved_cents_7d),
    runs30d: num(row.runs_30d),
    savedUnits30d: num(row.saved_units_30d),
    savedCents30d: num(row.saved_cents_30d),
  };
}

function parseIssueRow(row: unknown): AnalyticsIssueRow | null {
  if (!isRecord(row)) return null;
  const issueId = str(row.issue_id);
  if (!issueId) return null;
  return {
    issueId,
    issueNumber: num(row.issue_number),
    issueTitle: str(row.issue_title),
    runCount: num(row.run_count),
    savedUnits: num(row.saved_units),
    savedCents: num(row.saved_cents),
  };
}

function parseRunRow(row: unknown): AnalyticsRunRow | null {
  if (!isRecord(row)) return null;
  const taskId = str(row.task_id);
  if (!taskId) return null;
  let detailJson: unknown;
  if (row.detail_json !== undefined && row.detail_json !== null) {
    if (typeof row.detail_json === "string") {
      try {
        detailJson = JSON.parse(row.detail_json);
      } catch {
        detailJson = row.detail_json;
      }
    } else {
      detailJson = row.detail_json;
    }
  }
  return {
    taskId,
    createdAt: str(row.created_at),
    mode: str(row.mode),
    applied: row.applied === true,
    heldOut: row.held_out === true,
    metric: (str(row.metric) || "platform_calls") as CostSavingMetric,
    baselineValue: num(row.baseline_value),
    effectiveValue: num(row.effective_value),
    savedUnits: num(row.saved_units),
    savedCents: num(row.saved_cents),
    detailJson,
  };
}

export function parseAnalyticsSummaryResponse(
  body: unknown,
): AnalyticsSavingSummary[] {
  if (!isRecord(body) || !Array.isArray(body.savings)) return [];
  const out: AnalyticsSavingSummary[] = [];
  for (const row of body.savings) {
    const parsed = parseSavingSummary(row);
    if (parsed) out.push(parsed);
  }
  return out;
}

export function parseAnalyticsIssuesResponse(body: unknown): AnalyticsIssueRow[] {
  if (!isRecord(body) || !Array.isArray(body.issues)) return [];
  const out: AnalyticsIssueRow[] = [];
  for (const row of body.issues) {
    const parsed = parseIssueRow(row);
    if (parsed) out.push(parsed);
  }
  return out;
}

export function parseAnalyticsRunsResponse(body: unknown): AnalyticsRunRow[] {
  if (!isRecord(body) || !Array.isArray(body.runs)) return [];
  const out: AnalyticsRunRow[] = [];
  for (const row of body.runs) {
    const parsed = parseRunRow(row);
    if (parsed) out.push(parsed);
  }
  return out;
}

/** Legacy rows stored avoided platform calls as units; translate for display. */
const LEGACY_TOKENS_PER_AVOIDED_PLATFORM_CALL = 800;

function legacyCallsToTokens(units: number): number {
  return units * LEGACY_TOKENS_PER_AVOIDED_PLATFORM_CALL;
}

function normalizeSavedTokens(metric: CostSavingMetric, units: number): number {
  if (metric === "platform_calls") {
    return legacyCallsToTokens(units);
  }
  return units;
}

/** Total saved tokens (handles legacy platform_calls rows). */
export function formatAnalyticsSaved(
  metric: CostSavingMetric,
  units: number,
  cents: number,
): string {
  if (metric === "model_cost") {
    return formatUsd(cents);
  }
  const tokens = normalizeSavedTokens(metric, units);
  const label = `${tokens.toLocaleString("en-US")} tokens`;
  return cents > 0 ? `${label} (${formatUsd(cents)})` : label;
}

/** Average tokens saved per measured run — the headline number for analytics. */
export function formatAnalyticsPerRun(
  metric: CostSavingMetric,
  savedUnits: number,
  savedCents: number,
  runCount: number,
): string {
  if (runCount <= 0) {
    return "—";
  }
  if (metric === "model_cost") {
    const perRun = Math.round(savedCents / runCount);
    return `${formatUsd(perRun)} / run`;
  }
  const tokens = Math.round(normalizeSavedTokens(metric, savedUnits) / runCount);
  const label = `${tokens.toLocaleString("en-US")} tokens/run`;
  return savedCents > 0
    ? `${label} (${formatUsd(Math.round(savedCents / runCount))})`
    : label;
}
