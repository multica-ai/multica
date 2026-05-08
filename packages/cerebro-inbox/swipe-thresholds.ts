/**
 * Tunable parameters for the cerebro inbox swipe gesture.
 *
 * Iteration log:
 *  - v1: fixed 80px commit (~20% of 414px row) — too sensitive; drift during
 *        a vertical scroll could fire archive.
 *  - v2: 30% commit + 12px deadzone + tight direction-lock — too restrictive
 *        in the other direction. The deadzone made the row stop tracking the
 *        finger early ("kan kun swipe lidt"), and the direction-lock often
 *        bailed to vertical when natural finger arc made dy briefly exceed
 *        dx in the first 8px.
 *  - v3 (current): match Gmail's mobile pattern — row follows the finger
 *        directly with no deadzone, and a "full swipe" (50% of row width)
 *        commits the action on release. Direction-lock only bails when
 *        vertical motion is clearly dominant (dy > 1.5 × dx).
 *
 * Sources:
 *  - Gmail mobile inbox swipe (Android/iOS): row tracks finger across full
 *    row, commit on release at ~50%.
 *  - Material 3 swipe-to-dismiss: positional threshold 25–30% (we sit
 *    slightly above so destructive archives need real intent).
 *  - iOS Mail swipe: row tracks finger; "full swipe" commits at >50% width.
 */

/** Commit threshold as a fraction of row width — full-ish swipe like Gmail. */
export const SWIPE_COMMIT_FRACTION = 0.5;
export const SWIPE_COMMIT_MIN_PX = 120;
export const SWIPE_COMMIT_MAX_PX = 260;

/**
 * Total movement (Pythagorean) before we decide which axis dominates. Below
 * this, neither axis is locked — we wait for clearer intent. The number is
 * small (8 px) so the row begins tracking the finger almost immediately
 * once the user is clearly swiping.
 */
export const DIRECTION_DECIDE_PX = 8;

/**
 * Vertical-dominance ratio. We only bail to vertical scroll if |dy| is at
 * least this many times |dx|. A pure horizontal swipe naturally has small
 * dy from finger arc; the previous "any dy>dx" check let arc-y dominance
 * win on the first sample and stuck the row on screen. 1.5× lets a swipe
 * stay horizontal even with mild drift.
 */
export const VERTICAL_DOMINANCE_RATIO = 1.5;

/** Long-press duration before the action drawer opens. */
export const LONG_PRESS_MS = 500;

/** Width (px) of the swipe-left action panel once it locks open. */
export const LEFT_PANEL_REVEAL_PX = 144;

/**
 * Commit threshold (px) needed to fire the archive / panel-reveal action
 * for a row of the given width. 50% of the row, clamped to [120, 260] px
 * so the gesture is meaningful on small phones and doesn't require
 * crossing the entire screen on tablets.
 *
 *   iPhone SE (320px) -> 160 px (50%)
 *   iPhone Pro (414px) -> 207 px (50%)
 *   iPhone 16 Pro Max (430px) -> 215 px (50%)
 *   iPad split-view (~600px) -> 260 px (43%, hits clamp cap)
 */
export function commitThresholdPx(rowWidth: number): number {
  const target = rowWidth * SWIPE_COMMIT_FRACTION;
  return Math.max(SWIPE_COMMIT_MIN_PX, Math.min(target, SWIPE_COMMIT_MAX_PX));
}
