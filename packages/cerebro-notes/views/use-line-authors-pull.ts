"use client";

// FIR-2810: temporary reveal of the line-authors gutter, Apple Notes-style.
// Desktop: grab the thin handle at the left edge of the note body and drag
// right. Mobile: press and pull the note body to the right (vertical
// scrolling stays native via touch-action: pan-y). While pulling, the body
// slides right and the gutter fades in underneath.
//
// Drag-then-hold (Jesper, 2026-07-11 round 4): the gesture is push, drag,
// HOLD — the pull arms on horizontal drag intent (no pre-hold, so taps,
// scrolling and the long-press selection toolbar all behave natively), and
// holding the finger still at the pulled-open position latches the gutter
// open. Releasing before the hold completes springs back. A click/tap on the
// revealed gutter field closes it — there is no pull-to-close gesture. On
// desktop the strip drag latches on release past the threshold, and the same
// click-to-close applies. The permanent view stays available as the
// "Line authors" toggle in the ⋯ menu.

import * as React from "react";

// The width the temporary reveal slides open to — with the gutter text
// left-aligned this is just enough to read the author codes ("JEH · MOP")
// and time (Jesper, 2026-07-11 round 4: only far enough to see who edited).
export const PULL_WIDTH = 56;

// Touch travel is amplified so the gutter opens well before the finger has
// crossed a full gutter-width of screen (~35px of travel opens it fully).
export const TOUCH_GAIN = 1.6;

// Horizontal finger travel that arms the touch pull. Below this the gesture
// is undecided; vertical-dominant movement past it hands over to scrolling.
export const INTENT_SLOP = 12;

// Holding the finger still at the pulled position this long latches open.
export const LATCH_HOLD_MS = 400;

// Finger jitter allowed while holding; more restarts the hold.
export const HOLD_SLOP = 10;

// The pull must be at least this open for a hold (touch) or a release
// (desktop strip) to latch it.
export const OPEN_THRESHOLD = PULL_WIDTH * 0.4;

// appliedOffset maps a raw pointer movement (dx, scaled by gain) to the
// applied slide offset: clamped at 0, following the pointer up to PULL_WIDTH,
// then rubber-banding (every extra pixel counts 20%) so a long pull feels
// anchored.
export function appliedOffset(dx: number, gain: number): number {
  const raw = dx * gain;
  if (raw <= 0) return 0;
  if (raw <= PULL_WIDTH) return raw;
  return PULL_WIDTH + (raw - PULL_WIDTH) * 0.2;
}

// dragIntent classifies early finger movement: a rightward, horizontal-
// dominant pull arms the gesture; vertical-dominant movement is a scroll and
// cancels it; anything within the slop is still undecided.
export function dragIntent(
  dx: number,
  dy: number,
): "pull" | "scroll" | undefined {
  if (dx > INTENT_SLOP && dx > Math.abs(dy)) return "pull";
  if (Math.abs(dy) > INTENT_SLOP && Math.abs(dy) >= Math.abs(dx)) {
    return "scroll";
  }
  if (dx < -INTENT_SLOP) return "scroll";
  return undefined;
}

// holdBroken decides whether finger movement since the hold anchor is enough
// to restart the latch hold (the finger is still dragging, not holding).
export function holdBroken(dx: number, dy: number): boolean {
  return Math.abs(dx) > HOLD_SLOP || Math.abs(dy) > HOLD_SLOP;
}

// shouldLatchOpen decides whether the pull is open enough to latch — checked
// when the touch hold completes, and on release of a desktop strip drag.
export function shouldLatchOpen(offset: number): boolean {
  return offset >= OPEN_THRESHOLD;
}

interface DragState {
  pointerId: number;
  kind: "strip" | "body";
  startX: number;
  startY: number;
  // Pointer-travel multiplier (touch is amplified, mouse is 1:1).
  gain: number;
  // The last applied offset, used to decide latching.
  offset: number;
  // Body drags start inactive: they arm on horizontal drag intent. The
  // desktop strip activates on its first movement.
  active: boolean;
  // Anchor of the current latch hold; movement past HOLD_SLOP re-anchors.
  holdX: number;
  holdY: number;
}

export function useLineAuthorsPull({
  enabled,
  slideRef,
  gutterRef,
}: {
  enabled: boolean;
  // The element that slides right while pulling (the note body column).
  slideRef: React.RefObject<HTMLDivElement | null>;
  // The gutter container that fades in as the pull progresses.
  gutterRef: React.RefObject<HTMLDivElement | null>;
}): {
  // Mount the gutter while true (during the pull, while latched open, and
  // through the spring-back animation).
  visible: boolean;
  // Spread onto the desktop grab-strip element.
  stripProps: React.HTMLAttributes<HTMLDivElement>;
  // Spread onto the note-body wrapper for the mobile touch pull.
  wrapperProps: React.HTMLAttributes<HTMLDivElement>;
  // Spread onto the revealed gutter field: a click on it closes the gutter.
  gutterProps: React.HTMLAttributes<HTMLDivElement>;
} {
  const [visible, setVisible] = React.useState(false);
  const drag = React.useRef<DragState | null>(null);
  const latched = React.useRef(false);
  const hideTimer = React.useRef<number | null>(null);
  const holdTimer = React.useRef<number | null>(null);

  const clearHoldTimer = React.useCallback(() => {
    if (holdTimer.current !== null) {
      window.clearTimeout(holdTimer.current);
      holdTimer.current = null;
    }
  }, []);

  const apply = React.useCallback(
    (offset: number, animate: boolean) => {
      const el = slideRef.current;
      if (el) {
        el.style.transition = animate ? "transform 200ms ease-out" : "";
        el.style.transform =
          offset > 0 ? `translateX(${offset}px)` : "";
      }
      const g = gutterRef.current;
      if (g) {
        g.style.transition = animate ? "opacity 200ms ease-out" : "";
        g.style.opacity = String(Math.min(1, offset / PULL_WIDTH));
      }
    },
    [slideRef, gutterRef],
  );

  const springClosed = React.useCallback(() => {
    latched.current = false;
    apply(0, true);
    if (hideTimer.current !== null) window.clearTimeout(hideTimer.current);
    hideTimer.current = window.setTimeout(() => {
      setVisible(false);
      const el = slideRef.current;
      if (el) el.style.transition = "";
      const g = gutterRef.current;
      if (g) g.style.transition = "";
    }, 220);
  }, [apply, slideRef, gutterRef]);

  const latchOpen = React.useCallback(() => {
    latched.current = true;
    apply(PULL_WIDTH, true);
  }, [apply]);

  const endDrag = React.useCallback(() => {
    clearHoldTimer();
    const d = drag.current;
    drag.current = null;
    if (!d || !d.active) return;
    if (latched.current) {
      // The hold already latched it open; the release just settles there.
      apply(PULL_WIDTH, true);
      return;
    }
    if (d.kind === "strip" && shouldLatchOpen(d.offset)) {
      latchOpen();
      return;
    }
    // A touch release before the hold completes springs back.
    springClosed();
  }, [apply, clearHoldTimer, latchOpen, springClosed]);

  React.useEffect(() => {
    return () => {
      if (hideTimer.current !== null) window.clearTimeout(hideTimer.current);
      if (holdTimer.current !== null) window.clearTimeout(holdTimer.current);
    };
  }, []);

  // Turning the pull off (permanent gutter toggled on, feature flag flipped)
  // must drop any latched slide, or the body stays translated under the
  // padded layout.
  React.useEffect(() => {
    if (enabled) return;
    latched.current = false;
    drag.current = null;
    clearHoldTimer();
    apply(0, false);
    setVisible(false);
  }, [enabled, apply, clearHoldTimer]);

  const showGutterNow = React.useCallback(() => {
    if (hideTimer.current !== null) {
      window.clearTimeout(hideTimer.current);
      hideTimer.current = null;
    }
    setVisible(true);
  }, []);

  // The latch hold: (re)armed while a touch pull sits past the open
  // threshold. The finger staying within HOLD_SLOP of the anchor for
  // LATCH_HOLD_MS latches the gutter open mid-drag.
  const restartHold = React.useCallback(
    (d: DragState, x: number, y: number) => {
      d.holdX = x;
      d.holdY = y;
      clearHoldTimer();
      holdTimer.current = window.setTimeout(() => {
        holdTimer.current = null;
        const cur = drag.current;
        if (!cur || !cur.active || latched.current) return;
        if (shouldLatchOpen(cur.offset)) latchOpen();
      }, LATCH_HOLD_MS);
    },
    [clearHoldTimer, latchOpen],
  );

  // Window-level move/up handlers live for the duration of one drag.
  const beginTracking = React.useCallback(() => {
    const onMove = (e: PointerEvent) => {
      const d = drag.current;
      if (!d || e.pointerId !== d.pointerId) return;
      const dx = e.clientX - d.startX;
      const dy = e.clientY - d.startY;
      if (!d.active) {
        if (d.kind === "strip") {
          // Desktop strip: the first movement activates the drag.
          d.active = true;
          showGutterNow();
        } else {
          // Body touch: arm on horizontal pull intent, hand vertical
          // movement back to native scrolling untouched.
          const intent = dragIntent(dx, dy);
          if (intent === "scroll") {
            cleanup();
            drag.current = null;
            return;
          }
          if (intent !== "pull") return;
          const sel = document.getSelection();
          if (sel && !sel.isCollapsed) {
            // A text selection is being dragged; leave it alone.
            cleanup();
            drag.current = null;
            return;
          }
          d.active = true;
          showGutterNow();
        }
      }
      e.preventDefault();
      // Once the hold latched it open, the rest of the touch is inert — the
      // gutter stays put until the field is clicked closed.
      if (latched.current) return;
      d.offset = appliedOffset(dx, d.gain);
      apply(d.offset, false);
      if (d.kind === "body") {
        if (!shouldLatchOpen(d.offset)) {
          clearHoldTimer();
          d.holdX = e.clientX;
          d.holdY = e.clientY;
        } else if (
          holdTimer.current === null ||
          holdBroken(e.clientX - d.holdX, e.clientY - d.holdY)
        ) {
          restartHold(d, e.clientX, e.clientY);
        }
      }
    };
    const onUp = () => {
      cleanup();
      endDrag();
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
    };
    window.addEventListener("pointermove", onMove, { passive: false });
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
  }, [apply, clearHoldTimer, endDrag, restartHold, showGutterNow]);

  const startDrag = React.useCallback(
    (e: React.PointerEvent<HTMLDivElement>, kind: "strip" | "body") => {
      drag.current = {
        pointerId: e.pointerId,
        kind,
        startX: e.clientX,
        startY: e.clientY,
        gain: kind === "body" ? TOUCH_GAIN : 1,
        offset: 0,
        active: false,
        holdX: e.clientX,
        holdY: e.clientY,
      };
      beginTracking();
    },
    [beginTracking],
  );

  const stripProps: React.HTMLAttributes<HTMLDivElement> = {
    onPointerDown: (e) => {
      if (!enabled || drag.current || latched.current) return;
      e.preventDefault();
      startDrag(e, "strip");
    },
    // The strip sits over the left edge of the revealed field; clicking it
    // while open closes like the rest of the field.
    onClick: () => {
      if (latched.current) springClosed();
    },
  };

  const wrapperProps: React.HTMLAttributes<HTMLDivElement> = {
    // Vertical panning stays native; horizontal touch movement reaches us.
    style: { touchAction: "pan-y" },
    onPointerDown: (e) => {
      if (!enabled || drag.current || latched.current) return;
      if (e.pointerType !== "touch") return;
      startDrag(e, "body");
    },
  };

  const gutterProps: React.HTMLAttributes<HTMLDivElement> = {
    onClick: () => {
      if (latched.current) springClosed();
    },
  };

  return { visible, stripProps, wrapperProps, gutterProps };
}
