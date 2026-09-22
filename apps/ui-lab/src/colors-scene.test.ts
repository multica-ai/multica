// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  baseline,
  colorGroups,
  colorTokens,
  colorAliases,
  emptyDraft,
  isDraft,
  updateToken,
  exportCss,
  previewCss,
} from "./tokens";
import { colorToHex } from "./color";
import { isColorSelection } from "./protocol";

describe("semantic color catalog", () => {
  it("includes each role once, with editable values from both source themes", () => {
    expect(new Set(colorTokens.map(([key]) => key)).size).toBe(
      colorTokens.length,
    );
    expect(new Set(colorGroups.map((group) => group.id)).size).toBe(
      colorGroups.length,
    );
    for (const [key] of colorTokens) {
      for (const theme of ["light", "dark"] as const) {
        const value = baseline[theme][key]!;
        expect(value, `${theme}: ${key}`).toMatch(/^oklch\(/);
        expect(
          isDraft({ ...emptyDraft(), [theme]: { [key]: value } }),
          `${theme}: ${key}`,
        ).toBe(true);
        expect(colorToHex(value)).toMatch(/^#[0-9A-F]{6}$/);
      }
    }
  });
  it("keeps aliases linked to source roles instead of allowing independent overrides", () => {
    for (const [alias, target] of colorAliases) {
      expect(baseline.light[alias]).toBe(`var(${target})`);
      expect(baseline.dark[alias]).toBe(`var(${target})`);
      expect(colorTokens.some(([key]) => key === target)).toBe(true);
      expect(
        isDraft({ ...emptyDraft(), light: { [alias]: "oklch(0.5 0.1 255)" } }),
      ).toBe(false);
    }
  });
  it("exports newly available feedback colors only to their edited theme", () => {
    const draft = updateToken(
      emptyDraft(),
      "dark",
      "--success",
      "oklch(0.7 0.1 150)",
    );
    expect(isDraft(draft)).toBe(true);
    expect(exportCss(draft)).toContain(
      ".dark {\n  --success: oklch(0.7 0.1 150);\n}",
    );
    expect(previewCss(draft)).not.toContain(":root");
  });
  it("accepts only known color selections from the frame", () => {
    expect(
      isColorSelection({
        type: "multica-ui-lab:color-select",
        token: "--brand",
      }),
    ).toBe(true);
    for (const token of [
      "--radius",
      "--background",
      "--not-a-color",
      "red; display: none",
      undefined,
    ]) {
      expect(
        isColorSelection({ type: "multica-ui-lab:color-select", token }),
      ).toBe(false);
    }
    expect(isColorSelection({ type: "other", token: "--brand" })).toBe(false);
    expect(isColorSelection(null)).toBe(false);
  });
});
