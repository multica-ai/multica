// @vitest-environment node
import { describe, expect, it } from "vitest";
import { colorToHex, hexToOklch, isSrgb } from "./color";
import {
  baseline,
  colorAlpha,
  withColorAlpha,
  emptyDraft,
  exportCss,
  isDraft,
  updateToken,
} from "./tokens";
import { decodeSession, encodeSession } from "./storage";

describe("color editing boundary", () => {
  it.each(["#FFFFFF", "#000000", "#FF0000", "#0000FF", "#00FF00", "#2563EB"])(
    "round trips %s through persisted OKLCH",
    (hex) => {
      const value = hexToOklch(hex)!;
      expect(colorToHex(value)).toBe(hex);
      const draft = updateToken(emptyDraft(), "light", "--brand", value);
      expect(isDraft(draft)).toBe(true);
      expect(
        decodeSession(encodeSession({ draft, designs: [] })).draft,
      ).toEqual(draft);
      expect(exportCss(draft)).toContain(value);
    },
  );
  it("accepts short HEX while rejecting alpha, CSS and malformed values", () => {
    expect(colorToHex(hexToOklch("abc")!)).toBe("#AABBCC");
    for (const value of [
      "#12345",
      "#12345678",
      "red",
      "#GGFFFF",
      "",
      "#000; color: red",
    ])
      expect(hexToOklch(value)).toBeNull();
  });
  it("displays a gamut-mapped approximation without mutating source tokens", () => {
    const source = "oklch(0.7 0.4 150)";
    expect(isSrgb(source)).toBe(false);
    expect(colorToHex(source)).toMatch(/^#[A-F0-9]{6}$/);
    expect(colorToHex(baseline.light["--brand"]!)).toMatch(/^#[A-F0-9]{6}$/);
  });
});

describe("transparent semantic colors", () => {
  it("preserves opacity when changing hue and round trips it through storage", () => {
    const value = withColorAlpha(
      hexToOklch("#123456")!,
      colorAlpha("oklch(1 0 0 / 6%)"),
    );
    expect(colorAlpha(value)).toBe(0.06);
    expect(colorToHex(value)).toBe("#123456");
    const draft = updateToken(emptyDraft(), "dark", "--border", value);
    expect(isDraft(draft)).toBe(true);
    expect(decodeSession(encodeSession({ draft, designs: [] })).draft).toEqual(
      draft,
    );
    expect(exportCss(draft)).toContain(" / 0.06)");
  });
  it("restores equivalent percentage and decimal alpha without leaving a change", () => {
    const original = baseline.dark["--border"]!;
    expect(withColorAlpha(original, colorAlpha(original))).toBe(original);
    const draft = updateToken(
      emptyDraft(),
      "dark",
      "--border",
      "oklch(1 0 0 / 0.2)",
    );
    expect(
      updateToken(draft, "dark", "--border", "oklch(1 0 0 / 0.06)"),
    ).toEqual(emptyDraft());
    for (const value of [
      "oklch(1 0 0 / 0)",
      "oklch(1 0 0 / 1)",
      "oklch(1 0 0 / 100%)",
    ]) {
      expect(isDraft({ ...emptyDraft(), dark: { "--border": value } })).toBe(
        true,
      );
    }
    for (const value of [
      "oklch(1 0 0 / 101%)",
      "oklch(1 0 0 / 1.1)",
      "oklch(1 0 0 / .)",
      "oklch(1 0 0 / -1)",
      "oklch(1 0 0 / 5%); color: red",
    ]) {
      expect(isDraft({ ...emptyDraft(), dark: { "--border": value } })).toBe(
        false,
      );
    }
  });
});
