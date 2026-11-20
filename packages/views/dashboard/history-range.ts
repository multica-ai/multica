import type { WorkspacePlanLimits } from "@multica/core/types";
import { TIME_RANGES, type TimeRange, type TimeRangeOption } from "./components/dashboard-shared";

/**
 * Plan-limited dashboard ranges (MUL-9490).
 *
 * `maxDays` is the enforced history limit, null for none. A range longer than
 * it is locked in the menu, and the page reads the longest range that fits.
 */

/** Whether the plan covers a range. */
export function isRangeAllowed(days: TimeRange, maxDays: number | null): boolean {
  return maxDays === null || days <= maxDays;
}

/**
 * The range the page reads: the selection when it is offered and the plan
 * covers it, otherwise the longest offered range that fits. Derived on every render and never written back,
 * so a plan change moves the page and an upgrade restores the selection.
 */
export function effectiveRange(
  selected: TimeRange,
  maxDays: number | null,
  ranges: readonly TimeRangeOption[] = TIME_RANGES,
): TimeRange {
  const offered = ranges.some((r) => r.days === selected);
  if (offered && isRangeAllowed(selected, maxDays)) return selected;
  let best: TimeRange = TIME_RANGES[0].days;
  for (const range of ranges) {
    if (range.days <= (maxDays ?? Infinity) && range.days > best) best = range.days;
  }
  return best;
}

export type UpgradePlan = "pro" | "business";

/**
 * The cheapest paid plan whose history covers a range, from the limits Cloud
 * enforces. Null when the limits are unknown or no plan covers it, so the menu
 * shows only the lock rather than pointing at a plan that may be wrong.
 */
export function planForRange(
  days: TimeRange,
  planLimits: WorkspacePlanLimits | null | undefined,
): UpgradePlan | null {
  if (!planLimits) return null;
  for (const plan of ["pro", "business"] as const) {
    const limit = planLimits[plan].dashboardHistoryDays;
    if (limit !== null && limit >= days) return plan;
  }
  return null;
}

/**
 * The first calendar day the plan lets the page read, as an ISO date, or null
 * when nothing is enforced. `today` is today's ISO date in the viewing zone.
 */
export function earliestReadableDay(
  today: string,
  maxDays: number | null,
  addDays: (iso: string, delta: number) => string,
): string | null {
  return maxDays === null ? null : addDays(today, -(maxDays - 1));
}
