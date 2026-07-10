"use client";

// FIR-2810: temporary reveal of the line-authors gutter, Apple Notes-style.
// Desktop: grab the thin handle at the left edge of the note body and drag
// right. Mobile: touch the note body and pull right (vertical scrolling stays
// native via touch-action: pan-y). While held, the body slides right and the
// gutter fades in underneath; on release everything springs back. The
// permanent view stays available as the "Line authors" toggle in the ⋯ menu —
// this gesture is the quick peek.

import * as React from "react";

// The gutter is 96px wide (w-24); the pull saturates there.
export const PULL_WIDTH = 96;

// pullOffset maps a raw horizontal drag distance to the applied slide offset:
// clamped at 0, following the finger up to PULL_WIDTH, then rubber-banding
// (every extra pixel counts 20%) so a long pull feels anchored.
export function pullOffset(dx: number): number {
  if (dx <= 0) return 0;
  if (dx <= PULL_WIDTH) return dx;
  return PULL_WIDTH + (dx - PULL_WIDTH) * 0.2;
}

// isHorizontalPull decides whether an initial touch movement is a
// right-pull (reveal the gutter) rather than a vertical scroll: it must move
// right and be clearly more horizontal than vertical.
export function isHorizontalPull(dx: number, dy: number): boolean {
  if (dx < 10) return false;
  return dx > Math.abs(dy) * 1.5;
}

interface DragState {
  pointerId: number;
  startX: number;
  startY: number;
  // touch drags start undecided: they become active only once the movement
  // is clearly a horizontal right-pull, so vertical scrolling is untouched.
  active: boolean;
  decided: boolean;
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
  // Mount the gutter while true (during the pull and its spring-back).
  visible: boolean;
  // Spread onto the desktop grab-strip element.
  stripProps: React.HTMLAttributes<HTMLDivElement>;
  // Spread onto the note-body wrapper for the mobile touch pull.
  wrapperProps: React.HTMLAttributes<HTMLDivElement>;
} {
  const [visible, setVisible] = React.useState(false);
  const drag = React.useRef<DragState | null>(null);
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

  const endDrag = React.useCallback(() => {
    if (!drag.current) return;
    const wasActive = drag.current.active;
    drag.current = null;
    if (!wasActive) return;
    // Spring back, then unmount the gutter once the animation is done.
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

  React.useEffect(() => {
    return () => {
      if (hideTimer.current !== null) window.clearTimeout(hideTimer.current);
    };
  }, []);

  // Window-level move/up handlers live for the duration of one drag.
  const beginTracking = React.useCallback(
    (decideIntent: boolean) => {
      const onMove = (e: PointerEvent) => {
        const d = drag.current;
        if (!d || e.pointerId !== d.pointerId) return;
        const dx = e.clientX - d.startX;
        const dy = e.clientY - d.startY;
        if (!d.decided) {
          if (decideIntent) {
            // Touch pull: wait until the direction is clear.
            if (Math.abs(dx) < 10 && Math.abs(dy) < 10) return;
            d.decided = true;
            d.active = isHorizontalPull(dx, dy);
            if (!d.active) {
              cleanup();
              drag.current = null;
              return;
            }
          } else {
            d.decided = true;
            d.active = true;
          }
          if (hideTimer.current !== null) {
            window.clearTimeout(hideTimer.current);
            hideTimer.current = null;
          }
          setVisible(true);
        }
        if (!d.active) return;
        e.preventDefault();
        apply(pullOffset(dx), false);
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
    },
    [apply, endDrag],
  );

  const stripProps: React.HTMLAttributes<HTMLDivElement> = {
    onPointerDown: (e) => {
      if (!enabled || drag.current) return;
      e.preventDefault();
      drag.current = {
        pointerId: e.pointerId,
        startX: e.clientX,
        startY: e.clientY,
        active: false,
        decided: false,
      };
      beginTracking(false);
    },
  };

  const wrapperProps: React.HTMLAttributes<HTMLDivElement> = {
    // Vertical panning stays native; horizontal touch movement reaches us.
    style: { touchAction: "pan-y" },
    onPointerDown: (e) => {
      if (!enabled || drag.current) return;
      if (e.pointerType !== "touch") return;
      drag.current = {
        pointerId: e.pointerId,
        startX: e.clientX,
        startY: e.clientY,
        active: false,
        decided: false,
      };
      beginTracking(true);
    },
  };

  return { visible, stripProps, wrapperProps };
}
