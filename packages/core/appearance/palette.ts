/**
 * The colour slots a theme can recolour labels, custom statuses and select
 * options through. Every colour people pick is a `#rrggbb`; it renders as
 * `var(--palette-<slot>, <its own hex>)`, so with no slot declared it looks
 * exactly as picked. A preset lands on its own slot. Any other colour is
 * placed in OKLab: one with little chroma on gray, the rest on the slot whose
 * hue is nearest, so a pale or dark shade stays with its hue.
 */

export const PALETTE_SLOTS = [
  "gray",
  "red",
  "orange",
  "yellow",
  "green",
  "teal",
  "blue",
  "indigo",
  "purple",
  "pink",
] as const;

export type PaletteSlot = (typeof PALETTE_SLOTS)[number];

/** The colour picker's presets, one per slot, in the picker's order. */
export const PALETTE_PRESETS: Readonly<Record<PaletteSlot, string>> = {
  gray: "#6b7280",
  red: "#ef4444",
  orange: "#f97316",
  yellow: "#eab308",
  green: "#22c55e",
  teal: "#14b8a6",
  blue: "#3b82f6",
  indigo: "#6366f1",
  purple: "#a855f7",
  pink: "#ec4899",
};

const HEX = /^#?([0-9a-f]{6})$/i;

type Lab = readonly [number, number, number];

const linear = (c: number) => (c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4));

function hexToOklab(hex: string): Lab | null {
  const match = HEX.exec(hex.trim());
  if (!match) return null;
  const n = parseInt(match[1]!, 16);
  const r = linear(((n >> 16) & 255) / 255);
  const g = linear(((n >> 8) & 255) / 255);
  const b = linear((n & 255) / 255);
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

/** OKLab chroma below which a colour reads as gray; the gray preset's is 0.023. */
const NEUTRAL_CHROMA = 0.04;

const hueOf = ([, a, b]: Lab) => Math.atan2(b, a);

const PRESET_HUES = PALETTE_SLOTS.filter((slot) => slot !== "gray").map(
  (slot) => [slot, hueOf(hexToOklab(PALETTE_PRESETS[slot])!)] as const,
);

/** The slot a `#rrggbb` colour belongs to; null when it is not one. */
export function paletteSlot(hex: string): PaletteSlot | null {
  const lab = hexToOklab(hex);
  if (!lab) return null;
  if (Math.hypot(lab[1], lab[2]) < NEUTRAL_CHROMA) return "gray";
  const hue = hueOf(lab);
  let nearest: PaletteSlot = "gray";
  let best = Infinity;
  for (const [slot, presetHue] of PRESET_HUES) {
    const turn = Math.abs(hue - presetHue);
    const distance = Math.min(turn, 2 * Math.PI - turn);
    if (distance < best) {
      best = distance;
      nearest = slot;
    }
  }
  return nearest;
}

/** The label text colours the app picks between: dark, then light. */
export const LABEL_TEXT_COLORS = ["#111827", "#f9fafb"] as const;

/**
 * Text that reads on a label of this colour when the theme sets no
 * `--label-chip-color`: dark above a BT.601 perceived luminance of 0.55,
 * light below, dark for anything that is not a hex.
 */
export function labelTextColor(hex: string): string {
  const [dark, light] = LABEL_TEXT_COLORS;
  const match = HEX.exec(hex.trim());
  if (!match) return dark;
  const n = parseInt(match[1]!, 16);
  const luminance = (0.299 * ((n >> 16) & 255) + 0.587 * ((n >> 8) & 255) + 0.114 * (n & 255)) / 255;
  return luminance > 0.55 ? dark : light;
}

/**
 * The CSS colour to paint a picked colour with: its slot's variable, falling
 * back to the colour itself. Anything that is not a `#rrggbb` comes back as
 * is, so a caller's existing handling of it does not change.
 */
export function paletteColor(hex: string): string;
export function paletteColor(hex: string | null | undefined): string | undefined;
export function paletteColor(hex: string | null | undefined): string | undefined {
  if (!hex) return hex ?? undefined;
  const slot = paletteSlot(hex);
  return slot ? `var(--palette-${slot}, ${hex})` : hex;
}
