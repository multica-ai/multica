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
      // An option value may set a colour the check cannot read (Pop's signal colours mix in color-mix()).
      expect(report.issues.filter((i) => !i.message.startsWith("Contrast not checked ("))).toEqual([]);
      expect(report.issues.every((i) => i.level === "warning")).toBe(true);
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

  describe("option values", () => {
    const options = [
      {
        id: "ink",
        type: "select",
        label: { en: "Ink", "zh-Hans": "墨色" },
        default: "dark",
        choices: [
          { value: "dark", label: "Dark" },
          { value: "pale", label: { en: "Pale", "zh-Hans": "浅" } },
          { value: "soft", label: "Soft" },
        ],
      },
      { id: "wash", type: "toggle", label: "Wash", default: true },
    ];

    it("measures each other choice of a select, naming it", () => {
      for (const css of [
        ':root[data-theme-option-ink="pale"] { --foreground: #ffffff; }',
        ':root[data-theme-option-ink]:not([data-theme-option-ink="dark"]):not([data-theme-option-ink="soft"]) { --foreground: #ffffff; }',
      ]) {
        expect(errors(css, { options })).toEqual([
          expect.stringMatching(/^Contrast, light mode \(Ink = Pale\): --foreground is below 4\.5:1 on .*--page-canvas \(1\.0\d:1\)/),
          expect.stringMatching(/^Contrast, light mode \(display chip, Ink = Pale\): --foreground is below 4\.5:1 on --input/),
        ]);
      }
      const report = check(':root[data-theme-option-ink="soft"].theme-light { --muted-foreground: #bbbbbb; }', { options });
      expect(report.issues.map((i) => i.message)).toEqual([
        expect.stringMatching(/^Contrast, light mode \(Ink = Soft\): --muted-foreground is below 4\.5:1/),
      ]);
      expect(report.contrast.filter((c) => c.ratio < c.required).every((c) => c.variant?.option === "ink" && c.variant.value === "soft")).toBe(true);
    });

    it("measures the other state of a toggle", () => {
      const off = errors(":root:not([data-theme-option-wash]).theme-light { --page-canvas: #444444; }", { options });
      expect(off.length).toBeGreaterThan(0);
      expect(off.every((m) => m.startsWith("Contrast, light mode (Wash = off): "))).toBe(true);
      const on = errors(":root[data-theme-option-wash].theme-light { --page-canvas: #444444; }", {
        options: [{ id: "wash", type: "toggle", label: "Wash", default: false }],
      });
      expect(on.length).toBeGreaterThan(0);
      expect(on.every((m) => m.startsWith("Contrast, light mode (Wash = on): "))).toBe(true);
    });

    it("reports a failure at the defaults once, not again for every option value", () => {
      const found = errors(".theme-light { --muted-foreground: #bbbbbb; }", { options });
      expect(found).toEqual([expect.stringMatching(/^Contrast, light mode: --muted-foreground/)]);
    });

    it("changes one option at a time", () => {
      // Fails only with ink "pale" and wash off together.
      const css = ':root[data-theme-option-ink="pale"]:not([data-theme-option-wash]) { --foreground: #ffffff; }';
      expect(check(css, { options }).issues).toEqual([]);
    });

    it("keeps variable options at their defaults", () => {
      const found = errors("", {
        options: [{ id: "muted-foreground", type: "variable-select", label: "Muted", default: "var(--foreground)", choices: [
          { value: "var(--foreground)", label: "Strong" },
          { value: "#888888", label: "Grey" },
        ] }],
      });
      expect(found).toEqual([]);
    });

    it("follows frame rules behind an option", () => {
      const frame = [
        { id: "frame", type: "select", label: "Frame", default: "paper", choices: [{ value: "paper", label: "Paper" }, { value: "ink", label: "Ink" }] },
      ];
      const ink = ':root.theme-light[data-theme-option-frame="ink"]';
      const rail = '[data-mc="app-rail"]';
      // Without the frame rule, the ink shell would fail against the page's dark text.
      const surfaces = ["background", "page-canvas", "surface", "surface-raised", "surface-hover", "surface-selected", "muted"]
        .map((name) => `--${name}: #222222;`)
        .join(" ");
      const good = `${ink} { --app-shell: #111111; }\n${ink} ${rail} { ${surfaces} --foreground: #f5f5f5; --muted-foreground: #cccccc; --faint-foreground: #999999; --sidebar-accent: #333333; --sidebar-accent-foreground: #ffffff; }`;
      expect(check(good, { options: frame }).issues).toEqual([]);
      const bad = good.replace("--sidebar-accent-foreground: #ffffff", "--sidebar-accent-foreground: #444444");
      expect(errors(bad, { options: frame })).toEqual([
        expect.stringMatching(
          /^Contrast, light mode \(frame, Frame = Ink\): --sidebar-accent-foreground is below 4\.5:1 on --sidebar-accent \(1\.\d\d:1\) and --rail-item-hover-background/,
        ),
      ]);
    });

    it("names the option values whose colours it could not read", () => {
      const report = check(':root[data-theme-option-ink="pale"] { --surface: color-mix(in oklab, red, blue); }', { options });
      expect(report.ok).toBe(true);
      const warnings = report.issues.map((i) => i.message);
      expect(warnings.every((m) => m.startsWith("Contrast not checked (Ink = Pale), "))).toBe(true);
      expect(warnings).toEqual(
        expect.arrayContaining([
          expect.stringMatching(/, light: --surface is `color-mix/),
          expect.stringMatching(/, dark: --surface is `color-mix/),
        ]),
      );
    });
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

  it("measures label text on the colour slots the theme declares", () => {
    expect(errors(".theme-light { --palette-blue: #bfdbfe; }")).toEqual([
      expect.stringMatching(
        /^Contrast, light mode \(label\): --label-chip-color is below 4\.5:1 on --palette-blue \(1\.\d\d:1\)\. Without --label-chip-color/,
      ),
    ]);
    expect(check(".theme-light { --palette-blue: #bfdbfe; --label-chip-color: #111827; }")).toMatchObject({ ok: true, issues: [] });
    expect(errors(".theme-dark { --palette-yellow: #fde68a; --palette-red: #fecaca; --label-chip-color: #ffffff; }")).toEqual([
      expect.stringMatching(/^Contrast, dark mode \(label\): --label-chip-color is below 4\.5:1 on --palette-red \(.*\) and --palette-yellow \([\d.]+:1\)\.$/),
    ]);
    expect(check(".theme-light { --radius: 0; }").contrast.some((c) => c.area === "label")).toBe(false);
  });

  it("measures the worse of the dark and light label text a slot can get", () => {
    // A label picked as #fecaca falls in the red slot with dark text, which a
    // dark red slot cannot carry, although the red preset itself takes light text.
    const checked = check(".theme-light { --palette-red: #7f1d1d; }");
    expect(checked.contrast.filter((c) => c.area === "label")).toEqual([
      expect.objectContaining({ background: "--palette-red", ratio: 1.77, defaulted: true }),
    ]);
    expect(errors(".theme-light { --palette-red: #7f1d1d; }")).toEqual([
      expect.stringMatching(/^Contrast, light mode \(label\): --label-chip-color is below 4\.5:1 on --palette-red \(1\.77:1\)\./),
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
    // A variable set on <html> is inherited as computed there, not re-read in the popover.
    expect(
      errors(
        [
          ".theme-light { --display-chip-pressed-color: var(--foreground); --display-chip-pressed-background: #111111; }",
          '.theme-light [data-slot="popover-content"] { --foreground: #ffffff; }',
        ].join("\n"),
      ),
    ).toEqual([expect.stringMatching(/^Contrast, light mode \(display chip\): --display-chip-pressed-color is below 4\.5:1 .*\(1\.\d\d:1\)/)]);
    // The more specific slot rule wins over a later, less specific one.
    expect(
      errors(
        [
          '.theme-light [data-slot="popover-content"] { --display-chip-pressed-background: #ffffff; --display-chip-pressed-color: #ffffff; }',
          '[data-slot="popover-content"] { --display-chip-pressed-background: #111111; --display-chip-pressed-color: #ffffff; }',
        ].join("\n"),
      ),
    ).toEqual([expect.stringMatching(/^Contrast, light mode \(display chip\): .*\(1\.00:1\)/)]);
    // Slot rules follow option attributes like rules on <html>.
    const options = [{ id: "ink", type: "select", label: "Ink", default: "dark", choices: [{ value: "dark", label: "Dark" }, { value: "pale", label: "Pale" }] }];
    expect(errors(':root[data-theme-option-ink="pale"] [data-slot="popover-content"] { --input: #111111; }', { options })).toEqual([
      expect.stringMatching(/^Contrast, light mode \(display chip, Ink = Pale\): --foreground is below 4\.5:1 on --input/),
    ]);
    // Dark mode's default --input is translucent; it is measured over the popover.
    const chips = check("").contrast.filter((c) => c.area === "display chip");
    expect(chips.map((c) => c.mode)).toEqual(["light", "dark"]);
    expect(chips.every((c) => c.ratio >= c.required)).toBe(true);
  });

  describe("rail items", () => {
    const rail = (declarations: string) => `.theme-light [data-mc="app-rail"] { ${declarations} }`;
    const fills = "--rail-item-hover-background: #000000; --rail-item-active-background: #000000;";

    it("pairs the rail's own colours, as the rail paints them", () => {
      expect(check(rail(`${fills} --rail-item-active-color: #ffffff;`)).issues).toEqual([]);
    });

    it("fails rail colours without enough contrast", () => {
      expect(errors(rail(`${fills} --rail-item-active-color: #333333;`))).toEqual([
        expect.stringMatching(
          /^Contrast, light mode \(frame\): --rail-item-active-color is below 4\.5:1 on --rail-item-active-background \(1\.\d\d:1\) and --rail-item-hover-background \(1\.\d\d:1\)\.$/,
        ),
      ]);
      // Without its own colour, the rail falls back to --sidebar-accent-foreground on the rail fills.
      expect(errors(rail(fills))).toEqual([
        expect.stringMatching(/^Contrast, light mode \(frame\): --sidebar-accent-foreground is below 4\.5:1 on --rail-item-active-background/),
      ]);
      expect(errors(rail("--rail-item-color: #dddddd;"))).toEqual([
        expect.stringMatching(/^Contrast, light mode \(frame\): --rail-item-color is below 4\.5:1 on --app-shell/),
      ]);
    });

    it("keeps --sidebar-accent-foreground on --sidebar-accent for the other frame controls", () => {
      expect(errors(rail(`${fills} --rail-item-active-color: #ffffff; --sidebar-accent: #000000;`))).toEqual([
        expect.stringMatching(/^Contrast, light mode \(frame\): --sidebar-accent-foreground is below 4\.5:1 on --sidebar-accent \(1\.\d\d:1\)\.$/),
      ]);
    });

    it("measures rail colours behind a non-default option value", () => {
      const options = [
        { id: "frame", type: "select", label: "Frame", default: "paper", choices: [{ value: "paper", label: "Paper" }, { value: "ink", label: "Ink" }] },
      ];
      const ink = (color: string) =>
        `:root.theme-light[data-theme-option-frame="ink"] [data-mc="app-rail"] { ${fills} --rail-item-active-color: ${color}; }`;
      expect(check(ink("#ffffff"), { options }).issues).toEqual([]);
      expect(errors(ink("#333333"), { options })).toEqual([
        expect.stringMatching(/^Contrast, light mode \(frame, Frame = Ink\): --rail-item-active-color is below 4\.5:1/),
      ]);
    });

    describe("without frame rules", () => {
      // MS-DOS 1.2.0, light: white on a blue active item, but the default hover is a pale --sidebar-accent wash.
      const dos = ".theme-light { --rail-item-active-background: #0000aa; --rail-item-active-color: #ffffff; }";

      it("measures the rail on the page's colours", () => {
        expect(errors(dos)).toEqual([
          expect.stringMatching(/^Contrast, light mode: --rail-item-active-color is below 4\.5:1 on --rail-item-hover-background \(1\.\d\d:1\)\.$/),
        ]);
        expect(check(`${dos}\n.theme-light { --rail-item-hover-background: #0000aa; }`)).toMatchObject({ ok: true, issues: [] });
        const rail = check(dos).contrast.filter((c) => c.mode === "light" && c.foreground === "--rail-item-active-color");
        expect(rail.map((c) => [c.area, c.background])).toEqual([
          ["page", "--rail-item-active-background"],
          ["page", "--rail-item-hover-background"],
        ]);
      });

      it("falls back to --sidebar-accent-foreground on --sidebar-accent", () => {
        expect(errors(".theme-dark { --sidebar-accent-foreground: #333333; }")).toEqual([
          expect.stringMatching(
            /^Contrast, dark mode: --sidebar-accent-foreground is below 4\.5:1 on --sidebar-accent \(1\.\d\d:1\) and --rail-item-hover-background \(1\.\d\d:1\)\.$/,
          ),
        ]);
        expect(errors(".theme-light { --rail-item-color: #dddddd; }")).toEqual([
          expect.stringMatching(/^Contrast, light mode: --rail-item-color is below 4\.5:1 on --app-shell/),
        ]);
      });
    });
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

describe("theme check with a backdrop", () => {
  const assets = { "sky.svg": "data:image/svg+xml;base64,PHN2Zy8+" };
  const checkBackdrop = (css: string, extra: Record<string, unknown> = {}) =>
    checkThemePackage({ manifest: { ...manifest, ...extra }, css, assets }, base);
  const pair = (report: ReturnType<typeof checkBackdrop>, mode: string, foreground: string, background: string) =>
    report.contrast.find((c) => c.mode === mode && c.foreground === foreground && c.background === background && !c.variant);

  it("requires --backdrop-color, so the glass is never left unmeasured", () => {
    const report = checkBackdrop(".theme-dark { --backdrop: url(sky.svg) center / cover; --glass-opacity: 70%; }");
    expect(report.ok).toBe(false);
    expect(report.issues).toContainEqual({
      level: "error",
      message: expect.stringMatching(/^Backdrop, dark mode: --backdrop is set without --backdrop-color\./),
    });
    expect(
      checkBackdrop(".theme-dark { --backdrop: url(sky.svg); --backdrop-color: color-mix(in oklab, red, blue); }").issues,
    ).toContainEqual({ level: "error", message: expect.stringMatching(/--backdrop-color must be an opaque colour/) });
  });

  it("measures the frame and the page as glass over --backdrop-color", () => {
    const report = checkBackdrop(
      ".theme-light { --backdrop: url(sky.svg); --backdrop-color: #000000; --app-shell: #ffffff; --page-canvas: #ffffff; --glass-opacity: 50%; }",
    );
    const frame = { r: 128, g: 128, b: 128, a: 1 };
    const page = { r: 192, g: 192, b: 192, a: 1 };
    const foreground = resolveColor(base.light, "--foreground");
    if (!("color" in foreground)) throw new Error("tokens.css has no light --foreground");
    expect(pair(report, "light", "--foreground", "--app-shell")).toMatchObject({
      glass: true,
      ratio: Math.round(contrastRatio(foreground.color, frame) * 100) / 100,
    });
    expect(pair(report, "light", "--foreground", "--page-canvas")).toMatchObject({
      glass: true,
      ratio: Math.round(contrastRatio(foreground.color, page) * 100) / 100,
    });
    expect(pair(report, "light", "--foreground", "--surface")?.glass).toBeUndefined();
    expect(report.issues.filter((i) => i.message.startsWith("Contrast not checked"))).toEqual([]);
  });

  it("keeps the opaque colour for variables on <html> that read a glass surface", () => {
    const report = checkBackdrop(`.theme-light {
  --backdrop: #ffffff;
  --backdrop-color: #ffffff;
  --app-shell: #000000;
  --frame-glass-opacity: 0%;
  --glass-opacity: 90%;
  --button-primary: #000000;
  --button-primary-foreground: var(--app-shell);
}`);
    expect(report.ok).toBe(false);
    expect(pair(report, "light", "--button-primary-foreground", "--button-primary")).toMatchObject({ ratio: 1 });
    expect(report.issues).toEqual([
      { level: "error", message: "Contrast, light mode: --button-primary-foreground is below 4.5:1 on --button-primary (1.00:1)." },
    ]);
  });

  it("measures an alias of a glass surface, and portals, on the opaque colour", () => {
    const report = checkBackdrop(
      ".theme-light { --backdrop: url(sky.svg); --backdrop-color: #000000; --page-canvas: #ffffff; --background: #ffffff; --surface: var(--page-canvas); --tooltip: var(--background); --tooltip-foreground: #000000; --glass-opacity: 50%; }",
    );
    const foreground = resolveColor(base.light, "--foreground");
    if (!("color" in foreground)) throw new Error("tokens.css has no light --foreground");
    const white = { r: 255, g: 255, b: 255, a: 1 };
    const opaque = Math.round(contrastRatio(foreground.color, white) * 100) / 100;
    expect(pair(report, "light", "--foreground", "--surface")).toMatchObject({ ratio: opaque });
    expect(pair(report, "light", "--foreground", "--surface")?.glass).toBeUndefined();
    expect(pair(report, "light", "--foreground", "--page-canvas")?.ratio).toBeLessThan(opaque);
    const tooltip = report.contrast.find((c) => c.mode === "light" && c.area === "tooltip" && !c.variant);
    expect(tooltip?.glass).toBeUndefined();
    expect(tooltip).toMatchObject({ foreground: "--tooltip-foreground", ratio: 21 });
  });

  it("names the glass when text fails on it", () => {
    const found = checkBackdrop(".theme-light { --backdrop: url(sky.svg); --backdrop-color: #101010; --frame-glass-opacity: 0%; }")
      .issues.filter((i) => i.level === "error")
      .map((i) => i.message);
    expect(found).toContainEqual(
      expect.stringMatching(/^Contrast, light mode: --foreground is below 4\.5:1 on --app-shell \(1\.\d\d:1\)\. With the backdrop, it is measured as glass/),
    );
  });

  it("reads the glass opacity from an option", () => {
    const report = checkBackdrop(
      ".theme-dark { --backdrop: url(sky.svg) center / cover; --backdrop-color: #070b1d; --glass-opacity: var(--page-opacity); --frame-glass-opacity: 0%; }",
      { options: [{ id: "page-opacity", type: "slider", label: "Page opacity", default: 80, min: 40, max: 100, unit: "%" }] },
    );
    expect(report.issues).toEqual([]);
    expect(pair(report, "dark", "--foreground", "--page-canvas")?.glass).toBe(true);
  });

  it("warns when solid glass hides the backdrop", () => {
    const report = checkBackdrop(".theme-dark { --backdrop: url(sky.svg); --backdrop-color: #070b1d; }");
    expect(report.issues).toEqual([
      { level: "warning", message: expect.stringMatching(/^Backdrop, dark mode: .* cover the backdrop completely\.$/) },
    ]);
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
