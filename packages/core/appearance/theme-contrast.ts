/**
 * WCAG 2.x contrast for theme colours. A palette maps variable names
 * (with `--`) to CSS values; colours are resolved through `var()` chains to
 * literals in hex, rgb(), hsl(), oklab() or oklch(). Anything else
 * (color-mix(), a keyword) cannot be resolved, and the check says so
 * instead of guessing.
 */

export type Palette = ReadonlyMap<string, string>;

/** sRGB channels 0–255 (rounded, as the display paints them) and alpha 0–1. */
export interface Rgba {
  r: number;
  g: number;
  b: number;
  a: number;
}

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));
const encodeSrgb = (c: number) => (c <= 0.0031308 ? 12.92 * c : 1.055 * Math.pow(c, 1 / 2.4) - 0.055);
const decodeSrgb = (c: number) => (c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4));
const channel = (linear: number) => Math.round(clamp01(encodeSrgb(clamp01(linear))) * 255);

function oklabToRgb(lightness: number, a: number, b: number): [number, number, number] {
  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;
  return [
    channel(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    channel(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    channel(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  ];
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const k = (n: number) => (n + h / 30) % 12;
  const a = s * Math.min(l, 1 - l);
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  return [Math.round(f(0) * 255), Math.round(f(8) * 255), Math.round(f(4) * 255)];
}

const NAMED: Record<string, Rgba> = {
  white: { r: 255, g: 255, b: 255, a: 1 },
  black: { r: 0, g: 0, b: 0, a: 1 },
  transparent: { r: 0, g: 0, b: 0, a: 0 },
};

/** A number, a percentage of `percentOf`, or `none` (0). */
function num(token: string | undefined, percentOf = 1): number | null {
  if (token === undefined) return null;
  if (token === "none") return 0;
  const match = /^(-?(?:\d+\.?\d*|\.\d+))(%|deg)?$/.exec(token);
  if (!match) return null;
  const value = Number(match[1]);
  return match[2] === "%" ? (value / 100) * percentOf : value;
}

/** Parses a colour literal; null for anything that is not one. */
export function parseColor(value: string): Rgba | null {
  const text = value.trim().toLowerCase();
  if (NAMED[text]) return NAMED[text]!;
  const hex = /^#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$/.exec(text)?.[1];
  if (hex) {
    const full = hex.length <= 4 ? [...hex].map((c) => c + c).join("") : hex;
    const byte = (i: number) => Number.parseInt(full.slice(i, i + 2), 16);
    return { r: byte(0), g: byte(2), b: byte(4), a: full.length === 8 ? byte(6) / 255 : 1 };
  }
  const fn = /^(rgba?|hsla?|oklch|oklab)\(\s*([^()]*)\)$/.exec(text);
  if (!fn) return null;
  const [main = "", alphaPart] = fn[2]!.split("/");
  const parts = main.split(/[\s,]+/).filter(Boolean);
  let alphaToken = alphaPart?.trim();
  if (alphaToken === undefined && parts.length === 4) alphaToken = parts.pop();
  if (parts.length !== 3) return null;
  const alpha = alphaToken === undefined ? 1 : num(alphaToken);
  if (alpha === null) return null;
  const a = clamp01(alpha);
  const [x, y, z] = parts;
  switch (fn[1]) {
    case "rgb":
    case "rgba": {
      const values = [x, y, z].map((p) => num(p, 255));
      if (values.some((v) => v === null)) return null;
      const [r, g, b] = values.map((v) => Math.round(Math.min(255, Math.max(0, v!))));
      return { r: r!, g: g!, b: b!, a };
    }
    case "hsl":
    case "hsla": {
      const h = num(x);
      const s = num(y, 1);
      const l = num(z, 1);
      if (h === null || s === null || l === null) return null;
      const [r, g, b] = hslToRgb(((h % 360) + 360) % 360, clamp01(s > 1 ? s / 100 : s), clamp01(l > 1 ? l / 100 : l));
      return { r, g, b, a };
    }
    case "oklch": {
      const l = num(x, 1);
      const c = num(y, 0.4);
      const h = num(z);
      if (l === null || c === null || h === null) return null;
      const hue = (h * Math.PI) / 180;
      const [r, g, b] = oklabToRgb(l, c * Math.cos(hue), c * Math.sin(hue));
      return { r, g, b, a };
    }
    case "oklab": {
      const l = num(x, 1);
      const oa = num(y, 0.4);
      const ob = num(z, 0.4);
      if (l === null || oa === null || ob === null) return null;
      const [r, g, b] = oklabToRgb(l, oa, ob);
      return { r, g, b, a };
    }
  }
  return null;
}

/**
 * Follows `var(--a)` and `var(--a, fallback)` to a colour. Returns the
 * colour, or why it could not be read.
 */
export function resolveColor(palette: Palette, name: string): { color: Rgba } | { unresolved: string } {
  let value = `var(${name})`;
  for (let hops = 0; hops < 12; hops += 1) {
    const reference = /^var\(\s*(--[\w-]+)\s*(?:,\s*(.+))?\)$/s.exec(value.trim());
    if (!reference) {
      const color = parseColor(value);
      return color ? { color } : { unresolved: `${name} is \`${value.trim()}\`, which this check cannot read` };
    }
    const next = palette.get(reference[1]!) ?? reference[2];
    if (next === undefined) return { unresolved: `${reference[1]} is not set` };
    value = next;
  }
  return { unresolved: `${name} refers to itself` };
}

function luminance({ r, g, b }: Rgba): number {
  return 0.2126 * decodeSrgb(r / 255) + 0.7152 * decodeSrgb(g / 255) + 0.0722 * decodeSrgb(b / 255);
}

/** `top` painted over an opaque `bottom`. */
function over(top: Rgba, bottom: Rgba): Rgba {
  const mix = (t: number, b: number) => Math.round(t * top.a + b * (1 - top.a));
  return { r: mix(top.r, bottom.r), g: mix(top.g, bottom.g), b: mix(top.b, bottom.b), a: 1 };
}

/** The WCAG ratio of a foreground on an opaque background. */
export function contrastRatio(foreground: Rgba, background: Rgba): number {
  const a = luminance(over(foreground, background));
  const b = luminance(background);
  const [lighter, darker] = a > b ? [a, b] : [b, a];
  return (lighter + 0.05) / (darker + 0.05);
}

/** The custom properties declared in `selector { … }` of a stylesheet such as tokens.css. */
export function readDeclarations(css: string, selector: string): Map<string, string> {
  const start = css.indexOf(`${selector} {`);
  const declarations = new Map<string, string>();
  if (start < 0) return declarations;
  const end = css.indexOf("\n}", start);
  for (const match of css.slice(start, end < 0 ? undefined : end).matchAll(/(--[\w-]+):\s*([^;]+);/g)) {
    declarations.set(match[1]!, match[2]!.trim());
  }
  return declarations;
}

/** The app's own palette per mode, which a theme's variables are layered on. */
export interface BasePalette {
  light: Palette;
  dark: Palette;
}

/** Reads the base palette from packages/ui/styles/tokens.css. */
export function basePaletteFromTokens(tokensCss: string): BasePalette {
  const light = readDeclarations(tokensCss, ":root");
  return { light, dark: new Map([...light, ...readDeclarations(tokensCss, ".dark")]) };
}
