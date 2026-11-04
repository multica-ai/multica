// @vitest-environment node
import { describe, expect, it } from "vitest";
import { unitsFor } from "./office-protocol";

// The engine lays the studio out only from STUDIO_MIN_W x STUDIO_MIN_H units (see the patch), so the page
// must never ask for less.
const ENGINE_MIN = { w: 416, h: 171 };

describe("unitsFor", () => {
  it("asks for the engine's minimum before the page has a size", () => {
    expect(unitsFor(0, 0)).toEqual(ENGINE_MIN);
  });

  it("grows past the minimum on a wide or tall page, never under it", () => {
    const pages: [number, number][] = [
      [1280, 720],
      [1920, 1080],
      [2560, 1080],
      [1000, 720],
      [300, 200],
    ];
    for (const [width, height] of pages) {
      const units = unitsFor(width, height);
      expect(units.w).toBeGreaterThanOrEqual(ENGINE_MIN.w);
      expect(units.h).toBeGreaterThanOrEqual(ENGINE_MIN.h);
    }
  });

  it("keeps a worker's figure above 2.7 pixels per unit on a 1280 by 720 page", () => {
    const units = unitsFor(1280, 720);
    expect(1280 / units.w).toBeGreaterThan(2.7);
    expect(720 / units.h).toBeGreaterThan(2.7);
  });
});
