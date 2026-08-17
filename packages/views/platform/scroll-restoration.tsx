"use client";

import { createContext, useCallback, useContext, type ReactNode } from "react";

/**
 * Pull-based scroll restoration channel (MUL-4741 state-restoration
 * protocol).
 *
 * The platform (desktop tab host) captures scroll offsets when a view is
 * about to go away and serves them back through this context when the view
 * mounts again. Restoration is an INPUT to the view, not a post-hoc DOM
 * mutation from outside:
 *
 *   - virtualized lists feed the offset into `initialScrollTop`, so the
 *     list's first render already materializes the rows around the saved
 *     offset — no "start at top, then jump" and no fighting the list's own
 *     height model;
 *   - plain scroll containers assign the offset in their ref callback at
 *     attach time (pre-paint).
 *
 * Container keys mirror the `data-tab-scroll-root` attribute values the
 * capture side scans for; the platform scopes them per route + tab. On web
 * (no provider) every lookup returns undefined and views behave as before.
 */
export interface ScrollRestorationAdapter {
  /**
   * The saved offset for a container key on the current route, or undefined
   * when there is nothing to restore.
   */
  get(containerKey: string): { top: number; height: number } | undefined;
  /**
   * Generic restorable view-state entries — the memento's second kind of
   * cargo, for state that must survive the view's unmount but is not a
   * scroll offset (e.g. "this route's comment-highlight deep link already
   * landed"). Unlike scroll, these are not captured by DOM scan: the view
   * writes them through `setViewState` at the moment the state changes and
   * reads them back at mount. Scoped per route + tab like scroll entries.
   *
   * Optional so scroll-only adapters stay valid; the hooks treat a missing
   * implementation as "nothing stored / write ignored".
   */
  getViewState?(entryKey: string): string | undefined;
  /** Write (string) or clear (undefined) an entry for the current route. */
  setViewState?(entryKey: string, value: string | undefined): void;
}

const ScrollRestorationContext = createContext<ScrollRestorationAdapter | null>(
  null,
);

export function ScrollRestorationProvider({
  adapter,
  children,
}: {
  adapter: ScrollRestorationAdapter;
  children: ReactNode;
}) {
  return (
    <ScrollRestorationContext.Provider value={adapter}>
      {children}
    </ScrollRestorationContext.Provider>
  );
}

/**
 * The saved scroll offset for a container key on the current route, or
 * undefined. Read it once at mount (e.g. into a Virtuoso `initialScrollTop`)
 * — it reflects the state captured when this route was last left.
 */
export function useRestoredScrollOffset(containerKey: string): number | undefined {
  const adapter = useContext(ScrollRestorationContext);
  return adapter?.get(containerKey)?.top;
}

/**
 * Ref callback for PLAIN (non-virtualized) scroll containers: assigns the
 * saved offset when the element attaches, which happens during commit —
 * before paint — so the first visible frame is already at the restored
 * position. Compose it with other refs via a merged callback.
 */
export function useRestoredScrollRef(
  containerKey: string,
): (el: HTMLElement | null) => void {
  const adapter = useContext(ScrollRestorationContext);
  return useCallback(
    (el: HTMLElement | null) => {
      if (!el) return;
      const saved = adapter?.get(containerKey);
      if (!saved || saved.top <= 0) return;
      el.scrollTop = saved.top;
    },
    [adapter, containerKey],
  );
}

/**
 * The saved view-state entry for a key on the current route, or undefined.
 * Read at mount like the scroll offset — it reflects what this view recorded
 * before it was last unmounted.
 */
export function useRestoredViewState(entryKey: string): string | undefined {
  const adapter = useContext(ScrollRestorationContext);
  return adapter?.getViewState?.(entryKey);
}

/**
 * Imperative writer for view-state entries: call it when the restorable
 * state changes (pass undefined to clear). No-op without a provider or on
 * adapters that only track scroll.
 */
export function useViewStateWriter(): (
  entryKey: string,
  value: string | undefined,
) => void {
  const adapter = useContext(ScrollRestorationContext);
  return useCallback(
    (entryKey: string, value: string | undefined) => {
      adapter?.setViewState?.(entryKey, value);
    },
    [adapter],
  );
}
