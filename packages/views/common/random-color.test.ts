import { describe, it, expect } from "vitest";
import { randomOptionColor } from "./random-color";

const RUNS = 200;

function hexToHsl(hex: string): { h: number; s: number; l: number } {
  const r = parseInt(hex.slice(1, 3), 16) / 255;
  const g = parseInt(hex.slice(3, 5), 16) / 255;
  const b = parseInt(hex.slice(5, 7), 16) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const delta = max - min;
  const l = (max + min) / 2;
  if (delta === 0) return { h: 0, s: 0, l };
  const s = delta / (1 - Math.abs(2 * l - 1));
  let h: number;
  if (max === r) h = ((g - b) / delta) % 6;
  else if (max === g) h = (b - r) / delta + 2;
  else h = (r - g) / delta + 4;
  return { h: (h * 60 + 360) % 360, s, l };
}

function hueDistance(a: number, b: number): number {
  const diff = Math.abs(a - b) % 360;
  return Math.min(diff, 360 - diff);
}

describe("randomOptionColor", () => {
  it("returns a lowercase #rrggbb hex string", () => {
    for (let i = 0; i < RUNS; i++) {
      expect(randomOptionColor()).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  it("stays inside the vivid saturation/lightness band", () => {
    for (let i = 0; i < RUNS; i++) {
      const { s, l } = hexToHsl(randomOptionColor());
      // Rounding hex channels shifts HSL slightly; allow a small tolerance
      // around the generator's [0.62, 0.9] / [0.46, 0.62] sampling ranges.
      expect(s).toBeGreaterThan(0.55);
      expect(s).toBeLessThan(0.95);
      expect(l).toBeGreaterThan(0.42);
      expect(l).toBeLessThan(0.66);
    }
  });

  it("keeps a visible hue distance from the avoided color", () => {
    const avoid = "#f97316"; // orange preset, hue ~25
    const avoidHue = hexToHsl(avoid).h;
    for (let i = 0; i < RUNS; i++) {
      const next = randomOptionColor(avoid);
      expect(next).not.toBe(avoid);
      expect(hueDistance(hexToHsl(next).h, avoidHue)).toBeGreaterThanOrEqual(30);
    }
  });

  it("still returns a valid color when avoid is achromatic or malformed", () => {
    for (const avoid of ["#808080", "not-a-color", "#fff", ""]) {
      expect(randomOptionColor(avoid)).toMatch(/^#[0-9a-f]{6}$/);
    }
  });
});
