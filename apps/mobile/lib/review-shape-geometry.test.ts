import { describe, expect, it } from "vitest";
import {
  normalizeShape,
  rectToSvgProps,
  ellipseToSvgProps,
  pointsToSvgPath,
  arrowHeadPoints,
} from "./review-shape-geometry";

describe("normalizeShape", () => {
  it("flips negative width/height so x,y is the top-left corner", () => {
    const s = normalizeShape({ type: "rectangle", x: 0.5, y: 0.5, width: -0.2, height: -0.1, color: "#f00", strokeWidth: 2 });
    expect(s).toMatchObject({ x: 0.3, y: 0.4, width: 0.2, height: 0.1 });
  });

  it("returns null for degenerate shapes (both dimensions ~0)", () => {
    expect(
      normalizeShape({ type: "rectangle", x: 0.5, y: 0.5, width: 0.001, height: 0.002, color: "#f00", strokeWidth: 2 }),
    ).toBeNull();
  });

  it("passes point-based shapes through unchanged", () => {
    const pen = { type: "pen", x: 0, y: 0, width: 0, height: 0, points: [{ x: 0, y: 0 }, { x: 0.5, y: 0.5 }], color: "#f00", strokeWidth: 2 };
    expect(normalizeShape(pen)).toEqual(pen);
  });

  it("returns null for a point shape with fewer than 2 points", () => {
    expect(
      normalizeShape({ type: "pen", x: 0, y: 0, width: 0, height: 0, points: [{ x: 0.1, y: 0.1 }], color: "#f00", strokeWidth: 2 }),
    ).toBeNull();
  });
});

describe("px conversion", () => {
  // Normalized coordinates are fractions of the rendered media box; the SVG
  // overlay is that box in px, so conversion is a straight multiply. All
  // helpers must emit px numbers (not "%" strings): Path `d` and Polygon
  // `points` have no percent form in SVG, so mixing units was the original bug.
  it("rectToSvgProps multiplies by the layout size", () => {
    expect(
      rectToSvgProps({ type: "rectangle", x: 0.25, y: 0.5, width: 0.5, height: 0.25, color: "#f00", strokeWidth: 2 }, 400, 200),
    ).toEqual({ x: 100, y: 100, width: 200, height: 50 });
  });

  it("ellipseToSvgProps emits center + radii in px", () => {
    expect(
      ellipseToSvgProps({ type: "ellipse", x: 0.25, y: 0.5, width: 0.5, height: 0.25, color: "#f00", strokeWidth: 2 }, 400, 200),
    ).toEqual({ cx: 200, cy: 125, rx: 100, ry: 25 });
  });

  it("pointsToSvgPath builds an M/L path in px", () => {
    expect(
      pointsToSvgPath([{ x: 0, y: 0 }, { x: 0.5, y: 1 }], 100, 50),
    ).toBe("M 0 0 L 50 50");
  });

  it("arrowHeadPoints returns a 3-point polygon ending at the tip", () => {
    const pts = arrowHeadPoints({ x: 0, y: 0.5 }, { x: 1, y: 0.5 }, 100, 50);
    const [tip] = pts.split(" ");
    expect(tip).toBe("100,25");
    expect(pts.split(" ")).toHaveLength(3);
  });
});
