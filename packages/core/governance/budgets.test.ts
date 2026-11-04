import { describe, expect, it } from "vitest";
import { budgetPace } from "./budgets-pace";
import { BudgetListSchema, parseBudget } from "./budgets-schemas";
import type { Budget } from "./budgets-types";

const month = (over: Partial<Budget> = {}): Budget => ({
  id: "b1",
  scope_type: "company",
  scope_id: null,
  period: "month",
  amount_micros: 100_000_000,
  soft_percent: 80,
  mode: "hold",
  classes: ["routine", "delegated"],
  status: "active",
  spent_micros: 0,
  extended_micros: 0,
  limit_micros: 100_000_000,
  runs: 0,
  state: "ok",
  period_start: "2026-11-01T00:00:00.000Z",
  period_end: "2026-12-01T00:00:00.000Z",
  created_at: "2026-10-01T00:00:00.000Z",
  updated_at: "2026-10-01T00:00:00.000Z",
  ...over,
});

const midNovember = new Date("2026-11-16T00:00:00.000Z");

describe("budgetPace", () => {
  it("reports the elapsed share of the period and the share spent", () => {
    const p = budgetPace(month({ spent_micros: 50_000_000 }), midNovember);
    expect(p.elapsed).toBeCloseTo(0.5, 1);
    expect(p.used).toBeCloseTo(0.5, 5);
    expect(p.overrunAt).toBeNull();
  });

  it("projects the overrun date at the pace so far when spend will reach the limit", () => {
    // 60 of 100 spent at the halfway point reaches the limit at 5/6 of the month.
    const p = budgetPace(month({ spent_micros: 60_000_000 }), midNovember);
    expect(p.overrunAt).not.toBeNull();
    const at = p.overrunAt!.getTime();
    expect(at).toBeGreaterThan(Date.parse("2026-11-25T00:00:00.000Z"));
    expect(at).toBeLessThan(Date.parse("2026-12-01T00:00:00.000Z"));
  });

  it("projects nothing before any spend, and clamps the elapsed share to the period", () => {
    expect(budgetPace(month(), midNovember).overrunAt).toBeNull();
    expect(budgetPace(month(), new Date("2026-12-09T00:00:00.000Z")).elapsed).toBe(1);
    expect(budgetPace(month(), new Date("2026-10-01T00:00:00.000Z")).elapsed).toBe(0);
  });
});

describe("budget schemas", () => {
  it("degrades an unknown state and drops a malformed budget alone", () => {
    const raw = {
      budgets: [
        { id: "ok", scope_type: "squad", scope_id: "s1", state: "soft", amount_micros: "5", classes: ["routine"] },
        { id: "bad-scope", scope_type: "planet", state: "weird" },
        "not an object",
      ],
    };
    const parsed = BudgetListSchema.parse(raw);
    expect(parsed.budgets.map((b) => b.id)).toEqual(["ok", "bad-scope"]);
    expect(parsed.budgets[0]).toMatchObject({ state: "soft", amount_micros: 5, scope_id: "s1" });
    expect(parsed.budgets[1]).toMatchObject({ scope_type: "company", state: "unknown" });
  });

  it("reads a missing or non-list budgets field as none", () => {
    expect(BudgetListSchema.parse({}).budgets).toEqual([]);
    expect(BudgetListSchema.parse({ budgets: "nope" }).budgets).toEqual([]);
  });

  it("falls back to an empty budget with nothing spent for a malformed single response", () => {
    const b = parseBudget(null, "test");
    expect(b.spent_micros).toBe(0);
    expect(b.state).toBe("unknown");
  });
});
