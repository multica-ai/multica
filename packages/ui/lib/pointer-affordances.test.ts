// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  FINE_HOVER,
  coarseAlwaysFineRowHoverBlock,
  fineOnlyUntilRowHoverHidden,
  hideWhenFineHover,
  hoverRevealCodeOpacity,
  hoverRevealHeaderOpacity,
  hoverRevealOpacity,
  hoverRevealRowOpacity,
} from "./pointer-affordances";

describe("pointer affordances", () => {
  it("pairs hover with a fine pointer so iPadOS synthetic hover is not enough", () => {
    expect(FINE_HOVER).toBe("[@media(hover:hover)_and_(pointer:fine)]");
    expect(hideWhenFineHover).toBe(
      "[@media(hover:hover)_and_(pointer:fine)]:hidden",
    );
    expect(hoverRevealOpacity).toContain(FINE_HOVER);
    expect(hoverRevealOpacity).toContain("group-hover:opacity-100");
    expect(hoverRevealRowOpacity).toContain("group-hover/row:opacity-100");
    expect(hoverRevealHeaderOpacity).toContain(
      "group-hover/header:opacity-100",
    );
    expect(hoverRevealCodeOpacity).toContain("group-hover/code:opacity-100");
    expect(coarseAlwaysFineRowHoverBlock.startsWith("block ")).toBe(true);
    expect(fineOnlyUntilRowHoverHidden.startsWith("hidden ")).toBe(true);
  });
});
