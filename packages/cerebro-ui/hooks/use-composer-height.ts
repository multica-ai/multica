"use client";

import { useEffect, useState } from "react";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";

// Fractions of the visible viewport a compose field may occupy.
// On mobile `window.visualViewport.height` already excludes the on-screen
// keyboard, so these are fractions of the space left ABOVE the keyboard.
// On desktop there is no keyboard, so we cap against the window height and
// keep the collapsed field compact with a fixed pixel ceiling. TECH-3536.
const DEFAULT_FRACTION = 0.5;
const EXPANDED_FRACTION = 0.8;
const DESKTOP_COLLAPSED_MAX = 240;
const DESKTOP_EXPANDED_FRACTION = 0.7;

export interface ComposerHeight {
  /** Inline style to spread onto the editor's scroll container. Collapsed
   *  sets a `maxHeight` cap so the field auto-grows with content up to the
   *  cap; expanded sets a concrete `height` so the field visibly jumps to
   *  the larger size even when the draft is short. Undefined before the
   *  viewport is known (SSR / first paint). */
  containerStyle: { maxHeight?: number; height?: number } | undefined;
  isExpanded: boolean;
  toggleExpanded: () => void;
  /** True once a height is known — the expand toggle has a target to grow
   *  to. True on both mobile and desktop so the control is identical
   *  everywhere. */
  showExpandToggle: boolean;
}

/**
 * Caps a compose field so a long draft can't eat the screen, with an opt-in
 * expand to a larger size and back. One behavior for every composer
 * (issue comments, channel/DM, agent chat) so the expand control is
 * identical everywhere. TECH-3536.
 *
 * Mobile: caps against `visualViewport.height` (space above the keyboard) —
 * 50% collapsed, 80% expanded. Desktop: 240px collapsed, 70% of the window
 * height expanded.
 */
export function useComposerHeight(): ComposerHeight {
  const isMobile = useIsMobile();
  const [isExpanded, setIsExpanded] = useState(false);
  const [viewportHeight, setViewportHeight] = useState<number | null>(() => {
    if (typeof window === "undefined") return null;
    return window.visualViewport?.height ?? window.innerHeight ?? null;
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    const vv = window.visualViewport;
    const update = () =>
      setViewportHeight(vv?.height ?? window.innerHeight ?? null);
    update();
    vv?.addEventListener("resize", update);
    vv?.addEventListener("scroll", update);
    window.addEventListener("resize", update);
    return () => {
      vv?.removeEventListener("resize", update);
      vv?.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
    };
  }, []);

  const known = viewportHeight != null;

  let containerStyle: { maxHeight?: number; height?: number } | undefined;
  if (known) {
    if (isMobile) {
      containerStyle = isExpanded
        ? { height: Math.round(viewportHeight * EXPANDED_FRACTION) }
        : { maxHeight: Math.round(viewportHeight * DEFAULT_FRACTION) };
    } else {
      containerStyle = isExpanded
        ? { height: Math.round(viewportHeight * DESKTOP_EXPANDED_FRACTION) }
        : { maxHeight: DESKTOP_COLLAPSED_MAX };
    }
  }

  return {
    containerStyle,
    isExpanded,
    toggleExpanded: () => setIsExpanded((v) => !v),
    showExpandToggle: known,
  };
}
