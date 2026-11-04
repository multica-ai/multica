/** Budgets (spec 4.2): a spend limit for a scope and period, and the holds it places. */

export const BUDGET_SCOPE_TYPES = ["company", "floor", "squad", "initiative"] as const;
export type BudgetScopeType = (typeof BUDGET_SCOPE_TYPES)[number];

export const BUDGET_PERIODS = ["week", "month", "quarter"] as const;
export type BudgetPeriod = (typeof BUDGET_PERIODS)[number];

/** warn shows the state and holds nothing; hold keeps routine and delegated runs at the limit. */
export const BUDGET_MODES = ["warn", "hold"] as const;
export type BudgetMode = (typeof BUDGET_MODES)[number];

export const BUDGET_STATUSES = ["active", "paused"] as const;
export type BudgetStatus = (typeof BUDGET_STATUSES)[number];

/** A budget's runs that it can hold. A human run is never held, so it is not a choice. */
export const BUDGET_HOLDABLE_CLASSES = ["routine", "delegated"] as const;
export type BudgetHoldableClass = (typeof BUDGET_HOLDABLE_CLASSES)[number];

/** ok below the soft line, soft at soft_percent of the limit, hard at the limit. */
export type BudgetState = "ok" | "soft" | "hard" | "unknown";

/** One budget with its current period's counters, as the server reports it. */
export interface Budget {
  id: string;
  scope_type: BudgetScopeType;
  /** Null for the company scope. */
  scope_id: string | null;
  period: BudgetPeriod;
  amount_micros: number;
  soft_percent: number;
  mode: BudgetMode;
  classes: string[];
  status: BudgetStatus;
  spent_micros: number;
  extended_micros: number;
  /** amount plus the extension granted this period. */
  limit_micros: number;
  runs: number;
  state: BudgetState;
  period_start: string | null;
  period_end: string | null;
  created_at: string;
  updated_at: string;
}

export interface BudgetPeriodRow {
  period_start: string;
  period_end: string;
  spent_micros: number;
  extended_micros: number;
  runs: number;
  state: BudgetState;
}

/** A run held for its budget and not yet released. */
export interface BudgetHold {
  id: string;
  budget_id: string;
  task_id: string;
  agent_id: string | null;
  squad_id: string | null;
  run_class: string;
  scope_type: BudgetScopeType;
  scope_id: string | null;
  period: BudgetPeriod;
  created_at: string;
}

export interface CreateBudgetRequest {
  scope_type: BudgetScopeType;
  scope_id?: string | null;
  period: BudgetPeriod;
  amount_micros: number;
  soft_percent?: number;
  mode: BudgetMode;
  classes?: BudgetHoldableClass[];
}

export interface UpdateBudgetRequest {
  amount_micros?: number;
  soft_percent?: number;
  mode?: BudgetMode;
  classes?: BudgetHoldableClass[];
  status?: BudgetStatus;
}

export interface BudgetPace {
  /** Share of the period that has elapsed, 0 to 1. */
  elapsed: number;
  /** Spent over the limit, as a share of the limit; above 1 is an overrun. */
  used: number;
  /** When the spend reaches the limit at this pace, or null when it will not, or spend is zero. */
  overrunAt: Date | null;
}

/** The workspace's budgets, as the list endpoint returns them. */
export interface BudgetList {
  budgets: Budget[];
}
