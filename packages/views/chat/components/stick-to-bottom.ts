"use client";

import { useCallback, useEffect, useRef } from "react";

/** Maximum distance from the bottom that still counts as following the live end. */
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

/** Returns a downward-only scroll target, or `null` when no scrolling is needed. */
export function bottomPinTarget(m: ScrollMetrics): number | null {
  const target = Math.max(0, m.scrollHeight - m.clientHeight);
  return target > m.scrollTop ? target : null;
}

/**
 * Keeps the viewport pinned while the reader follows the live end.
 * The content must be observed separately because ResizeObserver does not
 * report changes to an element's scroll extent.
 */
export function useStickToBottom(
  scrollEl: HTMLElement | null,
  contentEl: HTMLElement | null,
): () => boolean {
  // The list initially opens at its last item.
  const pinned = useRef(true);
  const lastTop = useRef(0);

  useEffect(() => {
    if (!scrollEl) return;
    lastTop.current = scrollEl.scrollTop;

    const onScroll = () => {
      const top = scrollEl.scrollTop;
      // The hook only scrolls down, so an upward move releases the pin.
      pinned.current = top < lastTop.current ? false : isAtLiveEnd(scrollEl);
      lastTop.current = top;
    };
    scrollEl.addEventListener("scroll", onScroll, { passive: true });

    const observer = new ResizeObserver(() => {
      if (!pinned.current) return;
      const target = bottomPinTarget(scrollEl);
      if (target !== null) {
        scrollEl.scrollTop = target;
        lastTop.current = target;
      }
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
