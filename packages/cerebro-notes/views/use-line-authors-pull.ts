"use client";

// FIR-2810: temporary reveal of the line-authors gutter, Apple Notes-style.
// Desktop: grab the thin handle at the left edge of the note body and drag
// right. Mobile: press and HOLD the note body, then pull right (vertical
// scrolling stays native via touch-action: pan-y). While pulling, the body
// slides right and the gutter fades in underneath.
//
// Latching (Jesper, 2026-07-10): a released pull past the open threshold
// LATCHES the gutter open instead of springing back, and touch travel is
// amplified so opening doesn't take a full gutter-width of finger movement.
// While latched, a hold + small pull back to the left closes it. Releasing
// before the threshold still springs back. The permanent view stays available
// as the "Line authors" toggle in the ⋯ menu.
//
// Hold requirement (Jesper, 2026-07-11): the touch pull arms only after the
// finger has stayed still on the note for HOLD_MS. Before that, any movement,
// lift, or a text selection forming (the long-press selection toolbar)
// cancels it — so ordinary scrolling, taps and text selection are never
// captured by the gesture.

import * as React from "react";

// The width the temporary reveal slides open to — just enough to read the
// author codes ("JEH · MOP") and time in the gutter, narrower than the
// permanent w-24 gutter (Jesper, 2026-07-11: only far enough to see who
// edited).
export const PULL_WIDTH = 64;

// Touch travel is amplified so the gutter opens well before the finger has
// crossed a full gutter-width of screen (~40px of travel opens it fully).
export const TOUCH_GAIN = 1.6;

// The finger must stay still on the note this long before the pull arms.
export const HOLD_MS = 500;

// Finger travel allowed during the hold; more means a scroll or a selection
// drag, and the pull is cancelled.
export const HOLD_SLOP = 10;

// A released pull whose applied offset passed this latches open.
export const OPEN_THRESHOLD = PULL_WIDTH * 0.4;

// While latched, a release after pulling at least this many applied px back
// to the left closes the gutter.
export const CLOSE_THRESHOLD = 24;

// appliedOffset maps a raw pointer movement (dx, scaled by gain) on top of the
// drag's starting offset to the applied slide offset: clamped at 0, following
// the pointer up to PULL_WIDTH, then rubber-banding (every extra pixel counts
// 20%) so a long pull feels anchored.
export function appliedOffset(base: number, dx: number, gain: number): number {
  const raw = base + dx * gain;
  if (raw <= 0) return 0;
  if (raw <= PULL_WIDTH) return raw;
  return PULL_WIDTH + (raw - PULL_WIDTH) * 0.2;
}

// holdBroken decides whether finger movement during the hold phase is enough
// to cancel the pull (the user is scrolling or dragging a selection).
export function holdBroken(dx: number, dy: number): boolean {
  return Math.abs(dx) > HOLD_SLOP || Math.abs(dy) > HOLD_SLOP;
}

// shouldLatchOpen / shouldClose decide what a released drag does with the
// gutter, from the applied offset at release.
export function shouldLatchOpen(offset: number): boolean {
  return offset >= OPEN_THRESHOLD;
}

export function shouldClose(offset: number): boolean {
  return offset <= PULL_WIDTH - CLOSE_THRESHOLD;
}

interface DragState {
  pointerId: number;
  startX: number;
  startY: number;
  // The applied offset the drag started from (PULL_WIDTH when latched open).
  base: number;
  // Pointer-travel multiplier (touch is amplified, mouse is 1:1).
  gain: number;
  // The last applied offset, used to decide latch/close on release.
  offset: number;
  // Touch drags start inactive: they arm only after the hold completes.
  // The desktop strip activates on its first movement.
  active: boolean;
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
} {
  const [visible, setVisible] = React.useState(false);
  const drag = React.useRef<DragState | null>(null);
  const latched = React.useRef(false);
  const hideTimer = React.useRef<number | null>(null);

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

  const endDrag = React.useCallback(() => {
    const d = drag.current;
    drag.current = null;
    if (!d || !d.active) return;
    if (latched.current) {
      // Open gutter: a small pull left closes it, otherwise settle back open.
      if (shouldClose(d.offset)) {
        springClosed();
      } else {
        apply(PULL_WIDTH, true);
      }
      return;
    }
    if (shouldLatchOpen(d.offset)) {
      latched.current = true;
      apply(PULL_WIDTH, true);
      return;
    }
    springClosed();
  }, [apply, springClosed]);

  React.useEffect(() => {
    return () => {
      if (hideTimer.current !== null) window.clearTimeout(hideTimer.current);
    };
  }, []);

  // Turning the pull off (permanent gutter toggled on, feature flag flipped)
  // must drop any latched slide, or the body stays translated under the
  // padded layout.
  React.useEffect(() => {
    if (enabled) return;
    latched.current = false;
    drag.current = null;
    apply(0, false);
    setVisible(false);
  }, [enabled, apply]);

  const showGutterNow = React.useCallback(() => {
    if (hideTimer.current !== null) {
      window.clearTimeout(hideTimer.current);
      hideTimer.current = null;
    }
    setVisible(true);
  }, []);

  // Window-level move/up handlers live for the duration of one drag.
  const beginTracking = React.useCallback(() => {
    const onMove = (e: PointerEvent) => {
      const d = drag.current;
      if (!d || e.pointerId !== d.pointerId) return;
      if (!d.active) {
        // Desktop strip: the first movement activates the drag.
        d.active = true;
        showGutterNow();
      }
      e.preventDefault();
      d.offset = appliedOffset(d.base, e.clientX - d.startX, d.gain);
      apply(d.offset, false);
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
  }, [apply, endDrag, showGutterNow]);

  // Touch hold phase: the pull arms only if the finger stays still for
  // HOLD_MS. Movement past the slop, lifting the finger, or a text selection
  // forming (long-press selection — its toolbar must win) cancels it and
  // leaves the native behavior untouched.
  const beginHold = React.useCallback(() => {
    const cancel = () => {
      window.clearTimeout(timer);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", cancel);
      window.removeEventListener("pointercancel", cancel);
      document.removeEventListener("selectionchange", onSelection);
      drag.current = null;
    };
    const onMove = (e: PointerEvent) => {
      const d = drag.current;
      if (!d || e.pointerId !== d.pointerId) return;
      if (holdBroken(e.clientX - d.startX, e.clientY - d.startY)) cancel();
    };
    const onSelection = () => {
      const sel = document.getSelection();
      if (sel && !sel.isCollapsed) cancel();
    };
    const timer = window.setTimeout(() => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", cancel);
      window.removeEventListener("pointercancel", cancel);
      document.removeEventListener("selectionchange", onSelection);
      const d = drag.current;
      if (!d) return;
      d.active = true;
      showGutterNow();
      apply(d.offset, false);
      beginTracking();
    }, HOLD_MS);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", cancel);
    window.addEventListener("pointercancel", cancel);
    document.addEventListener("selectionchange", onSelection);
  }, [apply, beginTracking, showGutterNow]);

  const startDrag = React.useCallback(
    (e: React.PointerEvent<HTMLDivElement>, gain: number) => {
      drag.current = {
        pointerId: e.pointerId,
        startX: e.clientX,
        startY: e.clientY,
        base: latched.current ? PULL_WIDTH : 0,
        gain,
        offset: latched.current ? PULL_WIDTH : 0,
        active: false,
      };
    },
    [],
  );

  const stripProps: React.HTMLAttributes<HTMLDivElement> = {
    onPointerDown: (e) => {
      if (!enabled || drag.current) return;
      e.preventDefault();
      startDrag(e, 1);
      beginTracking();
    },
  };

  const wrapperProps: React.HTMLAttributes<HTMLDivElement> = {
    // Vertical panning stays native; horizontal touch movement reaches us.
    style: { touchAction: "pan-y" },
    onPointerDown: (e) => {
      if (!enabled || drag.current) return;
      if (e.pointerType !== "touch") return;
      startDrag(e, TOUCH_GAIN);
      beginHold();
    },
  };

  return { visible, stripProps, wrapperProps };
}
