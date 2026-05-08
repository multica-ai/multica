import { describe, expect, it } from "vitest";
import {
  SWIPE_COMMIT_MAX_PX,
  SWIPE_COMMIT_MIN_PX,
  commitThresholdPx,
} from "./swipe-thresholds";

describe("commitThresholdPx", () => {
  it("returns 50% of width inside the clamp band", () => {
    // 414px (iPhone Pro) -> 207 -> not clamped
    expect(commitThresholdPx(414)).toBe(207);
    // 430px (iPhone 16 Pro Max) -> 215 -> not clamped
    expect(commitThresholdPx(430)).toBe(215);
  });

  it("clamps to the minimum on small phones", () => {
    // 200px hypothetical narrow row -> 100 -> clamps up to 120
    expect(commitThresholdPx(200)).toBe(SWIPE_COMMIT_MIN_PX);
  });

  it("clamps to the maximum on tablets", () => {
    // 800px (iPad portrait) -> 400 -> clamps down to 260
    expect(commitThresholdPx(800)).toBe(SWIPE_COMMIT_MAX_PX);
  });

  it("never falls below the minimum even at zero", () => {
    expect(commitThresholdPx(0)).toBe(SWIPE_COMMIT_MIN_PX);
  });
});
