import { describe, expect, it } from "vitest";
import {
  ARCHIVE_HOLD_COMMIT_MS,
  POST_SWIPE_CLICK_SUPPRESS_MS,
  SWIPE_COMMIT_MAX_PX,
  SWIPE_COMMIT_MIN_PX,
  commitThresholdPx,
  shouldCommitHeldSwipe,
} from "./swipe-thresholds";

describe("POST_SWIPE_CLICK_SUPPRESS_MS", () => {
  it("covers iOS Safari's synthetic-click delay with a small safety margin", () => {
    // iOS Safari fires the synthetic click within ~300 ms of touchend
    // when it fires at all. The window must exceed that so a legit
    // post-swipe synthetic click can be swallowed, but stay short enough
    // that a user's deliberate tap on the reveal panel (typically >500 ms
    // after the swipe) goes through.
    expect(POST_SWIPE_CLICK_SUPPRESS_MS).toBeGreaterThan(300);
    expect(POST_SWIPE_CLICK_SUPPRESS_MS).toBeLessThan(500);
  });
});

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

describe("ARCHIVE_HOLD_COMMIT_MS", () => {
  it("requires a deliberate hold without making the gesture feel stuck", () => {
    expect(ARCHIVE_HOLD_COMMIT_MS).toBeGreaterThanOrEqual(350);
    expect(ARCHIVE_HOLD_COMMIT_MS).toBeLessThan(700);
  });
});

describe("shouldCommitHeldSwipe", () => {
  it("does not commit before the hold timer has completed", () => {
    expect(shouldCommitHeldSwipe(160, 414, false)).toBe(false);
  });

  it("commits after hold when still beyond threshold", () => {
    expect(shouldCommitHeldSwipe(160, 414, true)).toBe(true);
  });

  it("cancels when the user moves back under threshold before release", () => {
    expect(shouldCommitHeldSwipe(80, 414, true)).toBe(false);
  });
});
