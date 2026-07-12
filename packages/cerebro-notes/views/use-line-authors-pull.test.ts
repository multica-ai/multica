import { describe, expect, it } from "vitest";
import {
  HOLD_SLOP,
  INTENT_SLOP,
  LATCH_HOLD_MS,
  OPEN_THRESHOLD,
  PULL_WIDTH,
  TOUCH_GAIN,
  appliedOffset,
  dragIntent,
  holdBroken,
  shouldLatchOpen,
} from "./use-line-authors-pull";

describe("appliedOffset", () => {
  it("ignores leftward and zero movement from a closed gutter", () => {
    expect(appliedOffset(-30, 1)).toBe(0);
    expect(appliedOffset(0, 1)).toBe(0);
  });

  it("follows the pointer up to the reveal width", () => {
    expect(appliedOffset(40, 1)).toBe(40);
    expect(appliedOffset(PULL_WIDTH, 1)).toBe(PULL_WIDTH);
  });

  it("rubber-bands past the reveal width", () => {
    expect(appliedOffset(PULL_WIDTH + 100, 1)).toBe(PULL_WIDTH + 20);
  });

  it("amplifies touch travel so the reveal opens on a short pull", () => {
    // ~35px of finger travel fully opens the 56px reveal.
    expect(appliedOffset(PULL_WIDTH / TOUCH_GAIN, TOUCH_GAIN)).toBe(
      PULL_WIDTH,
    );
    expect(appliedOffset(30, TOUCH_GAIN)).toBe(48);
  });
});

describe("drag intent", () => {
  it("stays undecided within the slop", () => {
    expect(dragIntent(0, 0)).toBeUndefined();
    expect(dragIntent(INTENT_SLOP, 0)).toBeUndefined();
    expect(dragIntent(0, INTENT_SLOP)).toBeUndefined();
  });

  it("arms the pull on a rightward, horizontal-dominant drag", () => {
    expect(dragIntent(INTENT_SLOP + 1, 0)).toBe("pull");
    expect(dragIntent(30, 10)).toBe("pull");
  });

  it("hands vertical-dominant movement back to scrolling", () => {
    expect(dragIntent(0, INTENT_SLOP + 1)).toBe("scroll");
    expect(dragIntent(0, -(INTENT_SLOP + 1))).toBe("scroll");
    expect(dragIntent(10, 30)).toBe("scroll");
  });

  it("never arms on a leftward drag", () => {
    expect(dragIntent(-(INTENT_SLOP + 1), 0)).toBe("scroll");
    expect(dragIntent(-30, -5)).toBe("scroll");
  });
});

describe("latch hold", () => {
  it("tolerates finger jitter within the slop", () => {
    expect(holdBroken(0, 0)).toBe(false);
    expect(holdBroken(HOLD_SLOP, -HOLD_SLOP)).toBe(false);
  });

  it("restarts on movement past the slop in any direction", () => {
    expect(holdBroken(HOLD_SLOP + 1, 0)).toBe(true);
    expect(holdBroken(-(HOLD_SLOP + 1), 0)).toBe(true);
    expect(holdBroken(0, HOLD_SLOP + 1)).toBe(true);
    expect(holdBroken(0, -(HOLD_SLOP + 1))).toBe(true);
  });

  it("requires a deliberate but snappy hold", () => {
    expect(LATCH_HOLD_MS).toBeGreaterThanOrEqual(250);
    expect(LATCH_HOLD_MS).toBeLessThanOrEqual(600);
  });

  it("latches only once the pull passes the threshold", () => {
    expect(shouldLatchOpen(OPEN_THRESHOLD)).toBe(true);
    expect(shouldLatchOpen(PULL_WIDTH)).toBe(true);
    expect(shouldLatchOpen(OPEN_THRESHOLD - 1)).toBe(false);
  });
});
