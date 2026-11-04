import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import type { Budget, BudgetHold, BudgetPeriodRow, BudgetState } from "./budgets-types";

// Lenient on purpose (see parseWithFallback): a missing field takes its default,
// an unknown enum degrades, and one malformed item is dropped alone.

const num = z.coerce.number().catch(0).default(0);
const str = z.string().catch("").default("");
const nullableStr = z.string().nullable().catch(null).default(null);

const STATES: readonly BudgetState[] = ["ok", "soft", "hard"];
const stateSchema = z
  .string()
  .catch("unknown")
  .transform((v): BudgetState => (STATES.includes(v as BudgetState) ? (v as BudgetState) : "unknown"));

const SCOPE_TYPES = ["company", "floor", "squad", "initiative"] as const;
const PERIODS = ["week", "month", "quarter"] as const;
const MODES = ["warn", "hold"] as const;

export const BudgetSchema = z
  .object({
    id: str,
    scope_type: z.enum(SCOPE_TYPES).catch("company").default("company"),
    scope_id: nullableStr,
    period: z.enum(PERIODS).catch("month").default("month"),
    amount_micros: num,
    soft_percent: z.coerce.number().catch(80).default(80),
    mode: z.enum(MODES).catch("warn").default("warn"),
    classes: z.array(z.string()).catch([]).default([]),
    status: z.enum(["active", "paused"]).catch("active").default("active"),
    spent_micros: num,
    extended_micros: num,
    limit_micros: num,
    runs: num,
    state: stateSchema.default("unknown"),
    period_start: nullableStr,
    period_end: nullableStr,
    created_at: str,
    updated_at: str,
  })
  .loose();

export const BudgetPeriodSchema = z
  .object({
    period_start: str,
    period_end: str,
    spent_micros: num,
    extended_micros: num,
    runs: num,
    state: stateSchema.default("unknown"),
  })
  .loose();

export const BudgetHoldSchema = z
  .object({
    id: str,
    budget_id: str,
    task_id: str,
    agent_id: nullableStr,
    squad_id: nullableStr,
    run_class: str,
    scope_type: z.enum(SCOPE_TYPES).catch("company").default("company"),
    scope_id: nullableStr,
    period: z.enum(PERIODS).catch("month").default("month"),
    created_at: str,
  })
  .loose();

/** A list read item by item: a missing or non-list value is none, a bad item is dropped. */
function listOfItems<T>(schema: z.ZodType<T>) {
  return z.unknown().transform((value) =>
    Array.isArray(value)
      ? value.flatMap((item) => {
          const parsed = schema.safeParse(item);
          return parsed.success ? [parsed.data] : [];
        })
      : [],
  );
}

export const BudgetListSchema = z.object({ budgets: listOfItems(BudgetSchema) }).loose();
export const BudgetPeriodListSchema = z.object({ periods: listOfItems(BudgetPeriodSchema) }).loose();
export const BudgetHoldListSchema = z.object({ holds: listOfItems(BudgetHoldSchema) }).loose();

export const FALLBACK_BUDGET_LIST: { budgets: Budget[] } = { budgets: [] };
export const FALLBACK_BUDGET_PERIODS: { periods: BudgetPeriodRow[] } = { periods: [] };
export const FALLBACK_BUDGET_HOLDS: { holds: BudgetHold[] } = { holds: [] };

/** Parses one budget response; the fallback is the same budget with no spend, so a bad body shows nothing spent. */
export function parseBudget(raw: unknown, endpoint: string): Budget {
  return parseWithFallback<Budget>(raw, BudgetSchema, emptyBudget(), { endpoint }) as Budget;
}

function emptyBudget(): Budget {
  return {
    id: "",
    scope_type: "company",
    scope_id: null,
    period: "month",
    amount_micros: 0,
    soft_percent: 80,
    mode: "warn",
    classes: [],
    status: "active",
    spent_micros: 0,
    extended_micros: 0,
    limit_micros: 0,
    runs: 0,
    state: "unknown",
    period_start: null,
    period_end: null,
    created_at: "",
    updated_at: "",
  };
}
