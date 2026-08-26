"use client";

import { useEffect, useRef } from "react";

// Bottom-stick for the chat list (TIM-55).
//
// Virtuoso's `followOutput` only fires when the ITEM COUNT changes. A streaming
// assistant turn is ONE row that keeps growing, so the count never moves while
// the reply extends past the fold and the reader watches a truncated tail.
// Virtuoso does have a SIZE_INCREASED recovery, but it is armed for only 100ms
// after a count change — a multi-minute stream leaves that window behind on its
// first token. Nothing re-arms it, so the list simply stops following.
//
// The other half is the viewport, not the content: the composer grows with the
// draft, banners and the queue appear above it, and each one shrinks the list's
// clientHeight. scrollTop stays where it was, so the tail slides out of view
// under the composer.
//
// Both are the same event — the distance to the bottom grew without the reader
// asking for it — so one controller handles both: while the reader is at the
// live end, any content growth or viewport shrink re-pins the scroller to the
// bottom. Scrolling away releases the pin until they come back.
//
// Unlike the newest-first transcript (see transcript-follow.ts), this list is
// bottom-anchored: growth appends BELOW the viewport and never displaces it, so
// absolute position is a truthful signal of reader intent and no gesture
// tracking is needed.

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

export interface BottomStick {
  isPinned(): boolean;
  /** Scroll event: position alone decides whether the reader is still following. */
  onScroll(m: ScrollMetrics): void;
  /**
   * Content grew or the viewport shrank. Returns the scrollTop to apply while
   * the reader is following, or `null` to leave the viewport alone.
   */
  onResize(m: ScrollMetrics): number | null;
}

export function createBottomStick(threshold = STICK_EDGE_THRESHOLD): BottomStick {
  // Chat opens pinned — the list mounts at `initialTopMostItemIndex: LAST`.
  let pinned = true;

  return {
    isPinned: () => pinned,
    onScroll(m) {
      pinned = distanceFromBottom(m) <= threshold;
    },
    onResize(m) {
      if (!pinned) return null;
      const target = Math.max(0, m.scrollHeight - m.clientHeight);
      // Only ever pin DOWNWARD. When content shrinks (a collapsible closing)
      // the browser clamps scrollTop on its own; yanking the viewport back up
      // to a smaller extent would move the reader for no reason.
      return target > m.scrollTop ? target : null;
    },
  };
}

function readMetrics(el: HTMLElement): ScrollMetrics {
  return {
    scrollTop: el.scrollTop,
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  };
}

/**
 * Wires a {@link BottomStick} to a scroll container and the content inside it.
 * Observes BOTH boxes: the container for viewport shrink (composer growth), the
 * content for growth (streaming). Returns the controller so the caller can ask
 * whether the reader is still following.
 */
export function useStickToBottom(
  scrollEl: HTMLElement | null,
  contentEl: HTMLElement | null,
): BottomStick {
  const ref = useRef<BottomStick | null>(null);
  ref.current ??= createBottomStick();
  const stick = ref.current;

  useEffect(() => {
    if (!scrollEl) return;

    const onScroll = () => stick.onScroll(readMetrics(scrollEl));
    scrollEl.addEventListener("scroll", onScroll, { passive: true });

    const observer = new ResizeObserver(() => {
      const target = stick.onResize(readMetrics(scrollEl));
      if (target !== null) scrollEl.scrollTop = target;
    });
    observer.observe(scrollEl);
    if (contentEl) observer.observe(contentEl);

    return () => {
      scrollEl.removeEventListener("scroll", onScroll);
      observer.disconnect();
    };
  }, [scrollEl, contentEl, stick]);

  return stick;
}
