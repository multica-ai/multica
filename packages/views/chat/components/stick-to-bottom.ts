"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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

/**
 * Marks the newest row so the list can tell "the reader is looking at the
 * newest message" from "the viewport is where that message was predicted to
 * be". See `useStickToBottom`'s `hasReachedLiveEnd`.
 */
export const LIVE_END_ROW_ATTR = "data-chat-live-end";

/**
 * Whether the newest row's own box has arrived at the bottom of the viewport.
 *
 * Measured, never predicted: scroll geometry is derived from `scrollHeight`,
 * which is an estimate over the unrendered rows, and while that estimate is
 * still wrong it reports the viewport as being at the live end. The row's
 * rect comes from layout, so it only lines up once the rows underneath it
 * have actually been measured.
 */
export function isShowingLiveEndRow(scrollEl: HTMLElement): boolean {
  const row = scrollEl.querySelector(`[${LIVE_END_ROW_ATTR}]`);
  if (!row) return false;
  const viewport = scrollEl.getBoundingClientRect();
  const rect = row.getBoundingClientRect();
  // Its end is on screen — at or above the fold (the footer inset keeps it a
  // little above the edge), and not scrolled off the top.
  return rect.bottom <= viewport.bottom + 1 && rect.bottom > viewport.top;
}

/**
 * How long the newest message must sit at the fold, with the scroll extent
 * unchanged, before the list is revealed.
 *
 * Reaching the live end once is not enough: rows keep settling after their
 * first paint (an image decodes, a code block highlights, a font swaps), and
 * each one moves the content under a reader who is pinned to the bottom. This
 * is the window the list waits for that to stop — raise it to absorb slower
 * content at the cost of showing the conversation later, lower it to paint
 * sooner and let late arrivals shift the view.
 */
const LIVE_END_SETTLE_MS = 120;

// A list whose content never stops moving must not stay hidden. Past this,
// reveal whatever Virtuoso has: a shift beats a blank chat.
const REVEAL_DEADLINE_MS = 1000;

export interface StickToBottom {
  /** For `followOutput`: the reader is still following the live end. */
  isFollowing(): boolean;
  /** Wire to Virtuoso's `totalListHeightChanged`: the content resized. */
  onContentHeightChanged(): void;
  /**
   * False until the newest message has actually arrived at the bottom of the
   * viewport. Keep the list hidden until then: Virtuoso opens it by scrolling
   * to where it PREDICTS that message is, paints there, and only then
   * measures and corrects — so the first painted frame shows the wrong part
   * of the conversation and the next one jumps (MUL-6879).
   */
  hasReachedLiveEnd: boolean;
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

  // Polled rather than driven off Virtuoso's callbacks: `atBottomStateChange`
  // still reads true from before the rows existed, `itemsRendered` reports
  // rows that a measuring pass is about to unmount again, and neither fires
  // on the frame the correcting scroll lands. A frame loop that stops as soon
  // as the list holds still costs a handful of rect reads at open.
  const [hasReachedLiveEnd, setHasReachedLiveEnd] = useState(false);
  useEffect(() => {
    if (!scrollEl || hasReachedLiveEnd) return;
    let frame = 0;
    // The scroll extent is the cheapest proxy for "some row changed size":
    // every late arrival that would shift the view also moves it.
    let lastHeight = -1;
    let steadySince: number | null = null;
    const poll = (now: number) => {
      const height = scrollEl.scrollHeight;
      const steady = height === lastHeight && isShowingLiveEndRow(scrollEl);
      lastHeight = height;
      if (!steady) steadySince = null;
      else if (steadySince === null) steadySince = now;
      else if (now - steadySince >= LIVE_END_SETTLE_MS) {
        setHasReachedLiveEnd(true);
        return;
      }
      frame = requestAnimationFrame(poll);
    };
    frame = requestAnimationFrame(poll);
    const deadline = setTimeout(() => setHasReachedLiveEnd(true), REVEAL_DEADLINE_MS);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(deadline);
    };
  }, [scrollEl, hasReachedLiveEnd]);

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
      hasReachedLiveEnd,
    }),
    [follow, onResize, hasReachedLiveEnd],
  );
}
