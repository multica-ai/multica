"use client";

/**
 * Tap-vs-drag gesture for a horizontally scrollable element.
 *
 * Solves a specific collision: the inline Mermaid diagram opens the viewer when
 * clicked, but a wide diagram also invites dragging to see the rest of it.
 * Without an intent test every drag ends in a `click` and the viewer opens on
 * top of the user, which reads as the diagram fighting back.
 *
 * The rule (product decision, MUL-4908): once the pointer travels past a small
 * threshold the gesture is a drag for good, and releasing must not tap — even
 * when the element had no room to scroll, so an unscrollable diagram cannot
 * surprise the user by opening on release either.
 *
 * Touch is the one pointer type the browser already drag-scrolls by itself on
 * an `overflow-x: auto` element, and whose vertical drags belong to the page —
 * so touch is left alone here and only watched for intent, with the browser
 * announcing its takeover via `pointercancel`. Every other pointer (mouse, pen)
 * has no native drag-to-scroll and is panned below.
 */

import { useCallback, useRef, type PointerEvent as ReactPointerEvent } from "react";

// Kim specced 4–6px. 5 sits in the middle: past the jitter of a real click
// (including a shaky trackpad tap) while still well under a deliberate drag.
export const DRAG_THRESHOLD_PX = 5;

interface GestureState {
  pointerId: number;
  pointerType: string;
  startX: number;
  startY: number;
  startScrollLeft: number;
  dragged: boolean;
}

function isTouch(gesture: GestureState): boolean {
  return gesture.pointerType === "touch";
}

export interface DragToScrollHandlers {
  onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerMove: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerUp: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerCancel: (event: ReactPointerEvent<HTMLElement>) => void;
}

/**
 * @param onTap Fired on release only when the gesture never became a drag.
 */
export function useDragToScroll({
  onTap,
}: {
  onTap: () => void;
}): DragToScrollHandlers {
  // A ref, not state: a drag updates on every pointermove, and re-rendering the
  // diagram (and its iframe) at that rate would stutter the pan it is driving.
  const gestureRef = useRef<GestureState | null>(null);

  const onPointerDown = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    // Secondary buttons open the context menu; they are not a tap or a drag.
    if (event.pointerType === "mouse" && event.button !== 0) return;

    gestureRef.current = {
      pointerId: event.pointerId,
      pointerType: event.pointerType,
      startX: event.clientX,
      startY: event.clientY,
      startScrollLeft: event.currentTarget.scrollLeft,
      dragged: false,
    };
  }, []);

  const onPointerMove = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const gesture = gestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) return;

    const deltaX = event.clientX - gesture.startX;
    const deltaY = event.clientY - gesture.startY;

    if (!gesture.dragged) {
      if (Math.hypot(deltaX, deltaY) <= DRAG_THRESHOLD_PX) return;
      gesture.dragged = true;
      // Capture only once it is definitely a drag, and never for touch: taking
      // the pointer at pointerdown would swallow ordinary clicks, and capturing
      // a touch would fight the native scroll this relies on.
      if (!isTouch(gesture)) {
        event.currentTarget.setPointerCapture?.(event.pointerId);
      }
    }

    // Touch scrolls itself; driving scrollLeft here too would double the speed.
    if (isTouch(gesture)) return;
    // Assigning past either end is clamped by the browser, so an unscrollable
    // diagram simply stays put — the gesture is still a drag, and still
    // suppresses the tap below.
    event.currentTarget.scrollLeft = gesture.startScrollLeft - deltaX;
  }, []);

  const onPointerUp = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      const gesture = gestureRef.current;
      gestureRef.current = null;
      if (!gesture || gesture.pointerId !== event.pointerId) return;

      if (event.currentTarget.hasPointerCapture?.(event.pointerId)) {
        event.currentTarget.releasePointerCapture?.(event.pointerId);
      }

      if (!gesture.dragged) onTap();
    },
    [onTap],
  );

  // The browser took the gesture over (native touch scroll, or the pointer was
  // otherwise lost). That is a drag by definition, so it must not tap.
  const onPointerCancel = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const gesture = gestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) return;
    gestureRef.current = null;
  }, []);

  return { onPointerDown, onPointerMove, onPointerUp, onPointerCancel };
}
