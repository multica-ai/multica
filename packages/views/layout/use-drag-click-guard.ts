"use client";

import { useCallback, useEffect, useRef } from "react";

// dnd-kit keeps swallowing clicks for this long after a drag ends.
const DRAG_CLICK_GUARD_MS = 50;

function preventDefault(event: Event) {
  event.preventDefault();
}

/**
 * The release that ends a drag must not also open the row. Once a drag
 * activates, dnd-kit stops that click's propagation at the document but
 * leaves its default, so a link would still navigate natively. Where React
 * listens below the document (Desktop mounts on `#root`), it never sees that
 * click at all. A window capture listener runs first and cancels it.
 *
 * Call `arm` from `onDragStart` and `release` from `onDragEnd` and
 * `onDragCancel`.
 */
export function useDragClickGuard() {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const disarm = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    window.removeEventListener("click", preventDefault, true);
  }, []);
  useEffect(() => disarm, [disarm]);
  return {
    arm: useCallback(() => {
      disarm();
      window.addEventListener("click", preventDefault, true);
    }, [disarm]),
    release: useCallback(() => {
      timer.current = setTimeout(disarm, DRAG_CLICK_GUARD_MS);
    }, [disarm]),
  };
}
