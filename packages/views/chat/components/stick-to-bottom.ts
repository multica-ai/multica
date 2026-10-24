"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { flushSync } from "react-dom";
import type { VirtuosoHandle } from "react-virtuoso";
import { CHAT_LIVE_END_THRESHOLD, createChatFollow, distanceFromBottom, type ScrollMetrics } from "./chat-follow";

export { distanceFromBottom, type ScrollMetrics } from "./chat-follow";

interface ReaderAnchor {
  itemIndex: number;
  offset: number;
  firstItemIndex: number;
}

// Bottom-stick for the chat list.
//
// Virtuoso's `followOutput` only fires when the ITEM COUNT changes. A
// streaming assistant reply is ONE row that keeps growing, and a growing
// composer shrinks the list's viewport — neither changes the count, so the
// list has to re-pin itself while the reader is at the live end.
//
// Input stays eligible across rendering frames; a matching change in the
// list's own scrollTop confirms it. Content growth changes distance from the
// bottom without reader motion, so that distance never attributes input.

export function isAtLiveEnd(m: ScrollMetrics): boolean {
  return distanceFromBottom(m) <= CHAT_LIVE_END_THRESHOLD;
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

// Virtuoso 4.18.7 keeps an outstanding index request alive for at most 1200ms.
// Recovery only covers that request, never the reader's whole paused session.
const RELEASE_RECOVERY_MS = 1200;

export interface StickToBottom {
  /** The synchronous latch also guards callbacks retained by Virtuoso. */
  isFollowing(): boolean;
  /** Literal false disables Virtuoso's internal resize-follow branches too. */
  followOutput: false | ((atBottom: boolean) => "auto" | false);
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
 * Corrections MUST scroll through Virtuoso
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
  virtuosoRef: RefObject<VirtuosoHandle | null>,
  active = true,
  fetchingOlderMessages = false,
): StickToBottom {
  const followRef = useRef<ReturnType<typeof createChatFollow> | null>(null);
  if (followRef.current === null) {
    followRef.current = createChatFollow();
  }
  const follow = followRef.current;
  const activeRef = useRef(active);
  activeRef.current = active;
  const [following, setFollowing] = useState(true);
  const followingRef = useRef(true);
  const readerAnchor = useRef<ReaderAnchor | null>(null);
  const correctingReader = useRef(false);
  const releaseRecoveryUntil = useRef(0);

  const pin = useCallback(() => {
    if (!activeRef.current || !follow.isFollowing()) return;
    const before = scrollEl?.scrollTop;
    virtuosoRef.current?.scrollToIndex({ index: "LAST", align: "end" });
    // Some requests land synchronously, others wait for row measurements.
    // Record only an observed programmatic move, never an assumed target.
    if (scrollEl && scrollEl.scrollTop !== before) follow.syncPosition(scrollEl.scrollTop);
  }, [scrollEl, virtuosoRef, follow]);

  const scrollToReader = useCallback((behavior: "auto" | "smooth") => {
    if (!readerAnchor.current || !scrollEl) return;
    const row = scrollEl.querySelector<HTMLElement>("[data-index][data-item-index]");
    if (!row) return;
    const firstItemIndex = Number(row.dataset.itemIndex) - Number(row.dataset.index);
    const before = scrollEl.scrollTop;
    // DOM item indexes include firstItemIndex; scrollToIndex is relative.
    // An absolute saved anchor also survives prepending older history.
    virtuosoRef.current?.scrollToIndex({
      index: readerAnchor.current.itemIndex - firstItemIndex,
      align: "start",
      offset: readerAnchor.current.offset,
      behavior,
    });
    if (scrollEl.scrollTop !== before) follow.syncPosition(scrollEl.scrollTop);
  }, [follow, scrollEl, virtuosoRef]);

  const publishFollowing = useCallback(() => {
    const next = follow.isFollowing();
    if (next === followingRef.current) return;
    followingRef.current = next;
    releaseRecoveryUntil.current = next ? 0 : Date.now() + RELEASE_RECOVERY_MS;
    // Run before Virtuoso's bubble-phase scroll listener can resize-follow.
    flushSync(() => setFollowing(next));
    if (next) {
      readerAnchor.current = null;
      correctingReader.current = false;
    }
  }, [follow]);

  // Polled rather than driven off Virtuoso's callbacks: `atBottomStateChange`
  // still reads true from before the rows existed, `itemsRendered` reports
  // rows that a measuring pass is about to unmount again, and neither fires
  // on the frame the correcting scroll lands. A frame loop that stops as soon
  // as the list holds still costs a handful of rect reads at open.
  const [hasReachedLiveEnd, setHasReachedLiveEnd] = useState(false);
  useEffect(() => {
    if (!active || !scrollEl || hasReachedLiveEnd) return;
    let frame = 0;
    // The scroll extent is the cheapest proxy for "some row changed size":
    // every late arrival that would shift the view also moves it.
    let lastHeight = -1;
    let steadySince: number | null = null;
    const poll = (now: number) => {
      if (!activeRef.current) return;
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
  }, [active, scrollEl, hasReachedLiveEnd]);

  // Content grew or the viewport resized — displacement with no scroll event,
  // so it can never promote staged reader input.
  const onResize = useCallback(() => {
    if (!activeRef.current || !scrollEl) return;
    if (follow.onResize(scrollEl)) pin();
    else if (!follow.isFollowing() && readerAnchor.current && Date.now() < releaseRecoveryUntil.current) {
      // While released requests settle, size changes can also trigger
      // Virtuoso's upward-scroll compensation. Anchor this known layout
      // change, without treating arbitrary scroll events as old requests.
      correctingReader.current = true;
      scrollToReader("auto");
    }
  }, [scrollEl, follow, pin, scrollToReader]);

  useEffect(() => {
    if (!active || !scrollEl) return;

    follow.syncPosition(scrollEl.scrollTop);
    const stageInput = (delta: number, event: Event) => {
      if (!activeRef.current) return;
      if (event.defaultPrevented || nestedScrollerConsumes(event.target, scrollEl, delta)
        || (delta < 0 && scrollEl.scrollTop <= 0)) {
        follow.clearInput();
        return;
      }
      follow.input(delta, () => !event.defaultPrevented);
    };
    const onWheel = (e: WheelEvent) => {
      if (e.ctrlKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) {
        follow.clearInput();
        return;
      }
      if (e.deltaY) stageInput(e.deltaY, e);
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
      if (lastTouchY !== null) stageInput(lastTouchY - touch.clientY, e);
      lastTouchY = touch.clientY;
    };
    const onTouchEnd = (e: TouchEvent) => {
      if (touchId === null || trackedTouch(e.touches)) return;
      touchId = null;
      lastTouchY = null;
      follow.touchEnd();
      onResize();
    };
    const onNavigation = () => {
      if (follow.isFollowing()) return;
      // Focus and browser find can legitimately scroll all the way to the
      // bottom even inside the recovery window. Their navigation takes over.
      releaseRecoveryUntil.current = 0;
      follow.clearInput();
      if (correctingReader.current) {
        readerAnchor.current = captureReaderAnchor(scrollEl);
        correctingReader.current = false;
        scrollToReader("smooth");
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.defaultPrevented && !e.isComposing
        && (e.key === "Tab" || ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f"))) onNavigation();
      if (e.defaultPrevented || e.isComposing || keyboardControlConsumes(e)) {
        follow.clearInput();
        return;
      }
      if (["ArrowUp", "PageUp", "Home"].includes(e.key)) stageInput(-1, e);
      else if (["ArrowDown", "PageDown"].includes(e.key)) stageInput(1, e);
      else if (e.key === " ") stageInput(e.shiftKey ? -1 : 1, e);
      else if (e.key === "End") {
        follow.resume();
        publishFollowing();
        pin();
      }
    };
    const onPointerDown = (e: MouseEvent) => {
      follow.pointerDown(e.target === scrollEl);
    };
    const onPointerUp = () => {
      follow.pointerUp();
      onResize();
    };
    const onScroll = () => {
      if (!activeRef.current) return;
      const { pin: shouldPin, readerMoved, moved } = follow.onScroll(scrollEl);
      const changed = follow.isFollowing() !== followingRef.current;
      if (!follow.isFollowing() && readerMoved) readerAnchor.current = captureReaderAnchor(scrollEl);
      publishFollowing();
      if (!follow.isFollowing()) {
        if (correctingReader.current && readerAnchor.current) {
          const anchor = readerAnchor.current;
          const rendered = scrollEl.querySelector<HTMLElement>("[data-index][data-item-index]");
          const firstItemIndex = rendered && Number(rendered.dataset.itemIndex) - Number(rendered.dataset.index);
          if (firstItemIndex !== null && firstItemIndex !== anchor.firstItemIndex
            && Date.now() < releaseRecoveryUntil.current) {
            // A prepend can commit between the LAST landing and recovery.
            // Rebase that specific request once its new DOM indexes arrive.
            anchor.firstItemIndex = firstItemIndex;
            scrollToReader("auto");
            return;
          }
        }
        if (readerMoved && (changed || correctingReader.current)) {
          // Cancel the outstanding index request. Smooth at the current
          // position completes immediately instead of opening an auto retry.
          correctingReader.current = false;
          scrollToReader("smooth");
        } else if (!readerMoved && moved > 1
          && Date.now() < releaseRecoveryUntil.current && isAtLiveEnd(scrollEl)) {
          // Virtuoso may have queued resize-follow before the prop became
          // false. Only recover a late landing at the bottom, within its
          // lifetime; allow other browser/programmatic navigation to proceed.
          correctingReader.current = true;
          scrollToReader("auto");
        } else if (!readerMoved && Math.abs(moved) > 1) {
          const anchor = captureReaderAnchor(scrollEl);
          // Prepending changes the virtual origin and scrollTop together.
          // Keep the saved logical row through that layout adjustment.
          if (anchor && anchor.firstItemIndex === readerAnchor.current?.firstItemIndex) {
            readerAnchor.current = anchor;
            if (correctingReader.current) {
              correctingReader.current = false;
              scrollToReader("smooth");
            }
          }
        }
      }
      if (shouldPin) pin();
    };

    // The composer growing (or banners appearing) shrinks the container's box
    // without any scroll event; content growth arrives separately through
    // `onContentHeightChanged`.
    const observer = new ResizeObserver(onResize);
    observer.observe(scrollEl);
    scrollEl.addEventListener("scroll", onScroll, { passive: true, capture: true });
    scrollEl.addEventListener("wheel", onWheel, { passive: true });
    scrollEl.addEventListener("touchstart", onTouchStart, { passive: true });
    scrollEl.addEventListener("touchmove", onTouchMove, { passive: true });
    scrollEl.addEventListener("touchend", onTouchEnd, { passive: true });
    scrollEl.addEventListener("touchcancel", onTouchEnd, { passive: true });
    scrollEl.addEventListener("keydown", onKeyDown);
    scrollEl.addEventListener("focusin", onNavigation);
    scrollEl.addEventListener("mousedown", onPointerDown);
    window.addEventListener("mouseup", onPointerUp, { capture: true });

    return () => {
      follow.clearInput();
      observer.disconnect();
      scrollEl.removeEventListener("scroll", onScroll, { capture: true });
      scrollEl.removeEventListener("wheel", onWheel);
      scrollEl.removeEventListener("touchstart", onTouchStart);
      scrollEl.removeEventListener("touchmove", onTouchMove);
      scrollEl.removeEventListener("touchend", onTouchEnd);
      scrollEl.removeEventListener("touchcancel", onTouchEnd);
      scrollEl.removeEventListener("keydown", onKeyDown);
      scrollEl.removeEventListener("focusin", onNavigation);
      scrollEl.removeEventListener("mousedown", onPointerDown);
      window.removeEventListener("mouseup", onPointerUp, { capture: true });
      // The scroller can detach mid-drag; a stuck held-mouse flag would
      // suppress pinning forever.
      follow.pointerUp();
      follow.touchEnd();
    };
  }, [active, scrollEl, follow, pin, onResize, publishFollowing, scrollToReader]);

  return useMemo(
    () => ({
      isFollowing: () => active && follow.isFollowing(),
      followOutput: !active || !following || fetchingOlderMessages ? false :
        (atBottom: boolean) => activeRef.current && atBottom && follow.isFollowing() ? "auto" as const : false,
      onContentHeightChanged: onResize,
      hasReachedLiveEnd,
    }),
    [active, following, fetchingOlderMessages, follow, onResize, hasReachedLiveEnd],
  );
}

function captureReaderAnchor(scrollEl: HTMLElement): ReaderAnchor | null {
  const viewportTop = scrollEl.getBoundingClientRect().top + scrollEl.clientTop;
  const row = Array.from(scrollEl.querySelectorAll<HTMLElement>("[data-index][data-item-index]"))
    .find((item) => item.getBoundingClientRect().bottom > viewportTop);
  return row ? {
    itemIndex: Number(row.dataset.itemIndex),
    offset: viewportTop - row.getBoundingClientRect().top,
    firstItemIndex: Number(row.dataset.itemIndex) - Number(row.dataset.index),
  } : null;
}

function nestedScrollerConsumes(target: EventTarget | null, root: HTMLElement, delta: number) {
  for (let el = target instanceof Element ? target : null; el && el !== root; el = el.parentElement) {
    const style = getComputedStyle(el);
    if (!/(auto|scroll|overlay)/.test(style.overflowY)) continue;
    const canScroll = delta < 0 ? el.scrollTop > 0 : el.scrollTop < el.scrollHeight - el.clientHeight;
    if (canScroll || ["contain", "none"].includes(style.overscrollBehaviorY)) return true;
  }
  return false;
}

function keyboardControlConsumes(event: KeyboardEvent) {
  const target = event.target instanceof Element ? event.target : null;
  if (event.metaKey || event.altKey || (event.ctrlKey && !["Home", "End"].includes(event.key))) return true;
  if (target?.closest("input, textarea, select, [contenteditable]:not([contenteditable='false']), [role='textbox'], [role='combobox'], [role='listbox'], [role='slider'], [role='spinbutton'], [role='menu'], [role='tree']")) return true;
  return event.key === " " && !event.shiftKey && !!target?.closest("button, [role='button'], a[href]");
}
