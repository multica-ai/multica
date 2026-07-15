// Random color generation for the label / property-option color pickers.
//
// Colors are sampled in HSL and constrained to the vivid band the preset
// palettes live in, so a random pick never lands on near-white, near-black,
// or a washed-out gray. When the current color is passed as `avoid`, the new
// hue keeps a minimum circular distance from it so every click produces a
// visibly different color.

const MIN_HUE_DISTANCE = 40;
const SATURATION_RANGE: readonly [number, number] = [0.62, 0.9];
const LIGHTNESS_RANGE: readonly [number, number] = [0.46, 0.62];

function randomIn([min, max]: readonly [number, number]): number {
  return min + Math.random() * (max - min);
}

function hexToHue(hex: string): number | null {
  const match = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!match) return null;
  const value = match[1] ?? "";
  const r = parseInt(value.slice(0, 2), 16) / 255;
  const g = parseInt(value.slice(2, 4), 16) / 255;
  const b = parseInt(value.slice(4, 6), 16) / 255;
  const max = Math.max(r, g, b);
  const delta = max - Math.min(r, g, b);
  if (delta === 0) return null; // achromatic — no hue to keep distance from
  let hue: number;
  if (max === r) hue = ((g - b) / delta) % 6;
  else if (max === g) hue = (b - r) / delta + 2;
  else hue = (r - g) / delta + 4;
  return (hue * 60 + 360) % 360;
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
  const avoidHue = avoid ? hexToHue(avoid) : null;
  const hue =
    avoidHue === null
      ? Math.random() * 360
      : (avoidHue +
          MIN_HUE_DISTANCE +
          Math.random() * (360 - MIN_HUE_DISTANCE * 2)) %
        360;
  return hslToHex(hue, randomIn(SATURATION_RANGE), randomIn(LIGHTNESS_RANGE));
}
