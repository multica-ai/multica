// FIR-1702 — keep the inbox viewport visually still when its rows re-sort.
"use client";

import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";

/**
 * Starting an agent on an inbox row creates a fresh `issue_started`
 * notification whose sort-time is "now", so the row jumps to the top of its
 * box and the whole list reflows under the cursor — the "screen jumps right
 * after" Jesper reported (FIR-1702). This anchors the viewport on a stable row
 * (the open row, else the row whose agent just started) and offsets the scroll
 * position by however far that row moved, so the reorder lands without the page
 * lurching.
 *
 * Pure scroll-position bookkeeping: it never touches the data or the sort, so
 * it cannot reorder anything itself — worst case it no-ops. The anchored row is
 * found in the DOM by its `data-inbox-entry-key` attribute (set on every row).
 */
export function useInboxScrollAnchor(
  scrollRef: RefObject<HTMLElement | null>,
  anchorKey: string | null,
): void {
  // The anchor row's top (relative to the scroll container) together with the
  // key it belongs to, captured on the previous commit / user scroll — i.e.
  // BEFORE any reorder we are about to react to. Keyed so that selecting a
  // different row resets the baseline instead of yanking the scroll.
  const prevRef = useRef<{ key: string; top: number } | null>(null);

  const measure = (): number | null => {
    const c = scrollRef.current;
    if (!c || !anchorKey) return null;
    const el = c.querySelector<HTMLElement>(
      `[data-inbox-entry-key="${cssEscape(anchorKey)}"]`,
    );
    if (!el) return null;
    return el.getBoundingClientRect().top - c.getBoundingClientRect().top;
  };

  // Runs after every commit. When a reorder moved the anchor row, shift
  // scrollTop by the delta so the row stays put in the viewport.
  useLayoutEffect(() => {
    const c = scrollRef.current;
    if (!c) return;
    const newTop = measure();
    if (newTop == null || anchorKey == null) {
      prevRef.current = null;
      return;
    }
    const prev = prevRef.current;
    if (prev && prev.key === anchorKey && Math.abs(newTop - prev.top) > 0.5) {
      c.scrollTop += newTop - prev.top;
    }
    const settled = measure();
    prevRef.current = settled == null ? null : { key: anchorKey, top: settled };
  });

  // User scrolls don't trigger React commits, so refresh the baseline on scroll
  // — otherwise the next reorder would correct against a stale pre-scroll top.
  useEffect(() => {
    const c = scrollRef.current;
    if (!c) return;
    const onScroll = () => {
      const top = measure();
      prevRef.current = anchorKey && top != null ? { key: anchorKey, top } : null;
    };
    c.addEventListener("scroll", onScroll, { passive: true });
    return () => c.removeEventListener("scroll", onScroll);
    // measure closes over scrollRef + anchorKey; re-bind when the anchor changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scrollRef, anchorKey]);
}

function cssEscape(value: string): string {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") {
    return CSS.escape(value);
  }
  return value.replace(/["\\]/g, "\\$&");
}
