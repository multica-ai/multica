import { describe, expect, it } from "vitest";
import {
  CLOSE_THRESHOLD,
  HOLD_MS,
  HOLD_SLOP,
  OPEN_THRESHOLD,
  PULL_WIDTH,
  TOUCH_GAIN,
  appliedOffset,
  holdBroken,
  shouldClose,
  shouldLatchOpen,
} from "./use-line-authors-pull";

describe("appliedOffset", () => {
  it("ignores leftward and zero movement from a closed gutter", () => {
    expect(appliedOffset(0, -30, 1)).toBe(0);
    expect(appliedOffset(0, 0, 1)).toBe(0);
  });

  it("follows the pointer up to the reveal width", () => {
    expect(appliedOffset(0, 40, 1)).toBe(40);
    expect(appliedOffset(0, PULL_WIDTH, 1)).toBe(PULL_WIDTH);
  });

  it("rubber-bands past the reveal width", () => {
    expect(appliedOffset(0, PULL_WIDTH + 100, 1)).toBe(PULL_WIDTH + 20);
  });

  it("amplifies touch travel so the reveal opens on a short pull", () => {
    // ~40px of finger travel fully opens the 64px reveal.
    expect(appliedOffset(0, PULL_WIDTH / TOUCH_GAIN, TOUCH_GAIN)).toBe(
      PULL_WIDTH,
    );
    expect(appliedOffset(0, 30, TOUCH_GAIN)).toBe(48);
  });

  it("tracks a latched-open gutter back toward closed on a left pull", () => {
    expect(appliedOffset(PULL_WIDTH, -20, 1)).toBe(PULL_WIDTH - 20);
    expect(appliedOffset(PULL_WIDTH, -200, 1)).toBe(0);
  });
});

describe("hold requirement", () => {
  it("tolerates finger jitter within the slop", () => {
    expect(holdBroken(0, 0)).toBe(false);
    expect(holdBroken(HOLD_SLOP, -HOLD_SLOP)).toBe(false);
  });

  it("cancels on movement past the slop in any direction", () => {
    expect(holdBroken(HOLD_SLOP + 1, 0)).toBe(true);
    expect(holdBroken(-(HOLD_SLOP + 1), 0)).toBe(true);
    expect(holdBroken(0, HOLD_SLOP + 1)).toBe(true);
    expect(holdBroken(0, -(HOLD_SLOP + 1))).toBe(true);
  });

  it("requires a real hold before the pull arms", () => {
    expect(HOLD_MS).toBeGreaterThanOrEqual(400);
  });
});

describe("latching", () => {
  it("latches open once the pull passes the threshold", () => {
    expect(shouldLatchOpen(OPEN_THRESHOLD)).toBe(true);
    expect(shouldLatchOpen(PULL_WIDTH)).toBe(true);
    expect(shouldLatchOpen(OPEN_THRESHOLD - 1)).toBe(false);
  });

  it("closes after a small pull back to the left", () => {
    expect(shouldClose(PULL_WIDTH - CLOSE_THRESHOLD)).toBe(true);
    expect(shouldClose(0)).toBe(true);
    expect(shouldClose(PULL_WIDTH)).toBe(false);
    expect(shouldClose(PULL_WIDTH - CLOSE_THRESHOLD + 1)).toBe(false);
  });
});
