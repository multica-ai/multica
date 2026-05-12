import { useLayoutEffect, useState, type RefObject } from "react";

/**
 * Tracks the bounding rect of `anchorRef` while `enabled` is true. Returns
 * `null` when disabled or before the first measurement.
 *
 * Used by pinned inputs to position themselves with `position: fixed` while
 * keeping their React tree position stable — the caller renders the editor
 * in the same slot regardless of pin state and only toggles inline styling.
 * This guarantees no remount across the pin toggle, so a Tiptap draft/caret
 * survives.
 */
export interface FloatRect {
  left: number;
  width: number;
}

export function useFloatPosition(
  anchorRef: RefObject<HTMLElement | null>,
  enabled: boolean,
): FloatRect | null {
  const [rect, setRect] = useState<FloatRect | null>(null);

  useLayoutEffect(() => {
    if (!enabled) {
      setRect(null);
      return;
    }
    const node = anchorRef.current;
    if (!node) return;
    const update = () => {
      const r = node.getBoundingClientRect();
      setRect({ left: r.left, width: r.width });
    };
    update();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(update) : null;
    ro?.observe(node);
    window.addEventListener("resize", update);
    return () => {
      ro?.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [enabled, anchorRef]);

  return rect;
}
