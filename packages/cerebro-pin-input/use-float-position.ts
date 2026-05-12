import { useEffect, useState, type RefObject } from "react";

/**
 * Tracks the bounding rect of `anchorRef` while `enabled` is true. Returns
 * `null` when disabled or — in `sticky-bottom` mode — when the anchor is
 * still visible above the viewport bottom.
 *
 * Used by pinned inputs to position themselves with `position: fixed` while
 * keeping their React tree position stable — the caller renders the editor
 * in the same slot regardless of pin state and only toggles inline styling.
 * This guarantees no remount across the pin toggle, so a Tiptap draft/caret
 * survives.
 *
 * Modes:
 *   - `always` (default): return the anchor rect whenever enabled. The caller
 *     renders the input as fixed at all times — used for the bottom
 *     CommentInput, where the user expects the field to follow them up the
 *     page the moment they pin.
 *   - `sticky-bottom`: only return a rect once `anchor.bottom > window.innerHeight`,
 *     so the input stays in its natural slot inside the thread while it is
 *     still on screen and only starts floating once the bottom of the screen
 *     reaches the field. Used for reply inputs inside threads (JEH-1065).
 */
export interface FloatRect {
  left: number;
  width: number;
  height: number;
}

export interface UseFloatPositionOptions {
  mode?: "always" | "sticky-bottom";
}

export function useFloatPosition(
  anchorRef: RefObject<HTMLElement | null>,
  enabled: boolean,
  { mode = "always" }: UseFloatPositionOptions = {},
): FloatRect | null {
  const [rect, setRect] = useState<FloatRect | null>(null);

  useEffect(() => {
    if (!enabled) {
      setRect(null);
      return;
    }
    const node = anchorRef.current;
    if (!node) return;
    const update = () => {
      const r = node.getBoundingClientRect();
      if (mode === "sticky-bottom" && r.bottom <= window.innerHeight) {
        setRect(null);
        return;
      }
      setRect({ left: r.left, width: r.width, height: r.height });
    };
    update();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(update) : null;
    ro?.observe(node);
    window.addEventListener("resize", update);
    if (mode === "sticky-bottom") {
      // Capture-phase scroll so we react to inner scroll containers (issue
      // detail body), not just window scroll.
      window.addEventListener("scroll", update, { passive: true, capture: true });
    }
    return () => {
      ro?.disconnect();
      window.removeEventListener("resize", update);
      if (mode === "sticky-bottom") {
        window.removeEventListener("scroll", update, true);
      }
    };
  }, [enabled, mode, anchorRef]);

  return rect;
}
