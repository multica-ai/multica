import { describe, it, expect } from "vitest";
import {
  clampTransform,
  computeCropRect,
  coverBaseScale,
  maxOffset,
} from "./avatar-crop";

describe("coverBaseScale", () => {
  it("scales the short side to the viewport", () => {
    expect(coverBaseScale(1000, 500, 256)).toBeCloseTo(256 / 500);
    expect(coverBaseScale(500, 1000, 256)).toBeCloseTo(256 / 500);
    expect(coverBaseScale(256, 256, 256)).toBe(1);
  });

  it("guards against a zero dimension", () => {
    expect(Number.isFinite(coverBaseScale(0, 0, 256))).toBe(true);
  });
});

describe("maxOffset", () => {
  it("is zero when the image exactly covers the viewport", () => {
    expect(maxOffset(256, 256)).toBe(0);
  });

  it("is half the overflow otherwise", () => {
    expect(maxOffset(512, 256)).toBe(128);
  });

  it("never goes negative", () => {
    expect(maxOffset(100, 256)).toBe(0);
  });
});

describe("clampTransform", () => {
  const square = { imageWidth: 1000, imageHeight: 1000, viewport: 256 };

  it("pins a cover-fit image to the center (no pan room)", () => {
    const t = clampTransform({ ...square, scale: 1, offsetX: 200, offsetY: -50 });
    expect(t.offsetX).toBe(0);
    expect(t.offsetY).toBe(0);
  });

  it("allows pan up to the overflow half at higher zoom", () => {
    // scale 2 → drawn 512, overflow half = 128.
    const t = clampTransform({ ...square, scale: 2, offsetX: 999, offsetY: -999 });
    expect(t.offsetX).toBe(128);
    expect(t.offsetY).toBe(-128);
  });

  it("only allows pan on the overflowing axis for a wide image", () => {
    const wide = { imageWidth: 1000, imageHeight: 500, viewport: 256 };
    const t = clampTransform({ ...wide, scale: 1, offsetX: 999, offsetY: 999 });
    expect(t.offsetX).toBe(128); // drawnW 512 → half overflow 128
    expect(t.offsetY).toBe(0); // drawnH 256 → no vertical room
  });
});

describe("computeCropRect", () => {
  it("returns the full image for a centered cover fit", () => {
    const rect = computeCropRect({
      imageWidth: 1000,
      imageHeight: 1000,
      viewport: 256,
      scale: 1,
      offsetX: 0,
      offsetY: 0,
    });
    expect(rect.sx).toBeCloseTo(0);
    expect(rect.sy).toBeCloseTo(0);
    expect(rect.size).toBeCloseTo(1000);
  });

  it("samples the centered square of a wide image", () => {
    const rect = computeCropRect({
      imageWidth: 1000,
      imageHeight: 500,
      viewport: 256,
      scale: 1,
      offsetX: 0,
      offsetY: 0,
    });
    expect(rect.sx).toBeCloseTo(250);
    expect(rect.sy).toBeCloseTo(0);
    expect(rect.size).toBeCloseTo(500);
  });

  it("shrinks and recenters the sampled rect as zoom increases", () => {
    const rect = computeCropRect({
      imageWidth: 1000,
      imageHeight: 1000,
      viewport: 256,
      scale: 2,
      offsetX: 0,
      offsetY: 0,
    });
    expect(rect.size).toBeCloseTo(500);
    expect(rect.sx).toBeCloseTo(250);
    expect(rect.sy).toBeCloseTo(250);
  });

  it("moves the sampled rect opposite the pan direction", () => {
    // A positive offsetX shifts the image right, so the viewport center reveals
    // a source pixel to the LEFT of the image center → smaller sx.
    const base = computeCropRect({
      imageWidth: 1000,
      imageHeight: 1000,
      viewport: 256,
      scale: 2,
      offsetX: 0,
      offsetY: 0,
    });
    const panned = computeCropRect({
      imageWidth: 1000,
      imageHeight: 1000,
      viewport: 256,
      scale: 2,
      offsetX: 64,
      offsetY: 0,
    });
    expect(panned.sx).toBeLessThan(base.sx);
  });
});
