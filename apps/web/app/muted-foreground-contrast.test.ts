import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Is muted text actually readable on the surfaces it lands on?
 *
 * `text-muted-foreground` is the single most-used colour class in the app
 * (~2k call sites, including the primary nav labels in
 * `packages/views/layout/app-sidebar.tsx`), so one token decides whether a
 * large share of the product's secondary text clears WCAG AA. Light mode used
 * to fail it: at `oklch(0.552 ...)` the nav-hover pair (--sidebar-accent) was
 * 3.98:1 against a 4.5:1 floor.
 *
 * This asserts on the token file rather than on rendered components because
 * the token IS the contract — every consumer inherits whatever value lives
 * here, and a component test would only ever prove that a class name is
 * present, not that the pixels are legible.
 */

const repoRoot = resolve(process.cwd(), "../..");
const WCAG_AA_NORMAL_TEXT = 4.5;

/**
 * Surfaces that muted text is painted on. Border/input/ring tokens are
 * excluded: they are not text backgrounds, and several carry alpha, which has
 * no single contrast answer without knowing what is underneath.
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

function expectMutedForegroundPassesAA(declarations: Map<string, string>) {
  const foreground = oklchToRgb(resolveToken(declarations, "--muted-foreground"));

  for (const token of backgroundTokens) {
    const background = oklchToRgb(resolveToken(declarations, token));
    const ratio = contrastRatio(foreground, background);

    expect(
      Number(ratio.toFixed(2)),
      `--muted-foreground on ${token} is ${ratio.toFixed(2)}:1`,
    ).toBeGreaterThanOrEqual(WCAG_AA_NORMAL_TEXT);
  }
}

const tokensCss = () =>
  readFileSync(resolve(repoRoot, "packages/ui/styles/tokens.css"), "utf8");

describe("muted-foreground contrast", () => {
  it("clears WCAG AA on every light surface", () => {
    expectMutedForegroundPassesAA(readBlock(tokensCss(), ":root"));
  });

  it("clears WCAG AA on every dark surface", () => {
    expectMutedForegroundPassesAA(readBlock(tokensCss(), ".dark"));
  });

  // The landing route tree re-declares the light palette so token-driven
  // components stay light under next-themes' `.dark` class. That copy is only
  // correct while it matches the source, so drift here is a bug in itself.
  it("keeps the landing-light copy in sync with the light token", () => {
    const landing = readBlock(
      readFileSync(resolve(repoRoot, "apps/web/app/custom.css"), "utf8"),
      ".landing-light",
    );

    expect(resolveToken(landing, "--muted-foreground")).toBe(
      resolveToken(readBlock(tokensCss(), ":root"), "--muted-foreground"),
    );
  });
});
