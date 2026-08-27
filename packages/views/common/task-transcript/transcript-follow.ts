// Live-end follow latch, shared by the newest-first transcript (#5921) and the
// bottom-anchored chat list (chat/components/stick-to-bottom.ts).
//
// Both surfaces have the same problem: the system moves the viewport on its
// own (prepend anchoring in the transcript; streaming growth, composer
// resizes and the resulting scroll clamps in the chat list), so "am I at the
// live end right now" cannot distinguish a reader who scrolled away from a
// viewport the system displaced. This latch keeps that distinction:
//
// - Follow disengages only on accumulated USER displacement away from the
//   live end (wheel/touch/key deltas, or a scrollbar drag) beyond the edge
//   threshold. System displacement never counts, no matter how far it moves
//   the viewport.
// - While following, any non-user displacement is pinned straight back to
//   the live end (`onScroll` returns the verdict) — but never while the user
//   is mid-gesture or holding the mouse down (text selection autoscroll must
//   not be fought).
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

// How long after the last wheel/touch/key input the viewport is still treated
// as user-controlled (suppresses pinning mid-gesture, including momentum).
const INPUT_INTENT_WINDOW_MS = 300;

// WheelEvent.deltaMode 1 (lines) / 2 (pages) conversion.
export const LINE_SCROLL_PX = 40;

export interface LiveEndFollow {
  /** Whether the surface is live; everything is inert while inactive. */
  setActive(active: boolean): void;
  /** New list instance (task/sort/filter change): back to following. */
  reset(): void;
  isFollowing(): boolean;
  /** Explicit navigation away from the live end (e.g. segment click, Home key). */
  disengage(): void;
  /** User scroll input in px; positive = away from the live end. */
  input(delta: number): void;
  /** Mousedown inside the scroller; `onScroller` = on the element itself (scrollbar). */
  pointerDown(onScroller: boolean): void;
  pointerUp(): void;
  onAtEdgeChange(atEdge: boolean): void;
  /**
   * The viewport moved relative to the live end (scroll event, content or
   * viewport resize). Takes the current distance from the live end and
   * returns whether to pin the viewport back to it.
   */
  onScroll(distance: number): boolean;
}

export function createLiveEndFollow(now: () => number = () => Date.now()): LiveEndFollow {
  let active = false;
  let following = true;
  // Accumulated user-caused displacement away from the live end. Compared
  // against the edge threshold instead of absolute position: position mixes
  // user and system displacement (a system shift can land inside the intent
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
    onAtEdgeChange(atEdge: boolean) {
      if (!active || !atEdge) return;
      following = true;
      pendingAway = 0;
    },
    onScroll(distance: number): boolean {
      if (!active) return false;
      // A scrollbar drag is fully user-controlled: absolute position is the
      // user's displacement, so the plain threshold applies.
      if (scrollbarDrag && distance > FOLLOW_EDGE_THRESHOLD) {
        following = false;
        return false;
      }
      if (following && !userControlsViewport() && distance > 0) {
        // System displacement got corrected; drop any sub-threshold residue
        // so old nudges don't accumulate into a spurious disengage later.
        pendingAway = 0;
        return true;
      }
      return false;
    },
  };
}
