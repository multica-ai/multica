import { describe, expect, it } from "vitest";
import { formatCost, formatDuration } from "./format";

describe("formatCost", () => {
  it("renders cents as kroner with two decimals", () => {
    expect(formatCost(12)).toBe("0.12 kr");
    expect(formatCost(1050)).toBe("10.50 kr");
    expect(formatCost(0)).toBe("0.00 kr");
  });
  it("degrades non-finite input to zero", () => {
    expect(formatCost(NaN)).toBe("0.00 kr");
  });
});

describe("formatDuration", () => {
  it("shows milliseconds under a second", () => {
    expect(formatDuration(340)).toBe("340 ms");
    expect(formatDuration(0)).toBe("0 ms");
  });
  it("shows seconds with one decimal from a second up", () => {
    expect(formatDuration(3400)).toBe("3.4 s");
    expect(formatDuration(1000)).toBe("1.0 s");
  });
  it("degrades non-finite input to zero", () => {
    expect(formatDuration(NaN)).toBe("0 ms");
  });
});
