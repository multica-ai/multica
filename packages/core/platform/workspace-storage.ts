import type { StateStorage } from "zustand/middleware";
import type { StorageAdapter } from "../types/storage";

// Paired module vars — always set/cleared together by the workspace layout.
// _currentSlug is the primary identifier (matches the URL segment).
// _currentWsId is derived (from the React Query workspace list) and used for
// query keys and path-embedded API calls where UUID is required.
let _currentSlug: string | null = null;
let _currentWsId: string | null = null;

const _rehydrateFns: Array<() => void> = [];
const _slugSubscribers = new Set<(slug: string | null) => void>();
let _pendingNotify = false;
let _pendingRehydrate = false;

/**
 * Set both the current workspace slug and UUID at once.
 * Called by the workspace layout's render-phase ref guard.
 * Notifies slug subscribers (e.g. WSProvider via useSyncExternalStore).
 */
export function setCurrentWorkspace(slug: string | null, wsId: string | null) {
  const slugChanged = _currentSlug !== slug;
  _currentSlug = slug;
  _currentWsId = wsId;
  if (slugChanged && !_pendingNotify) {
    _pendingNotify = true;
    // Defer and deduplicate subscriber notifications:
    // 1. Defer: avoids "cannot update component B while rendering A"
    //    (React 19 render-phase restriction).
    // 2. Deduplicate: rapid A→B switches only notify once with the
    //    final slug, avoiding a wasted WS connect+disconnect cycle.
    // The module vars are already updated synchronously above, so
    // authHeaders() and getCurrentSlug() return the correct value
    // immediately — subscribers are only for async consumers like
    // WSProvider that need to reconnect the WebSocket.
    queueMicrotask(() => {
      _pendingNotify = false;
      const current = _currentSlug;
      for (const fn of _slugSubscribers) {
        fn(current);
      }
    });
  }
}

/** Current workspace slug (from URL). */
export function getCurrentSlug(): string | null {
  return _currentSlug;
}

/** Current workspace UUID (derived from slug + workspace list cache). */
export function getCurrentWsId(): string | null {
  return _currentWsId;
}

/**
 * Subscribe to changes of the current workspace slug. Returns an unsubscribe
 * function. Designed for React's `useSyncExternalStore` (WSProvider reconnect).
 */
export function subscribeToCurrentSlug(
  fn: (slug: string | null) => void,
): () => void {
  _slugSubscribers.add(fn);
  return () => {
    _slugSubscribers.delete(fn);
  };
}

/** Register a persist store's rehydrate function to be called on workspace switch. */
export function registerForWorkspaceRehydration(fn: () => void) {
  _rehydrateFns.push(fn);
}

/**
 * Rehydrate all registered workspace-scoped persist stores from the new
 * namespace. Deferred to a microtask + deduplicated for the same reason
 * as slug subscriber notification: Zustand persist rehydrate synchronously
 * setState()s the store, which schedules updates on any component
 * subscribed to that store. Calling this from a component's render phase
 * would violate React 19's "no cross-component updates during render"
 * rule. Persist stores can tolerate one microtask of staleness — they're
 * UI preferences, not security-critical state.
 */
export function rehydrateAllWorkspaceStores() {
  if (_pendingRehydrate) return;
  _pendingRehydrate = true;
  queueMicrotask(() => {
    _pendingRehydrate = false;
    for (const fn of _rehydrateFns) {
      fn();
    }
  });
}

/**
 * Storage that automatically namespaces keys with the current workspace slug.
 * Reads _currentSlug at call time, so it follows workspace switches dynamically.
 */
export function createWorkspaceAwareStorage(adapter: StorageAdapter): StateStorage {
  const resolve = (key: string) =>
    _currentSlug ? `${key}:${_currentSlug}` : key;

  return {
    getItem: (key) => adapter.getItem(resolve(key)),
    setItem: (key, value) => adapter.setItem(resolve(key), value),
    removeItem: (key) => adapter.removeItem(resolve(key)),
  };
}
