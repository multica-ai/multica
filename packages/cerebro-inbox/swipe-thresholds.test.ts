import { describe, expect, it } from "vitest";
import {
  ARCHIVE_HOLD_COMMIT_MS,
  POST_SWIPE_CLICK_SUPPRESS_MS,
  SWIPE_COMMIT_MAX_PX,
  SWIPE_COMMIT_MIN_PX,
  commitThresholdPx,
  leftPanelCommitThresholdPx,
  shouldCommitHeldSwipe,
  shouldInstantArchive,
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
  it("returns 70% of width inside the clamp band", () => {
    // iPhone Pro (414 px) -> 289.8 -> not clamped
    expect(commitThresholdPx(414)).toBeCloseTo(289.8, 1);
    // iPhone 16 Pro Max (430 px) -> 301.0 -> not clamped
    expect(commitThresholdPx(430)).toBeCloseTo(301.0, 1);
  });

  it("clamps to the minimum on very narrow rows", () => {
    // 50 px hypothetical narrow row -> 40 -> clamps up to 80
    expect(commitThresholdPx(50)).toBe(SWIPE_COMMIT_MIN_PX);
  });

  it("clamps to the maximum on tablets", () => {
    // 800 px (iPad portrait) -> 640 -> clamps down to 400
    expect(commitThresholdPx(800)).toBe(SWIPE_COMMIT_MAX_PX);
  });

  it("never falls below the minimum even at zero", () => {
    expect(commitThresholdPx(0)).toBe(SWIPE_COMMIT_MIN_PX);
  });
});

describe("leftPanelCommitThresholdPx", () => {
  it("locks the left action panel at 40% of a phone-width row", () => {
    expect(leftPanelCommitThresholdPx(414)).toBeCloseTo(165.6, 1);
  });

  it("never reveals less than the full action panel width", () => {
    expect(leftPanelCommitThresholdPx(320)).toBe(144);
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
    // 70% of 414 = 289.8 — must exceed that
    expect(shouldCommitHeldSwipe(295, 414, true)).toBe(true);
  });

  it("cancels when the user moves back under threshold before release", () => {
    expect(shouldCommitHeldSwipe(160, 414, true)).toBe(false);
  });
});

describe("shouldInstantArchive", () => {
  it("commits when swipe reaches 70% of row width", () => {
    // 414 px row: 70% = 289.8 px
    expect(shouldInstantArchive(290, 414)).toBe(true);
  });

  it("does not commit just below 70% of row width", () => {
    // 414 px row: 69% = 285.66 px
    expect(shouldInstantArchive(285, 414)).toBe(false);
  });

  it("commits at exactly 70%", () => {
    const rowWidth = 400;
    expect(shouldInstantArchive(rowWidth * 0.70, rowWidth)).toBe(true);
  });

  it("does not commit below the 70% threshold", () => {
    // 50% of 414 = 207 px — clearly below threshold
    expect(shouldInstantArchive(207, 414)).toBe(false);
  });
});

