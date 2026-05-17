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
 *  - v4: native TouchEvent listeners with `{ passive: false }` so we can
 *        `preventDefault()` once the gesture is horizontal — the textbook
 *        fix for iOS Safari's pointer-handoff. Commit dropped to 35% to
 *        match Gmail's actual feel; click is also suppressed on any
 *        meaningful swipe attempt (>16 px), not just on commit, so a tiny
 *        swipe that springs back doesn't navigate the user into the issue.
 *  - v5: timestamp-based click suppression. v4 used a boolean
 *        "swipeJustCompleted" flag reset by the synthetic click — but iOS
 *        Safari doesn't fire a click after a real drag, so the flag stayed
 *        stuck and swallowed the user's NEXT tap on the reveal panel
 *        (mute / mark-unread "did nothing"). Replaced with a timestamp
 *        window so the flag self-clears after `POST_SWIPE_CLICK_SUPPRESS_MS`
 *        regardless of whether the synthetic click ever fires.
 *  - v6: archive is hold-to-commit while the finger is still down.
 *        Release no longer leaves a 1s "stuck" destructive state. The user
 *        must hold beyond the threshold briefly, and moving back under the
 *        threshold cancels before release.
 *  - v7 (current): full-flick instant archive. A swipe covering ≥95% of the
 *        row width commits on release without any hold delay — the intent is
 *        unambiguous at that distance. Hold-to-commit still applies for
 *        shorter swipes beyond the 35% threshold.
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

/** Milliseconds after a swipe ends during which we still suppress the
 * synthetic click on the row. iOS Safari only fires a synthetic click for
 * tap-like touch sequences — once a touch has enough movement to count as
 * a drag, no click is dispatched. The previous boolean "did we just swipe"
 * flag relied on the click firing to reset itself, so when no click ever
 * arrived (the common case after a real swipe) the flag stayed stuck and
 * swallowed the user's NEXT real tap — e.g. on the swipe-left reveal
 * panel's mark-unread / mute buttons. A timestamp window lets the flag
 * age out on its own, making tap-after-swipe reliable. */
export const POST_SWIPE_CLICK_SUPPRESS_MS = 350;

/** Time the swipe-right gesture must stay past the archive threshold before
 * release can commit it. Moving back under the threshold cancels the hold. */
export const ARCHIVE_HOLD_COMMIT_MS = 450;

/** Snap-back / reveal transition duration. Slightly slower than the previous
 * 200ms so swipe-to-archive feels less abrupt. */
export const SWIPE_ROW_TRANSITION_MS = 320;

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

export function shouldCommitHeldSwipe(
  offsetX: number,
  rowWidth: number,
  holdReady: boolean,
): boolean {
  return holdReady && offsetX >= commitThresholdPx(rowWidth);
}

/** Fraction of row width that triggers instant archive on release
 * without requiring a hold — the gesture is unambiguous at this distance. */
export const SWIPE_INSTANT_ARCHIVE_FRACTION = 0.95;

/** Returns true when the user has swiped far enough that archive should
 * commit immediately on release, bypassing the hold-to-commit timer. */
export function shouldInstantArchive(offsetX: number, rowWidth: number): boolean {
  return offsetX >= rowWidth * SWIPE_INSTANT_ARCHIVE_FRACTION;
}
