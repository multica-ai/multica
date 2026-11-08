// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  APPEARANCE_STORAGE_KEY,
  BUILT_IN_THEMES,
  DEFAULT_APPEARANCE,
  appearanceRootVariables,
  appearanceStylesheet,
  appearanceThemeAttributes,
  isSafeThemeValue,
  sanitizeAppearance,
  themeStylesheet,
  useAppearanceStore,
  type ThemeManifest,
} from ".";

describe("appearance store", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
  });

  it("saves the preferences on this device", () => {
    useAppearanceStore.getState().set({ theme: "github", fontSize: 16 });
    const saved = JSON.parse(localStorage.getItem(APPEARANCE_STORAGE_KEY) ?? "{}");
    expect(saved.state).toMatchObject({ theme: "github", fontSize: 16, density: "default" });
  });

  it("resets everything to the defaults", () => {
    useAppearanceStore.getState().set({ theme: "pop", accent: "green", highContrast: true });
    useAppearanceStore.getState().reset();
    expect(sanitizeAppearance(useAppearanceStore.getState())).toEqual(DEFAULT_APPEARANCE);
  });

  it("reads the first version's style as the theme", async () => {
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { style: "pop" }, version: 0 }),
    );
    await useAppearanceStore.persist.rehydrate();
    expect(useAppearanceStore.getState().theme).toBe("pop");
  });
});

describe("sanitizeAppearance", () => {
  it("falls back per field for unknown or missing values", () => {
    expect(
      sanitizeAppearance({ theme: "solarized", fontSize: 40, density: "comfortable", reduceMotion: "yes" }),
    ).toEqual({ ...DEFAULT_APPEARANCE, density: "comfortable" });
    expect(sanitizeAppearance(null)).toEqual(DEFAULT_APPEARANCE);
  });
});

describe("appearanceRootVariables", () => {
  it("leaves every variable unset for the defaults", () => {
    expect(Object.values(appearanceRootVariables(DEFAULT_APPEARANCE)).every((v) => v === null)).toBe(true);
  });

  it("maps fonts, size and density onto variables the stylesheets read", () => {
    const vars = appearanceRootVariables({
      ...DEFAULT_APPEARANCE,
      uiFont: "serif",
      cjkFont: "kai",
      codeFont: "jetbrains",
      fontSize: 16,
      density: "compact",
    });
    expect(vars["--font-ui"]).toBe("var(--font-ui-serif)");
    expect(vars["--font-cjk"]).toBe("var(--font-cjk-kai)");
    expect(vars["--font-mono"]).toMatch(/^var\(--font-code-jetbrains\)/);
    expect(vars["--font-scale"]).toBe(String(16 / 14));
    expect(vars["--issue-row-height"]).toBe("32px");
  });
});

describe("appearanceStylesheet", () => {
  it("is empty for the defaults", () => {
    expect(appearanceStylesheet(DEFAULT_APPEARANCE)).toBe("");
    expect(appearanceThemeAttributes(DEFAULT_APPEARANCE)).toEqual({ theme: null, traits: null });
  });

  it("puts the accent and contrast after the theme so they win", () => {
    const css = appearanceStylesheet({
      ...DEFAULT_APPEARANCE,
      theme: "github",
      accent: "violet",
      highContrast: true,
    });
    const theme = css.indexOf("--button-primary:");
    const accent = css.indexOf("--brand: oklch(0.52 0.2 292)");
    const contrast = css.indexOf("--muted-foreground: color-mix");
    expect(theme).toBeGreaterThanOrEqual(0);
    expect(accent).toBeGreaterThan(theme);
    expect(contrast).toBeGreaterThan(accent);
  });

  it("names the theme and its traits for appearance.css", () => {
    expect(appearanceThemeAttributes({ ...DEFAULT_APPEARANCE, theme: "pop" })).toEqual({
      theme: "pop",
      traits: "offset-shadow status-blocks outlined-chips",
    });
    expect(appearanceThemeAttributes({ ...DEFAULT_APPEARANCE, theme: "notion" })).toEqual({
      theme: "notion",
      traits: null,
    });
  });
});

describe("themes", () => {
  it("gives every built-in theme a unique id and safe values", () => {
    expect(new Set(BUILT_IN_THEMES.map((t) => t.id)).size).toBe(BUILT_IN_THEMES.length);
    for (const theme of BUILT_IN_THEMES) {
      for (const tokens of [theme.tokens.light, theme.tokens.dark, theme.frame?.light, theme.frame?.dark]) {
        for (const value of Object.values(tokens ?? {})) expect(isSafeThemeValue(value)).toBe(true);
      }
    }
  });

  it("drops unknown tokens and unsafe values from a manifest", () => {
    const manifest = {
      id: "evil",
      name: "Evil",
      tokens: {
        light: {
          brand: "red; } body { display: none",
          "page-canvas": "url(https://example.com/x.png)",
          surface: "oklch(1 0 0)",
          position: "fixed",
        },
        dark: {},
      },
    } as unknown as ThemeManifest;
    expect(themeStylesheet(manifest)).toBe(":root:not(.dark) { --surface: oklch(1 0 0); }");
  });
});
