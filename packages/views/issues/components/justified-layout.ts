/**
 * Justified rows for a set of images (MUL-7649): every image in a row shares
 * one height and a full row spans the container exactly — the album layout,
 * instead of fixed-height tiles that wrap and leave a ragged edge.
 *
 * Rows take as many images as fit without dropping below `minHeight`, so a
 * comment's handful of screenshots stays one tidy row even in a narrow
 * column. No row grows past `maxHeight` (two screenshots in a wide column
 * stop short of the edge rather than turning huge), and a final row that
 * does not fill keeps the height of the row above it, never larger.
 */

export interface JustifiedRow {
  /** Indexes into the input, in order. */
  items: number[];
  height: number;
}

export interface JustifyOptions {
  gap: number;
  minHeight: number;
  maxHeight: number;
}

// Past these, an image would claim an absurd share of the row (a panorama)
// or a sliver of it (a phone-length screenshot); it is cropped to fit instead.
const MIN_RATIO = 0.5;
const MAX_RATIO = 3;

export function clampRatio(ratio: number): number {
  if (!Number.isFinite(ratio) || ratio <= 0) return 1;
  return Math.min(MAX_RATIO, Math.max(MIN_RATIO, ratio));
}

export function justifyRows(
  ratios: ReadonlyArray<number>,
  width: number,
  { gap, minHeight, maxHeight }: JustifyOptions,
): JustifiedRow[] {
  if (ratios.length === 0) return [];
  if (width <= 0) {
    return [{ items: ratios.map((_, i) => i), height: maxHeight }];
  }
  // A pixel of slack keeps sub-pixel rounding from wrapping the last image.
  const heightFor = (count: number, sum: number) =>
    (width - gap * (count - 1) - 1) / sum;

  const rows: JustifiedRow[] = [];
  let items: number[] = [];
  let ratioSum = 0;
  ratios.forEach((raw, index) => {
    const ratio = clampRatio(raw);
    if (items.length > 0 && heightFor(items.length + 1, ratioSum + ratio) < minHeight) {
      rows.push({
        items,
        height: Math.min(maxHeight, heightFor(items.length, ratioSum)),
      });
      items = [];
      ratioSum = 0;
    }
    items.push(index);
    ratioSum += ratio;
  });

  const above = rows.length > 0 ? rows[rows.length - 1]!.height : maxHeight;
  rows.push({
    items,
    height: Math.min(maxHeight, above, heightFor(items.length, ratioSum)),
  });
  return rows;
}
