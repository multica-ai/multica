// @vitest-environment node
import { describe, expect, it } from "vitest";
import { contrastPasses, contrastRatio } from "./contrast";

describe("rendered color contrast", () => {
  it("uses WCAG relative luminance", () => {
    expect(contrastRatio("black", ["white"])).toBeCloseTo(21, 5);
    expect(contrastRatio("white", ["white"])).toBeCloseTo(1, 5);
    expect(contrastRatio("#777", ["white"])).toBeCloseTo(4.478, 3);
  });
  it("composites translucent surfaces and foregrounds in paint order", () => {
    expect(contrastRatio("rgb(0 0 0 / 50%)", ["white"])).toBeCloseTo(
      contrastRatio("rgb(127.5 127.5 127.5)", ["white"]),
      8,
    );
    expect(
      contrastRatio("white", [
        "black",
        "rgb(255 255 255 / 10%)",
        "rgb(255 255 255 / 50%)",
      ]),
    ).toBeCloseTo(contrastRatio("white", ["rgb(140.25 140.25 140.25)"]), 8);
    expect(contrastRatio("transparent", ["white"])).toBeCloseTo(1, 8);
  });
  it("does not round a failing ratio into a pass", () => {
    expect(contrastPasses(4.4999, 4.5)).toBe(false);
    expect(contrastPasses(4.5, 4.5)).toBe(true);
    expect(contrastPasses(2.9999, 3)).toBe(false);
  });
  it("requires a known canvas and rejects unsupported colors", () => {
    expect(() => contrastRatio("black", [])).toThrow();
    expect(() => contrastRatio("black", ["transparent"])).toThrow();
    expect(() => contrastRatio("var(--unknown)", ["white"])).toThrow();
  });
});
