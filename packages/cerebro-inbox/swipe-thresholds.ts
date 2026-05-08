/**
 * Tunable parameters for the cerebro inbox swipe gesture.
 *
 * Iteration log:
 *  - v1: fixed 80px commit (~20% of 414px row) — too sensitive; horizontal
 *        drift during a vertical scroll fired archive accidentally.
 *  - v2: 30% commit + 12px deadzone + tight direction-lock — too restrictive;
 *        the deadzone made the row stop tracking the finger, and the lock
 *        bailed to vertical when natural finger arc made dy briefly exceed
 *        dx in the first 8 px ("kan kun swipe lidt").
 *  - v3: 50% commit, no deadzone, dominance ratio 1.5× — Gmail-style on
 *        paper, but iOS Safari hijacked the gesture: PointerEvents stopped
 *        firing when the browser committed to native vertical scroll mid-
 *        gesture, giving an "on/off" feel where the row sometimes tracked
 *        and sometimes didn't.
 *  - v4 (current): native TouchEvent listeners with `{ passive: false }` so
 *        we can `preventDefault()` once the gesture is horizontal — the
 *        textbook fix for iOS Safari's pointer-handoff. Commit dropped to
 *        35% to match Gmail's actual feel; click is also suppressed on any
 *        meaningful swipe attempt (>16 px), not just on commit, so a tiny
 *        swipe that springs back doesn't navigate the user into the issue.
 *
 * Sources:
 *  - "Pointer events vs touch events for swipe" — react-swipeable's README
 *    enumerates the same iOS hijack we hit and uses passive:false touch.
 *  - Gmail mobile: row tracks finger; archive commits at ~30–40% width.
 *  - Material 3 swipe-to-dismiss positional threshold — 25–30%.
 */

export const SWIPE_COMMIT_FRACTION = 0.35;
export const SWIPE_COMMIT_MIN_PX = 80;
export const SWIPE_COMMIT_MAX_PX = 200;

/** Pixels of finger travel that count as "the user tried to swipe" — used
 * to suppress the synthetic click after touchend even when the swipe didn't
 * cross the commit threshold. Without this, a tiny swipe that springs back
 * would still trigger the row's onClick and navigate the user away. */
export const SWIPE_INTENT_PX = 16;

/** Total movement before we decide which axis dominates. Below this, we
 * wait — the very first sample of a touch is noisy. */
export const DIRECTION_DECIDE_PX = 8;

/** Long-press duration before the action drawer opens. */
export const LONG_PRESS_MS = 500;

/** Width (px) of the swipe-left action panel once it locks open. */
export const LEFT_PANEL_REVEAL_PX = 144;

/**
 * Commit threshold (px) needed to fire archive / panel-reveal for a row of
 * the given width. 35% with a [80, 200] clamp:
 *
 *   iPhone SE (320 px) → 112 px (35%)
 *   iPhone Pro (414 px) → 145 px (35%)
 *   iPhone 16 Pro Max (430 px) → 151 px (35%)
 *   iPad split-view (~600 px) → 200 px (clamp cap, ~33%)
 */
export function commitThresholdPx(rowWidth: number): number {
  const target = rowWidth * SWIPE_COMMIT_FRACTION;
  return Math.max(SWIPE_COMMIT_MIN_PX, Math.min(target, SWIPE_COMMIT_MAX_PX));
}
