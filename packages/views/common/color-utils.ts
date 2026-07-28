// Color math behind the shared color picker (see color-picker.tsx).
//
// Values travel as `#rrggbb` hex strings (what the API stores); the picker
// edits in HSV because saturation/value map directly onto its 2D area.

export interface Hsv {
  /** Hue in degrees, 0–360. */
  h: number;
  /** Saturation 0–1. */
  s: number;
  /** Value (brightness) 0–1. */
  v: number;
}

/** Normalizes to lowercase `#rrggbb`, or null when not a 6-digit hex color. */
export function normalizeHex(input: string): string | null {
  const match = /^#?([0-9a-f]{6})$/i.exec(input.trim());
  return match ? `#${match[1]?.toLowerCase()}` : null;
}

export function hexToHsv(hex: string): Hsv | null {
  const normalized = normalizeHex(hex);
  if (!normalized) return null;
  const r = parseInt(normalized.slice(1, 3), 16) / 255;
  const g = parseInt(normalized.slice(3, 5), 16) / 255;
  const b = parseInt(normalized.slice(5, 7), 16) / 255;
  const max = Math.max(r, g, b);
  const delta = max - Math.min(r, g, b);
  let h = 0;
  if (delta !== 0) {
    if (max === r) h = ((g - b) / delta) % 6;
    else if (max === g) h = (b - r) / delta + 2;
    else h = (r - g) / delta + 4;
    h = (h * 60 + 360) % 360;
  }
  return { h, s: max === 0 ? 0 : delta / max, v: max };
}

export function hsvToHex({ h, s, v }: Hsv): string {
  const channel = (n: number): string => {
    const k = (n + h / 60) % 6;
    const value = v - v * s * Math.max(0, Math.min(k, 4 - k, 1));
    return Math.round(value * 255)
      .toString(16)
      .padStart(2, "0");
  };
  return `#${channel(5)}${channel(3)}${channel(1)}`;
}

// Random picks are sampled in HSL and constrained to the vivid band the
// preset palette lives in, so a random color never lands on near-white,
// near-black, or a washed-out gray. When the current color is passed as
// `avoid`, the new hue keeps a minimum circular distance from it so every
// click produces a visibly different color.

const MIN_HUE_DISTANCE = 40;
const SATURATION_RANGE: readonly [number, number] = [0.62, 0.9];
const LIGHTNESS_RANGE: readonly [number, number] = [0.46, 0.62];

function randomIn([min, max]: readonly [number, number]): number {
  return min + Math.random() * (max - min);
}

function hslToHex(hue: number, saturation: number, lightness: number): string {
  const channel = (n: number): string => {
    const k = (n + hue / 30) % 12;
    const a = saturation * Math.min(lightness, 1 - lightness);
    const value = lightness - a * Math.max(-1, Math.min(k - 3, 9 - k, 1));
    return Math.round(value * 255)
      .toString(16)
      .padStart(2, "0");
  };
  return `#${channel(0)}${channel(8)}${channel(4)}`;
}

/**
 * Returns a random vivid color as `#rrggbb`. Pass the current color via
 * `avoid` to guarantee the result reads as a different color.
 */
export function randomOptionColor(avoid?: string): string {
  const avoidHsv = avoid ? hexToHsv(avoid) : null;
  const avoidHue =
    avoidHsv && avoidHsv.s > 0 && avoidHsv.v > 0 ? avoidHsv.h : null;
  const hue =
    avoidHue === null
      ? Math.random() * 360
      : (avoidHue +
          MIN_HUE_DISTANCE +
          Math.random() * (360 - MIN_HUE_DISTANCE * 2)) %
        360;
  return hslToHex(hue, randomIn(SATURATION_RANGE), randomIn(LIGHTNESS_RANGE));
}
