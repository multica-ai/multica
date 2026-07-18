import { describe, expect, it } from "vitest";
import {
  MAX_SCALE,
  MIN_SCALE,
  centerTransform,
  clampScale,
  clampTransform,
  computeFitScale,
  computeFitTransform,
  distanceBetween,
  midpointOf,
  panBy,
  wheelZoomFactor,
  zoomByAtCenter,
  zoomToAt,
  type DiagramTransform,
} from "./diagram-transform";

const VIEWPORT = { width: 1000, height: 600 };

describe("clampScale", () => {
  it("holds zoom inside the 25%–400% product range", () => {
    expect(clampScale(0.01)).toBe(MIN_SCALE);
    expect(clampScale(99)).toBe(MAX_SCALE);
    expect(clampScale(1.5)).toBe(1.5);
  });

  it("falls back to 1 for non-finite input rather than propagating NaN into the transform", () => {
    // NaN/Infinity can only reach here from a degenerate measurement; a safe
    // default beats poisoning every later transform with NaN.
    expect(clampScale(Number.NaN)).toBe(1);
    expect(clampScale(Number.POSITIVE_INFINITY)).toBe(1);
  });
});

describe("computeFitScale", () => {
  it("shrinks an oversized diagram to the tighter axis", () => {
    // Width needs 0.5, height needs 0.6 — the diagram only fits at 0.5.
    expect(computeFitScale({ width: 2000, height: 1000 }, VIEWPORT)).toBe(0.5);
  });

  it("leaves a small diagram at natural size instead of magnifying it", () => {
    expect(computeFitScale({ width: 100, height: 80 }, VIEWPORT)).toBe(1);
  });

  it("clamps a diagram too large for even the minimum zoom", () => {
    expect(computeFitScale({ width: 100000, height: 100000 }, VIEWPORT)).toBe(MIN_SCALE);
  });

  it("returns 1 when either size is unknown, so a 0x0 measure never divides by zero", () => {
    expect(computeFitScale({ width: 0, height: 0 }, VIEWPORT)).toBe(1);
    expect(computeFitScale({ width: 100, height: 100 }, { width: 0, height: 0 })).toBe(1);
  });
});

describe("computeFitTransform", () => {
  it("centers the fitted diagram", () => {
    const transform = computeFitTransform({ width: 2000, height: 1000 }, VIEWPORT);

    expect(transform.scale).toBe(0.5);
    // 2000*0.5 = 1000 wide → exactly fills, no horizontal offset.
    expect(transform.x).toBe(0);
    // 1000*0.5 = 500 tall in a 600 viewport → 50px of slack each side.
    expect(transform.y).toBe(50);
  });
});

describe("clampTransform", () => {
  const content = { width: 400, height: 300 };

  it("keeps the diagram reachable when panned far off the left edge", () => {
    const clamped = clampTransform({ scale: 1, x: -5000, y: 0 }, content, VIEWPORT);

    // At least 48px of the 400px-wide diagram stays on screen.
    expect(clamped.x).toBe(48 - 400);
    expect(clamped.x + content.width).toBe(48);
  });

  it("keeps the diagram reachable when panned far off the right edge", () => {
    const clamped = clampTransform({ scale: 1, x: 9999, y: 0 }, content, VIEWPORT);

    expect(clamped.x).toBe(VIEWPORT.width - 48);
  });

  it("requires a diagram smaller than the margin to stay fully visible", () => {
    // A 20px-wide diagram must not be allowed to hide 48px off-screen —
    // that would leave nothing at all on the canvas.
    const clamped = clampTransform({ scale: 1, x: -9999, y: 0 }, { width: 20, height: 20 }, VIEWPORT);

    expect(clamped.x).toBe(0);
  });

  it("leaves an in-bounds transform untouched", () => {
    const transform: DiagramTransform = { scale: 1, x: 100, y: 50 };

    expect(clampTransform(transform, content, VIEWPORT)).toEqual(transform);
  });
});

describe("zoomToAt", () => {
  const content = { width: 1000, height: 1000 };

  it("pins the content point under the anchor so zoom tracks the cursor", () => {
    const start: DiagramTransform = { scale: 1, x: 0, y: 0 };
    const anchor = { x: 200, y: 100 };

    const zoomed = zoomToAt(start, 2, anchor, content, VIEWPORT);

    // The content coordinate under the anchor before and after must match.
    const contentXBefore = (anchor.x - start.x) / start.scale;
    const contentXAfter = (anchor.x - zoomed.x) / zoomed.scale;
    expect(contentXAfter).toBeCloseTo(contentXBefore, 5);

    const contentYBefore = (anchor.y - start.y) / start.scale;
    const contentYAfter = (anchor.y - zoomed.y) / zoomed.scale;
    expect(contentYAfter).toBeCloseTo(contentYBefore, 5);
  });

  it("clamps to the max zoom rather than overshooting", () => {
    const zoomed = zoomToAt({ scale: 1, x: 0, y: 0 }, 50, { x: 0, y: 0 }, content, VIEWPORT);

    expect(zoomed.scale).toBe(MAX_SCALE);
  });

  it("clamps to the min zoom rather than undershooting", () => {
    const zoomed = zoomToAt({ scale: 1, x: 0, y: 0 }, 0.001, { x: 0, y: 0 }, content, VIEWPORT);

    expect(zoomed.scale).toBe(MIN_SCALE);
  });

  it("is a no-op once already at the limit, so held keys/wheel do not drift the diagram", () => {
    const atMax: DiagramTransform = { scale: MAX_SCALE, x: 10, y: 20 };

    expect(zoomToAt(atMax, MAX_SCALE * 2, { x: 100, y: 100 }, content, VIEWPORT)).toBe(atMax);
  });
});

describe("zoomByAtCenter", () => {
  it("zooms about the viewport center for button/keyboard input", () => {
    const start: DiagramTransform = { scale: 1, x: 0, y: 0 };
    const center = { x: VIEWPORT.width / 2, y: VIEWPORT.height / 2 };

    const zoomed = zoomByAtCenter(start, 2, { width: 1000, height: 1000 }, VIEWPORT);

    expect(zoomed.scale).toBe(2);
    expect((center.x - zoomed.x) / zoomed.scale).toBeCloseTo(center.x - start.x, 5);
  });
});

describe("panBy", () => {
  it("moves by the delta and re-clamps", () => {
    const panned = panBy({ scale: 1, x: 0, y: 0 }, 50, -20, { width: 400, height: 300 }, VIEWPORT);

    expect(panned).toEqual({ scale: 1, x: 50, y: -20 });
  });

  it("cannot pan the diagram out of sight", () => {
    const panned = panBy({ scale: 1, x: 0, y: 0 }, -100000, 0, { width: 400, height: 300 }, VIEWPORT);

    expect(panned.x + 400).toBe(48);
  });
});

describe("centerTransform", () => {
  it("centers content at an explicit scale", () => {
    expect(centerTransform({ width: 400, height: 200 }, VIEWPORT, 1)).toEqual({
      scale: 1,
      x: 300,
      y: 200,
    });
  });
});

describe("wheelZoomFactor", () => {
  it("zooms in when scrolling up and out when scrolling down", () => {
    expect(wheelZoomFactor(-100, 0)).toBeGreaterThan(1);
    expect(wheelZoomFactor(100, 0)).toBeLessThan(1);
  });

  it("is symmetric, so equal opposite scrolls return to the original scale", () => {
    expect(wheelZoomFactor(-100, 0) * wheelZoomFactor(100, 0)).toBeCloseTo(1, 10);
  });

  it("normalizes line and page delta modes so a mouse wheel is not 16x weaker than a trackpad", () => {
    // deltaMode 1 (lines) — one line is treated as 16 pixels.
    expect(wheelZoomFactor(1, 1)).toBeCloseTo(wheelZoomFactor(16, 0), 10);
    // deltaMode 2 (pages) — one page is treated as 100 pixels.
    expect(wheelZoomFactor(1, 2)).toBeCloseTo(wheelZoomFactor(100, 0), 10);
  });
});

describe("pinch helpers", () => {
  it("measures distance between two pointers", () => {
    expect(distanceBetween({ x: 0, y: 0 }, { x: 3, y: 4 })).toBe(5);
  });

  it("finds the midpoint used as the pinch anchor", () => {
    expect(midpointOf({ x: 0, y: 0 }, { x: 10, y: 20 })).toEqual({ x: 5, y: 10 });
  });
});
