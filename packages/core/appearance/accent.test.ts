// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { ACCENT_PRESETS, READING_SURFACE_DEFAULTS, accentContrast, accentForeground } from "./accent";
import { BUILT_IN_THEMES, findBuiltInTheme } from "./built-in-themes";
import { basePaletteFromTokens } from "./theme-contrast";
import type { Theme } from "./themes";

const base = basePaletteFromTokens(readFileSync(new URL("../../ui/styles/tokens.css", import.meta.url), "utf8"));
const defaults = findBuiltInTheme("default")!;
const failing = (results: ReturnType<typeof accentContrast>) => results.filter((r) => r.ratio < r.required);

describe("accent", () => {
  it("measures against the reading surfaces tokens.css declares", () => {
    for (const mode of ["light", "dark"] as const) {
      for (const [name, value] of Object.entries(READING_SURFACE_DEFAULTS[mode])) {
        expect(base[mode].get(name), `${mode} ${name}`).toBe(value);
      }
    }
  });

  it("labels a fill in light ink while that reads, and in dark ink otherwise", () => {
    expect(accentForeground("#2563eb")).toBe("oklch(0.985 0 0)");
    expect(accentForeground("#fde047")).toBe("oklch(0.21 0.006 285.885)");
    expect(accentForeground("not a colour")).toBe("oklch(0.985 0 0)");
  });

  it.each(BUILT_IN_THEMES.map((theme) => [theme.name, theme] as const))(
    "offers presets that pass AA in both modes on %s",
    (_name, theme) => {
      for (const preset of ACCENT_PRESETS) {
        expect(failing(accentContrast(preset.color, () => theme, {})), preset.id).toEqual([]);
      }
    },
  );

  it("reports where a custom accent falls short, per mode", () => {
    const results = accentContrast({ light: "#fde047", dark: "#1e3a8a" }, () => defaults, {});
    expect(failing(results).map((r) => `${r.mode} ${r.foreground} ${r.background}`)).toEqual([
      "light --brand --page-canvas",
      "light --brand --surface",
      "light --brand --surface-raised",
      "light --brand --background",
      "dark --brand --page-canvas",
      "dark --brand --surface",
      "dark --brand --surface-raised",
      "dark --brand --background",
    ]);
    expect(results.find((r) => r.mode === "light" && r.foreground === "--brand-foreground")!.ratio).toBeGreaterThan(4.5);
  });

  it("measures over the theme each mode shows, with its options", () => {
    const paper: Theme = {
      apiVersion: 1,
      id: "paper",
      name: "Paper",
      css: ".theme-dark { --page-canvas: var(--paper); --surface: color-mix(in oklab, white 10%, black); }",
      options: [{ id: "paper", type: "color", label: "Paper", default: { light: "#ffffff", dark: "#000000" } }],
    };
    const accent = { light: "#2563eb", dark: "#3b82f6" };
    const themeFor = (mode: "light" | "dark") => (mode === "dark" ? paper : defaults);
    const darkCanvas = (options: Record<string, Record<string, { dark: string }>>) =>
      accentContrast(accent, themeFor, options).find((r) => r.mode === "dark" && r.background === "--page-canvas")!;
    expect(darkCanvas({}).ratio).toBeGreaterThan(4.5);
    expect(darkCanvas({ paper: { paper: { dark: "#3b82f6" } } }).ratio).toBe(1);
    expect(accentContrast(accent, themeFor, {}).some((r) => r.mode === "dark" && r.background === "--surface")).toBe(false);
  });
});
