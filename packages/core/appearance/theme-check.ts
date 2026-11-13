import type { ArchiveFile } from "./theme-archive";
import { parseTheme, readThemeFiles, type ThemePackageInput } from "./theme-package";
import { THEME_VARIABLE_NAMES } from "./theme-variables";
import { contrastRatio, paintOver, resolveColor, type BasePalette, type Rgba } from "./theme-contrast";
import {
  THEME_OPTION_ATTR_PREFIX,
  localizedText,
  themeOptionAttributes,
  themeOptionVariables,
  type Theme,
  type ThemeManifest,
  type ThemeOptionValues,
} from "./themes";

/**
 * The theme validator behind `pnpm theme:check` and the theme store's
 * registry checks. On top of what import enforces (the manifest and the CSS
 * sanitizer), it reports what import lets through but a theme should not
 * do: set variables that are not public, name generic families in Latin
 * font stacks, or paint text without enough contrast. Contrast is measured
 * with the options at their defaults, then once per other value of each
 * `select` and `toggle` with the rest at their defaults.
 */

export interface ThemeCheckIssue {
  level: "error" | "warning";
  message: string;
}

/** One option set to a value other than its default. */
export interface ContrastVariant {
  option: string;
  /** The choice's value, or `on` / `off` for a toggle. */
  value: string;
  /** For messages: `Screen = MS-DOS Editor`. */
  label: string;
}

export interface ContrastCheck {
  mode: "light" | "dark";
  /**
   * Where the pair is painted: the page, the frame (rail, toolbar, tabs),
   * a tooltip, or a pressed chip in a display popover.
   */
  area: "page" | "frame" | "tooltip" | "display chip";
  foreground: string;
  background: string;
  ratio: number;
  required: number;
  /** Set when one option was off its default for this measurement. */
  variant?: ContrastVariant;
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
/** Frame surfaces under the text tones. --sidebar-accent and the rail item fills carry their own text colours. */
const FRAME_BACKGROUNDS = [
  "--app-shell",
  "--background",
  "--page-canvas",
  "--surface",
  "--surface-raised",
  "--surface-hover",
  "--surface-selected",
  "--muted",
];

const GENERIC_FAMILY =
  /^(?:serif|sans-serif|monospace|cursive|fantasy|system-ui|math|emoji|fangsong|ui-[a-z-]+)$/i;
const LATIN_STACKS = new Set(["font-ui", "font-content-latin"]);
const FRAME_ANCHOR = /data-mc="(?:app-rail|window-toolbar|tab-bar|bottom-bar)"/;
const SLOT = /^\[data-slot=(["']?)([\w-]+)\1\]$/;
/** Slots portalled to <body> whose own variables the check follows. */
type Slot = "tooltip-content" | "popover-content";
const SLOTS: readonly Slot[] = ["tooltip-content", "popover-content"];

/** How to fix a pair the theme reaches only through a default. */
function contrastHint({ area, foreground, background }: ContrastCheck): string {
  if (area === "tooltip" && foreground === "--foreground") {
    return " Tooltip text is the app's dark-mode --foreground in both modes; set --tooltip-foreground to choose it.";
  }
  if (area === "display chip" && background === "--input") {
    return " Pressed chips fill with --input, the input border colour; set --display-chip-pressed-background to colour them on their own.";
  }
  return "";
}

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
 * classes, and the theme's option attributes as given. `"inactive"` when
 * those attributes do not match; `null` for any other selector.
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

/**
 * The modes a frame rule applies in: a selector on <html> that `rootSelector`
 * follows, then a frame anchor below it. Empty for any other selector.
 */
function frameModes(selector: string, attributes: ReadonlyMap<string, string>): Mode[] {
  const i = firstCombinator(selector);
  if (i < 0 || !FRAME_ANCHOR.test(selector.slice(i))) return [];
  const head = rootSelector(selector.slice(0, i), attributes);
  return head && head !== "inactive" ? head.modes : [];
}

/** Where the first descendant combinator outside brackets and quotes is, or -1. */
function firstCombinator(selector: string): number {
  let depth = 0;
  let quote = "";
  for (let i = 0; i < selector.length; i += 1) {
    const c = selector[i]!;
    if (quote) {
      if (c === quote) quote = "";
    } else if (c === '"' || c === "'") quote = c;
    else if (c === "(" || c === "[") depth += 1;
    else if (c === ")" || c === "]") depth = Math.max(0, depth - 1);
    else if (/\s/.test(c) && depth === 0) return i;
  }
  return -1;
}

/**
 * The slots a rule sets variables on, in which modes, and how specific it
 * is: `[data-slot="x"]`, alone or in an `:is()` / `:where()` list, either
 * bare or below a selector on <html> that `rootSelector` follows.
 * `"inactive"` when that selector does not match; null for any other.
 */
function slotSelector(
  selector: string,
  attributes: ReadonlyMap<string, string>,
): { modes: Mode[]; slots: Slot[]; specificity: number } | "inactive" | null {
  const i = firstCombinator(selector);
  const head = i < 0 ? null : rootSelector(selector.slice(0, i), attributes);
  if (i >= 0 && !head) return null;
  const target = (i < 0 ? selector : selector.slice(i)).trim();
  const list = /^:(is|where)\(([\s\S]*)\)$/.exec(target);
  const args = list ? splitSelectors(list[2]!) : [target];
  const slots = args.map((s) => SLOT.exec(s)?.[2]).filter((s): s is Slot => SLOTS.includes(s as Slot));
  if (slots.length === 0) return null;
  if (head === "inactive") return "inactive";
  // `:is()` counts as its most specific argument and `:where()` as nothing;
  // the arguments here are attribute selectors, as specific as a class.
  const own = list?.[1] === "where" ? 0 : Math.max(...args.map((s) => (s.match(/\[|\.|:(?!:)/g)?.length ?? 0) * 10));
  return { modes: head ? head.modes : ["light", "dark"], slots, specificity: (head ? head.specificity : 0) + own };
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

/**
 * The variables of an element below <html>, as the browser computes them:
 * its own declarations, resolved on the element, and everything else
 * inherited with the value already computed on <html>.
 */
class ScopePalette extends ReadTracker {
  private readonly parent: Map<string, string>;
  constructor(read: Set<string>, parent: Map<string, string>, own: Iterable<readonly [string, string]>) {
    super(read, own);
    this.parent = parent;
  }
  override get(name: string) {
    if (super.has(name)) return super.get(name);
    if (!this.parent.has(name)) return undefined;
    const inherited = resolveColor(this.parent, name);
    if (!("color" in inherited)) return this.parent.get(name);
    const { r, g, b, a } = inherited.color;
    return `rgb(${r} ${g} ${b} / ${a})`;
  }
  override has(name: string) {
    return super.has(name) || this.parent.has(name);
  }
}

/** Declarations by name, the most specific winning and, at equal specificity, the later one. */
type Cascade = Map<string, { value: string; specificity: number }>;

function cascadeSet(cascade: Cascade, entries: Iterable<readonly [string, string]>, specificity: number) {
  for (const [name, value] of entries) {
    const current = cascade.get(name);
    if (!current || specificity >= current.specificity) cascade.set(name, { value, specificity });
  }
}

const cascadeValues = (cascade: Cascade) => [...cascade].map(([name, { value }]) => [name, value] as const);

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

/**
 * Every option value the contrast check tries besides the defaults: each
 * other choice of a `select` and the other state of a `toggle`, one option
 * at a time. Variable options (`color`, `variable-select` and the rest) stay
 * at their defaults.
 */
function optionVariants(theme: ThemeManifest | undefined): { chosen: ThemeOptionValues; variant: ContrastVariant }[] {
  const out: { chosen: ThemeOptionValues; variant: ContrastVariant }[] = [];
  for (const option of theme?.options ?? []) {
    const label = localizedText(option.label, "en");
    if (option.type === "select") {
      for (const choice of option.choices) {
        if (choice.value === option.default) continue;
        out.push({
          chosen: { [option.id]: choice.value },
          variant: { option: option.id, value: choice.value, label: `${label} = ${localizedText(choice.label, "en")}` },
        });
      }
    } else if (option.type === "toggle") {
      const value = option.default ? "off" : "on";
      out.push({ chosen: { [option.id]: !option.default }, variant: { option: option.id, value, label: `${label} = ${value}` } });
    }
  }
  return out;
}

/**
 * Contrast with the options as chosen (the rest at their defaults), through
 * the same helpers the app uses to apply them. Without a parsed theme there
 * are no options to apply.
 */
function measureContrast(
  clean: string,
  parsed: readonly Rule[],
  theme: ThemeManifest | undefined,
  chosen: ThemeOptionValues | undefined,
  base: BasePalette,
  variant: ContrastVariant | undefined,
): { results: ContrastCheck[]; unresolved: Set<string> } {
  // The cascade on <html>, per mode: the base tokens (`:root` and `.dark`),
  // then the theme's root and mode rules by specificity and order.
  const cascade = {
    light: new Map([...base.light].map(([name, value]) => [name, { value, specificity: 10 }])),
    dark: new Map([...base.dark].map(([name, value]) => [name, { value, specificity: 10 }])),
  };
  const frame = { light: [] as [string, string][], dark: [] as [string, string][] };
  // What the theme declares on each slot, per mode. The tooltip starts from
  // `.dark` in tokens.css, which the theme's CSS follows at equal specificity.
  const slotted = (seed: Iterable<readonly [string, string]>) => {
    const out = { light: new Map() as Cascade, dark: new Map() as Cascade };
    cascadeSet(out.light, seed, 10);
    cascadeSet(out.dark, seed, 10);
    return out;
  };
  const slots: Record<Slot, Record<Mode, Cascade>> = {
    "tooltip-content": slotted(base.darkScope),
    "popover-content": slotted([]),
  };
  const unmodelled = new Map<string, string>();
  const attributes = new Map<string, string>();
  for (const [name, value] of Object.entries(theme ? themeOptionAttributes(theme, chosen) : {})) {
    if (value !== null) attributes.set(name.slice(THEME_OPTION_ATTR_PREFIX.length), value);
  }
  for (const rule of parsed) {
    for (const selector of rule.selectors) {
      const target = rule.nested ? null : rootSelector(selector, attributes);
      if (target === "inactive") continue;
      const slot = rule.nested || target ? null : slotSelector(selector, attributes);
      if (slot === "inactive") continue;
      if (target) {
        for (const m of target.modes) cascadeSet(cascade[m], declarations(rule.body), target.specificity);
      } else if (slot) {
        for (const s of slot.slots) {
          for (const m of slot.modes) cascadeSet(slots[s][m], declarations(rule.body), slot.specificity);
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
    if (rule.nested) continue;
    const modes = new Set(rule.selectors.flatMap((selector) => frameModes(selector, attributes)));
    for (const m of modes) frame[m].push(...declarations(rule.body));
  }
  const options = theme ? themeOptionVariables(theme, chosen) : { light: {}, dark: {} };
  const results: ContrastCheck[] = [];
  const unresolved = new Set<string>();
  const read = new Set<string>();

  for (const m of ["light", "dark"] as const) {
    // Option values are written after the theme as `:root.theme-<mode>`, so they win.
    const palette = new ReadTracker(read, [...cascadeValues(cascade[m]), ...Object.entries(options[m])]);
    const measure = (
      area: ContrastCheck["area"],
      p: Map<string, string>,
      foreground: string,
      backgrounds: readonly string[],
      required: number,
      /** Where the backgrounds are resolved, when not where the text is; and what a translucent one sits on. */
      { surface = p, under }: { surface?: Map<string, string>; under?: string } = {},
    ) => {
      const fg = resolveColor(p, foreground);
      if ("unresolved" in fg) {
        unresolved.add(`${m}: ${fg.unresolved}`);
        return;
      }
      for (const background of backgrounds) {
        const bg = resolveColor(surface, background);
        if ("unresolved" in bg) {
          unresolved.add(`${m}: ${bg.unresolved}`);
          continue;
        }
        let fill: Rgba = bg.color;
        if (fill.a < 1 && under) {
          const floor = resolveColor(surface, under);
          if ("color" in floor && floor.color.a === 1) fill = paintOver(fill, floor.color);
        }
        if (fill.a < 1) {
          unresolved.add(`${m}: ${background} is translucent, so its contrast depends on what is underneath`);
          continue;
        }
        const ratio = contrastRatio(fg.color, fill);
        results.push({
          mode: m,
          area,
          foreground,
          background,
          ratio: Math.round(ratio * 100) / 100,
          required,
          ...(variant ? { variant } : {}),
        });
      }
    };
    // Rail items read their own variables, with the fallbacks in theme-variables.css:
    // the label on the shell at rest, and the active colour on the active and hover fills.
    const measureRail = (area: ContrastCheck["area"], p: Map<string, string>) => {
      if (p.has("--rail-item-color")) measure(area, p, "--rail-item-color", ["--app-shell"], TEXT);
      const railText = p.has("--rail-item-active-color") ? "--rail-item-active-color" : "--sidebar-accent-foreground";
      const railSurfaces = [p.has("--rail-item-active-background") ? "--rail-item-active-background" : "--sidebar-accent"];
      if (p.has("--rail-item-hover-background")) railSurfaces.push("--rail-item-hover-background");
      else {
        // The default hover is --sidebar-accent at 60% over the shell.
        const accent = resolveColor(p, "--sidebar-accent");
        const shell = resolveColor(p, "--app-shell");
        if ("color" in accent && "color" in shell && shell.color.a === 1) {
          const { r, g, b } = paintOver({ ...accent.color, a: accent.color.a * 0.6 }, shell.color);
          p.set("--rail-item-hover-background", `rgb(${r} ${g} ${b})`);
          railSurfaces.push("--rail-item-hover-background");
        }
      }
      // The frame measures --sidebar-accent-foreground on --sidebar-accent for its other controls.
      measure(
        area,
        p,
        railText,
        railSurfaces.filter(
          (surface) => area !== "frame" || railText !== "--sidebar-accent-foreground" || surface !== "--sidebar-accent",
        ),
        TEXT,
      );
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
      for (const tone of ["--foreground", "--muted-foreground"]) {
        measure("frame", framePalette, tone, FRAME_BACKGROUNDS, TEXT);
      }
      measure("frame", framePalette, "--faint-foreground", FRAME_BACKGROUNDS, NON_TEXT);
      // Toolbar and layout buttons paint --sidebar-accent-foreground on --sidebar-accent on hover.
      measure("frame", framePalette, "--sidebar-accent-foreground", ["--sidebar-accent"], TEXT);
      measureRail("frame", framePalette);
    } else {
      // Without frame rules the rail sits on the page's own colours.
      measureRail("page", new ReadTracker(read, palette));
    }

    // The tooltip is a `.dark` scope in both modes: its text takes the app's
    // dark tokens, and the theme's values only through rules on the tooltip or
    // --tooltip-foreground. Its fill is resolved outside, in the page's mode.
    const tooltip = new ScopePalette(read, palette, cascadeValues(slots["tooltip-content"][m]));
    const tipText = tooltip.has("--tooltip-foreground") ? "--tooltip-foreground" : "--foreground";
    measure("tooltip", tooltip, tipText, ["--tooltip"], TEXT, { surface: palette });

    // Display chips sit in popovers; pressed, they fill with
    // --display-chip-pressed-background, which defaults to --input (translucent
    // in dark mode, over the popover's --surface-raised).
    const popover = new ScopePalette(read, palette, cascadeValues(slots["popover-content"][m]));
    const chipText = popover.has("--display-chip-pressed-color") ? "--display-chip-pressed-color" : "--foreground";
    const chipFill = popover.has("--display-chip-pressed-background") ? "--display-chip-pressed-background" : "--input";
    measure("display chip", popover, chipText, [chipFill], TEXT, { under: "--surface-raised" });
  }

  for (const [name, where] of unmodelled) {
    if (read.has(name)) unresolved.add(`${where}, which the check does not follow`);
  }
  return { results, unresolved };
}

const pairKey = (c: ContrastCheck) => `${c.mode} ${c.area} ${c.foreground} ${c.background}`;

/** One error per mode, area and colour, listing the surfaces it fails on. */
function reportFailing(checks: readonly ContrastCheck[], issues: ThemeCheckIssue[]) {
  const failing = new Map<string, ContrastCheck[]>();
  for (const check of checks) {
    if (check.ratio >= check.required) continue;
    const key = `${check.mode} ${check.area} ${check.foreground}`;
    failing.set(key, [...(failing.get(key) ?? []), check]);
  }
  for (const group of failing.values()) {
    const { mode: m, area, foreground, required, variant } = group[0]!;
    const surfaces = group.map((c) => `${c.background} (${c.ratio.toFixed(2)}:1)`);
    const list = surfaces.length > 1 ? `${surfaces.slice(0, -1).join(", ")} and ${surfaces.at(-1)}` : surfaces[0];
    const where = [...(area === "page" ? [] : [area]), ...(variant ? [variant.label] : [])];
    issues.push({
      level: "error",
      message: `Contrast, ${m} mode${where.length > 0 ? ` (${where.join(", ")})` : ""}: ${foreground} is below ${required}:1 on ${list}.${contrastHint(group[0]!)}`,
    });
  }
}

/**
 * Contrast at the default options, then once per other option value. An
 * option value reports only what does not already fail at the defaults.
 */
function checkContrast(
  css: string,
  theme: ThemeManifest | undefined,
  base: BasePalette,
  issues: ThemeCheckIssue[],
): ContrastCheck[] {
  const clean = stripComments(css);
  const parsed = rules(clean);
  const defaults = measureContrast(clean, parsed, theme, undefined, base, undefined);
  reportFailing(defaults.results, issues);
  const failingByDefault = new Set(defaults.results.filter((c) => c.ratio < c.required).map(pairKey));
  const results = [...defaults.results];
  const unresolvedWith = new Map<string, string[]>();
  for (const { chosen, variant } of optionVariants(theme)) {
    const run = measureContrast(clean, parsed, theme, chosen, base, variant);
    results.push(...run.results);
    reportFailing(run.results.filter((c) => !failingByDefault.has(pairKey(c))), issues);
    for (const reason of run.unresolved) {
      if (!defaults.unresolved.has(reason)) unresolvedWith.set(reason, [...(unresolvedWith.get(reason) ?? []), variant.label]);
    }
  }
  for (const reason of defaults.unresolved) {
    issues.push({ level: "warning", message: `Contrast not checked, ${reason}.` });
  }
  for (const [reason, labels] of unresolvedWith) {
    issues.push({ level: "warning", message: `Contrast not checked (${labels.join(", ")}), ${reason}.` });
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
    const theme = parsed.ok ? parsed.theme : undefined;
    if (!theme && manifestOptions(input.manifest).length > 0) {
      issues.push({
        level: "warning",
        message: "Contrast was checked without the theme's options, because the theme did not pass the checks above.",
      });
    }
    contrast = checkContrast(input.css, theme, base, issues);
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
