import { describe, expect, it } from "vitest";
import {
  SWIPE_COMMIT_MAX_PX,
  SWIPE_COMMIT_MIN_PX,
  commitThresholdPx,
} from "./swipe-thresholds";

describe("commitThresholdPx", () => {
  it("returns 30% of width inside the clamp band", () => {
    // 414px (iPhone Pro) -> 124.2 -> not clamped
    expect(commitThresholdPx(414)).toBeCloseTo(124.2, 1);
    // 430px (iPhone 16 Pro Max) -> 129
    expect(commitThresholdPx(430)).toBeCloseTo(129, 1);
  });

  it("clamps to the minimum on small phones", () => {
    // 320px (iPhone SE) -> 96, clamps to min 100
    expect(commitThresholdPx(320)).toBe(SWIPE_COMMIT_MIN_PX);
  });

  it("clamps to the maximum on tablets", () => {
    // 800px (iPad portrait) -> 240, clamps to max 180
    expect(commitThresholdPx(800)).toBe(SWIPE_COMMIT_MAX_PX);
  });

  it("never falls below the minimum even at zero", () => {
    expect(commitThresholdPx(0)).toBe(SWIPE_COMMIT_MIN_PX);
  });
});
