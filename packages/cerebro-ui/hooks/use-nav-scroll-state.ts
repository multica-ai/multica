"use client";
import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

interface NavScrollState {
  /** 0–100: how far the scroll container is scrolled. */
  scrollPercent: number;
  /** True when the tabs anchor is at or above the viewport midpoint. */
  isInTabsArea: boolean;
  /** Scroll to page top, OR to the top of the current comment thread (when in tabs area). */
  scrollToTop: () => void;
  /** Scroll so the tabs section appears at the top of the container. */
  scrollToTabs: () => void;
}

/**
 * Tracks scroll position and "tabs area" state for the NavOverlayButton.
 * tabsRef should point to a zero-height anchor element placed immediately
 * before the Tabs component in the scroll container.
 */
export function useNavScrollState(
  containerRef: RefObject<HTMLElement | null>,
  tabsRef: RefObject<HTMLElement | null>,
): NavScrollState {
  const [scrollPercent, setScrollPercent] = useState(0);
  const [isInTabsArea, setIsInTabsArea] = useState(false);
  const isInTabsAreaRef = useRef(false);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const update = () => {
      const max = el.scrollHeight - el.clientHeight;
      setScrollPercent(max > 0 ? Math.round((el.scrollTop / max) * 100) : 0);
      const tabs = tabsRef.current;
      if (tabs) {
        const inTabs =
          tabs.getBoundingClientRect().top <=
          el.getBoundingClientRect().top + el.clientHeight / 2;
        isInTabsAreaRef.current = inTabs;
        setIsInTabsArea(inTabs);
      }
    };
    el.addEventListener("scroll", update, { passive: true });
    update();
    return () => el.removeEventListener("scroll", update);
  }, [containerRef, tabsRef]);

  const scrollToTop = useCallback(() => {
    const c = containerRef.current;
    if (!c) return;
    if (isInTabsAreaRef.current) {
      // In comments area: scroll to the top of the currently-visible comment thread.
      const cTop = c.getBoundingClientRect().top;
      const comments = Array.from(c.querySelectorAll('[id^="comment-"]'));
      const current = comments.find(
        (el) => el.getBoundingClientRect().bottom > cTop + 2,
      );
      if (current) {
        const delta = current.getBoundingClientRect().top - cTop;
        c.scrollTo({ top: c.scrollTop + delta, behavior: "smooth" });
        return;
      }
    }
    c.scrollTo({ top: 0, behavior: "smooth" });
  }, [containerRef]);

  const scrollToTabs = useCallback(() => {
    const c = containerRef.current;
    const t = tabsRef.current;
    if (!c || !t) return;
    const delta = t.getBoundingClientRect().top - c.getBoundingClientRect().top;
    c.scrollTo({ top: c.scrollTop + delta, behavior: "smooth" });
  }, [containerRef, tabsRef]);

  return { scrollPercent, isInTabsArea, scrollToTop, scrollToTabs };
}
