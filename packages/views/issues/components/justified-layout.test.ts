// @vitest-environment node
import { describe, expect, it } from "vitest";
import { clampRatio, justifyRows } from "./justified-layout";

const OPTIONS = { gap: 8, minHeight: 100, maxHeight: 240 };

function rowWidth(ratios: number[], row: { items: number[]; height: number }) {
  return (
    row.items.reduce((sum, i) => sum + clampRatio(ratios[i]!) * row.height, 0) +
    OPTIONS.gap * (row.items.length - 1)
  );
}

describe("justifyRows", () => {
  it("keeps a comment's three screenshots in one full row in a narrow column", () => {
    const ratios = [1.6, 1.78, 1.6];
    const rows = justifyRows(ratios, 585, OPTIONS);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.items).toEqual([0, 1, 2]);
    expect(rowWidth(ratios, rows[0]!)).toBeCloseTo(584, 0);
  });

  it("caps the height, so two images in a wide column stop short of the edge", () => {
    const rows = justifyRows([1.6, 1.6], 1200, OPTIONS);
    expect(rows).toEqual([{ items: [0, 1], height: 240 }]);
  });

  it("wraps before a row would get too short, and the last row keeps the height above", () => {
    const ratios = Array.from({ length: 5 }, () => 1.6);
    const rows = justifyRows(ratios, 585, OPTIONS);
    expect(rows.map((r) => r.items.length)).toEqual([3, 2]);
    expect(rowWidth(ratios, rows[0]!)).toBeCloseTo(584, 0);
    expect(rows[1]!.height).toBeCloseTo(rows[0]!.height, 5);
  });

  it("lays everything out in one row before the width is known", () => {
    expect(justifyRows([1.6, 1.6, 1.6], 0, OPTIONS)).toEqual([
      { items: [0, 1, 2], height: 240 },
    ]);
    expect(justifyRows([], 600, OPTIONS)).toEqual([]);
  });
});

describe("clampRatio", () => {
  it("bounds extreme shapes and falls back on nonsense", () => {
    expect(clampRatio(10)).toBe(3);
    expect(clampRatio(0.1)).toBe(0.5);
    expect(clampRatio(Number.NaN)).toBe(1);
    expect(clampRatio(0)).toBe(1);
    expect(clampRatio(1.6)).toBe(1.6);
  });
});
