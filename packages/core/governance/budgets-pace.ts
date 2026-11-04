import type { Budget, BudgetPace } from "./budgets-types";

/**
 * How a budget is paced through its period: how much of the period has passed,
 * how much of the limit is spent, and, at the rate so far, when the limit will
 * be reached. The overrun date is null when the pace stays inside the limit.
 */
export function budgetPace(budget: Budget, now: Date): BudgetPace {
  const start = budget.period_start ? Date.parse(budget.period_start) : NaN;
  const end = budget.period_end ? Date.parse(budget.period_end) : NaN;
  const length = end - start;
  const elapsedMs = now.getTime() - start;
  const elapsed = Number.isFinite(length) && length > 0 ? clamp01(elapsedMs / length) : 0;
  const limit = budget.limit_micros;
  const used = limit > 0 ? budget.spent_micros / limit : budget.spent_micros > 0 ? Number.POSITIVE_INFINITY : 0;

  let overrunAt: Date | null = null;
  if (budget.spent_micros > 0 && limit > 0 && elapsedMs > 0 && Number.isFinite(length)) {
    // Spend so far divided by the elapsed time is the rate; the limit is reached
    // when the rate has carried the spend up to it.
    const msToLimit = (limit / budget.spent_micros) * elapsedMs;
    const at = start + msToLimit;
    if (at < end) overrunAt = new Date(at);
  }
  return { elapsed, used, overrunAt };
}

function clamp01(n: number): number {
  return Math.min(1, Math.max(0, n));
}
