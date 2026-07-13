import { useEffect, useLayoutEffect, useRef } from "react";
import { useTabStore, getTabById } from "@/stores/tab-store";

/**
 * Persist and restore a tab's scroll positions across its mount cycle.
 *
 * In the Session model only the active tab is rendered; switching tabs unmounts
 * the previous tab's subtree entirely (losing DOM scrollTop). This hook records
 * every marked container's `scrollTop` while the tab is mounted, persists them
 * into the tab session when the subtree unmounts (switch away / close), and
 * restores them the next time the tab mounts — before the browser paints.
 *
 * Mark scroll containers in views with `data-tab-scroll-root`. The attribute
 * value is the cache key — defaults to `"main"` for unnamed roots. Named keys
 * are only needed when a page has multiple independently scrollable regions.
 *
 * Offsets are tagged with the tab's path. On intra-tab navigation (path change)
 * the saved offsets are dropped — the new route shares the marker key but is a
 * different page, so restoring the old offset would land the user somewhere
 * arbitrary. Restoration also ignores a stored offset captured on a different
 * path.
 *
 * For virtualized children (Virtuoso, react-virtual, etc.) the single
 * synchronous `scrollTop = saved` inside useLayoutEffect isn't enough: the
 * child registers its observers in a passive useEffect that fires later, so at
 * restore time the container's scrollHeight has collapsed to clientHeight and
 * the browser clamps our assignment to 0. The restore loops across rAF frames
 * until the assignment sticks, which lets virtualization rehydrate first.
 */
export function useTabScrollRestore(tabId: string, tabPath: string) {
  const containerRef = useRef<HTMLDivElement>(null);
  const savedRef = useRef<Map<string, number> | null>(null);
  const prevPathRef = useRef(tabPath);

  // Hydrate once from the tab's persisted scroll — but only if it was captured
  // on the current path. Reading the store imperatively (not subscribing) keeps
  // this a one-time mount concern.
  if (savedRef.current === null) {
    const stored = getTabById(useTabStore.getState(), tabId)?.scroll;
    savedRef.current =
      stored && stored.path === tabPath
        ? new Map(Object.entries(stored.offsets))
        : new Map();
  }

  if (prevPathRef.current !== tabPath) {
    savedRef.current.clear();
    prevPathRef.current = tabPath;
  }

  // Restore before paint on mount. The synchronous set handles the common case;
  // the rAF retry covers virtualized lists (see the docstring).
  useLayoutEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const saved = savedRef.current;
    if (!saved) return;
    const els = root.querySelectorAll<HTMLElement>("[data-tab-scroll-root]");
    const cancellers: Array<() => void> = [];
    els.forEach((el) => {
      const key = scrollKey(el);
      const target = saved.get(key);
      if (target === undefined) return;
      el.scrollTop = target;
      if (el.scrollTop === target) return;

      let cancelled = false;
      let attempts = 0;
      const maxAttempts = 30; // ~500ms at 60fps
      const tick = () => {
        if (cancelled) return;
        el.scrollTop = target;
        attempts++;
        if (el.scrollTop === target) return;
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
      savedRef.current?.set(scrollKey(target), target.scrollTop);
    };
    // Scroll events don't bubble, but capture catches them anyway.
    root.addEventListener("scroll", onScroll, { capture: true, passive: true });
    return () => root.removeEventListener("scroll", onScroll, true);
  }, []);

  // Persist offsets into the tab session when the subtree unmounts, so
  // switching back can restore them. No-ops if the tab was closed.
  useEffect(() => {
    return () => {
      const saved = savedRef.current;
      if (!saved) return;
      useTabStore.getState().updateTabScroll(tabId, {
        path: prevPathRef.current,
        offsets: Object.fromEntries(saved),
      });
    };
  }, [tabId]);

  return containerRef;
}

function scrollKey(el: HTMLElement): string {
  return el.getAttribute("data-tab-scroll-root") || "main";
}
