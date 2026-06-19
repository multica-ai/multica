"use client";

import { useEffect, useState } from "react";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";

// Collapsed cap: the field shows at most COLLAPSED_LINES lines of body text,
// then scrolls. The editor body is 16px on mobile / 14px on desktop
// (`text-base md:text-sm`) at line-height 1.625 (see editor prose.css), so one
// line is ~26px on mobile and ~23px on desktop. FIR-1625.
const COLLAPSED_LINES = 4;
const MOBILE_LINE_HEIGHT = 26; // 16px * 1.625
const DESKTOP_LINE_HEIGHT = 23; // 14px * 1.625
const MOBILE_COLLAPSED_MAX = COLLAPSED_LINES * MOBILE_LINE_HEIGHT; // 104
const DESKTOP_COLLAPSED_MAX = COLLAPSED_LINES * DESKTOP_LINE_HEIGHT; // 92

// Expanded fractions of the visible viewport a compose field may occupy.
// On mobile `window.visualViewport.height` already excludes the on-screen
// keyboard, so this is a fraction of the space left ABOVE the keyboard. On
// desktop there is no keyboard, so we cap against the window height. TECH-3536.
const EXPANDED_FRACTION = 0.8;
const DESKTOP_EXPANDED_FRACTION = 0.7;

export interface ComposerHeight {
  /** Inline style to spread onto the editor's scroll container. Collapsed
   *  sets a `maxHeight` cap so the field auto-grows with content up to the
   *  cap; expanded sets a concrete `height` so the field visibly jumps to
   *  the larger size even when the draft is short. `flexBasis` mirrors the
   *  expanded `height` because the scroll container is a `flex-1` item
   *  (`flex-basis: 0%`), which would otherwise win over `height` and leave
   *  the field at its collapsed size — i.e. expand would do nothing.
   *  Undefined before the viewport is known (SSR / first paint). */
  containerStyle:
    | { maxHeight?: number; height?: number; flexBasis?: number }
    | undefined;
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
 * Collapsed: at most 4 lines of body text (~104px mobile, ~92px desktop),
 * then the field scrolls (FIR-1625). Expanded caps against the viewport —
 * 80% of the space above the keyboard on mobile, 70% of the window on desktop.
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

  let containerStyle:
    | { maxHeight?: number; height?: number; flexBasis?: number }
    | undefined;
  if (known) {
    if (isExpanded) {
      const expandedHeight = Math.round(
        viewportHeight * (isMobile ? EXPANDED_FRACTION : DESKTOP_EXPANDED_FRACTION),
      );
      // flexBasis must match height: the scroll container is `flex-1`
      // (flex-basis: 0%), which overrides a bare `height` on a flex item.
      // Pinning flexBasis makes the field actually grow on expand.
      containerStyle = { height: expandedHeight, flexBasis: expandedHeight };
    } else {
      // Collapsed: cap at 4 lines of body text, then scroll. Fixed line-based
      // ceiling (not a viewport fraction) so a long draft never grows the
      // field past 4 lines. FIR-1625.
      containerStyle = {
        maxHeight: isMobile ? MOBILE_COLLAPSED_MAX : DESKTOP_COLLAPSED_MAX,
      };
    }
  }

  return {
    containerStyle,
    isExpanded,
    toggleExpanded: () => setIsExpanded((v) => !v),
    showExpandToggle: known,
  };
}
