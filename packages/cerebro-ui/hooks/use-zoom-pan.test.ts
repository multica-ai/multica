import { describe, expect, it } from "vitest";
import { anchoredZoom } from "./use-zoom-pan";

const viewport = { width: 1000, height: 800 };
const center = { x: 500, y: 400 };

describe("anchoredZoom", () => {
  it("clamps above maxScale and below minScale", () => {
    const up = anchoredZoom({
      prevScale: 4,
      nextScale: 99,
      anchor: center,
      viewport,
      offset: { x: 0, y: 0 },
      minScale: 1,
      maxScale: 8,
    });
    expect(up.scale).toBe(8);

    const down = anchoredZoom({
      prevScale: 2,
      nextScale: 0.1,
      anchor: center,
      viewport,
      offset: { x: 0, y: 0 },
      minScale: 1,
      maxScale: 8,
    });
    expect(down.scale).toBe(1);
  });

  it("recenters (offset 0,0) when snapping back to fit", () => {
    const { scale, offset } = anchoredZoom({
      prevScale: 4,
      nextScale: 1,
      anchor: { x: 10, y: 10 },
      viewport,
      offset: { x: 120, y: -60 },
      minScale: 1,
      maxScale: 8,
    });
    expect(scale).toBe(1);
    expect(offset).toEqual({ x: 0, y: 0 });
  });

  it("keeps the viewport center fixed when zooming at the center", () => {
    const { offset } = anchoredZoom({
      prevScale: 1,
      nextScale: 2,
      anchor: center,
      viewport,
      offset: { x: 0, y: 0 },
      minScale: 1,
      maxScale: 8,
    });
    // Anchor == center == current offset origin → no pan needed.
    expect(offset).toEqual({ x: 0, y: 0 });
  });

  it("pans so the pixel under an off-center anchor stays put", () => {
    // Zooming 1x→2x anchored at the top-left corner. The corner is
    // (-500,-400) from center; doubling scale must shift content by +that.
    const { scale, offset } = anchoredZoom({
      prevScale: 1,
      nextScale: 2,
      anchor: { x: 0, y: 0 },
      viewport,
      offset: { x: 0, y: 0 },
      minScale: 1,
      maxScale: 8,
    });
    expect(scale).toBe(2);
    // ax = 0 - 500 - 0 = -500; offset.x = 0 - (-500)*(2-1) = 500
    expect(offset).toEqual({ x: 500, y: 400 });
  });

  it("is a no-op when the target equals the current scale", () => {
    const offset = { x: 33, y: 44 };
    const result = anchoredZoom({
      prevScale: 3,
      nextScale: 3,
      anchor: center,
      viewport,
      offset,
      minScale: 1,
      maxScale: 8,
    });
    expect(result.scale).toBe(3);
    expect(result.offset).toBe(offset);
  });
});
