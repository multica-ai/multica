// Live-end follow latch, shared by the newest-first transcript (#5921) and the
// bottom-anchored chat list (chat/components/stick-to-bottom.ts).
//
// Both surfaces have the same problem: the system moves the viewport on its
// own (prepend anchoring in the transcript; streaming growth, composer
// resizes and the resulting scroll clamps in the chat list), so "am I at the
// live end right now" cannot distinguish a reader who scrolled away from a
// viewport the system displaced. This latch keeps that distinction:
//
// - Input alone never releases: it only opens a BUDGET, and the surface's own
//   scroll spends it. Each scroll attributes the displacement the surface
//   actually took to the reader, capped by what they pushed — so input the
//   surface never consumed (a wheel over a nested scroller, a flick on a list
//   too short to scroll, a key a control swallowed) attributes nothing, and
//   system displacement landing inside a gesture attributes at most the
//   gesture's own size.
// - Attributed displacement ACCUMULATES until the reader returns to the live
//   end; it does not expire with the gesture and pins do not erase it. A
//   trackpad flick and five discrete wheel notches at reading pace both cross
//   the same threshold, and repeated sub-threshold attempts against a fast
//   stream eventually win instead of being re-pinned forever.
// - While following, displacement not attributed to the reader is pinned
//   straight back to the live end (`onScroll` / `onResize` return the
//   verdict) — immediately, with no intent-window timer for a final event to
//   hide behind — but never while the mouse is held down (text selection
//   autoscroll must not be fought).
// - Arriving back within the edge zone re-engages the follow.
//
// The state machine is direction-agnostic: callers feed it away-positive
// input deltas and the current distance from THEIR live end (`scrollTop` for
// the newest-first transcript, distance-from-bottom for the chat list).
//
// Pure state machine so the decision table is unit-testable; each surface
// owns wiring it to DOM events.

// Forgiving "at the live end" zone: within this distance of the live edge the
// reader counts as following. The chat list also passes this to Virtuoso as
// `atBottomThreshold`, so both judges of "at the bottom" agree.
export const FOLLOW_EDGE_THRESHOLD = 120;

// How long after the last input its unspent budget is still attributable to
// the same gesture. Only budgets expire with this window — attributed
// displacement and pin decisions do not depend on it.
const INPUT_INTENT_WINDOW_MS = 300;

// WheelEvent.deltaMode 1 (lines) / 2 (pages) conversion.
export const LINE_SCROLL_PX = 40;

export interface LiveEndFollow {
  /** Whether the surface is live; everything is inert while inactive. */
  setActive(active: boolean): void;
  /** New list instance (task/sort/filter change): back to following. */
  reset(): void;
  isFollowing(): boolean;
  /** Explicit navigation away from the live end (e.g. segment click). */
  disengage(): void;
  /**
   * User scroll input in px; positive = away from the live end. Opens a
   * budget the surface's own scroll can attribute displacement against.
   */
  input(delta: number): void;
  /** Mousedown inside the scroller; `onScroller` = on the element itself (scrollbar). */
  pointerDown(onScroller: boolean): void;
  pointerUp(): void;
  onAtEdgeChange(atEdge: boolean): void;
  /**
   * The surface itself scrolled. Attributes the observed displacement to the
   * reader up to their input budget, releasing past the threshold; returns
   * whether to pin the viewport back to the live end.
   */
  onScroll(distance: number): boolean;
  /**
   * The live end moved without a scroll (content grew, viewport resized).
   * Never attributes displacement to the reader; returns whether to pin back.
   */
  onResize(distance: number): boolean;
}

export function createLiveEndFollow(now: () => number = () => Date.now()): LiveEndFollow {
  let active = false;
  let following = true;
  // Unspent input budgets for the current gesture, by direction. A budget is
  // a claim, not displacement: it converts to `awayTaken` only as the
  // surface's scrolls confirm movement, and expires with its gesture.
  let awayBudget = 0;
  let towardBudget = 0;
  let lastInputAt = -INPUT_INTENT_WINDOW_MS;
  // Displacement the reader actually took, confirmed scroll by scroll. Does
  // not expire and survives pins: repeated sub-threshold attempts against a
  // fast stream accumulate into a release instead of losing every round.
  let awayTaken = 0;
  let lastDistance = 0;
  let mouseHeld = false;
  let scrollbarDrag = false;

  const inputFresh = () => now() - lastInputAt < INPUT_INTENT_WINDOW_MS;

  const pinVerdict = (distance: number): boolean =>
    following && !mouseHeld && !scrollbarDrag && distance > 0;

  return {
    setActive(a: boolean) {
      active = a;
    },
    reset() {
      following = true;
      awayBudget = 0;
      towardBudget = 0;
      awayTaken = 0;
      lastDistance = 0;
      mouseHeld = false;
      scrollbarDrag = false;
    },
    isFollowing: () => active && following,
    disengage() {
      if (active) following = false;
    },
    input(delta: number) {
      if (!active) return;
      // A fresh gesture opens fresh budgets: a claim the surface never
      // honored must not linger into a later gesture.
      if (!inputFresh()) {
        awayBudget = 0;
        towardBudget = 0;
      }
      lastInputAt = now();
      if (delta > 0) awayBudget += delta;
      else towardBudget -= delta;
    },
    pointerDown(onScroller: boolean) {
      if (!active) return;
      mouseHeld = true;
      if (onScroller) scrollbarDrag = true;
    },
    pointerUp() {
      mouseHeld = false;
      scrollbarDrag = false;
    },
    onAtEdgeChange(atEdge: boolean) {
      if (!active || !atEdge) return;
      following = true;
      awayBudget = 0;
      towardBudget = 0;
      awayTaken = 0;
    },
    onScroll(distance: number): boolean {
      if (!active) return false;
      const moved = distance - lastDistance;
      lastDistance = distance;
      // A scrollbar drag is fully user-controlled: absolute position is the
      // user's displacement, so the plain threshold applies.
      if (scrollbarDrag) {
        if (distance > FOLLOW_EDGE_THRESHOLD) following = false;
        return false;
      }
      if (moved > 0 && inputFresh() && awayBudget > 0) {
        // The reader pushed and the surface moved: attribute the smaller of
        // the two. A system shift landing inside the gesture (a transcript
        // prepend) can inflate `moved` past any threshold, so the budget cap
        // is what keeps it from ever counting as reader intent.
        const attributed = Math.min(awayBudget, moved);
        awayBudget -= attributed;
        awayTaken += attributed;
        if (following && awayTaken > FOLLOW_EDGE_THRESHOLD) {
          following = false;
          return false;
        }
        // Wholly the reader's own move: leave them be. Any unattributed
        // remainder is the system's and falls through to the pin.
        if (attributed === moved) return false;
      } else if (moved < 0 && inputFresh() && towardBudget > 0) {
        const attributed = Math.min(towardBudget, -moved);
        towardBudget -= attributed;
        awayTaken = Math.max(0, awayTaken - attributed);
        // The reader walked themselves all the way back to the live end.
        if (distance === 0) {
          following = true;
          awayBudget = 0;
          towardBudget = 0;
          awayTaken = 0;
        }
        return false;
      }
      return pinVerdict(distance);
    },
    onResize(distance: number): boolean {
      if (!active) return false;
      if (pinVerdict(distance)) {
        // The caller pins to the live end; record the destination so the
        // pin's own scroll (or its absence under test) reads as no movement.
        lastDistance = 0;
        return true;
      }
      lastDistance = distance;
      return false;
    },
  };
}
