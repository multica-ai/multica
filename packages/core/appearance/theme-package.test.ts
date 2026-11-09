// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { INSTALLED_THEMES_STORAGE_KEY, findTheme, useInstalledThemesStore } from "./installed-themes";
import { parseThemePackage, themeCss } from "./theme-package";

const FONT = "data:font/woff2;base64,d09GMgABAAAAAA==";

const paper = {
  apiVersion: 1,
  id: "paper",
  name: "Paper",
  author: "Someone",
  version: "1.2.0",
  tokens: {
    light: { "page-canvas": "#f6efdf", "font-content-latin": '"Paper Serif", serif', "not-a-token": "red" },
    dark: { "page-canvas": "oklch(0.2 0.01 60)" },
  },
  options: [
    { id: "header", type: "select", label: { en: "Headers", "zh-Hans": "分组标题" }, default: "block", choices: [
      { value: "flat", label: "Flat" },
      { value: "block", label: "Block", swatch: "oklch(0.8 0.1 90)" },
    ] },
    { id: "hyphens", type: "toggle", label: "Hyphenate", default: true },
  ],
  css: '@font-face { font-family: "Paper Serif"; src: url(fonts/serif.woff2); }\n:root[data-theme-option-hyphens] [data-mc="content"] p { hyphens: auto; }',
  assets: { "fonts/serif.woff2": FONT },
  extra: "ignored",
};

describe("parseThemePackage", () => {
  it("rebuilds a valid package from what passes validation", () => {
    const result = parseThemePackage(paper);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.theme).toMatchObject({
      id: "paper",
      name: "Paper",
      author: "Someone",
      version: "1.2.0",
      tokens: {
        light: { "page-canvas": "#f6efdf", "font-content-latin": '"Paper Serif", serif' },
        dark: { "page-canvas": "oklch(0.2 0.01 60)" },
      },
    });
    expect(result.theme.tokens.light).not.toHaveProperty("not-a-token");
    expect(result.theme).not.toHaveProperty("extra");
    expect(result.theme.options).toHaveLength(2);
    expect(themeCss(result.theme)).toContain(`url("${FONT}")`);
  });

  it("lists every problem instead of stopping at the first", () => {
    const result = parseThemePackage({
      apiVersion: 2,
      id: "Bad Id",
      tokens: {},
      css: "body { background: url(https://evil.test); }",
      assets: { "../x.woff2": FONT, "y.png": "https://evil.test/y.png" },
      options: [{ id: "x", type: "select", label: "X", choices: [{ value: "a", label: "A" }] }],
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.errors.join("\n")).toMatch(/apiVersion/);
    expect(result.errors.join("\n")).toMatch(/^id must/m);
    expect(result.errors.join("\n")).toMatch(/name is required/);
    expect(result.errors.join("\n")).toMatch(/css line 1/);
    expect(result.errors.join("\n")).toMatch(/Asset "\.\.\/x\.woff2"/);
    expect(result.errors.join("\n")).toMatch(/Asset "y\.png"/);
    expect(result.errors.join("\n")).toMatch(/at least two/);
  });

  it("refuses to shadow a built-in theme", () => {
    const result = parseThemePackage({ ...paper, id: "pop" });
    expect(result.ok).toBe(false);
  });

  it("rejects packages over the size limit", () => {
    const result = parseThemePackage({ ...paper, description: "x".repeat(1024 * 1024) });
    expect(result.ok).toBe(false);
  });
});

describe("installed themes", () => {
  beforeEach(() => {
    localStorage.clear();
    useInstalledThemesStore.setState({ themes: [] });
  });

  it("installs, replaces by id and uninstalls", () => {
    const parsed = parseThemePackage(paper);
    if (!parsed.ok) throw new Error("fixture");
    const { install, uninstall } = useInstalledThemesStore.getState();
    install(parsed.theme);
    install({ ...parsed.theme, version: "1.3.0" });
    expect(useInstalledThemesStore.getState().themes).toHaveLength(1);
    expect(findTheme("paper")?.version).toBe("1.3.0");
    uninstall("paper");
    expect(findTheme("paper")).toBeUndefined();
  });

  it("checks stored packages again when loading them", async () => {
    localStorage.setItem(
      INSTALLED_THEMES_STORAGE_KEY,
      JSON.stringify({
        state: { themes: [paper, { ...paper, id: "tampered", css: "body { background: url(https://evil.test); }" }] },
        version: 0,
      }),
    );
    await useInstalledThemesStore.persist.rehydrate();
    expect(useInstalledThemesStore.getState().themes.map((t) => t.id)).toEqual(["paper"]);
  });
});
