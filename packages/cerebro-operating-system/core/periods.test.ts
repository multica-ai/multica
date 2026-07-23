import { describe, expect, it } from "vitest";

import { formatPeriodRange, latestPeriod, nextPeriodInput } from "./periods";
import type { OperatingPeriod } from "./types";

const period = (overrides: Partial<OperatingPeriod>): OperatingPeriod => ({
  id: "period-1", workspace_id: "workspace-1", name: "Q3 2026", unit: "quarter", starts_on: "2026-07-01", ends_on: "2026-09-30", ...overrides,
});

describe("nextPeriodInput", () => {
  it("plans the following quarter after the latest quarter period", () => {
    expect(nextPeriodInput([period({})])).toEqual({ name: "Q4 2026", unit: "quarter", starts_on: "2026-10-01", ends_on: "2026-12-31" });
  });

  it("rolls a Q4 quarter into Q1 of the next year", () => {
    expect(nextPeriodInput([period({ starts_on: "2026-10-01", ends_on: "2026-12-31", name: "Q4 2026" })])).toEqual({ name: "Q1 2027", unit: "quarter", starts_on: "2027-01-01", ends_on: "2027-03-31" });
  });

  it("plans the following month after the latest month period", () => {
    expect(nextPeriodInput([period({ unit: "month", starts_on: "2026-12-01", ends_on: "2026-12-31", name: "December 2026" })])).toEqual({ name: "January 2027", unit: "month", starts_on: "2027-01-01", ends_on: "2027-01-31" });
  });

  it("uses the latest period by end date, not array order", () => {
    const next = nextPeriodInput([
      period({ id: "later", starts_on: "2026-10-01", ends_on: "2026-12-31" }),
      period({ id: "earlier" }),
    ]);
    expect(next.starts_on).toBe("2027-01-01");
  });

  it("keeps the same length for custom periods", () => {
    const next = nextPeriodInput([period({ unit: "custom", starts_on: "2026-07-01", ends_on: "2026-07-14" })]);
    expect(next).toMatchObject({ unit: "custom", starts_on: "2026-07-15", ends_on: "2026-07-28" });
  });

  it("falls back to the current quarter when no periods exist", () => {
    expect(nextPeriodInput([], "2026-07-17")).toEqual({ name: "Q3 2026", unit: "quarter", starts_on: "2026-07-01", ends_on: "2026-09-30" });
  });
});

describe("latestPeriod", () => {
  it("returns null for an empty list", () => {
    expect(latestPeriod([])).toBeNull();
  });
});

describe("formatPeriodRange", () => {
  it("formats a compact readable range", () => {
    expect(formatPeriodRange({ starts_on: "2026-07-01", ends_on: "2026-09-30" })).toBe("Jul 1, 2026 – Sep 30, 2026");
  });
});
