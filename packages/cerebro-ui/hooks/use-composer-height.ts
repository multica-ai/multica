"use client";

import { useEffect, useState } from "react";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";

// Fractions of the visible viewport a mobile compose field may occupy.
// `window.visualViewport.height` already excludes the on-screen keyboard, so
// these are fractions of the space left ABOVE the keyboard. TECH-3536.
const DEFAULT_FRACTION = 0.5;
const EXPANDED_FRACTION = 0.8;

export interface ComposerHeight {
  /** Pixel cap to spread onto the editor scroll container's style, or
   *  undefined on desktop where the field grows with its flex parent. */
  maxHeight: number | undefined;
  isExpanded: boolean;
  toggleExpanded: () => void;
  /** True only while a cap is in effect (mobile + a known viewport height) —
   *  the expand toggle has nothing to do otherwise, so callers hide it. */
  showExpandToggle: boolean;
}

/**
 * Caps a mobile compose field so a long draft can't grow past half the space
 * above the keyboard (default), with an opt-in expand to 80%. On desktop the
 * cap is off and the field keeps its previous unbounded-within-flex behavior.
 *
 * The keyboard already shrinks `visualViewport.height` on every modern mobile
 * browser, so a fraction of that height is "a fraction of the space the user
 * can actually see" — not a fraction of the whole screen behind the keyboard.
 */
export function useComposerHeight(): ComposerHeight {
  const isMobile = useIsMobile();
  const [isExpanded, setIsExpanded] = useState(false);
  const [viewportHeight, setViewportHeight] = useState<number | null>(() => {
    if (typeof window === "undefined" || !window.visualViewport) return null;
    return window.visualViewport.height;
  });

  useEffect(() => {
    if (typeof window === "undefined" || !window.visualViewport) return;
    const vv = window.visualViewport;
    const update = () => setViewportHeight(vv.height);
    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);

  const canCap = isMobile && viewportHeight != null;
  const maxHeight = canCap
    ? Math.round(viewportHeight * (isExpanded ? EXPANDED_FRACTION : DEFAULT_FRACTION))
    : undefined;

  return {
    maxHeight,
    isExpanded,
    toggleExpanded: () => setIsExpanded((v) => !v),
    showExpandToggle: canCap,
  };
}
