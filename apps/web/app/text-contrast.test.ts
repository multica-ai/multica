import { readFileSync, readdirSync, statSync } from "node:fs";
import { resolve, join, relative } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Is every tone the product paints text and marks with actually legible, and is
 * hierarchy still expressed with tokens rather than with transparency?
 *
 * The two questions are one contract, because the answer to the first used to
 * depend on the second. `text-muted-foreground/70` reads as "muted, a bit
 * quieter"; on a light surface it is 2.69:1, which is not quieter, it is gone.
 * 152 call sites spread across /30 to /80 and every one failed AA in light
 * mode — not because anyone chose an illegible grey, but because the palette
 * stopped at --muted-foreground and transparency was the only way left to say
 * "quieter".
 *
 * So the palette got one more step, and it is deliberately a NON-TEXT step.
 * --faint-foreground clears 3:1 (WCAG 1.4.11, icons and glyphs) but not 4.5:1,
 * because there is no room for a third readable text tone: AA caps a lighter
 * text tone at L 0.523 on our darkest light surface and muted already sits at
 * 0.505. A tier 0.018 apart is a tier nobody can see, so text keeps exactly one
 * floor and it is --muted-foreground.
 *
 * Assertions recompute contrast from the token files rather than hard-coding
 * ratios, so editing a token value is what fails the test — the only way a
 * colour guard stays true. Ratios are WCAG 2.x relative luminance.
 */

const repoRoot = resolve(process.cwd(), "../..");

/** WCAG 1.4.3 for body and label text; 1.4.11 for icons and other marks. */
const WCAG_AA_NORMAL_TEXT = 4.5;
const WCAG_AA_NON_TEXT = 3.0;

/**
 * Surfaces that product text and marks are painted on. Border/input/ring tokens
 * are excluded: they are not text backgrounds, and several carry alpha, which
 * has no single contrast answer without knowing what is underneath.
 */
const backgroundTokens = [
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

type Rgb = [number, number, number];

function readBlock(source: string, selector: string): Map<string, string> {
  const start = source.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`${selector} block not found`);
  const end = source.indexOf("\n}", start);
  if (end < 0) throw new Error(`${selector} block is unterminated`);

  const declarations = new Map<string, string>();
  for (const match of source.slice(start, end).matchAll(/(--[\w-]+):\s*([^;]+);/g)) {
    const [, name, value] = match;
    if (name && value) declarations.set(name, value.trim());
  }
  return declarations;
}

/** Follows `--card: var(--surface)` style indirection to a literal colour. */
function resolveToken(declarations: Map<string, string>, name: string): string {
  let current = name;

  for (let hops = 0; hops < 8; hops++) {
    const value = declarations.get(current);
    if (value === undefined) {
      throw new Error(`${current} is not declared (while resolving ${name})`);
    }
    const alias = /^var\((--[\w-]+)\)$/.exec(value)?.[1];
    if (!alias) return value;
    current = alias;
  }
  throw new Error(`${name} did not resolve to a literal colour`);
}

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));
const encodeSrgb = (c: number) =>
  c <= 0.0031308 ? 12.92 * c : 1.055 * Math.pow(c, 1 / 2.4) - 0.055;
const decodeSrgb = (c: number) =>
  c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);

/**
 * Quantises to 8-bit on purpose: contrast is judged on what the display
 * actually paints, not on the unrounded float behind it.
 */
function oklchToRgb(value: string): Rgb {
  const match = /^oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*\)$/.exec(value);
  if (!match?.[1] || !match[2] || !match[3]) {
    throw new Error(`expected an alpha-free oklch() colour, got "${value}"`);
  }

  const lightness = Number(match[1]);
  const chroma = Number(match[2]);
  const hue = (Number(match[3]) * Math.PI) / 180;
  const a = chroma * Math.cos(hue);
  const b = chroma * Math.sin(hue);

  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;

  const channel = (linear: number) =>
    Math.round(clamp01(encodeSrgb(clamp01(linear))) * 255);

  return [
    channel(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    channel(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    channel(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  ];
}

function relativeLuminance([r, g, b]: Rgb): number {
  return (
    0.2126 * decodeSrgb(r / 255) +
    0.7152 * decodeSrgb(g / 255) +
    0.0722 * decodeSrgb(b / 255)
  );
}

function contrastRatio(foreground: Rgb, background: Rgb): number {
  const a = relativeLuminance(foreground);
  const b = relativeLuminance(background);
  const [lighter, darker] = a > b ? [a, b] : [b, a];
  return (lighter + 0.05) / (darker + 0.05);
}

function expectTonePasses(
  declarations: Map<string, string>,
  tone: string,
  floor: number,
) {
  const foreground = oklchToRgb(resolveToken(declarations, tone));

  for (const token of backgroundTokens) {
    const background = oklchToRgb(resolveToken(declarations, token));
    const ratio = contrastRatio(foreground, background);

    expect(
      Number(ratio.toFixed(2)),
      `${tone} on ${token} is ${ratio.toFixed(2)}:1`,
    ).toBeGreaterThanOrEqual(floor);
  }
}

const tokensCss = () =>
  readFileSync(resolve(repoRoot, "packages/ui/styles/tokens.css"), "utf8");
const landingCss = () =>
  readFileSync(resolve(repoRoot, "apps/web/app/custom.css"), "utf8");

// ── source scanning ────────────────────────────────────────────────────────

const scanRoots = ["packages/ui", "packages/views", "apps/web", "apps/desktop/src"];
const skipDirs = new Set(["node_modules", ".next", "dist", "out", "build", ".turbo"]);
const sourceExtensions = [".ts", ".tsx", ".css"];

/**
 * Colours whose hierarchy is fully expressed by solid tokens, so a fraction of
 * one is always someone inventing a tier. `white` and `background` are absent
 * on purpose: they paint text on photos, gradients, and inverted cards, where
 * no solid secondary token exists and transparency is the honest mechanism.
 */
const guardedColors = [
  "foreground",
  "muted-foreground",
  "sidebar-foreground",
  "destructive",
  "current",
];

/**
 * Transparency behind an interaction or disabled state is not a hierarchy
 * level: the resting state carries the contrast obligation and it is solid,
 * and WCAG exempts disabled controls outright. `dark:` is deliberately absent —
 * it paints a resting state.
 *
 * The test for admission to this list is narrow: the state must be transient
 * and user-driven, and the element's resting appearance must already be solid.
 * `dragging` qualifies for the same reason `active` does — a drag ghost at 60%
 * is feedback for an in-flight gesture, not a tier the text lives in.
 *
 * The exemption is never decided by "a state word appears somewhere nearby".
 * That is how `"text-muted-foreground hover:text-foreground opacity-50"` slips
 * through: the `hover:` belongs to the colour, not to the opacity, and the
 * opacity is resting-state. It is decided by exactly two things — the variant
 * prefix carried by the opacity utility itself, and the condition that governs
 * the literal the utility sits in — because those are the only two places the
 * state can actually live.
 */
const stateWords =
  /\b(?:hover|focus|focus-visible|focus-within|active|disabled|visited|dragging|isDragging|group-hover|group-focus|peer-hover|peer-focus|aria-disabled|data-disabled)\b/;

/**
 * `text-muted-foreground/70`, and equally `/[0.5]` and `/[50%]` — Tailwind
 * accepts an arbitrary alpha too, and a guard that only knows the bare-number
 * form is a guard with a documented way around it.
 */
const alphaOnText = new RegExp(
  String.raw`[\w:\[\]./-]*\btext-(?:${guardedColors.join("|")})/(?:\d+|\[[^\]]+\])`,
  "g",
);

/**
 * A standalone `opacity-*` utility dims the element it sits on just as a slash
 * alpha does, so `text-muted-foreground opacity-70` is the same defect wearing
 * different syntax.
 *
 * The colour and the opacity are correlated across the whole class expression,
 * not within one string. `cn("… text-muted-foreground", suppressed &&
 * "opacity-60")` is one element wearing both, and that split-literal shape is
 * the common one — it is how the comment trigger chips dimmed live text to
 * 2.55:1 while a per-literal check called them clean.
 */
const guardedColorInClass = new RegExp(String.raw`\btext-(?:${guardedColors.join("|")})\b`);
const classLiteral = /"([^"\\]*(?:\\.[^"\\]*)*)"|`([^`\\]*(?:\\.[^`\\]*)*)`/gs;
const opacityUtility = /(?<![\w:-])opacity-(\d{1,3})\b/g;

/** Walks to the `)` matching the `(` at `open`, or -1 if unbalanced. */
function matchingParen(source: string, open: number): number {
  let depth = 0;
  for (let i = open; i < source.length; i += 1) {
    if (source[i] === "(") depth += 1;
    else if (source[i] === ")") {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

/**
 * One element's worth of classes: a whole `cn(...)` call, or a string/template
 * literal that is not already inside one.
 */
function classExpressions(source: string): { start: number; end: number }[] {
  const units: { start: number; end: number }[] = [];
  const calls: [number, number][] = [];

  for (const call of source.matchAll(/\bcn\(/g)) {
    const open = call.index + call[0].length - 1;
    const close = matchingParen(source, open);
    if (close < 0) continue;
    if (calls.some(([s, e]) => call.index >= s && close <= e)) continue;
    calls.push([call.index, close]);
    units.push({ start: call.index, end: close + 1 });
  }

  for (const literal of source.matchAll(classLiteral)) {
    if (calls.some(([s, e]) => literal.index >= s && literal.index < e)) continue;
    units.push({ start: literal.index, end: literal.index + literal[0].length });
  }

  return units;
}

/**
 * The pieces of a class expression that each carry their own condition: every
 * quoted literal, plus the static chunks of a template literal between its
 * `${…}` holes. Splitting this finely is what lets
 * `` `… ${disabled ? "opacity-60" : ""}` `` be judged by its own ternary
 * instead of by the template it happens to sit in.
 */
function classSegments(
  source: string,
  unit: { start: number; end: number },
): { body: string; bodyStart: number; delimiter: number }[] {
  const text = source.slice(unit.start, unit.end);
  const segments: { body: string; bodyStart: number; delimiter: number }[] = [];

  for (const quoted of text.matchAll(/"([^"\\]*(?:\\.[^"\\]*)*)"/g)) {
    segments.push({
      body: quoted[1] ?? "",
      bodyStart: unit.start + quoted.index + 1,
      delimiter: unit.start + quoted.index,
    });
  }

  for (const template of text.matchAll(/`([^`\\]*(?:\\.[^`\\]*)*)`/gs)) {
    const inner = template[1] ?? "";
    const innerStart = unit.start + template.index + 1;
    const delimiter = unit.start + template.index;
    let cursor = 0;
    for (const hole of inner.matchAll(/\$\{[^}]*\}/gs)) {
      segments.push({
        body: inner.slice(cursor, hole.index),
        bodyStart: innerStart + cursor,
        delimiter,
      });
      cursor = hole.index + hole[0].length;
    }
    segments.push({ body: inner.slice(cursor), bodyStart: innerStart + cursor, delimiter });
  }

  return segments;
}

/**
 * The condition governing a segment: whatever sits between its opening
 * delimiter and the nearest structural boundary — `suppressed &&`,
 * `disabled ?`, or nothing at all. When it is nothing, the literal is a leading
 * argument, so the property key the call is assigned to decides instead
 * (`disabled: cn("… opacity-50")`). That key has to look like `identifier:`
 * immediately before the call, so a neighbouring `disabled={disabled}` prop
 * cannot be mistaken for one.
 */
function governingCondition(source: string, unitStart: number, delimiter: number): string {
  const boundary = Math.max(
    ...[",", "(", "{", "[", "}", "`"].map((c) => source.lastIndexOf(c, delimiter - 1)),
  );
  const between = source.slice(Math.max(boundary + 1, Math.min(unitStart, delimiter)), delimiter);
  if (between.trim()) return between;

  const beforeCall = source.slice(Math.max(0, unitStart - 60), unitStart);
  return /([\w$]+)\s*:\s*$/.exec(beforeCall)?.[1] ?? "";
}

/**
 * Comments name the old classes when explaining what replaced them, so they are
 * stripped first. Over-stripping can only hide a violation, never invent one.
 */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

function collectSourceFiles(dir: string, found: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (skipDirs.has(entry)) continue;
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      collectSourceFiles(path, found);
      continue;
    }
    if (!sourceExtensions.some((ext) => entry.endsWith(ext))) continue;
    if (/\.test\.tsx?$/.test(entry)) continue;
    found.push(path);
  }
  return found;
}

const lineOf = (source: string, index: number) =>
  source.slice(0, index).split("\n").length;

/** Both ways of writing "dim this tone", reported together. */
function findTransparencyAsHierarchy(source: string): { line: number; found: string }[] {
  const stripped = stripComments(source);
  const violations: { line: number; found: string }[] = [];

  for (const match of stripped.matchAll(alphaOnText)) {
    if (stateWords.test(match[0])) continue;
    violations.push({ line: lineOf(stripped, match.index), found: match[0] });
  }

  for (const unit of classExpressions(stripped)) {
    const text = stripped.slice(unit.start, unit.end);
    if (!guardedColorInClass.test(text)) continue;

    for (const segment of classSegments(stripped, unit)) {
      if (stateWords.test(governingCondition(stripped, unit.start, segment.delimiter))) continue;

      for (const dim of segment.body.matchAll(opacityUtility)) {
        // opacity-0 and opacity-100 are show/hide, not a tone.
        if (dim[1] === "0" || dim[1] === "100") continue;
        // Only the variant prefix the utility itself carries counts here; a
        // `hover:` belonging to some other class in the same string does not.
        const prefix = /(\S*)$/.exec(segment.body.slice(0, dim.index))?.[1] ?? "";
        if (stateWords.test(prefix)) continue;
        violations.push({
          line: lineOf(stripped, segment.bodyStart + dim.index),
          found: dim[0],
        });
      }
    }
  }

  return violations;
}

// ── the contract ───────────────────────────────────────────────────────────

describe("text contrast", () => {
  describe("--muted-foreground, the floor for text", () => {
    it("clears WCAG AA on every light surface", () => {
      expectTonePasses(readBlock(tokensCss(), ":root"), "--muted-foreground", WCAG_AA_NORMAL_TEXT);
    });

    it("clears WCAG AA on every dark surface", () => {
      expectTonePasses(readBlock(tokensCss(), ".dark"), "--muted-foreground", WCAG_AA_NORMAL_TEXT);
    });
  });

  describe("--faint-foreground, for marks that are not text", () => {
    it("clears non-text contrast on every light surface", () => {
      expectTonePasses(readBlock(tokensCss(), ":root"), "--faint-foreground", WCAG_AA_NON_TEXT);
    });

    it("clears non-text contrast on every dark surface", () => {
      expectTonePasses(readBlock(tokensCss(), ".dark"), "--faint-foreground", WCAG_AA_NON_TEXT);
    });

    it.each([
      ["light", ":root"],
      ["dark", ".dark"],
    ])("stays quieter than muted-foreground in %s mode", (_mode, selector) => {
      const declarations = readBlock(tokensCss(), selector);
      const surface = oklchToRgb(resolveToken(declarations, "--surface"));
      const faint = oklchToRgb(resolveToken(declarations, "--faint-foreground"));
      const muted = oklchToRgb(resolveToken(declarations, "--muted-foreground"));

      expect(
        contrastRatio(faint, surface),
        "faint must read as the quieter step, or the two tiers have swapped",
      ).toBeLessThan(contrastRatio(muted, surface));
    });
  });

  // The landing route tree re-declares the light palette so token-driven
  // components stay light under next-themes' `.dark` class. That copy is only
  // correct while it matches the source, so drift here is a bug in itself — a
  // tone missing from the copy silently inherits its `.dark` value on a white
  // surface.
  it.each(["--muted-foreground", "--faint-foreground"])(
    "keeps the landing-light copy of %s in sync with the light token",
    (tone) => {
      expect(resolveToken(readBlock(landingCss(), ".landing-light"), tone)).toBe(
        resolveToken(readBlock(tokensCss(), ":root"), tone),
      );
    },
  );

  /**
   * The detector is the part of this guard most likely to rot, because every
   * hole in it is silent: the sweep still reports "clean". Both earlier
   * versions passed the repo scan while real violations sat in the tree — one
   * because it only looked inside a single string, one because it accepted any
   * state word near the utility. So the shapes it must and must not catch are
   * pinned here, next to the reason each one exists.
   */
  describe("the detector itself", () => {
    const caught = (source: string) => findTransparencyAsHierarchy(source).length > 0;

    it.each([
      ["slash alpha", `<i className="text-muted-foreground/50" />`],
      ["arbitrary decimal alpha", `<i className="text-muted-foreground/[0.5]" />`],
      ["arbitrary percent alpha", `<i className="text-muted-foreground/[50%]" />`],
      ["opacity in the same string", `<i className="text-muted-foreground opacity-70" />`],
      [
        "opacity in a sibling cn() argument",
        `<i className={cn("text-muted-foreground", "opacity-50")} />`,
      ],
      [
        "opacity behind a non-state condition",
        `<i className={cn("text-muted-foreground", suppressed && "opacity-60")} />`,
      ],
      [
        "resting opacity next to a hover-prefixed colour",
        `<i className="text-muted-foreground hover:text-foreground opacity-50" />`,
      ],
    ])("catches %s", (_shape, source) => {
      expect(caught(source)).toBe(true);
    });

    it.each([
      ["a variant on the utility itself", `<i className="text-muted-foreground disabled:opacity-50" />`],
      [
        "a disabled ternary",
        "<i className={`text-muted-foreground ${disabled ? \"opacity-60\" : \"\"}`} />",
      ],
      [
        "a disabled classNames key",
        `{ disabled: cn("text-muted-foreground opacity-50", defaults.disabled) }`,
      ],
      ["opacity-0 / opacity-100 show-hide", `<i className="text-muted-foreground opacity-0" />`],
      ["opacity with no guarded colour in the expression", `<i className="text-white opacity-50" />`],
      ["the colour alone", `<i className="text-muted-foreground" />`],
    ])("leaves %s alone", (_shape, source) => {
      expect(caught(source)).toBe(false);
    });

    it("does not mistake an apostrophe in a comment for a class string", () => {
      expect(caught(`// a block's own opacity-50 text-muted-foreground\nconst x = 1;`)).toBe(false);
    });
  });

  it("product UI expresses text hierarchy with tokens, not transparency", () => {
    const violations: string[] = [];

    for (const root of scanRoots) {
      for (const path of collectSourceFiles(resolve(repoRoot, root))) {
        for (const { line, found } of findTransparencyAsHierarchy(
          readFileSync(path, "utf8"),
        )) {
          violations.push(
            `${relative(repoRoot, path)}:${line}  ${found}  ` +
              `(use a solid tone: text-foreground / text-muted-foreground, ` +
              `or text-faint-foreground for icons and glyphs)`,
          );
        }
      }
    }

    expect(
      violations,
      `Transparency standing in for a text tone:\n${violations.join("\n")}`,
    ).toEqual([]);
  });
});
