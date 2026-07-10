import { describe, expect, it } from "vitest";
import {
  CLOSE_THRESHOLD,
  OPEN_THRESHOLD,
  PULL_WIDTH,
  TOUCH_GAIN,
  appliedOffset,
  isHorizontalPull,
  shouldClose,
  shouldLatchOpen,
} from "./use-line-authors-pull";

describe("appliedOffset", () => {
  it("ignores leftward and zero movement from a closed gutter", () => {
    expect(appliedOffset(0, -30, 1)).toBe(0);
    expect(appliedOffset(0, 0, 1)).toBe(0);
  });

  it("follows the pointer up to the gutter width", () => {
    expect(appliedOffset(0, 40, 1)).toBe(40);
    expect(appliedOffset(0, PULL_WIDTH, 1)).toBe(PULL_WIDTH);
  });

  it("rubber-bands past the gutter width", () => {
    expect(appliedOffset(0, PULL_WIDTH + 100, 1)).toBe(PULL_WIDTH + 20);
  });

  it("amplifies touch travel so the gutter opens on a short pull", () => {
    // ~60px of finger travel fully opens the 96px gutter.
    expect(appliedOffset(0, 60, TOUCH_GAIN)).toBe(PULL_WIDTH);
    expect(appliedOffset(0, 30, TOUCH_GAIN)).toBe(48);
  });

  it("tracks a latched-open gutter back toward closed on a left pull", () => {
    expect(appliedOffset(PULL_WIDTH, -20, 1)).toBe(PULL_WIDTH - 20);
    expect(appliedOffset(PULL_WIDTH, -200, 1)).toBe(0);
  });
});

describe("isHorizontalPull", () => {
  it("accepts a clear rightward pull", () => {
    expect(isHorizontalPull(30, 5, false)).toBe(true);
  });

  it("rejects leftward movement while closed", () => {
    expect(isHorizontalPull(-30, 0, false)).toBe(false);
  });

  it("accepts leftward movement while latched open (closing pull)", () => {
    expect(isHorizontalPull(-30, 0, true)).toBe(true);
  });

  it("rejects mostly-vertical movement (scrolling)", () => {
    expect(isHorizontalPull(12, 40, false)).toBe(false);
    expect(isHorizontalPull(-12, 40, true)).toBe(false);
  });

  it("rejects tiny movements", () => {
    expect(isHorizontalPull(5, 1, false)).toBe(false);
    expect(isHorizontalPull(-5, 1, true)).toBe(false);
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
