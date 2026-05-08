import { describe, expect, it } from "vitest";
import {
  SWIPE_COMMIT_MAX_PX,
  SWIPE_COMMIT_MIN_PX,
  commitThresholdPx,
} from "./swipe-thresholds";

describe("commitThresholdPx", () => {
  it("returns 35% of width inside the clamp band", () => {
    // iPhone Pro (414 px) -> 144.9 -> not clamped
    expect(commitThresholdPx(414)).toBeCloseTo(144.9, 1);
    // iPhone 16 Pro Max (430 px) -> 150.5 -> not clamped
    expect(commitThresholdPx(430)).toBeCloseTo(150.5, 1);
  });

  it("clamps to the minimum on small phones", () => {
    // 200 px hypothetical narrow row -> 70 -> clamps up to 80
    expect(commitThresholdPx(200)).toBe(SWIPE_COMMIT_MIN_PX);
  });

  it("clamps to the maximum on tablets", () => {
    // 800 px (iPad portrait) -> 280 -> clamps down to 200
    expect(commitThresholdPx(800)).toBe(SWIPE_COMMIT_MAX_PX);
  });

  it("never falls below the minimum even at zero", () => {
    expect(commitThresholdPx(0)).toBe(SWIPE_COMMIT_MIN_PX);
  });
});
