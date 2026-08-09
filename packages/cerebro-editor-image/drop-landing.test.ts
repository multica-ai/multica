import { describe, expect, it } from "vitest";
import { dropLanding, type DropBlock } from "./drop-landing";

// Three stacked blocks, 20px tall, 10px apart, in document order.
const BLOCKS: DropBlock[] = [
  { top: 0, bottom: 20 },
  { top: 30, bottom: 50 },
  { top: 60, bottom: 80 },
];

describe("dropLanding", () => {
  it("lands above a block when the pointer is in its top half", () => {
    // 34px is inside block 1 (30–50), above its 40px midpoint.
    expect(dropLanding(34, BLOCKS)).toEqual({
      blockIndex: 1,
      side: "above",
      lineY: 30,
    });
  });

  it("lands below a block when the pointer is in its bottom half", () => {
    // 44px is inside block 1, below its 40px midpoint; the line sits at the
    // block's bottom (50).
    expect(dropLanding(44, BLOCKS)).toEqual({
      blockIndex: 1,
      side: "below",
      lineY: 50,
    });
  });

  it("snaps to the nearest block when the pointer is in a gap", () => {
    // 26px sits in the 20–30 gap: 6px from block 0's bottom, 4px from block 1's
    // top, so block 1 is nearest — land above block 1.
    expect(dropLanding(26, BLOCKS)).toEqual({
      blockIndex: 1,
      side: "above",
      lineY: 30,
    });
  });

  it("lands at the very top when the pointer is above the first block", () => {
    expect(dropLanding(-100, BLOCKS)).toEqual({
      blockIndex: 0,
      side: "above",
      lineY: 0,
    });
  });

  it("lands below the last block — inline, not the tray", () => {
    // The bottom half of the last block, and anywhere past it, lands below the
    // last block. Whether that becomes the tray is the DOM layer's hit-test on
    // the thumbnail strip, never this geometry.
    expect(dropLanding(75, BLOCKS)).toEqual({
      blockIndex: 2,
      side: "below",
      lineY: 80,
    });
    expect(dropLanding(500, BLOCKS)).toEqual({
      blockIndex: 2,
      side: "below",
      lineY: 80,
    });
  });

  it("returns null only when there is no block to land against", () => {
    expect(dropLanding(10, [])).toBeNull();
  });
});
