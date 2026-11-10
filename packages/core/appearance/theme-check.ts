import type { ArchiveFile } from "./theme-archive";
import { parseTheme, readThemeFiles, type ThemePackageInput } from "./theme-package";
import { THEME_VARIABLE_NAMES } from "./theme-variables";
import { contrastRatio, resolveColor, type BasePalette } from "./theme-contrast";
import type { Theme } from "./themes";

/**
 * The theme validator behind `pnpm theme:check` and the theme store's
 * registry checks. On top of what import enforces (the manifest and the CSS
 * sanitizer), it reports what import lets through but a theme should not
 * do: set variables that are not public, name generic families in Latin
 * font stacks, or paint text without enough contrast. It reads the theme
 * with its default options.
 */

export interface ThemeCheckIssue {
  level: "error" | "warning";
  message: string;
}

export interface ContrastCheck {
  mode: "light" | "dark";
  /** Where the pair is painted: the page, or the frame (rail, toolbar, tabs). */
  area: "page" | "frame";
  foreground: string;
  background: string;
  ratio: number;
  required: number;
}

export interface ThemeCheckReport {
  ok: boolean;
  theme?: Theme;
  issues: ThemeCheckIssue[];
  /** Every pair that was measured, passing or not. */
  contrast: ContrastCheck[];
}

const TEXT = 4.5;
const NON_TEXT = 3;

/** Surfaces text is painted on; borders and inputs are left out. */
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
];
const READING_SURFACES = ["--background", "--page-canvas", "--surface", "--surface-raised"];
const FRAME_BACKGROUNDS = [
  "--app-shell",
  "--background",
  "--page-canvas",
  "--surface",
  "--surface-raised",
  "--surface-hover",
  "--surface-selected",
  "--muted",
  "--sidebar-accent",
];

const GENERIC_FAMILY =
  /^(?:serif|sans-serif|monospace|cursive|fantasy|system-ui|math|emoji|fangsong|ui-[a-z-]+)$/i;
const LATIN_STACKS = new Set(["font-ui", "font-content-latin"]);
const FRAME_ANCHOR = /data-mc="(?:app-rail|window-toolbar|tab-bar)"/;

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, " "));

const lineAt = (css: string, index: number) => css.slice(0, index).split("\n").length;

interface Rule {
  selectors: string[];
  body: string;
  /** Offset of the selector in the CSS, for line numbers. */
  index: number;
  /** Inside an at-rule or another rule, so it applies only under a condition. */
  nested: boolean;
}

type Mode = "light" | "dark";

/**
 * Where a selector on <html> applies and how specific it is, for the
 * selectors the contrast check follows: `:root` or `html`, the mode
 * classes, and the theme's option attributes, read at their defaults.
 * `"inactive"` when the default options do not match; `null` for any
 * other selector.
 */
function rootSelector(
  selector: string,
  attributes: ReadonlyMap<string, string>,
): { modes: Mode[]; specificity: number } | "inactive" | null {
  const root = /^(:root|html)/.exec(selector)?.[1];
  let specificity = root === ":root" ? 10 : root ? 1 : 0;
  let rest = selector.slice(root?.length ?? 0);
  let mode: Mode | undefined;
  let matches = true;
  const ATTRIBUTE = String.raw`\[data-theme-option-([a-z0-9-]+)(?:="([^"]*)")?\]`;
  while (rest.length > 0) {
    const token =
      /^\.(theme-light|theme-dark|dark)/.exec(rest) ??
      new RegExp(`^${ATTRIBUTE}`).exec(rest) ??
      new RegExp(`^:not\\(${ATTRIBUTE}\\)`).exec(rest);
    if (!token) return null;
    specificity += 10;
    if (token[0].startsWith(".")) {
      const next = token[1] === "theme-light" ? "light" : "dark";
      if (mode && mode !== next) matches = false;
      mode = next;
    } else {
      const value = attributes.get(token[1]!);
      const present = value !== undefined && (token[2] === undefined || token[2] === value);
      if (present === token[0].startsWith(":not")) matches = false;
    }
    rest = rest.slice(token[0].length);
  }
  if (specificity === 0) return null;
  if (!matches) return "inactive";
  return { modes: mode ? [mode] : ["light", "dark"], specificity };
}

/** The `data-theme-option-*` attributes the theme's default options set. */
function optionAttributes(manifest: unknown): Map<string, string> {
  const out = new Map<string, string>();
  for (const option of manifestOptions(manifest)) {
    if (option.type === "toggle" && option.default === true) out.set(String(option.id), "on");
    if (option.type === "select" && typeof option.default === "string") out.set(String(option.id), option.default);
  }
  return out;
}

/** Selectors in a list, split on the commas outside parentheses. */
function splitSelectors(prelude: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < prelude.length; i += 1) {
    if (prelude[i] === "(") depth += 1;
    else if (prelude[i] === ")") depth = Math.max(0, depth - 1);
    else if (prelude[i] === "," && depth === 0) {
      out.push(prelude.slice(start, i).trim());
      start = i + 1;
    }
  }
  out.push(prelude.slice(start).trim());
  return out;
}

/** A palette that records which variables a contrast check read. */
class ReadTracker extends Map<string, string> {
  private readonly read: Set<string>;
  constructor(read: Set<string>, entries: Iterable<readonly [string, string]>) {
    super(entries);
    this.read = read;
  }
  override get(name: string) {
    this.read.add(name);
    return super.get(name);
  }
}

/** Rules that hold declarations, top-level or nested, enough to find variables. */
function rules(css: string): Rule[] {
  const out: Rule[] = [];
  let depth = 0;
  let scanned = 0;
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (; scanned < m.index; scanned += 1) {
      if (css[scanned] === "{") depth += 1;
      else if (css[scanned] === "}") depth = Math.max(0, depth - 1);
    }
    // A statement before the rule (`@import …;`) ends at its semicolon.
    const cut = m[1]!.lastIndexOf(";") + 1;
    const prelude = m[1]!.slice(cut);
    out.push({
      selectors: splitSelectors(prelude),
      body: m[2]!,
      index: m.index + cut + (prelude.length - prelude.trimStart().length),
      nested: depth > 0,
    });
  }
  return out;
}

function declarations(body: string): [string, string][] {
  return [...body.matchAll(/(--[\w-]+)\s*:\s*([^;]+)/g)].map((m) => [m[1]!, m[2]!.trim()]);
}

/** Option ids from the manifest, raw, so the checks run even when it fails to parse. */
function manifestOptions(manifest: unknown): Record<string, unknown>[] {
  return isRecord(manifest) && Array.isArray(manifest.options) ? manifest.options.filter(isRecord) : [];
}

function checkVariables(css: string, manifest: unknown, issues: ThemeCheckIssue[]) {
  const clean = stripComments(css);
  const read = new Set([...clean.matchAll(/var\(\s*(--[\w-]+)/g)].map((m) => m[1]!));
  const options = new Set(manifestOptions(manifest).map((o) => `--${String(o.id)}`));
  const reported = new Set<string>();
  for (const match of clean.matchAll(/(?<![\w-])(--[\w-]+)\s*:/g)) {
    const name = match[1]!;
    if (THEME_VARIABLE_NAMES.has(name.slice(2)) || options.has(name) || read.has(name) || reported.has(name)) {
      continue;
    }
    reported.add(name);
    issues.push({
      level: "error",
      message: `theme.css line ${lineAt(clean, match.index)}: ${name} is not a public variable. Use the variables in docs/theming.md; a variable of the theme's own is fine when the theme reads it with var(${name}).`,
    });
  }
}

function genericFamilies(stack: string): string[] {
  return stack
    .split(",")
    .map((family) => family.trim())
    .filter((family) => GENERIC_FAMILY.test(family));
}

function checkFonts(css: string, manifest: unknown, issues: ThemeCheckIssue[]) {
  const clean = stripComments(css);
  for (const match of clean.matchAll(/(?<![\w-])--(font-ui|font-content-latin)\s*:\s*([^;}]+)/g)) {
    const generic = genericFamilies(match[2]!);
    if (generic.length > 0) {
      issues.push({
        level: "error",
        message: `theme.css line ${lineAt(clean, match.index)}: --${match[1]} names ${generic.join(", ")}. Leave generic families out of Latin font stacks, or Chinese, Japanese and Korean text ignores the person's CJK font.`,
      });
    }
  }
  // In @font-face, font-family names the face being declared; blank those
  // blocks out, keeping the newlines so line numbers still match.
  const rules = clean.replace(/@font-face\s*\{[^}]*\}/gi, (block) => block.replace(/[^\n]/g, " "));
  for (const match of rules.matchAll(/(?<![\w-])font-family\s*:/g)) {
    issues.push({
      level: "warning",
      message: `theme.css line ${lineAt(clean, match.index)}: font-family is set directly. Set --font-ui or --font-content-latin instead, so the Appearance font choices still apply.`,
    });
  }
  for (const option of manifestOptions(manifest)) {
    if (!LATIN_STACKS.has(String(option.id))) continue;
    const stacks = [option.default, ...(Array.isArray(option.choices) ? option.choices.map((c) => (isRecord(c) ? c.value : undefined)) : [])];
    for (const stack of stacks) {
      if (typeof stack !== "string") continue;
      const generic = genericFamilies(stack);
      if (generic.length > 0) {
        issues.push({
          level: "error",
          message: `Option "${String(option.id)}" offers ${JSON.stringify(stack)}, which names ${generic.join(", ")}. Leave generic families out of Latin font stacks.`,
        });
      }
    }
  }
}

/** Variables the default option values set, per mode. */
function optionDefaults(manifest: unknown): { light: [string, string][]; dark: [string, string][] } {
  const out = { light: [] as [string, string][], dark: [] as [string, string][] };
  for (const option of manifestOptions(manifest)) {
    const name = `--${String(option.id)}`;
    const def = option.default;
    if (option.type === "color") {
      const light = typeof def === "string" ? def : isRecord(def) ? def.light : undefined;
      const dark = typeof def === "string" ? def : isRecord(def) ? def.dark : undefined;
      if (typeof light === "string") out.light.push([name, light]);
      if (typeof dark === "string") out.dark.push([name, dark]);
    } else if (option.type === "text" || option.type === "font" || option.type === "variable-select") {
      if (typeof def === "string") {
        out.light.push([name, def]);
        out.dark.push([name, def]);
      }
    }
  }
  return out;
}

function checkContrast(
  css: string,
  manifest: unknown,
  base: BasePalette,
  issues: ThemeCheckIssue[],
): ContrastCheck[] {
  const clean = stripComments(css);
  // The cascade on <html>, per mode: the base tokens (`:root` and `.dark`),
  // then the theme's root and mode rules by specificity and order.
  const cascade = {
    light: new Map([...base.light].map(([name, value]) => [name, { value, specificity: 10 }])),
    dark: new Map([...base.dark].map(([name, value]) => [name, { value, specificity: 10 }])),
  };
  const frame = { light: [] as [string, string][], dark: [] as [string, string][] };
  const unmodelled = new Map<string, string>();
  const attributes = optionAttributes(manifest);
  for (const rule of rules(clean)) {
    for (const selector of rule.selectors) {
      const target = rule.nested ? null : rootSelector(selector, attributes);
      if (target === "inactive") continue;
      if (target) {
        for (const m of target.modes) {
          for (const [name, value] of declarations(rule.body)) {
            const current = cascade[m].get(name);
            if (!current || target.specificity >= current.specificity) {
              cascade[m].set(name, { value, specificity: target.specificity });
            }
          }
        }
      } else if (!/[\s>+~]/.test(selector)) {
        // Sets variables on <html> or <body> in a way the check does not follow.
        for (const [name] of declarations(rule.body)) {
          if (!unmodelled.has(name)) {
            unmodelled.set(name, `theme.css line ${lineAt(clean, rule.index)} sets ${name} under \`${selector}\``);
          }
        }
      }
    }
    const joined = rule.selectors.join(", ");
    const frameMode = /^(?::root|html)?\.theme-(light|dark)\s/.exec(joined)?.[1] as "light" | "dark" | undefined;
    if (!rule.nested && frameMode && FRAME_ANCHOR.test(joined)) frame[frameMode].push(...declarations(rule.body));
  }
  const defaults = optionDefaults(manifest);
  const results: ContrastCheck[] = [];
  const unresolved = new Set<string>();
  const read = new Set<string>();

  for (const m of ["light", "dark"] as const) {
    // Option values are written after the theme as `:root.theme-<mode>`, so they win.
    const palette = new ReadTracker(read, [
      ...[...cascade[m]].map(([name, { value }]) => [name, value] as const),
      ...defaults[m],
    ]);
    const measure = (
      area: ContrastCheck["area"],
      p: Map<string, string>,
      foreground: string,
      backgrounds: readonly string[],
      required: number,
    ) => {
      const fg = resolveColor(p, foreground);
      if ("unresolved" in fg) {
        unresolved.add(`${m}: ${fg.unresolved}`);
        return;
      }
      for (const background of backgrounds) {
        const bg = resolveColor(p, background);
        if ("unresolved" in bg) {
          unresolved.add(`${m}: ${bg.unresolved}`);
          continue;
        }
        if (bg.color.a < 1) {
          unresolved.add(`${m}: ${background} is translucent, so its contrast depends on what is underneath`);
          continue;
        }
        const ratio = contrastRatio(fg.color, bg.color);
        results.push({ mode: m, area, foreground, background, ratio: Math.round(ratio * 100) / 100, required });
      }
    };
    const hasFrame = frame[m].length > 0;
    const backgrounds = hasFrame ? BACKGROUNDS.filter((b) => b !== "--app-shell") : BACKGROUNDS;
    measure("page", palette, "--foreground", backgrounds, TEXT);
    measure("page", palette, "--muted-foreground", backgrounds, TEXT);
    measure("page", palette, "--faint-foreground", backgrounds, NON_TEXT);
    measure("page", palette, "--brand", READING_SURFACES, TEXT);
    if (palette.has("--button-primary")) {
      const label = palette.has("--button-primary-foreground") ? "--button-primary-foreground" : "--primary-foreground";
      measure("page", palette, label, ["--button-primary"], TEXT);
    }
    if (hasFrame) {
      const framePalette = new ReadTracker(read, [...palette, ...frame[m]]);
      for (const tone of ["--foreground", "--muted-foreground", "--sidebar-accent-foreground"]) {
        measure("frame", framePalette, tone, FRAME_BACKGROUNDS, TEXT);
      }
      measure("frame", framePalette, "--faint-foreground", FRAME_BACKGROUNDS, NON_TEXT);
    }
  }

  for (const [name, where] of unmodelled) {
    if (read.has(name)) unresolved.add(`${where}, which the check does not follow`);
  }

  const failing = new Map<string, ContrastCheck[]>();
  for (const check of results) {
    if (check.ratio >= check.required) continue;
    const key = `${check.mode} ${check.area} ${check.foreground}`;
    failing.set(key, [...(failing.get(key) ?? []), check]);
  }
  for (const checks of failing.values()) {
    const { mode: m, area, foreground, required } = checks[0]!;
    const surfaces = checks.map((c) => `${c.background} (${c.ratio.toFixed(2)}:1)`);
    const list = surfaces.length > 1 ? `${surfaces.slice(0, -1).join(", ")} and ${surfaces.at(-1)}` : surfaces[0];
    issues.push({
      level: "error",
      message: `Contrast, ${m} mode${area === "frame" ? " (frame)" : ""}: ${foreground} is below ${required}:1 on ${list}.`,
    });
  }
  for (const reason of unresolved) {
    issues.push({ level: "warning", message: `Contrast not checked, ${reason}.` });
  }
  return results;
}

/** Checks a theme package already split into manifest, CSS and assets. */
export function checkThemePackage(input: ThemePackageInput, base: BasePalette): ThemeCheckReport {
  const issues: ThemeCheckIssue[] = [];
  const parsed = parseTheme(input);
  if (!parsed.ok) issues.push(...parsed.errors.map((message) => ({ level: "error" as const, message })));
  let contrast: ContrastCheck[] = [];
  if (typeof input.css === "string") {
    checkVariables(input.css, input.manifest, issues);
    checkFonts(input.css, input.manifest, issues);
    contrast = checkContrast(input.css, input.manifest, base, issues);
  }
  return {
    ok: !issues.some((issue) => issue.level === "error"),
    theme: parsed.ok ? parsed.theme : undefined,
    issues,
    contrast,
  };
}

/** Checks a theme from its files: a folder's contents or a .zip's. */
export function checkThemeFiles(files: readonly ArchiveFile[], base: BasePalette): ThemeCheckReport {
  const read = readThemeFiles(files);
  if (!read.ok) return { ok: false, issues: [{ level: "error", message: read.error }], contrast: [] };
  return checkThemePackage(read.package, base);
}
