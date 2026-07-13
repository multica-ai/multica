import { useEffect, useLayoutEffect, useRef } from "react";

/**
 * Persist a tab's scroll positions across <Activity> visibility transitions.
 *
 * Tabs render under `<Activity mode="visible|hidden">`, which keeps React
 * state but loses DOM scrollTop — the subtree is taken out of layout while
 * hidden and rejoins with scrollTop=0. This hook records every marked
 * container's `scrollTop` while the tab is visible (continuously, via a
 * capture-phase scroll listener) and restores them in a `useLayoutEffect`
 * the next time the tab becomes visible, before the browser paints.
 *
 * Mark scroll containers in views with `data-tab-scroll-root`. The
 * attribute value is the cache key — defaults to `"main"` for unnamed
 * roots. Most pages have a single scroll container, so a bare attribute
 * is enough; named keys are only needed when a page has multiple
 * independently scrollable regions whose positions must all be restored.
 *
 * When the tab's path changes (intra-tab navigation), the saved offsets
 * are dropped — the new route's container shares the same marker key but
 * is a different page, and restoring the old offset would land the user
 * somewhere arbitrary on the new page.
 *
 * For virtualized children (Virtuoso, react-virtual, etc.) the single
 * synchronous `scrollTop = saved` inside useLayoutEffect isn't enough:
 * the child registers its observers in a passive useEffect that fires
 * later, so at restore time the container's scrollHeight has collapsed
 * to clientHeight and the browser clamps our assignment to 0. The
 * restore loops across rAF frames until the assignment sticks, which
 * lets virtualization rehydrate before we give up.
 */
export function useTabScrollRestore(tabPath: string) {
  const containerRef = useRef<HTMLDivElement>(null);
  const savedRef = useRef<Map<string, number>>(new Map());
  const prevPathRef = useRef(tabPath);

  if (prevPathRef.current !== tabPath) {
    savedRef.current.clear();
    prevPathRef.current = tabPath;
  }

  // <Activity> cleans up effects on hidden and re-mounts them on visible,
  // so an empty-deps useLayoutEffect runs exactly on every hidden → visible
  // transition. Restoring here (before the browser paints) handles the
  // common case without a flash.
  //
  // The synchronous set isn't enough for virtualized lists though
  // (issue-detail uses Virtuoso with customScrollParent). Virtuoso wires
  // its scroll/resize observers in a passive useEffect, which fires AFTER
  // useLayoutEffect — so at the moment we try to restore, the spacer that
  // gives the container its tall scrollHeight hasn't been re-established
  // yet. The browser silently clamps `scrollTop = saved` down to 0 because
  // `scrollHeight === clientHeight` in that window. Retry across rAF
  // frames until the set sticks (or we time out around the time any sane
  // child should have laid out, ~500ms).
  useLayoutEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const els = root.querySelectorAll<HTMLElement>("[data-tab-scroll-root]");
    const cancellers: Array<() => void> = [];
    els.forEach((el) => {
      const key = scrollKey(el);
      const saved = savedRef.current.get(key);
      if (saved === undefined) return;
      el.scrollTop = saved;
      if (el.scrollTop === saved) return;

      let cancelled = false;
      let attempts = 0;
      const maxAttempts = 30; // ~500ms at 60fps
      const tick = () => {
        if (cancelled) return;
        el.scrollTop = saved;
        attempts++;
        if (el.scrollTop === saved) return;
        if (attempts >= maxAttempts) return;
        requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
      cancellers.push(() => {
        cancelled = true;
      });
    });
    return () => cancellers.forEach((c) => c());
  }, []);

  useEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const onScroll = (e: Event) => {
      const target = e.target;
      if (!(target instanceof HTMLElement)) return;
      if (!target.hasAttribute("data-tab-scroll-root")) return;
      savedRef.current.set(scrollKey(target), target.scrollTop);
    };
    // Scroll events don't bubble, but capture catches them anyway.
    root.addEventListener("scroll", onScroll, { capture: true, passive: true });
    return () => root.removeEventListener("scroll", onScroll, true);
  }, []);

  return containerRef;
}

function scrollKey(el: HTMLElement): string {
  return el.getAttribute("data-tab-scroll-root") || "main";
}
