import { describe, expect, it } from "vitest";
import { snapWidthPct, WIDTH_STOPS } from "./resize-magnet";

// Column width chosen so the 12px magnet is a clean 2% either side (12/600).
const COL = 600;

describe("snapWidthPct", () => {
  it("snaps to the nearest stop when within the 12px magnet", () => {
    // 51% is 6px off 50% on a 600px column — inside the magnet.
    expect(snapWidthPct(51, COL)).toBe(50);
    expect(snapWidthPct(74, COL)).toBe(75);
    expect(snapWidthPct(26, COL)).toBe(25);
    expect(snapWidthPct(99, COL)).toBe(100);
  });

  it("leaves a free-drag value untouched outside the magnet", () => {
    // 58% is 48px off 50% and 102px off 75% — snaps to neither.
    expect(snapWidthPct(58, COL)).toBe(58);
    expect(snapWidthPct(41, COL)).toBe(41);
  });

  it("rounds a fractional drag to a whole percentage", () => {
    expect(snapWidthPct(58.6, COL)).toBe(59);
  });

  it("clamps below the minimum and never past 100", () => {
    expect(snapWidthPct(3, COL)).toBe(10);
    expect(snapWidthPct(-20, COL)).toBe(10);
    expect(snapWidthPct(130, COL)).toBe(100);
  });

  it("widens the magnet as the column narrows (12px is more percent)", () => {
    // On a 200px column, 12px is 6% — 44% is now inside 50%'s magnet.
    expect(snapWidthPct(44, 200)).toBe(50);
    // Same 44% on the wide column stays free.
    expect(snapWidthPct(44, COL)).toBe(44);
  });

  it("exposes the four magnet stops", () => {
    expect(WIDTH_STOPS).toEqual([25, 50, 75, 100]);
  });
});
