import { useMemo } from "react";
import { createMemoryRouter, type DataRouter } from "react-router-dom";
import { appRoutes } from "../routes";
import { HistoryMirror } from "./history-mirror";
import {
  useTabStore,
  getActiveTab,
  useActiveTabIdentity,
  type Tab,
  type TabSession,
} from "@/stores/tab-store";

/**
 * The desktop tab runtime registry (MUL-4475, blocker 4).
 *
 * A tab is now a serializable *session* in the store (path/title/icon + a
 * `{ entries, index }` history session). The live React-Router runtime — the
 * memory router and its history mirror — lives HERE, outside the store, keyed
 * by tab id. This is the single external runtime store that both the tab
 * subtree (RouterProvider) and shell-level navigation (DesktopNavigationProvider,
 * useTabHistory) read the active router from, so they always share one router
 * instance per tab.
 *
 * Lifecycle:
 *   - `getOrCreate` lazily builds a runtime, seeding the router + mirror from
 *     the tab's persisted session.
 *   - The registry subscribes to the store and disposes runtimes for tabs that
 *     no longer exist (close / closeOthers / reset / stale-workspace prune),
 *     so the store stays free of router-lifecycle bookkeeping.
 *   - Router disposal is deferred a tick so React unmounts the tab's
 *     RouterProvider first (a live router can't be disposed under a mounted
 *     provider).
 *
 * The mirror is fed back into the store as the tab session by
 * `useTabRuntimeSync` (see tab-content), so persistence / reload restore the
 * full history. Query cache and entity data are never touched here.
 */
export interface TabRuntime {
  /** Monotonic id; changes when a tab's runtime is rebuilt (reload) so the
   *  tab subtree can remount on a fresh router instead of mutating the
   *  `router` prop, which React Router forbids. */
  readonly id: string;
  readonly tabId: string;
  readonly router: DataRouter;
  readonly mirror: HistoryMirror;
}

function seedRouter(session: TabSession, fallbackPath: string): DataRouter {
  const entries = session.entries.length > 0 ? session.entries : [fallbackPath];
  const index = Math.max(0, Math.min(session.index, entries.length - 1));
  return createMemoryRouter(appRoutes, {
    initialEntries: entries,
    initialIndex: index,
  });
}

let runtimeSeq = 0;

class TabRuntimeRegistry {
  private runtimes = new Map<string, TabRuntime>();
  private subscribed = false;

  /**
   * Subscribe to the store lazily, on first runtime creation — nothing needs
   * disposing until a runtime exists, and a side-effect-free constructor keeps
   * module import cheap and test-friendly.
   */
  private ensureSubscribed(): void {
    if (this.subscribed) return;
    this.subscribed = true;
    useTabStore.subscribe(() => this.disposeRemovedTabs());
  }

  getOrCreate(tab: Tab): TabRuntime {
    const existing = this.runtimes.get(tab.id);
    if (existing) return existing;
    this.ensureSubscribed();
    const router = seedRouter(tab.session, tab.path);
    const mirror = new HistoryMirror(router, tab.session);
    const runtime: TabRuntime = {
      id: `rt-${(runtimeSeq += 1)}`,
      tabId: tab.id,
      router,
      mirror,
    };
    this.runtimes.set(tab.id, runtime);
    return runtime;
  }

  /** The active tab's live router, or null when no tab is active. */
  getActiveRouter(): DataRouter | null {
    const tab = getActiveTab(useTabStore.getState());
    return tab ? this.getOrCreate(tab).router : null;
  }

  /**
   * Rebuild the active tab's runtime from scratch — the crash-recovery path.
   * Disposes the current runtime and resets the tab's session to a single
   * entry at its current path; the store bump remounts the tab subtree, which
   * lazily re-acquires a fresh runtime.
   */
  reloadActive(): void {
    const tab = getActiveTab(useTabStore.getState());
    if (!tab) return;
    this.dispose(tab.id);
    useTabStore.getState().resetTabRuntime(tab.id);
  }

  private dispose(tabId: string): void {
    const runtime = this.runtimes.get(tabId);
    if (!runtime) return;
    this.runtimes.delete(tabId);
    runtime.mirror.dispose();
    // Defer router disposal so React unmounts its RouterProvider first.
    window.setTimeout(() => runtime.router.dispose(), 0);
  }

  private disposeRemovedTabs(): void {
    const state = useTabStore.getState();
    const live = new Set<string>();
    for (const slug of Object.keys(state.byWorkspace)) {
      for (const tab of state.byWorkspace[slug].tabs) live.add(tab.id);
    }
    for (const tabId of [...this.runtimes.keys()]) {
      if (!live.has(tabId)) this.dispose(tabId);
    }
  }

  /** Test seam: drop every runtime immediately. */
  disposeAll(): void {
    for (const tabId of [...this.runtimes.keys()]) this.dispose(tabId);
  }
}

export const tabRuntimeRegistry = new TabRuntimeRegistry();

/**
 * The active tab's router as a hook, re-resolved when the active tab changes.
 * Shell-level consumers (navigation adapter, back/forward) read from here
 * instead of the store, so they share the exact router instance the tab
 * subtree renders.
 */
export function useActiveTabRouter(): DataRouter | null {
  const { tabId } = useActiveTabIdentity();
  // eslint-disable-next-line react-hooks/exhaustive-deps -- re-resolve on active-tab change
  return useMemo(() => tabRuntimeRegistry.getActiveRouter(), [tabId]);
}
