// @vitest-environment node
import { describe, expect, it } from "vitest";

import { contrastRatio } from "./color";
import { deriveHexTokens, deriveTokens, PRODUCT_SKIN } from "./derive";

const BACKGROUNDS = [
  "--app-shell",
  "--page-canvas",
  "--background",
  "--surface",
  "--surface-raised",
  "--surface-hover",
  "--surface-selected",
  "--card",
  "--popover",
  "--muted",
  "--secondary",
  "--accent",
  "--sidebar",
  "--sidebar-accent",
] as const;

describe("product skin derivation", () => {
  it("keeps Linear catalog seeds", () => {
    expect(PRODUCT_SKIN.light.accent).toBe("#5e6ad2");
    expect(PRODUCT_SKIN.dark.foundation).toBe("#17181d");
  });

  it.each(["light", "dark"] as const)(
    "muted-foreground clears 4.5:1 on every %s surface",
    (variant) => {
      const tokens = deriveHexTokens(variant);
      const muted = tokens["--muted-foreground"];
      expect(muted).toMatch(/^#/);
      for (const key of BACKGROUNDS) {
        const background = tokens[key];
        expect(background, key).toMatch(/^#/);
        expect(
          contrastRatio(muted!, background!),
          `${variant} muted on ${key}`,
        ).toBeGreaterThanOrEqual(4.5);
      }
    },
  );

  it.each(["light", "dark"] as const)(
    "faint-foreground clears 3:1 and stays quieter than muted in %s",
    (variant) => {
      const tokens = deriveHexTokens(variant);
      const faint = tokens["--faint-foreground"]!;
      const muted = tokens["--muted-foreground"]!;
      const surface = tokens["--surface"]!;
      for (const key of BACKGROUNDS) {
        expect(contrastRatio(faint, tokens[key]!)).toBeGreaterThanOrEqual(3);
      }
      expect(contrastRatio(faint, surface)).toBeLessThan(contrastRatio(muted, surface));
    },
  );

  it("exposes hover and active slots for primary and brand", () => {
    const light = deriveTokens("light");
    expect(light["--primary-hover"]).toMatch(/^oklch\(/);
    expect(light["--primary-pressed"]).toMatch(/^oklch\(/);
    expect(light["--brand-hover"]).toMatch(/^oklch\(/);
    expect(light["--brand"]).not.toBe(light["--primary"]);
  });
});
