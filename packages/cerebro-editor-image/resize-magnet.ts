// Inline-image resize sizing (FIR-4699 Phase 5). Width is a percentage of the
// text column, never pixels — the Note edit design makes a narrow pane a
// first-class state, so a pixel width overflows the moment the comment rail
// opens. This module is the pure sizing brain the drag handles call; the DOM
// wiring lives in the node view.

/** The magnet stops, as a percentage of the text column. */
export const WIDTH_STOPS = [25, 50, 75, 100] as const;

/** Smallest inline width we let a drag settle on, so an image can't vanish. */
const MIN_WIDTH_PCT = 10;

/** Magnet radius in pixels — a handle within this of a stop snaps to it. */
const MAGNET_PX = 12;

/**
 * Snap a raw drag width (percent of the column) to a magnet stop when the
 * handle is within {@link MAGNET_PX} of it, otherwise keep the free-drag value.
 * The magnet is measured in pixels, so it widens (in percent) as the column
 * narrows — 12px is 2% on a 600px column but 6% on a 200px one. Result is a
 * whole percentage clamped to [{@link MIN_WIDTH_PCT}, 100].
 */
export function snapWidthPct(rawPct: number, columnWidthPx: number): number {
  const magnetPct = columnWidthPx > 0 ? (MAGNET_PX / columnWidthPx) * 100 : 0;
  const snapped = WIDTH_STOPS.find(
    (stop) => Math.abs(rawPct - stop) <= magnetPct,
  );
  const value = snapped ?? rawPct;
  return Math.min(100, Math.max(MIN_WIDTH_PCT, Math.round(value)));
}
