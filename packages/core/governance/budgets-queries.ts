import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { governanceKeys } from "./queries";
import type { BudgetHold, BudgetPeriodRow } from "./budgets-types";

export const budgetKeys = {
  list: (wsId: string) => [...governanceKeys.all(wsId), "budgets"] as const,
  periods: (wsId: string, budgetId: string) => [...governanceKeys.all(wsId), "budget-periods", budgetId] as const,
  holds: (wsId: string) => [...governanceKeys.all(wsId), "budget-holds"] as const,
};

/** Every budget of the workspace with its current period. */
export function budgetsOptions(wsId: string) {
  return queryOptions({
    queryKey: budgetKeys.list(wsId),
    queryFn: () => api.listBudgets(),
    enabled: !!wsId,
  });
}

/** One budget's periods, newest first. */
export function budgetPeriodsOptions(wsId: string, budgetId: string) {
  return queryOptions({
    queryKey: budgetKeys.periods(wsId, budgetId),
    queryFn: (): Promise<BudgetPeriodRow[]> => api.listBudgetPeriods(budgetId),
    enabled: !!wsId && !!budgetId,
  });
}

/** The runs held for a budget at its limit, not yet released. */
export function budgetHoldsOptions(wsId: string) {
  return queryOptions({
    queryKey: budgetKeys.holds(wsId),
    queryFn: (): Promise<BudgetHold[]> => api.listBudgetHolds(),
    enabled: !!wsId,
  });
}
