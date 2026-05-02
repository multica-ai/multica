import { describe, expect, test } from "vitest";
import { formatCurrency, formatDate, formatNumber } from "./format";

describe("format facade", () => {
  const fixed = new Date("2026-05-03T08:30:00Z");

  test("formatDate short uses zh-CN style for zh-CN locale", () => {
    const out = formatDate(fixed, "short", "zh-CN");
    expect(out).toMatch(/2026/);
    expect(out).toMatch(/年/);
  });

  test("formatDate short uses Mmm d, yyyy style for en locale", () => {
    const out = formatDate(fixed, "short", "en");
    expect(out).toMatch(/2026/);
    expect(out).not.toMatch(/年/);
  });

  test("formatNumber respects locale grouping", () => {
    expect(formatNumber(1234567, "en")).toBe("1,234,567");
    expect(formatNumber(1234567, "zh-CN")).toBe("1,234,567");
  });

  test("formatCurrency renders currency symbol", () => {
    expect(formatCurrency(99.5, "USD", "en")).toMatch(/\$/);
    expect(formatCurrency(99.5, "CNY", "zh-CN")).toMatch(/¥/);
  });
});
