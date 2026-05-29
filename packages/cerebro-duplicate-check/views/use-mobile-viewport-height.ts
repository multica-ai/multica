"use client";

import { useEffect, useState } from "react";

/**
 * Returns the height of `window.visualViewport` in pixels, or null when the
 * API is unavailable (SSR, very old browsers). FIR-2504.
 *
 * Background: on mobile, when the on-screen keyboard opens, the layout
 * viewport (and `100dvh`) does NOT always shrink — iOS Safari resizes it,
 * most Android browsers do not. The visualViewport API is the only signal
 * that shrinks in lockstep with the keyboard on every modern mobile browser.
 *
 * Apply the returned number to a full-screen modal's CSS height so the
 * modal's bottom edge stays flush with the top of the keyboard — otherwise
 * the footer + settings sit below it and the user can't reach them.
 */
export function useMobileViewportHeight(): number | null {
  const [height, setHeight] = useState<number | null>(() => {
    if (typeof window === "undefined" || !window.visualViewport) return null;
    return window.visualViewport.height;
  });

  useEffect(() => {
    if (typeof window === "undefined" || !window.visualViewport) return;
    const vv = window.visualViewport;
    const update = () => setHeight(vv.height);
    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);

  return height;
}
