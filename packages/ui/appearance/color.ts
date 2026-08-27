/**
 * OKLCH math for token derivation.
 *
 * Ported from the appearance solver we use in other apps: OKLCH is the
 * computation space; CSS `oklch(L C H)` with L in 0–1 is the wire format
 * this repo's contrast tests parse. Decorative roles blend in sRGB;
 * text roles binary-search lightness until a WCAG ratio holds after hex
 * quantization, hue and chroma preserved.
 */

export type Oklch = {
  readonly l: number;
  readonly c: number;
  readonly h: number;
};

export type Rgb = {
  readonly red: number;
  readonly green: number;
  readonly blue: number;
};

const srgbToLinear = (channel: number) =>
  channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
const linearToSrgb = (channel: number) =>
  channel <= 0.0031308 ? channel * 12.92 : 1.055 * channel ** (1 / 2.4) - 0.055;

type LinearRgb = { readonly r: number; readonly g: number; readonly b: number };

export function hexToRgb(hex: string): Rgb {
  const value = hex.replace("#", "").padEnd(6, "0");
  return {
    red: Number.parseInt(value.slice(0, 2), 16),
    green: Number.parseInt(value.slice(2, 4), 16),
    blue: Number.parseInt(value.slice(4, 6), 16),
  };
}

export function rgbToHex(value: Rgb): string {
  return `#${[value.red, value.green, value.blue]
    .map((channel) => channel.toString(16).padStart(2, "0"))
    .join("")}`;
}

export function hexToOklch(hex: string): Oklch {
  const value = hex.replace("#", "");
  return linearRgbToOklch({
    r: srgbToLinear(Number.parseInt(value.slice(0, 2), 16) / 255),
    g: srgbToLinear(Number.parseInt(value.slice(2, 4), 16) / 255),
    b: srgbToLinear(Number.parseInt(value.slice(4, 6), 16) / 255),
  });
}

function linearRgbToOklch({ r, g, b }: LinearRgb): Oklch {
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const lightness = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const a = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const bComponent = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  const hue = (Math.atan2(bComponent, a) * 180) / Math.PI;
  return {
    l: lightness,
    c: Math.hypot(a, bComponent),
    h: hue < 0 ? hue + 360 : hue,
  };
}

function oklchToLinearRgb({ l: lightness, c, h }: Oklch): LinearRgb {
  const radians = (h * Math.PI) / 180;
  const a = c * Math.cos(radians);
  const bComponent = c * Math.sin(radians);
  const l = (lightness + 0.3963377774 * a + 0.2158037573 * bComponent) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * bComponent) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.291485548 * bComponent) ** 3;
  return {
    r: 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    g: -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    b: -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  };
}

const GAMUT_EPSILON = 1e-6;

const inSrgbGamut = ({ r, g, b }: LinearRgb) =>
  r >= -GAMUT_EPSILON &&
  r <= 1 + GAMUT_EPSILON &&
  g >= -GAMUT_EPSILON &&
  g <= 1 + GAMUT_EPSILON &&
  b >= -GAMUT_EPSILON &&
  b <= 1 + GAMUT_EPSILON;

function clampToSrgbGamut(color: Oklch): Oklch {
  if (inSrgbGamut(oklchToLinearRgb(color))) return color;
  let representable = 0;
  let excessive = color.c;
  for (let iteration = 0; iteration < 32; iteration++) {
    const candidate = (representable + excessive) / 2;
    if (inSrgbGamut(oklchToLinearRgb({ ...color, c: candidate }))) {
      representable = candidate;
    } else {
      excessive = candidate;
    }
  }
  return { ...color, c: representable };
}

export function oklchToHex(color: Oklch): string {
  const { r, g, b } = oklchToLinearRgb(clampToSrgbGamut(color));
  const quantize = (channel: number) =>
    Math.round(Math.min(1, Math.max(0, linearToSrgb(channel))) * 255);
  return `#${[quantize(r), quantize(g), quantize(b)]
    .map((channel) => channel.toString(16).padStart(2, "0"))
    .join("")}`;
}

function wcagLuminance(hex: string): number {
  const value = hex.replace("#", "");
  const linear = (offset: number) =>
    srgbToLinear(Number.parseInt(value.slice(offset, offset + 2), 16) / 255);
  return 0.2126 * linear(0) + 0.7152 * linear(2) + 0.0722 * linear(4);
}

export function contrastRatio(foregroundHex: string, backgroundHex: string): number {
  const foreground = wcagLuminance(foregroundHex);
  const background = wcagLuminance(backgroundHex);
  const lighter = Math.max(foreground, background);
  const darker = Math.min(foreground, background);
  return (lighter + 0.05) / (darker + 0.05);
}

export function solveContrast(
  seedHex: string,
  backgroundHex: string,
  targetRatio: number,
): string {
  const seed = hexToOklch(seedHex);
  const backgroundLightness = hexToOklch(backgroundHex).l;
  const extreme = backgroundLightness > 0.5 ? 0 : 1;
  const extremeHex = oklchToHex({ ...seed, l: extreme });
  if (contrastRatio(extremeHex, backgroundHex) < targetRatio) return extremeHex;
  let passing = extreme;
  let failing = backgroundLightness;
  for (let iteration = 0; iteration < 40; iteration++) {
    const candidate = (passing + failing) / 2;
    if (contrastRatio(oklchToHex({ ...seed, l: candidate }), backgroundHex) >= targetRatio) {
      passing = candidate;
    } else {
      failing = candidate;
    }
  }
  return oklchToHex({ ...seed, l: passing });
}

export function blend(left: Rgb, right: Rgb, weight: number): Rgb {
  const value = clamp(weight);
  return {
    red: Math.round(left.red + (right.red - left.red) * value),
    green: Math.round(left.green + (right.green - left.green) * value),
    blue: Math.round(left.blue + (right.blue - left.blue) * value),
  };
}

export function blendHex(left: Rgb, right: Rgb, weight: number): string {
  return rgbToHex(blend(left, right, weight));
}

export function compositeOver(foregroundHex: string, backgroundHex: string, alpha: number): string {
  const over = hexToRgb(foregroundHex);
  const under = hexToRgb(backgroundHex);
  const t = clamp(alpha);
  return rgbToHex({
    red: Math.round(under.red + (over.red - under.red) * t),
    green: Math.round(under.green + (over.green - under.green) * t),
    blue: Math.round(under.blue + (over.blue - under.blue) * t),
  });
}

export function shiftLightness(hex: string, delta: number): string {
  const color = hexToOklch(hex);
  return oklchToHex({ ...color, l: clamp(color.l + delta) });
}

const round = (value: number, digits: number) => {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
};

/** Wire format this repo's contrast tests parse: unit lightness, no alpha. */
export function hexToOklchCss(hex: string, alpha?: number): string {
  const color = hexToOklch(hex);
  const lightness = round(color.l, 6);
  const chroma = round(color.c, 6);
  const hue = round(color.c < 1e-5 ? 0 : color.h, 3);
  if (alpha === undefined || alpha >= 1) {
    return `oklch(${lightness} ${chroma} ${hue})`;
  }
  return `oklch(${lightness} ${chroma} ${hue} / ${round(alpha, 3)})`;
}

export function clamp(value: number): number {
  return Math.min(1, Math.max(0, value));
}

export function luminance(hex: string): number {
  return wcagLuminance(hex);
}
