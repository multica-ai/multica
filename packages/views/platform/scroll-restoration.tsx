"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
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
 *
 * `left` is the horizontal offset, present only when it is > 0 (e.g. the
 * board's column strip scrolled sideways); an entry may then carry top 0.
 */
export interface ScrollRestorationEntry {
  top: number;
  height: number;
  left?: number;
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
   * Register a plain scroll container at attach time. The returned cleanup
   * captures it before detach, including same-route view changes. The platform
   * binds the capture to the tab/route that owned the element at registration.
   */
  registerScrollRoot?(containerKey: string, element: HTMLElement): () => void;
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
 * The saved vertical scroll offset for a container key on the current route,
 * or undefined when there is none (an entry saved only for its horizontal
 * offset counts as none). Read it once at mount (e.g. into a Virtuoso
 * `initialScrollTop`) — it reflects the state captured when this route was
 * last left.
 */
export function useRestoredScrollOffset(containerKey: string): number | undefined {
  const top = useRestoredScrollEntry(containerKey)?.top;
  return top !== undefined && top > 0 ? top : undefined;
}

/**
 * How long a restore holds the saved offset while the content settles. Long
 * enough for cached data and lazy sections to land after mount, short enough
 * that a slow network load does not yank the page later.
 */
const RESTORE_SETTLE_MS = 1500;

/** Inputs that mean the user is scrolling the container themselves. */
const USER_SCROLL_EVENTS = ["wheel", "touchstart", "pointerdown", "keydown"] as const;

/**
 * Assign the saved offsets to a container that just attached, then hold them
 * while the content settles. Sections that render a frame or two after mount
 * move the offset: the browser clamps it while the content is still short,
 * and content that grows or collapses above it shifts it. So until the
 * deadline, an offset moved by a change in the content's size is put back.
 * The restore yields for good once the user scrolls the container, or other
 * code moves it without the content changing size (a deep-link jump).
 * Returns a cancel for the pending hold.
 */
function restoreOffsets(
  el: HTMLElement,
  saved: ScrollRestorationEntry,
): () => void {
  const top = saved.top > 0 ? saved.top : undefined;
  const left = saved.left !== undefined && saved.left > 0 ? saved.left : undefined;
  if (top === undefined && left === undefined) return () => {};
  const apply = () => {
    if (top !== undefined && el.scrollTop !== top) el.scrollTop = top;
    if (left !== undefined && el.scrollLeft !== left) el.scrollLeft = left;
  };
  // Only the restored axes count: the other may be the view's own business.
  const position = () =>
    `${top === undefined ? "" : el.scrollTop}:${left === undefined ? "" : el.scrollLeft}`;
  const size = () => `${el.scrollHeight}:${el.scrollWidth}`;

  apply();
  if (typeof requestAnimationFrame !== "function") return () => {};

  const deadline = performance.now() + RESTORE_SETTLE_MS;
  let landed = position();
  let lastSize = size();
  let frame = 0;
  const stop = () => {
    cancelAnimationFrame(frame);
    for (const type of USER_SCROLL_EVENTS) el.removeEventListener(type, stop);
  };
  const tick = () => {
    if (!el.isConnected || performance.now() > deadline) return stop();
    const nextSize = size();
    if (position() !== landed && nextSize === lastSize) return stop();
    apply();
    landed = position();
    lastSize = nextSize;
    frame = requestAnimationFrame(tick);
  };
  for (const type of USER_SCROLL_EVENTS) {
    el.addEventListener(type, stop, { passive: true });
  }
  frame = requestAnimationFrame(tick);
  return stop;
}

/**
 * Ref callback for PLAIN (non-virtualized) scroll containers: assigns the
 * saved offsets (vertical and horizontal) when the element attaches, which
 * happens during commit — before paint — so the first visible frame is
 * already at the restored position when the content is there; content that
 * lands a few frames later is caught up by `restoreOffsets`. Compose it with
 * other refs via a merged callback. Detach captures the latest offsets
 * through the same adapter.
 */
export function useRestoredScrollRef(
  containerKey: string,
  options?: {
    /** Skip applying the saved pixel offset while keeping unmount capture active
     *  (e.g. when semantic anchor restoration or a deep-link landing owns scroll). */
    disabled?: boolean;
  },
): (el: HTMLElement | null) => void {
  const adapter = useContext(ScrollRestorationContext);
  const disabled = options?.disabled ?? false;
  const disabledRef = useRef(disabled);
  disabledRef.current = disabled;
  const cancelRestoreRef = useRef<(() => void) | undefined>(undefined);

  useEffect(() => {
    if (disabled) {
      cancelRestoreRef.current?.();
      cancelRestoreRef.current = undefined;
    }
  }, [disabled]);

  return useMemo(() => {
    let capture: (() => void) | undefined;
    return (el: HTMLElement | null) => {
      cancelRestoreRef.current?.();
      cancelRestoreRef.current = undefined;
      capture?.();
      capture = undefined;
      if (!el) return;
      if (!disabledRef.current) {
        const saved = adapter?.get(containerKey);
        if (saved) cancelRestoreRef.current = restoreOffsets(el, saved);
      }
      capture = adapter?.registerScrollRoot?.(containerKey, el);
    };
  }, [adapter, containerKey]);
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
