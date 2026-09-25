// Live-end follow latch, shared by the newest-first transcript (#5921) and the
// bottom-anchored chat list (chat/components/stick-to-bottom.ts).
//
// Both surfaces have the same problem: the system moves the viewport on its
// own (prepend anchoring in the transcript; streaming growth, composer
// resizes and the resulting scroll clamps in the chat list), so "am I at the
// live end right now" cannot distinguish a reader who scrolled away from a
// viewport the system displaced. This latch keeps that distinction:
//
// - Input alone never releases. It opens a claim for the current rendering
//   frame, and only the surface's own scroll can confirm reader-driven motion.
//   The wiring discards unused claims after that frame, so a later system shift
//   cannot inherit input consumed by a nested scroller.
// - Confirmed motion uses the surface's displacement, not raw input magnitude.
//   Continued same-direction movement inside the settle window remains reader
//   motion, including touch momentum and fractional drag frames.
// - Attributed displacement ACCUMULATES until the reader returns to the live
//   end; it does not expire with the gesture and pins do not erase it. A
//   trackpad flick and five discrete wheel notches at reading pace both cross
//   the same threshold, and repeated sub-threshold attempts against a fast
//   stream eventually win instead of being re-pinned forever.
// - While following, displacement not attributed to the reader is pinned
//   straight back to the live end (`onScroll` / `onResize` return the
//   verdict) — immediately, with no intent-window timer for a final event to
//   hide behind — but never during an active touch or while the mouse is held
//   down (text selection autoscroll must not be fought).
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

// Maximum gap between an input and its confirming scroll, or between scrolls
// in confirmed motion. DOM wiring normally clears unconfirmed input sooner,
// after the next animation frame.
const MOTION_SETTLE_WINDOW_MS = 300;

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
  /** Discards input the surface did not consume during its rendering frame. */
  endInputFrame(): void;
  /** An active touch prevents pinning while drag displacement is confirmed. */
  touchStart(): void;
  /** Ends the held state; confirmed momentum may continue within the settle window. */
  touchEnd(): void;
  /** Mousedown inside the scroller; `onScroller` = on the element itself (scrollbar). */
  pointerDown(onScroller: boolean): void;
  pointerUp(): void;
  onAtEdgeChange(atEdge: boolean): void;
  /**
   * The surface itself scrolled. Confirms staged input or continues recent
   * reader motion; returns whether to pin the viewport back to the live end.
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
  // Unspent input claims for the current frame, split by direction. A claim
  // becomes reader motion only when the surface confirms matching movement.
  let awayBudget = 0;
  let towardBudget = 0;
  let lastInputAt = -MOTION_SETTLE_WINDOW_MS;
  // Displacement the reader actually took, confirmed scroll by scroll. Does
  // not expire and survives pins: repeated sub-threshold attempts against a
  // fast stream accumulate into a release instead of losing every round.
  let awayTaken = 0;
  let lastDistance = 0;
  let mouseHeld = false;
  let scrollbarDrag = false;
  let touchHeld = false;
  let motionDirection: -1 | 0 | 1 = 0;
  let lastMotionAt = -MOTION_SETTLE_WINDOW_MS;
  // What set the current motion running, and how much of the claim behind it
  // the surface has not spent yet. Touch coasts past its claim (momentum is
  // not proportional to finger travel); everything else may only continue
  // for what the reader actually asked for, so a system shift arriving inside
  // the window has nothing to draw on.
  let motionSource: "touch" | "input" | null = null;
  let motionCarry = 0;

  const inputFresh = () => now() - lastInputAt < MOTION_SETTLE_WINDOW_MS;
  const motionFresh = () => now() - lastMotionAt < MOTION_SETTLE_WINDOW_MS;

  // Carry is spent in fractional steps, so its running total drifts from the
  // surface's own arithmetic by a few ulps. Scaled to the amount at hand, a
  // gap this small is rounding — the system moving the viewport is pixels.
  const withinCarry = (amount: number) =>
    amount <= motionCarry + Math.max(1, motionCarry) * 1e-9;

  const pinVerdict = (distance: number): boolean =>
    following && !mouseHeld && !scrollbarDrag && !touchHeld && distance > 0;

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
      touchHeld = false;
      motionDirection = 0;
      motionSource = null;
      motionCarry = 0;
      lastMotionAt = -MOTION_SETTLE_WINDOW_MS;
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
    endInputFrame() {
      awayBudget = 0;
      towardBudget = 0;
    },
    touchStart() {
      if (active) touchHeld = true;
    },
    touchEnd() {
      touchHeld = false;
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
      motionDirection = 0;
      motionSource = null;
      motionCarry = 0;
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
      // Motion already confirmed in this direction continues without a new
      // claim. Touch coasts freely — momentum is not proportional to finger
      // travel. Every other source is capped by the claim the surface has not
      // spent yet, so a system shift landing inside the window (a transcript
      // prepend) finds nothing to draw on and falls through to the pin.
      if (moved > 0 && motionDirection === 1 && (touchHeld || motionFresh())) {
        // A notch arriving mid-animation is confirmed by this same-direction
        // scroll, so fold it in: the frame that ends the claim would otherwise
        // discard it and pin the rest of the reader's own scroll back.
        if (inputFresh() && awayBudget > 0) {
          motionCarry += awayBudget;
          awayBudget = 0;
        }
        if (motionSource === "touch" || withinCarry(moved)) {
          if (motionSource !== "touch") motionCarry = Math.max(0, motionCarry - moved);
          awayTaken += moved;
          lastMotionAt = now();
          if (following && awayTaken > FOLLOW_EDGE_THRESHOLD) following = false;
          return false;
        }
      } else if (moved < 0 && motionDirection === -1 && (touchHeld || motionFresh())) {
        if (inputFresh() && towardBudget > 0) {
          motionCarry += towardBudget;
          towardBudget = 0;
        }
        if (motionSource === "touch" || withinCarry(-moved)) {
          if (motionSource !== "touch") motionCarry = Math.max(0, motionCarry + moved);
          awayTaken = Math.max(0, awayTaken + moved);
          lastMotionAt = now();
          if (distance === 0) {
            following = true;
            awayBudget = 0;
            towardBudget = 0;
            awayTaken = 0;
            motionDirection = 0;
            motionSource = null;
            motionCarry = 0;
          }
          return false;
        }
      }
      if (moved > 0 && inputFresh() && awayBudget > 0) {
        const attributed = Math.min(awayBudget, moved);
        awayBudget -= attributed;
        // Touch displacement need not match finger travel pixel-for-pixel.
        if (touchHeld || attributed === moved) {
          awayTaken += moved;
          motionDirection = 1;
          motionSource = touchHeld ? "touch" : "input";
          // One notch can animate across several frames; the wiring drops the
          // claim after the first, so carry its remainder into the motion.
          motionCarry = awayBudget;
          lastMotionAt = now();
          if (following && awayTaken > FOLLOW_EDGE_THRESHOLD) following = false;
          return false;
        }
      } else if (moved < 0 && inputFresh() && towardBudget > 0) {
        const attributed = Math.min(towardBudget, -moved);
        towardBudget -= attributed;
        if (touchHeld || attributed === -moved) {
          awayTaken = Math.max(0, awayTaken + moved);
          motionDirection = -1;
          motionSource = touchHeld ? "touch" : "input";
          motionCarry = towardBudget;
          lastMotionAt = now();
          if (distance === 0) {
            following = true;
            awayBudget = 0;
            towardBudget = 0;
            awayTaken = 0;
            motionDirection = 0;
          }
          return false;
        }
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
