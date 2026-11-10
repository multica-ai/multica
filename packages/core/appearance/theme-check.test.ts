// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { BUILT_IN_THEMES } from "./built-in-themes";
import { checkThemeFiles, checkThemePackage } from "./theme-check";
import { basePaletteFromTokens, contrastRatio, parseColor, resolveColor } from "./theme-contrast";
import { exportTheme, themeCopyTarget } from "./theme-export";

const base = basePaletteFromTokens(readFileSync(new URL("../../ui/styles/tokens.css", import.meta.url), "utf8"));

const manifest = { apiVersion: 1, id: "paper", name: "Paper" };
const check = (css: string, extra: Record<string, unknown> = {}) =>
  checkThemePackage({ manifest: { ...manifest, ...extra }, css }, base);
const errors = (css: string, extra?: Record<string, unknown>) =>
  check(css, extra)
    .issues.filter((issue) => issue.level === "error")
    .map((issue) => issue.message);

describe("theme check", () => {
  it.each(BUILT_IN_THEMES.map((theme) => [theme.name, theme] as const))(
    "passes %s, duplicated as a theme folder",
    (_name, theme) => {
      const report = checkThemeFiles(exportTheme(theme, themeCopyTarget(theme, [])), base);
      expect(report.issues.filter((issue) => issue.level === "error")).toEqual([]);
      expect(report.contrast.length).toBeGreaterThan(0);
    },
  );

  it("passes a minimal theme", () => {
    expect(check(".theme-light { --radius: 0.25rem; --page-canvas: #fffdf8; }")).toMatchObject({ ok: true, issues: [] });
  });

  it("holds colours the theme inherits to the surfaces it changes", () => {
    const found = errors(".theme-light { --page-canvas: #f6efdf; }");
    expect(found).toEqual([
      expect.stringMatching(
        /^Contrast, light mode: --brand is below 4\.5:1 on --background \(4\.\d\d:1\) and --page-canvas \(4\.\d\d:1\)\.$/,
      ),
    ]);
  });

  it("reports manifest errors and blocked CSS together", () => {
    const found = errors("body { background: url(https://evil.test/x.png); }\n@import 'x.css';", { apiVersion: 9, id: "Bad" });
    expect(found.join("\n")).toMatch(/apiVersion must be 1/);
    expect(found.join("\n")).toMatch(/^id must/m);
    expect(found.join("\n")).toMatch(/theme\.css line 1: url\("https:\/\/evil\.test\/x\.png"\)/);
    expect(found.join("\n")).toMatch(/theme\.css line 2: @import is not allowed/);
  });

  it("reports variables that are not public, but not the theme's own or its options'", () => {
    const found = errors(
      [
        ".theme-light { --sidebar-width: 20rem; --paper-tint: #eee; --ring: var(--paper-tint); }",
        ":root { --paper-color: #fff; }",
      ].join("\n"),
      { options: [{ id: "paper-color", type: "color", label: "Paper", default: "#ffffff" }] },
    );
    expect(found).toHaveLength(1);
    expect(found[0]).toMatch(/line 1: --sidebar-width is not a public variable/);
  });

  it("reports generic families in Latin font stacks", () => {
    const found = errors(
      '.theme-light { --font-ui: Inter, sans-serif; --font-content-latin: "Iowan Old Style", Georgia; }',
      {
        options: [
          { id: "font-content-latin", type: "variable-select", label: "Text", choices: [
            { value: "Georgia", label: "Georgia" },
            { value: "Georgia, serif", label: "Georgia (serif)" },
          ] },
        ],
      },
    );
    expect(found).toHaveLength(2);
    expect(found[0]).toMatch(/--font-ui names sans-serif/);
    expect(found[1]).toMatch(/"Georgia, serif"/);
    const warnings = check('@font-face {\n  font-family: "Paper";\n}\nh1 { font-family: Georgia; }').issues;
    expect(warnings).toEqual([
      expect.objectContaining({ level: "warning", message: expect.stringMatching(/^theme\.css line 4: font-family/) }),
    ]);
  });

  it("reports text without enough contrast, one line per mode and colour", () => {
    const found = errors(".theme-dark { --muted-foreground: #444; --brand: oklch(0.3 0.1 262); }");
    expect(found).toHaveLength(2);
    expect(found[0]).toMatch(/^Contrast, dark mode: --muted-foreground is below 4\.5:1 on .*--surface \(\d\.\d\d:1\)/);
    expect(found[1]).toMatch(/^Contrast, dark mode: --brand is below 4\.5:1 on .*--page-canvas/);
    const afterStatement = errors('@charset "utf-8";\n.theme-light { --muted-foreground: #a0a0a0; }');
    expect(afterStatement).toEqual([expect.stringMatching(/^Contrast, light mode: --muted-foreground is below .*--page-canvas/)]);
  });

  it("checks the primary button label and a frame of its own", () => {
    const found = errors(
      [
        ".theme-light { --button-primary: #ffd400; --button-primary-foreground: #ffffff; }",
        '.theme-light :is([data-mc="app-rail"]) { --app-shell: #000000; --foreground: #111111; }',
      ].join("\n"),
    );
    expect(found.some((m) => /--button-primary-foreground is below 4\.5:1 on --button-primary/.test(m))).toBe(true);
    expect(found.some((m) => /light mode \(frame\): --foreground is below 4\.5:1 on --app-shell/.test(m))).toBe(true);
  });

  it("warns instead of guessing when a colour cannot be read", () => {
    const report = check(".theme-light { --muted-foreground: color-mix(in oklab, red, blue); }");
    expect(report.ok).toBe(true);
    expect(report.issues.some((i) => i.level === "warning" && /cannot read/.test(i.message))).toBe(true);
  });

  it("explains a folder without a manifest", () => {
    expect(checkThemeFiles([], base)).toEqual({
      ok: false,
      issues: [{ level: "error", message: "manifest.json is missing" }],
      contrast: [],
    });
  });
});

describe("colour parsing", () => {
  it("reads the colour syntaxes themes use", () => {
    expect(parseColor("#fff")).toEqual({ r: 255, g: 255, b: 255, a: 1 });
    expect(parseColor("#00000080")).toMatchObject({ r: 0, a: 128 / 255 });
    expect(parseColor("rgb(31 35 40 / 0.04)")).toEqual({ r: 31, g: 35, b: 40, a: 0.04 });
    expect(parseColor("rgba(255, 0, 0, 0.5)")).toEqual({ r: 255, g: 0, b: 0, a: 0.5 });
    expect(parseColor("hsl(0 100% 50%)")).toEqual({ r: 255, g: 0, b: 0, a: 1 });
    expect(parseColor("oklch(1 0 0)")).toEqual({ r: 255, g: 255, b: 255, a: 1 });
    expect(parseColor("oklch(0% 0 0)")).toEqual({ r: 0, g: 0, b: 0, a: 1 });
    expect(parseColor("color-mix(in oklab, red, blue)")).toBeNull();
  });

  it("follows var() chains and fallbacks", () => {
    const palette = new Map([
      ["--a", "var(--b)"],
      ["--b", "var(--missing, #000)"],
      ["--loop", "var(--loop)"],
    ]);
    expect(resolveColor(palette, "--a")).toEqual({ color: { r: 0, g: 0, b: 0, a: 1 } });
    expect(resolveColor(palette, "--loop")).toEqual({ unresolved: "--loop refers to itself" });
    expect(resolveColor(palette, "--nope")).toEqual({ unresolved: "--nope is not set" });
  });

  it("measures WCAG ratios, compositing a translucent foreground", () => {
    const white = parseColor("#fff")!;
    expect(contrastRatio(parseColor("#000")!, white)).toBeCloseTo(21, 5);
    expect(contrastRatio(parseColor("rgb(0 0 0 / 0)")!, white)).toBeCloseTo(1, 5);
  });
});
