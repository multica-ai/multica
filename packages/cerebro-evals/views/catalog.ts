import type { CerebroEval, EvalStatus } from "../types";

// catalog.ts holds the pure filtering behind the eval catalog (FIR-3496 Fase 1):
// the status facets shown above the table, the combined search+status matcher
// the list uses, and the per-facet counts. Keeping it out of the React
// component lets the matcher be unit tested without a DOM and keeps the "all"
// facet, the counts, and the table in one place so the control and the list can
// never drift apart.

export type StatusFilter = EvalStatus | "all";

// STATUS_FILTERS is the fixed facet order: an "all" catch-all followed by the
// four lifecycle statuses in draft → retired order.
export const STATUS_FILTERS: StatusFilter[] = ["all", "draft", "active", "paused", "retired"];

// matchesSearch is the existing catalog search — title, key, objective and
// target kind, case-insensitive. An empty needle matches everything.
export function matchesSearch(item: CerebroEval, search: string): boolean {
  const needle = search.trim().toLowerCase();
  if (!needle) return true;
  return [item.title, item.key, item.objective, String(item.target.kind ?? "")].some((value) => value.toLowerCase().includes(needle));
}

// filterEvals applies the status facet and the search needle together. "all"
// keeps every status; any other value keeps only that status.
export function filterEvals(evals: CerebroEval[], status: StatusFilter, search: string): CerebroEval[] {
  return evals.filter((item) => (status === "all" || item.status === status) && matchesSearch(item, search));
}

// statusCounts tallies how many evals sit in each facet, including the "all"
// total, so the control can show a count next to every facet.
export function statusCounts(evals: CerebroEval[]): Record<StatusFilter, number> {
  const counts: Record<StatusFilter, number> = { all: evals.length, draft: 0, active: 0, paused: 0, retired: 0 };
  for (const item of evals) counts[item.status] += 1;
  return counts;
}
