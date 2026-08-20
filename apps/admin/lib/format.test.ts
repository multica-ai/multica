import { describe, expect, it } from "vitest";
import { formatCost } from "./format";

describe("formatCost", () => {
  it("renders an em dash for null", () => {
    expect(formatCost(null)).toBe("—");
  });

  it("renders zero as $0.00", () => {
    expect(formatCost(0)).toBe("$0.00");
  });

  it("renders a fractional value rounded to two decimals", () => {
    expect(formatCost(12.5)).toBe("$12.50");
  });
});
