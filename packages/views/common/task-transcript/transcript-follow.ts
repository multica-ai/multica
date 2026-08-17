// Live-follow latch for the newest-first transcript (#5921).
//
// Newest-first shows live events as PREPENDS, and the list's firstItemIndex
// anchoring holds the viewport across a prepend — which means every flush
// moves the viewport away from the top on its own. "Am I at the top right
// now" therefore cannot distinguish a reader who scrolled away from one the
// anchoring pushed down. This controller keeps that distinction:
//
// - Follow disengages only on accumulated USER displacement away from the
//   live end (wheel/touch/key deltas, or a scrollbar drag) beyond the edge
//   threshold. System displacement never counts, no matter how far it moves
//   the viewport.
// - While following, any non-user displacement is pinned straight back to
//   the live end (`pin` from onScroll) — but never while the user is
//   mid-gesture or holding the mouse down (text selection autoscroll must
//   not be fought).
// - Arriving back within the edge zone (atTop) re-engages the follow.
//
// Pure state machine so the decision table is unit-testable; the dialog owns
// wiring it to DOM events.

// Forgiving "at the live end" zone, matching the chat list's atBottomThreshold:
// within this distance of the live edge the reader counts as following.
export const FOLLOW_EDGE_THRESHOLD = 120;

// How long after the last wheel/touch/key input the viewport is still treated
// as user-controlled (suppresses pinning mid-gesture, including momentum).
const INPUT_INTENT_WINDOW_MS = 300;

// WheelEvent.deltaMode 1 (lines) / 2 (pages) conversion.
export const LINE_SCROLL_PX = 40;

export interface NewestFirstFollow {
  /** `isLive && sortDirection === "newest_first"`; everything is inert otherwise. */
  setActive(active: boolean): void;
  /** New list instance (task/sort/filter change): back to following. */
  reset(): void;
  isFollowing(): boolean;
  /** Explicit navigation away from the live end (e.g. timeline segment click). */
  disengage(): void;
  /** User scroll input in px; positive = away from the live end. */
  input(delta: number): void;
  /** Mousedown inside the scroller; `onScroller` = on the element itself (scrollbar). */
  pointerDown(onScroller: boolean): void;
  pointerUp(): void;
  onAtTopChange(atTop: boolean): void;
  /** Every scroll event. Returns whether to pin the viewport back to the top. */
  onScroll(scrollTop: number): boolean;
}

export function createNewestFirstFollow(now: () => number = () => Date.now()): NewestFirstFollow {
  let active = false;
  let following = true;
  // Accumulated user-caused displacement away from the live end. Compared
  // against the edge threshold instead of scrollTop: absolute position mixes
  // user and system displacement (a prepend can land inside the intent
  // window and push the viewport past any threshold on its own).
  let pendingAway = 0;
  let lastInputAt = -INPUT_INTENT_WINDOW_MS;
  let mouseHeld = false;
  let scrollbarDrag = false;

  const userControlsViewport = () =>
    mouseHeld || scrollbarDrag || now() - lastInputAt < INPUT_INTENT_WINDOW_MS;

  return {
    setActive(a: boolean) {
      active = a;
    },
    reset() {
      following = true;
      pendingAway = 0;
      mouseHeld = false;
      scrollbarDrag = false;
    },
    isFollowing: () => active && following,
    disengage() {
      if (active) following = false;
    },
    input(delta: number) {
      if (!active) return;
      lastInputAt = now();
      pendingAway = Math.max(0, pendingAway + delta);
      if (following && pendingAway > FOLLOW_EDGE_THRESHOLD) following = false;
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
    onAtTopChange(atTop: boolean) {
      if (!active || !atTop) return;
      following = true;
      pendingAway = 0;
    },
    onScroll(scrollTop: number): boolean {
      if (!active) return false;
      // A scrollbar drag is fully user-controlled: absolute position is the
      // user's displacement, so the plain threshold applies.
      if (scrollbarDrag && scrollTop > FOLLOW_EDGE_THRESHOLD) {
        following = false;
        return false;
      }
      if (following && !userControlsViewport() && scrollTop > 0) {
        // System displacement got corrected; drop any sub-threshold residue
        // so old nudges don't accumulate into a spurious disengage later.
        pendingAway = 0;
        return true;
      }
      return false;
    },
  };
}
