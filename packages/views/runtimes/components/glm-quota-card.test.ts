// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { GlmQuotaWindow } from "@multica/core/api";
import {
  glmWindowPeriod,
  glmWindowRemainingPercent,
  glmWindowResetPassed,
  glmWorstFirst,
} from "./glm-quota-card";

const NOW_SEC = 1_800_000_000;

describe("glmWindowRemainingPercent", () => {
  it("derives remaining from the provider's used percentage", () => {
    const w: GlmQuotaWindow = { type: "TIME_LIMIT", used_percent: 16 };
    expect(glmWindowRemainingPercent(w, NOW_SEC)).toBe(84);
  });
  it("computes from remaining/usage when percentage is absent (credit pools)", () => {
    const w: GlmQuotaWindow = { type: "CREDIT_LIMIT", usage: 500, remaining: 420 };
    expect(glmWindowRemainingPercent(w, NOW_SEC)).toBe(84);
  });
  it("returns null when nothing is derivable", () => {
    expect(glmWindowRemainingPercent({ type: "TOKENS_LIMIT" }, NOW_SEC)).toBeNull();
  });
  it("never reports below zero or above one hundred", () => {
    expect(glmWindowRemainingPercent({ type: "X", used_percent: 140 }, NOW_SEC)).toBe(0);
    expect(glmWindowRemainingPercent({ type: "X", usage: 10, remaining: 99 }, NOW_SEC)).toBe(100);
  });
  it("counts a window whose reset has passed as refilled", () => {
    const w: GlmQuotaWindow = { type: "TOKENS_LIMIT", used_percent: 90, resets_at: NOW_SEC };
    expect(glmWindowResetPassed(w, NOW_SEC)).toBe(true);
    expect(glmWindowRemainingPercent(w, NOW_SEC)).toBe(100);
    expect(glmWindowResetPassed(w, NOW_SEC - 1)).toBe(false);
    expect(glmWindowRemainingPercent(w, NOW_SEC - 1)).toBe(10);
  });
});

describe("glmWorstFirst", () => {
  it("leads with the lowest remaining percent; underivable sorts last", () => {
    const windows: GlmQuotaWindow[] = [
      { type: "A", used_percent: 10 },
      { type: "B", used_percent: 90 },
      { type: "C" },
    ];
    expect(glmWorstFirst(windows, NOW_SEC).map((w) => w.type)).toEqual(["B", "A", "C"]);
  });
  it("ranks a refilled window as full", () => {
    const windows: GlmQuotaWindow[] = [
      { type: "TOKENS_LIMIT", used_percent: 95, resets_at: NOW_SEC - 60 },
      { type: "TIME_LIMIT", used_percent: 40, resets_at: NOW_SEC + 86400 },
    ];
    expect(glmWorstFirst(windows, NOW_SEC).map((w) => w.type)).toEqual(["TIME_LIMIT", "TOKENS_LIMIT"]);
  });
});

describe("glmWindowPeriod", () => {
  it("classifies 5h and weekly windows by window_minutes", () => {
    expect(glmWindowPeriod({ type: "TOKENS_LIMIT", window_minutes: 300 })).toBe("five_hour");
    expect(glmWindowPeriod({ type: "CREDIT_LIMIT", window_minutes: 10080 })).toBe("weekly");
  });
  it("returns null for unlabeled, monthly or odd periods", () => {
    expect(glmWindowPeriod({ type: "TOKENS_LIMIT" })).toBeNull();
    expect(glmWindowPeriod({ type: "TOKENS_LIMIT", window_minutes: 0 })).toBeNull();
    // TIME_LIMIT monthly pools carry no minute count server-side.
    expect(glmWindowPeriod({ type: "TIME_LIMIT" })).toBeNull();
    // A non-5h hourly window must not be mislabeled as the 5h window.
    expect(glmWindowPeriod({ type: "TOKENS_LIMIT", window_minutes: 60 })).toBeNull();
  });
});
