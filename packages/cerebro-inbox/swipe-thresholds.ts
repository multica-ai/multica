/**
 * Tunable parameters for the cerebro inbox swipe gesture.
 *
 * The previous fixed 80px commit was ~20% of a 414px row — too sensitive
 * (small horizontal drift during a vertical scroll could fire archive).
 *
 * Sources:
 *  - Material 3 / Jetpack Compose `FractionalThreshold(0.3f)` — 30% of width
 *  - Material 3 swipe-to-dismiss positional threshold — 25%
 *  - iOS Mail "tap action" zone — ~30% of row
 *  - Wear OS swipe-to-dismiss edge — 20% (different context)
 *
 * Settled on 30% with a [100, 180] px clamp so the gesture stays consistent
 * across phone widths (iPhone SE 320px → 100px, iPhone 16 Pro Max 430px →
 * 129px, iPad split-view 600px row → 180px cap).
 */
export const SWIPE_COMMIT_FRACTION = 0.3;
export const SWIPE_COMMIT_MIN_PX = 100;
export const SWIPE_COMMIT_MAX_PX = 180;

/** Pixels of finger travel before the row visibly starts moving. */
export const HORIZONTAL_DEADZONE_PX = 12;

/** Movement past this in either axis triggers the direction lock. */
export const DIRECTION_DECIDE_PX = 8;

/**
 * Vertical-axis bail: if movement is dy-dominant AND past this, hand the
 * gesture back to the browser for native scroll. Stricter than the previous
 * |dy| > 24 (which allowed sloppy near-horizontal drifts to fire archive).
 */
export const VERTICAL_BAIL_PX = 10;

/** Long-press duration before the action drawer opens. */
export const LONG_PRESS_MS = 500;

/** Width (px) of the swipe-left action panel once it locks open. */
export const LEFT_PANEL_REVEAL_PX = 144;

/**
 * Returns the commit threshold (in px of horizontal travel) needed to fire
 * the archive / panel-reveal action for a row of the given width. Clamped
 * so the gesture isn't trivially short on small phones or punishingly long
 * on tablets.
 */
export function commitThresholdPx(rowWidth: number): number {
  const target = rowWidth * SWIPE_COMMIT_FRACTION;
  return Math.max(SWIPE_COMMIT_MIN_PX, Math.min(target, SWIPE_COMMIT_MAX_PX));
}
