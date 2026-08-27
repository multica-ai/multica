"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import {
  createLiveEndFollow,
  FOLLOW_EDGE_THRESHOLD,
  LINE_SCROLL_PX,
  type LiveEndFollow,
} from "../../common/task-transcript/transcript-follow";

// Bottom-stick for the chat list (TIM-55).
//
// Virtuoso's `followOutput` only fires when the ITEM COUNT changes. A
// streaming assistant reply is ONE row that keeps growing, and a growing
// composer shrinks the list's viewport — neither changes the count, so the
// list has to re-pin itself while the reader is at the live end.
//
// Reader intent comes from the shared live-end latch (transcript-follow.ts):
// scroll position and direction alone cannot separate a reader leaving the
// live end from the browser moving the viewport on its own (a scrollTop clamp
// after the composer collapses, scroll anchoring), so the latch judges intent
// from accumulated input deltas and releases only past FOLLOW_EDGE_THRESHOLD —
// the same forgiveness the list grants Virtuoso via `atBottomThreshold`.

export interface ScrollMetrics {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
}

export function distanceFromBottom(m: ScrollMetrics): number {
  return Math.max(0, m.scrollHeight - m.scrollTop - m.clientHeight);
}

export function isAtLiveEnd(m: ScrollMetrics): boolean {
  return distanceFromBottom(m) <= FOLLOW_EDGE_THRESHOLD;
}

/** Returns a downward-only scroll target, or `null` when no scrolling is needed. */
export function bottomPinTarget(m: ScrollMetrics): number | null {
  const target = Math.max(0, m.scrollHeight - m.clientHeight);
  return target > m.scrollTop ? target : null;
}

export interface StickToBottom {
  /** For `followOutput`: the reader is still following the live end. */
  isFollowing(): boolean;
  /** Wire to Virtuoso's `totalListHeightChanged`: the content resized. */
  onContentHeightChanged(): void;
}

/**
 * Keeps `scrollEl` pinned to the bottom while the reader follows the live
 * end. Viewport resizes (the composer) are observed here; content resizes
 * (streaming) must be reported through `onContentHeightChanged`, because a
 * ResizeObserver on the container never sees its scroll extent.
 */
export function useStickToBottom(scrollEl: HTMLElement | null): StickToBottom {
  const followRef = useRef<LiveEndFollow | null>(null);
  if (followRef.current === null) {
    followRef.current = createLiveEndFollow();
    // Unlike the transcript, the chat list is always live. Activated at
    // creation, not in an effect: `followOutput` reads the latch on the
    // very first render.
    followRef.current.setActive(true);
  }
  const follow = followRef.current;

  const enforce = useCallback(() => {
    if (!scrollEl) return;
    if (follow.onScroll(distanceFromBottom(scrollEl))) {
      const target = bottomPinTarget(scrollEl);
      if (target !== null) scrollEl.scrollTop = target;
    }
  }, [scrollEl, follow]);

  useEffect(() => {
    if (!scrollEl) return;

    // Mirror of the transcript dialog's wiring with the live end at the
    // BOTTOM: away from it is up, so every input sign flips.
    const onWheel = (e: WheelEvent) => {
      const scale =
        e.deltaMode === 1 ? LINE_SCROLL_PX : e.deltaMode === 2 ? scrollEl.clientHeight : 1;
      follow.input(-e.deltaY * scale);
    };
    let lastTouchY: number | null = null;
    const onTouchStart = (e: TouchEvent) => {
      lastTouchY = e.touches[0]?.clientY ?? null;
    };
    const onTouchMove = (e: TouchEvent) => {
      const y = e.touches[0]?.clientY;
      if (y === undefined) return;
      // Finger moving down scrolls the content up (away from the live end).
      if (lastTouchY !== null) follow.input(y - lastTouchY);
      lastTouchY = y;
    };
    const onKeyDown = (e: KeyboardEvent) => {
      // Only keys aimed at the scroller itself; Space/arrows bubbling from
      // row controls are not scroll intent.
      if (e.target !== scrollEl) return;
      if (e.key === "ArrowUp") follow.input(LINE_SCROLL_PX);
      else if (e.key === "ArrowDown") follow.input(-LINE_SCROLL_PX);
      else if (e.key === "PageUp") follow.input(scrollEl.clientHeight);
      else if (e.key === "PageDown" || e.key === " ") follow.input(-scrollEl.clientHeight);
      else if (e.key === "Home") follow.disengage();
    };
    const onPointerDown = (e: MouseEvent) => {
      follow.pointerDown(e.target === scrollEl);
    };
    const onPointerUp = () => {
      follow.pointerUp();
    };
    let atEdge = isAtLiveEnd(scrollEl);
    const onScroll = () => {
      const nowAtEdge = isAtLiveEnd(scrollEl);
      if (nowAtEdge !== atEdge) {
        atEdge = nowAtEdge;
        follow.onAtEdgeChange(nowAtEdge);
      }
      enforce();
    };

    // The composer growing (or banners appearing) shrinks the container's box
    // without any scroll event; content growth arrives separately through
    // `onContentHeightChanged`.
    const observer = new ResizeObserver(enforce);
    observer.observe(scrollEl);
    scrollEl.addEventListener("scroll", onScroll, { passive: true });
    scrollEl.addEventListener("wheel", onWheel, { passive: true });
    scrollEl.addEventListener("touchstart", onTouchStart, { passive: true });
    scrollEl.addEventListener("touchmove", onTouchMove, { passive: true });
    scrollEl.addEventListener("keydown", onKeyDown);
    scrollEl.addEventListener("mousedown", onPointerDown);
    window.addEventListener("mouseup", onPointerUp, { capture: true });

    return () => {
      observer.disconnect();
      scrollEl.removeEventListener("scroll", onScroll);
      scrollEl.removeEventListener("wheel", onWheel);
      scrollEl.removeEventListener("touchstart", onTouchStart);
      scrollEl.removeEventListener("touchmove", onTouchMove);
      scrollEl.removeEventListener("keydown", onKeyDown);
      scrollEl.removeEventListener("mousedown", onPointerDown);
      window.removeEventListener("mouseup", onPointerUp, { capture: true });
      // The scroller can detach mid-drag; a stuck held-mouse flag would
      // suppress pinning forever.
      follow.pointerUp();
    };
  }, [scrollEl, follow, enforce]);

  return useMemo(
    () => ({
      isFollowing: () => follow.isFollowing(),
      onContentHeightChanged: enforce,
    }),
    [follow, enforce],
  );
}
