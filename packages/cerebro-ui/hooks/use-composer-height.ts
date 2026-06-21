"use client";

import { useCallback, useEffect, useRef, useState } from "react";
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

// Vertical padding on the scroll container (Tailwind `py-2` → 8px top + 8px
// bottom). The editor body itself carries no padding, so the visible body area
// of the collapsed field is the cap minus this. FIR-1684.
const COLLAPSED_VERTICAL_PADDING = 16;

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
  /** Attach to the scroll container that wraps the editor body. The hook
   *  measures the body's content height through it to decide whether the
   *  field is full enough to warrant the expand toggle. FIR-1684. */
  scrollRef: (node: HTMLElement | null) => void;
  /** True only once the draft has grown enough to fill (and start to
   *  overflow) the collapsed field, or while the field is already expanded
   *  so the user can collapse it back. A short draft hides the toggle so it
   *  no longer floats over a single line of text. FIR-1684. */
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
 *
 * The expand toggle stays hidden until the draft is long enough to fill the
 * collapsed field — a single line of text gets no overlay pill (FIR-1684).
 */
export function useComposerHeight(): ComposerHeight {
  const isMobile = useIsMobile();
  const [isExpanded, setIsExpanded] = useState(false);
  const [viewportHeight, setViewportHeight] = useState<number | null>(() => {
    if (typeof window === "undefined") return null;
    return window.visualViewport?.height ?? window.innerHeight ?? null;
  });
  // True once the editor body has grown past the collapsed field's visible
  // area — i.e. the draft no longer fits in the short field and expanding
  // would help. FIR-1684.
  const [bodyFillsField, setBodyFillsField] = useState(false);

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

  // Measure the editor body (the scroll container's only child) and flip the
  // toggle on once it overflows the collapsed visible area. The body's own
  // height is independent of the container's padding, so adding the overlay
  // band when the toggle appears can't feed back into the measurement (no
  // flicker). Re-attaches if the editor remounts (e.g. session switch).
  const scrollElRef = useRef<HTMLElement | null>(null);
  const scrollRef = useCallback((node: HTMLElement | null) => {
    scrollElRef.current = node;
  }, []);
  useEffect(() => {
    const scrollEl = scrollElRef.current;
    if (!scrollEl || typeof ResizeObserver === "undefined") return;
    const collapsedMax = isMobile ? MOBILE_COLLAPSED_MAX : DESKTOP_COLLAPSED_MAX;
    const threshold = collapsedMax - COLLAPSED_VERTICAL_PADDING;
    const measure = () => {
      const body = scrollEl.firstElementChild as HTMLElement | null;
      setBodyFillsField(body != null && body.scrollHeight > threshold);
    };
    const ro = new ResizeObserver(measure);
    const observeBody = () => {
      const body = scrollEl.firstElementChild;
      if (body) ro.observe(body);
    };
    observeBody();
    measure();
    // The editor remounts on key changes; re-observe the fresh body node.
    const mo = new MutationObserver(() => {
      ro.disconnect();
      observeBody();
      measure();
    });
    mo.observe(scrollEl, { childList: true });
    return () => {
      ro.disconnect();
      mo.disconnect();
    };
  }, [isMobile]);

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
    scrollRef,
    // Show the toggle once the body fills the collapsed field, or whenever the
    // field is already expanded so the user can always collapse it. FIR-1684.
    showExpandToggle: known && (bodyFillsField || isExpanded),
  };
}
