"use client";

import { useEffect, useState } from "react";

/** Geometry of the visual viewport while the mobile soft keyboard is up. */
export interface VisualViewportKeyboard {
  /**
   * Distance (px) from the LAYOUT viewport's bottom edge up to the visual
   * viewport's bottom edge. A bottom-anchored overlay offset by this much
   * sits exactly on the visible bottom edge, wherever the browser has
   * panned the page.
   */
  occludedBottom: number;
  /** Visible height (px) — the tallest an overlay can be and stay on screen. */
  viewportHeight: number;
}

/**
 * iOS Safari's chrome expanding/collapsing also shrinks the visual viewport
 * by a few tens of px; only gaps at least this tall are treated as a
 * keyboard. Every mobile keyboard is well above this.
 */
const KEYBOARD_MIN_PX = 100;

/**
 * Reports visual-viewport geometry while the soft keyboard is open, null
 * otherwise (desktop, keyboard closed, pinch-zoomed, or no API).
 *
 * Mobile browsers do not shrink the layout viewport when the keyboard opens —
 * they shrink the *visual* viewport and, on iOS, pan it to keep the caret
 * visible. A bottom-anchored overlay therefore needs BOTH numbers: the pan
 * makes "distance to the visible bottom" differ from the keyboard's height,
 * and the shrunken height bounds how tall the overlay may stay.
 *
 * The layout height baseline must be documentElement.clientHeight, not
 * window.innerHeight: iOS Safari shrinks innerHeight together with the
 * visual viewport when the keyboard opens (observed 547 -> 385 on iOS
 * 17.5), which would make an innerHeight-based keyboard measure read near
 * zero exactly when the keyboard is up.
 */
export function useVisualViewportKeyboard(): VisualViewportKeyboard | null {
  const [keyboard, setKeyboard] = useState<VisualViewportKeyboard | null>(null);

  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;

    const update = () => {
      const zoomed = Math.abs(vv.scale - 1) > 0.01;
      const layoutHeight = document.documentElement.clientHeight;
      const keyboardHeight = layoutHeight - vv.height;
      const next: VisualViewportKeyboard | null =
        zoomed || keyboardHeight < KEYBOARD_MIN_PX
          ? null
          : {
              occludedBottom: Math.max(
                0,
                Math.round(layoutHeight - vv.height - vv.offsetTop),
              ),
              viewportHeight: Math.round(vv.height),
            };
      setKeyboard((prev) =>
        prev?.occludedBottom === next?.occludedBottom &&
        prev?.viewportHeight === next?.viewportHeight
          ? prev
          : next,
      );
    };

    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);

  return keyboard;
}
