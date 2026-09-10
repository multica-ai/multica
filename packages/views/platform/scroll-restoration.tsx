"use client";

import {
  createContext,
  useCallback,
  useContext,
  type ReactNode,
} from "react";

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

/**
 * One captured scroll entry. `contentKey` is an optional fingerprint of the
 * rendered content (multica-ai#6405): views that render untrusted HTML inside
 * a sandboxed iframe cannot be scrolled externally and register an external
 * capture source instead; they tag the entry with a hash of the rendered HTML
 * so a re-upload to the same attachment id refuses to restore a stale offset.
 * Plain containers leave it undefined and get identity comparison (undefined
 * === undefined), so they are unaffected.
 */
export interface ScrollRestorationEntry {
  top: number;
  height: number;
  contentKey?: string;
}

/**
 * A scroll source that lives outside the outer DOM — e.g. the document inside
 * a sandboxed iframe, which the platform cannot query for `scrollTop` due to
 * the opaque-origin boundary. The owning view reports latest values into a
 * ref and registers a source whose `capture()` reads it synchronously during
 * the pre-unmount store tick.
 */
export interface ExternalScrollSource {
  capture(): ScrollRestorationEntry | undefined;
}

export interface ScrollRestorationAdapter {
  /**
   * The saved entry for a container key on the current route, or undefined
   * when there is nothing to restore.
   */
  get(containerKey: string): ScrollRestorationEntry | undefined;
  /**
   * Register/unregister a scroll source that is not a `[data-tab-scroll-root]`
   * element (currently: a sandboxed iframe's internal document). Desktop
   * implements this; web does not and the method stays absent — views detect
   * that and no-op without importing any app code.
   */
  registerExternalSource?(
    containerKey: string,
    source: ExternalScrollSource | null,
  ): void;
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
 * The active adapter, or null on web (no provider). Prefer the narrower hooks
 * below; reach for this when a view needs to call `registerExternalSource` or
 * inspect whether restoration is active at all.
 */
export function useScrollRestorationAdapter(): ScrollRestorationAdapter | null {
  return useContext(ScrollRestorationContext);
}

/**
 * The full saved entry for a container key on the current route, including
 * `contentKey` — use this when a view must decide whether a saved offset is
 * still applicable to the currently rendered content (multica-ai#6405).
 */
export function useRestoredScrollEntry(
  containerKey: string,
): ScrollRestorationEntry | undefined {
  const adapter = useContext(ScrollRestorationContext);
  return adapter?.get(containerKey);
}

/**
 * The saved scroll offset for a container key on the current route, or
 * undefined. Read it once at mount (e.g. into a Virtuoso `initialScrollTop`)
 * — it reflects the state captured when this route was last left.
 */
export function useRestoredScrollOffset(containerKey: string): number | undefined {
  return useRestoredScrollEntry(containerKey)?.top;
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
