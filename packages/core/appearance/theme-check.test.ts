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
      expect(report.issues).toEqual([]);
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

  it("reads :root and mode rules in the order the browser applies them", () => {
    const white = "--foreground: #ffffff; --page-canvas: #ffffff;";
    expect(errors(`:root { ${white} }`).some((m) => /light mode: --foreground is below .*--page-canvas \(1\.00:1\)/.test(m))).toBe(true);
    expect(errors(`:root.dark { ${white} }`).some((m) => /^Contrast, dark mode: --foreground/.test(m))).toBe(true);
    // An element selector is less specific than the base tokens on :root.
    expect(check(`html { ${white} }`).ok).toBe(true);
    // At equal specificity the later rule wins (and :root reaches dark mode too).
    const later = errors(`.theme-light { ${white} }\n:root { --foreground: #111111; }`);
    expect(later.length).toBeGreaterThan(0);
    expect(later.every((m) => m.startsWith("Contrast, dark mode"))).toBe(true);
  });

  it("applies option values over the theme's CSS, as the app does", () => {
    const found = errors(".theme-light { --foreground: #000000; --page-canvas: #ffffff; }", {
      options: [{ id: "foreground", type: "color", label: "Text", default: "#ffffff" }],
    });
    expect(found.some((m) => /light mode: --foreground is below .*--page-canvas \(1\.00:1\)/.test(m))).toBe(true);
  });

  it("measures option rules at the options' defaults", () => {
    const options = [
      { id: "ink", type: "select", label: "Ink", default: "dark", choices: [{ value: "dark", label: "Dark" }, { value: "pale", label: "Pale" }] },
      { id: "wash", type: "toggle", label: "Wash", default: true },
    ];
    // Only applies when the person picks "pale", so the default check skips it.
    expect(check(':root[data-theme-option-ink="pale"] { --foreground: #ffffff; }', { options }).issues).toEqual([]);
    expect(check(":root[data-theme-option-ink]:not([data-theme-option-ink=\"dark\"]) { --foreground: #ffffff; }", { options }).issues).toEqual([]);
    // The toggle is on by default, so this one applies.
    expect(errors(":root[data-theme-option-wash].theme-light { --page-canvas: #444444; }", { options }).length).toBeGreaterThan(0);
  });

  it("fills in a missing default the way import does: the first choice", () => {
    const light = ".theme-light { --foreground: #000000; --page-canvas: #ffffff; }";
    const pale = [{ value: "pale", label: "Pale" }, { value: "dark", label: "Dark" }];
    const select = errors(`${light}\n:root[data-theme-option-ink="pale"] { --foreground: #ffffff; }`, {
      options: [{ id: "ink", type: "select", label: "Ink", choices: pale }],
    });
    expect(select.some((m) => /light mode: --foreground is below .*--page-canvas \(1\.00:1\)/.test(m))).toBe(true);
    const variable = errors(light, {
      options: [{ id: "foreground", type: "variable-select", label: "Ink", choices: [
        { value: "#ffffff", label: "Pale" },
        { value: "#000000", label: "Dark" },
      ] }],
    });
    expect(variable.some((m) => /light mode: --foreground is below .*--page-canvas \(1\.00:1\)/.test(m))).toBe(true);
  });

  it("says when it could not apply the options", () => {
    const report = check(".theme-light { --radius: 0; }", {
      id: "Bad",
      options: [{ id: "wash", type: "toggle", label: "Wash", default: true }],
    });
    expect(report.issues).toContainEqual({
      level: "warning",
      message: "Contrast was checked without the theme's options, because the theme did not pass the checks above.",
    });
  });

  it("keeps a selector list inside :is() together", () => {
    const report = check('.theme-light :is([data-mc="app-rail"], [data-mc="tab-bar"]) { --foreground: #ffffff; --app-shell: #000000; }');
    expect(report.issues.filter((i) => i.level === "warning")).toEqual([]);
    expect(report.contrast.some((c) => c.area === "frame")).toBe(true);
  });

  it("says what it did not measure instead of passing it", () => {
    const report = check(
      [
        "@media (prefers-contrast: more) { .theme-light { --foreground: #ffffff; } }",
        "body { --page-canvas: #ffffff; }",
        '.theme-light [data-mc="chat-window"] { --background: #ffffff; }',
      ].join("\n"),
    );
    const warnings = report.issues.filter((i) => i.level === "warning").map((i) => i.message);
    expect(warnings).toEqual(
      expect.arrayContaining([
        expect.stringMatching(/line 1 sets --foreground under `\.theme-light`, which the check does not follow/),
        expect.stringMatching(/line 2 sets --page-canvas under `body`/),
      ]),
    );
    expect(warnings.some((m) => m.includes("chat-window"))).toBe(false);
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

  it("measures tooltips with the dark-mode text they always use", () => {
    const pale = ".theme-light { --tooltip: #f4f4f4; }";
    expect(errors(pale)).toEqual([
      expect.stringMatching(/^Contrast, light mode \(tooltip\): --foreground is below 4\.5:1 on --tooltip \(1\.\d\d:1\)\. .*--tooltip-foreground/),
    ]);
    // MS-DOS 1.0.0: a grey tooltip under the app's near-white dark-mode text.
    expect(errors(".theme-dark { --tooltip: #aaaaaa; }")).toEqual([
      expect.stringMatching(/^Contrast, dark mode \(tooltip\): --foreground is below 4\.5:1 on --tooltip/),
    ]);
    expect(check(`${pale}\n.theme-light { --tooltip-foreground: #111111; }`)).toMatchObject({ ok: true, issues: [] });
    expect(check(`${pale}\n.theme-light [data-slot="tooltip-content"] { --foreground: #111111; }`)).toMatchObject({
      ok: true,
      issues: [],
    });
    const tip = check(`${pale}\n[data-slot='tooltip-content'] { --tooltip-foreground: #111111; }`).contrast.filter(
      (c) => c.area === "tooltip",
    );
    expect(tip.map((c) => [c.mode, c.foreground, c.background])).toEqual([
      ["light", "--tooltip-foreground", "--tooltip"],
      ["dark", "--tooltip-foreground", "--tooltip"],
    ]);
  });

  it("measures pressed display chips in the popover they sit in", () => {
    expect(errors(".theme-light { --input: #111111; }")).toEqual([
      expect.stringMatching(
        /^Contrast, light mode \(display chip\): --foreground is below 4\.5:1 on --input \(1\.\d\d:1\)\. .*--display-chip-pressed-background/,
      ),
    ]);
    expect(errors('.theme-dark [data-slot="popover-content"] { --input: oklch(0.985 0 0); }')).toEqual([
      expect.stringMatching(/^Contrast, dark mode \(display chip\): --foreground is below 4\.5:1 on --input \(1\.00:1\)/),
    ]);
    const split = ".theme-light { --input: #111111; --display-chip-pressed-background: #e6e6e6; }";
    expect(check(split)).toMatchObject({ ok: true, issues: [] });
    expect(errors(`${split}\n.theme-light { --display-chip-pressed-color: #ffffff; }`)).toEqual([
      expect.stringMatching(/^Contrast, light mode \(display chip\): --display-chip-pressed-color is below 4\.5:1 on --display-chip-pressed-background/),
    ]);
    // Dark mode's default --input is translucent; it is measured over the popover.
    const chips = check("").contrast.filter((c) => c.area === "display chip");
    expect(chips.map((c) => c.mode)).toEqual(["light", "dark"]);
    expect(chips.every((c) => c.ratio >= c.required)).toBe(true);
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
