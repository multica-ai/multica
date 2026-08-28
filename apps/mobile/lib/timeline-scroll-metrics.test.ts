// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  BOTTOM_CHIP_MIN_GAP_PX,
  computeBottomChipVisible,
  distToPhysicalEnd,
  shouldShowBottomChip,
  type ScrollGeometry,
} from "./timeline-scroll-metrics";

describe("timeline-scroll-metrics", () => {
  it("distance is contentHeight - offset - viewport", () => {
    expect(distToPhysicalEnd(1000, 300, 600)).toBe(100);
  });

  it("distance clamps at 0 when overscrolled past the end", () => {
    expect(distToPhysicalEnd(1000, 500, 600)).toBe(0);
  });

  it("chip shows strictly beyond the 48px band and hides inside it", () => {
    expect(shouldShowBottomChip(49)).toBe(true);
    expect(shouldShowBottomChip(48)).toBe(false);
    expect(shouldShowBottomChip(0)).toBe(false);
  });

  it("threshold constant is 48 (approved spec)", () => {
    expect(BOTTOM_CHIP_MIN_GAP_PX).toBe(48);
  });

  it("chip never shows when content is shorter than the viewport", () => {
    // contentHeight 500 < viewport 600 → the user can never scroll away
    // from the physical end; distance semantics must yield "at end".
    expect(distToPhysicalEnd(500, 0, 600)).toBe(0);
    expect(shouldShowBottomChip(distToPhysicalEnd(500, 0, 600))).toBe(false);
  });
});

describe("computeBottomChipVisible", () => {
  it("returns false before the first layout (no geometry yet)", () => {
    const empty: ScrollGeometry = {
      contentHeight: 0,
      offsetY: 0,
      viewportHeight: 0,
    };
    expect(computeBottomChipVisible(empty)).toBe(false);
  });

  it("returns true when saved geometry puts the viewport far from the end", () => {
    const geo: ScrollGeometry = {
      contentHeight: 4000,
      offsetY: 200,
      viewportHeight: 700,
    };
    // dist = 4000 - 900 = 3100 > 48 → visible, even though no scroll event
    // has fired yet (mount / data-growth path).
    expect(computeBottomChipVisible(geo)).toBe(true);
  });

  it("returns false inside the 48px band or when content fits the viewport", () => {
    expect(
      computeBottomChipVisible({
        contentHeight: 700,
        offsetY: 0,
        viewportHeight: 700,
      }),
    ).toBe(false);
    expect(
      computeBottomChipVisible({
        contentHeight: 740,
        offsetY: 0,
        viewportHeight: 700,
      }),
    ).toBe(false);
  });

  it("grows hidden when appended data is already on screen (still at end)", () => {
    // WS append while scrolled to the bottom: content grows, offset is
    // compensated by MVCP so the viewport stays at the physical end.
    const before: ScrollGeometry = {
      contentHeight: 4000,
      offsetY: 3300,
      viewportHeight: 700,
    };
    const after: ScrollGeometry = {
      contentHeight: 4400,
      offsetY: 3700,
      viewportHeight: 700,
    };
    expect(computeBottomChipVisible(before)).toBe(false);
    expect(computeBottomChipVisible(after)).toBe(false);
  });
});
