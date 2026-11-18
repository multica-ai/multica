// Text icon: a few typed characters on a solid color, drawn into a PNG.
//
// The result goes through the same upload + `onUploaded` path as a cropped
// image, so every surface that already shows an uploaded avatar shows it with
// no change. The square is fully filled; rounding stays display-only, as it is
// for cropped images.

import {
  PALETTE_PRESETS,
  PALETTE_SLOTS,
  labelTextColor,
} from "@multica/core/appearance";

/** Square side of the generated PNG. */
export const TEXT_AVATAR_SIZE = 256;

/** At most this many characters (grapheme clusters) are drawn. */
export const TEXT_AVATAR_MAX_CHARS = 2;

/**
 * Background presets: the colour picker's presets for labels, custom statuses
 * and select options, in the same order, so a workspace icon draws from the
 * colours the app already offers.
 */
export const TEXT_AVATAR_COLORS: readonly string[] = PALETTE_SLOTS.map(
  (slot) => PALETTE_PRESETS[slot],
);

/** Starting background: the blue preset. */
export const TEXT_AVATAR_DEFAULT_COLOR = PALETTE_PRESETS.blue;

/**
 * Text colour for a background: dark or light, picked the same way a label
 * chip picks its text colour.
 */
export function textAvatarTextColor(background: string): string {
  return labelTextColor(background);
}

const FALLBACK_FONT_FAMILY =
  'system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif';

/** Splits into user-perceived characters, so "👍🏽" or "é" count as one. */
function graphemes(text: string): string[] {
  if (typeof Intl !== "undefined" && "Segmenter" in Intl) {
    const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
    return Array.from(segmenter.segment(text), (part) => part.segment);
  }
  return Array.from(text);
}

/**
 * The text that will actually be drawn: whitespace removed, cut to
 * {@link TEXT_AVATAR_MAX_CHARS} characters. Case is kept as typed. An empty
 * result means there is nothing to save.
 */
export function normalizeAvatarText(input: string): string {
  return graphemes(input.replace(/\s+/g, ""))
    .slice(0, TEXT_AVATAR_MAX_CHARS)
    .join("");
}

/** Starting text for a name: its first character, upper-cased. */
export function defaultAvatarText(name: string): string {
  return (graphemes(name.trim())[0] ?? "").toUpperCase();
}

/**
 * Font size as a fraction of the square side. Two characters get a smaller
 * size so wide CJK pairs still fit; the canvas renderer additionally shrinks
 * anything that would overflow. The live preview uses the same ratio.
 */
export function textAvatarFontRatio(text: string): number {
  return graphemes(text).length > 1 ? 0.42 : 0.52;
}

/** Widest the text may be, as a fraction of the square side. */
const MAX_TEXT_WIDTH_RATIO = 0.8;

function resolveFontFamily(): string {
  if (typeof document === "undefined" || !document.body) {
    return FALLBACK_FONT_FAMILY;
  }
  const family = getComputedStyle(document.body).fontFamily;
  return family ? `${family}, ${FALLBACK_FONT_FAMILY}` : FALLBACK_FONT_FAMILY;
}

function canvasToPng(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("Canvas is empty"))),
      "image/png",
    );
  });
}

/**
 * Draws `text` (normalized first) on `color`, in the text colour that reads
 * on it (see {@link textAvatarTextColor}), into a
 * {@link TEXT_AVATAR_SIZE}px square PNG file. Rejects when the text is empty.
 */
export async function renderTextAvatar(
  text: string,
  color: string,
): Promise<File> {
  const value = normalizeAvatarText(text);
  if (!value) throw new Error("Text icon needs at least one character");

  // Web fonts may still be loading on a cold settings page; drawing before
  // they are ready would bake the fallback font into the image.
  if (typeof document !== "undefined" && document.fonts?.ready) {
    await document.fonts.ready;
  }

  const size = TEXT_AVATAR_SIZE;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Canvas 2D context unavailable");

  ctx.fillStyle = color;
  ctx.fillRect(0, 0, size, size);

  const family = resolveFontFamily();
  let fontSize = Math.round(size * textAvatarFontRatio(value));
  ctx.font = `600 ${fontSize}px ${family}`;
  const maxWidth = size * MAX_TEXT_WIDTH_RATIO;
  const measuredWidth = ctx.measureText(value).width;
  if (measuredWidth > maxWidth) {
    fontSize = Math.floor((fontSize * maxWidth) / measuredWidth);
    ctx.font = `600 ${fontSize}px ${family}`;
  }

  ctx.fillStyle = textAvatarTextColor(color);
  ctx.textAlign = "center";
  // Center on the glyphs' real ink box rather than the em box, so caps,
  // digits and CJK all sit visually in the middle.
  const metrics = ctx.measureText(value);
  const ascent = metrics.actualBoundingBoxAscent;
  const descent = metrics.actualBoundingBoxDescent;
  if (Number.isFinite(ascent) && Number.isFinite(descent) && ascent + descent > 0) {
    ctx.textBaseline = "alphabetic";
    ctx.fillText(value, size / 2, size / 2 + (ascent - descent) / 2);
  } else {
    ctx.textBaseline = "middle";
    ctx.fillText(value, size / 2, size / 2);
  }

  const blob = await canvasToPng(canvas);
  return new File([blob], "workspace-icon.png", { type: "image/png" });
}
