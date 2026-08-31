"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import {
  createLiveEndFollow,
  FOLLOW_EDGE_THRESHOLD,
  LINE_SCROLL_PX,
  type LiveEndFollow,
} from "../../common/task-transcript/transcript-follow";

// Bottom-stick for the chat list.
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
//
// Input is staged, and only the list's own scroll promotes it: rows contain
// nested scrollers (capped `overflow-auto` code blocks) whose wheel/touch
// events bubble here without moving the list, and on a conversation shorter
// than the viewport nothing scrolls at all. Unconsumed input must not release
// the follow. The same rule works in reverse for keys: any scroll key from
// anywhere in the container stages intent, and if the browser answers it by
// scrolling the list (focus on a row control page-scrolls the nearest
// scrollable ancestor), the scroll confirms it — the reader is never pinned
// back over their own keypress.

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
 *
 * `pinToLiveEnd` applies the correction and MUST scroll through Virtuoso
 * (`scrollToIndex` at the last row), never by writing `scrollTop` here. In a
 * virtualised list `scrollHeight` covers unrendered rows with an ESTIMATE, so
 * `scrollHeight - clientHeight` is not the bottom. Jumping there drops
 * Virtuoso outside its rendered window; it measures the rows it lands on, the
 * estimate moves, the "bottom" moves with it, and the next height change pins
 * again — a correction that never converges. Worse, each of those jumps
 * reaches the latch as displacement no input can account for, so the reader's
 * own wheel scrolls are refused attribution and pinned away: the follow never
 * releases and the list cannot be scrolled at all (MUL-6879). An index-based
 * scroll to the last row is what Virtuoso's own `followOutput` does, and it
 * converges because Virtuoso measures that row before deciding where to stop.
 */
export function useStickToBottom(
  scrollEl: HTMLElement | null,
  pinToLiveEnd: () => void,
): StickToBottom {
  const followRef = useRef<LiveEndFollow | null>(null);
  if (followRef.current === null) {
    followRef.current = createLiveEndFollow();
    // Unlike the transcript, the chat list is always live. Activated at
    // creation, not in an effect: `followOutput` reads the latch on the
    // very first render.
    followRef.current.setActive(true);
  }
  const follow = followRef.current;

  const pinRef = useRef(pinToLiveEnd);
  pinRef.current = pinToLiveEnd;
  const pin = useCallback(() => {
    pinRef.current();
  }, []);

  // Content grew or the viewport resized — displacement with no scroll event,
  // so it can never promote staged reader input.
  const onResize = useCallback(() => {
    if (!scrollEl) return;
    if (follow.onResize(distanceFromBottom(scrollEl))) pin();
  }, [scrollEl, follow, pin]);

  useEffect(() => {
    if (!scrollEl) return;

    // Mirror of the transcript dialog's wiring with the live end at the
    // BOTTOM: away from it is up, so every input sign flips.
    let inputFrame: number | null = null;
    const stageInput = (delta: number) => {
      follow.input(delta);
      if (inputFrame !== null) cancelAnimationFrame(inputFrame);
      inputFrame = requestAnimationFrame(() => {
        inputFrame = null;
        follow.endInputFrame();
      });
    };
    const onWheel = (e: WheelEvent) => {
      const scale =
        e.deltaMode === 1 ? LINE_SCROLL_PX : e.deltaMode === 2 ? scrollEl.clientHeight : 1;
      stageInput(-e.deltaY * scale);
    };
    let touchId: number | null = null;
    let lastTouchY: number | null = null;
    const trackedTouch = (touches: TouchList) =>
      Array.from(touches).find((touch) => touch.identifier === touchId);
    const onTouchStart = (e: TouchEvent) => {
      if (touchId !== null) return;
      const touch = e.changedTouches[0] ?? e.touches[0];
      if (!touch) return;
      touchId = touch.identifier;
      lastTouchY = touch.clientY;
      follow.touchStart();
    };
    const onTouchMove = (e: TouchEvent) => {
      const touch = trackedTouch(e.touches);
      if (!touch) return;
      // Finger moving down scrolls the content up (away from the live end).
      if (lastTouchY !== null) stageInput(touch.clientY - lastTouchY);
      lastTouchY = touch.clientY;
    };
    const onTouchEnd = (e: TouchEvent) => {
      if (touchId === null || trackedTouch(e.touches)) return;
      touchId = null;
      lastTouchY = null;
      follow.touchEnd();
    };
    // No target guard: this container is not focusable, so scroll keys always
    // arrive from a focused row control, and the browser answers them by
    // scrolling the nearest scrollable ancestor — this list. Staging makes
    // that safe in the other direction too: a key a control consumed (Space
    // activating a button, Home in a text field) never scrolls the list, so
    // the staged intent is never promoted.
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "ArrowUp") stageInput(LINE_SCROLL_PX);
      else if (e.key === "ArrowDown") stageInput(-LINE_SCROLL_PX);
      else if (e.key === "PageUp") stageInput(scrollEl.clientHeight);
      else if (e.key === "PageDown") stageInput(-scrollEl.clientHeight);
      // Shift+Space pages UP — away from the live end.
      else if (e.key === " ")
        stageInput(e.shiftKey ? scrollEl.clientHeight : -scrollEl.clientHeight);
      else if (e.key === "Home") stageInput(scrollEl.scrollHeight);
      else if (e.key === "End") stageInput(-scrollEl.scrollHeight);
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
      // The list itself moved: this is what promotes staged input.
      if (follow.onScroll(distanceFromBottom(scrollEl))) pin();
    };

    // The composer growing (or banners appearing) shrinks the container's box
    // without any scroll event; content growth arrives separately through
    // `onContentHeightChanged`.
    const observer = new ResizeObserver(onResize);
    observer.observe(scrollEl);
    scrollEl.addEventListener("scroll", onScroll, { passive: true });
    scrollEl.addEventListener("wheel", onWheel, { passive: true });
    scrollEl.addEventListener("touchstart", onTouchStart, { passive: true });
    scrollEl.addEventListener("touchmove", onTouchMove, { passive: true });
    scrollEl.addEventListener("touchend", onTouchEnd, { passive: true });
    scrollEl.addEventListener("touchcancel", onTouchEnd, { passive: true });
    scrollEl.addEventListener("keydown", onKeyDown);
    scrollEl.addEventListener("mousedown", onPointerDown);
    window.addEventListener("mouseup", onPointerUp, { capture: true });

    return () => {
      if (inputFrame !== null) cancelAnimationFrame(inputFrame);
      follow.endInputFrame();
      observer.disconnect();
      scrollEl.removeEventListener("scroll", onScroll);
      scrollEl.removeEventListener("wheel", onWheel);
      scrollEl.removeEventListener("touchstart", onTouchStart);
      scrollEl.removeEventListener("touchmove", onTouchMove);
      scrollEl.removeEventListener("touchend", onTouchEnd);
      scrollEl.removeEventListener("touchcancel", onTouchEnd);
      scrollEl.removeEventListener("keydown", onKeyDown);
      scrollEl.removeEventListener("mousedown", onPointerDown);
      window.removeEventListener("mouseup", onPointerUp, { capture: true });
      // The scroller can detach mid-drag; a stuck held-mouse flag would
      // suppress pinning forever.
      follow.pointerUp();
      follow.touchEnd();
    };
  }, [scrollEl, follow, pin, onResize]);

  return useMemo(
    () => ({
      isFollowing: () => follow.isFollowing(),
      onContentHeightChanged: onResize,
    }),
    [follow, onResize],
  );
}
