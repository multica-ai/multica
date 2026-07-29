import { describe, it, expect } from "vitest";
import {
  hexToHsv,
  hsvToHex,
  normalizeHex,
  randomOptionColor,
} from "./color-utils";

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

describe("normalizeHex", () => {
  it("normalizes case and missing hash", () => {
    expect(normalizeHex("#3B82F6")).toBe("#3b82f6");
    expect(normalizeHex("3b82f6")).toBe("#3b82f6");
    expect(normalizeHex("  #3b82f6  ")).toBe("#3b82f6");
  });

  it("rejects anything that is not a 6-digit hex color", () => {
    for (const bad of ["#fff", "3b82f", "#3b82f6ff", "red", ""]) {
      expect(normalizeHex(bad)).toBeNull();
    }
  });
});

describe("hexToHsv / hsvToHex", () => {
  it("converts primaries and grays to the expected HSV", () => {
    expect(hexToHsv("#ff0000")).toEqual({ h: 0, s: 1, v: 1 });
    expect(hexToHsv("#00ff00")).toEqual({ h: 120, s: 1, v: 1 });
    expect(hexToHsv("#0000ff")).toEqual({ h: 240, s: 1, v: 1 });
    expect(hexToHsv("#000000")).toEqual({ h: 0, s: 0, v: 0 });
    expect(hexToHsv("#ffffff")).toEqual({ h: 0, s: 0, v: 1 });
  });

  it("returns null for invalid input", () => {
    expect(hexToHsv("nope")).toBeNull();
  });

  it("round-trips every representable color it produces", () => {
    for (const hex of [
      "#6b7280",
      "#ef4444",
      "#f97316",
      "#eab308",
      "#22c55e",
      "#14b8a6",
      "#3b82f6",
      "#6366f1",
      "#a855f7",
      "#ec4899",
      "#000000",
      "#ffffff",
    ]) {
      const hsv = hexToHsv(hex);
      expect(hsv).not.toBeNull();
      expect(hsvToHex(hsv!)).toBe(hex);
    }
  });
});

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
    for (const avoid of ["#808080", "#000000", "not-a-color", "#fff", ""]) {
      expect(randomOptionColor(avoid)).toMatch(/^#[0-9a-f]{6}$/);
    }
  });
});
