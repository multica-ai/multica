"use client";

import { useCallback, useEffect, useRef } from "react";

// Bottom-stick for the chat list (TIM-55).
//
// Virtuoso's `followOutput` only fires when the ITEM COUNT changes. A streaming
// reply is one row that keeps growing, and a growing composer shrinks the
// viewport — neither changes the count, so the list has to re-pin itself: while
// the reader is at the live end, any content growth or viewport shrink scrolls
// back to the bottom. Scrolling away releases the pin until they come back.

/**
 * Within this distance of the bottom the reader still counts as following the
 * live end. Shared with Virtuoso's `atBottomThreshold` so both agree on who is
 * at the bottom.
 */
export const STICK_EDGE_THRESHOLD = 120;

export interface ScrollMetrics {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
}

export function distanceFromBottom(m: ScrollMetrics): number {
  return Math.max(0, m.scrollHeight - m.scrollTop - m.clientHeight);
}

export function isAtLiveEnd(m: ScrollMetrics): boolean {
  return distanceFromBottom(m) <= STICK_EDGE_THRESHOLD;
}

/**
 * The scrollTop that re-pins a follower to the bottom, or `null` when there is
 * nothing to do. Only ever pins DOWNWARD: when content shrinks (a collapsible
 * closing) the browser clamps scrollTop on its own, and yanking the viewport up
 * would move the reader for no reason.
 */
export function bottomPinTarget(m: ScrollMetrics): number | null {
  const target = Math.max(0, m.scrollHeight - m.clientHeight);
  return target > m.scrollTop ? target : null;
}

/**
 * Keeps `scrollEl` pinned to the bottom while the reader is at the live end.
 * Observes BOTH boxes: the container for viewport shrink (composer growth), the
 * content for growth (streaming) — a ResizeObserver reports an element's own
 * box, never its scroll extent. Returns a getter for whether the reader is
 * still following.
 */
export function useStickToBottom(
  scrollEl: HTMLElement | null,
  contentEl: HTMLElement | null,
): () => boolean {
  // Chat opens pinned — the list mounts at `initialTopMostItemIndex: LAST`.
  const pinned = useRef(true);

  useEffect(() => {
    if (!scrollEl) return;

    const onScroll = () => {
      pinned.current = isAtLiveEnd(scrollEl);
    };
    scrollEl.addEventListener("scroll", onScroll, { passive: true });

    const observer = new ResizeObserver(() => {
      if (!pinned.current) return;
      const target = bottomPinTarget(scrollEl);
      if (target !== null) scrollEl.scrollTop = target;
    });
    observer.observe(scrollEl);
    if (contentEl) observer.observe(contentEl);

    return () => {
      scrollEl.removeEventListener("scroll", onScroll);
      observer.disconnect();
    };
  }, [scrollEl, contentEl]);

  return useCallback(() => pinned.current, []);
}
