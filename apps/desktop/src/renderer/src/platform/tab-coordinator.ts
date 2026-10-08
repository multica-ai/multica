import type { DataRouter } from "react-router-dom";
import type { QueryClient } from "@tanstack/react-query";
import type {
  ExternalScrollSource,
  ScrollRestorationAdapter,
  ScrollRestorationEntry,
} from "@multica/views/platform";
import { createAppRouter } from "@/routes";
import {
  useTabStore,
  getActiveTab,
  splitTabUrl,
  scrollMementoKey,
} from "@/stores/tab-store";

/**
 * Tab Coordinator (MUL-4741 Phase 2) — the ONLY writer of the single app
 * router.
 *
 * Architecture: the tab store is the source of truth; the router is a
 * projection of the active session's URL. The Coordinator subscribes to the
 * store and *reconciles* the router to `activeSession.url`, stamping every
 * navigation with a token. All entry points (tab bar clicks, adapter
 * push/replace, shell back/forward, error recovery) mutate the store — none
 * of them touch the router. This makes invariant 1 hold by construction:
 *
 *   - Router location change WITH a pending token   → expected, consume it.
 *   - Router location change WITHOUT a pending token → protocol error →
 *     bounded recovery (re-reconcile toward the session URL, capped).
 *
 * The router's own history is never used: the Coordinator always navigates
 * with `replace`, and per-tab back/forward is a session operation over the
 * session's virtual history stack. A POP therefore can only come from
 * unforeseen code and is handled by the same protocol-error path.
 */

let router: DataRouter | null = null;
let initialized = false;

/** Navigations the Coordinator itself started and hasn't seen commit yet. */
let pendingTokens = 0;

/** Bounded recovery: consecutive protocol-error recoveries. */
let recoveryAttempts = 0;
const MAX_RECOVERY_ATTEMPTS = 5;

/** The active tab host's root element — where mementos are captured from. */
let activeHostElement: HTMLElement | null = null;

/** Query client for reload()'s current-page-scope invalidation. */
let queryClient: QueryClient | null = null;

/**
 * Scroll sources outside the outer DOM that the Coordinator cannot scan for
 * itself — currently the document inside a sandboxed HTML-attachment iframe
 * (opaque origin → `contentWindow.scrollY` throws). Keyed by tab id so each
 * tab's sources are captured only when that tab is outgoing; sources are
 * registered/unregistered by the per-tab view via the adapter's
 * `registerExternalSource` and vanish naturally when the tab's ActiveTabHost
 * unmounts. We still prune on capture so a tab id is never leaked after its
 * subtree is gone.
 */
const externalScrollSources = new Map<
  string,
  Map<string, ExternalScrollSource>
>();

/**
 * Register (or unregister, when `source` is null) an external scroll source
 * for a tab. The view's ScrollRestorationAdapter is per-tab, so it passes its
 * own tabId here. Safe to call with null on cleanup.
 */
export function registerExternalScrollSource(
  tabId: string,
  containerKey: string,
  source: ExternalScrollSource | null,
): void {
  let perTab = externalScrollSources.get(tabId);
  if (!source) {
    if (!perTab) return;
    perTab.delete(containerKey);
    if (perTab.size === 0) externalScrollSources.delete(tabId);
    return;
  }
  if (!perTab) {
    perTab = new Map();
    externalScrollSources.set(tabId, perTab);
  }
  perTab.set(containerKey, source);
}

/** Identity of what the host currently shows: slug:tabId:generation. */
let lastIdentity: string | null = null;
let lastActiveTabId: string | null = null;
/** The active session's url as last seen — the route the mounted DOM shows. */
let lastActiveUrl: string | null = null;

export function getAppRouter(): DataRouter {
  if (!router) router = createAppRouter();
  return router;
}

export function registerActiveHostElement(el: HTMLElement | null): void {
  activeHostElement = el;
}

export function registerCoordinatorQueryClient(qc: QueryClient): void {
  queryClient = qc;
}

/**
 * Serves saved scroll offsets back to mounting views (pull-based restore —
 * see ScrollRestorationProvider in @multica/views/platform). Offsets are
 * looked up live against the tab's memento, scoped to the route the view is
 * mounting under, so in-tab back/forward pulls each route's own offsets.
 *
 * The generic view-state entries ride the same channel: reads resolve
 * against the memento's `view` map, writes commit through the store — no
 * capture pass, the view pushes at the moment its restorable state changes.
 *
 * The adapter is constructed per ActiveTabHost (memoized on tabId in
 * tab-content.tsx), so `registerExternalSource` records this tab's
 * out-of-DOM scroll sources (iframe documents) under that tabId; the
 * Coordinator captures them when this tab is the outgoing one.
 */
export function createScrollRestorationAdapter(
  tabId: string,
): ScrollRestorationAdapter {
  const activeRouteKey = (): string | null => {
    const state = useTabStore.getState();
    const active = getActiveTab(state);
    if (!active || active.id !== tabId) return null;
    return splitTabUrl(active.url).pathname;
  };
  return {
    get(containerKey) {
      const routeKey = activeRouteKey();
      if (routeKey === null) return undefined;
      const active = getActiveTab(useTabStore.getState());
      return active?.memento.scroll[scrollMementoKey(routeKey, containerKey)];
    },
    getViewState(entryKey) {
      const routeKey = activeRouteKey();
      if (routeKey === null) return undefined;
      const active = getActiveTab(useTabStore.getState());
      return active?.memento.view[scrollMementoKey(routeKey, entryKey)];
    },
    setViewState(entryKey, value) {
      const routeKey = activeRouteKey();
      if (routeKey === null) return;
      useTabStore.getState().commitViewState(tabId, routeKey, entryKey, value);
    },
    registerExternalSource(containerKey, source) {
      registerExternalScrollSource(tabId, containerKey, source);
    },
  };
}

function currentRouterUrl(r: DataRouter): string {
  const { pathname, search, hash } = r.state.location;
  return `${pathname}${search ?? ""}${hash ?? ""}`;
}

function activeSessionUrl(): string | null {
  return getActiveTab(useTabStore.getState())?.url ?? null;
}

/**
 * Drive the router to the active session's URL (or park it at "/" when no
 * workspace/session is active — the zero-workspace overlay state). Always
 * `replace`: the router history is a projection, not a record.
 */
function reconcile(): void {
  const r = getAppRouter();
  const target = activeSessionUrl() ?? "/";
  if (currentRouterUrl(r) === target) {
    recoveryAttempts = 0;
    return;
  }
  pendingTokens++;
  r.navigate(target, { replace: true });
}

/**
 * Capture the mounted route's restorable view state. Runs inside the store
 * subscription, which zustand fires synchronously during `set()` — before
 * React re-renders — so the outgoing DOM (previous tab, or previous route
 * of the same tab) is still mounted.
 *
 * Plain scroll containers self-mark with `data-tab-scroll-root` (the
 * attribute value is the container key, "main" when bare). Containers sitting
 * at 0 are simply absent from the result — the store's per-route REPLACE
 * semantics turn that absence into "clear the stale offset".
 *
 * External sources (sandboxed iframe documents) are merged in for the
 * outgoing tab ONLY; unlike plain containers they are ALWAYS written,
 * including at top 0, because their entry carries a `contentKey` and a
 * top:0 write with the new key is how a content change replaces a stale
 * positive offset rather than resurrecting it.
 */
function captureScrollEntries(
  outgoingTabId: string | null,
): Record<string, ScrollRestorationEntry> {
  const entries: Record<string, ScrollRestorationEntry> = {};
  if (activeHostElement) {
    const els = activeHostElement.querySelectorAll<HTMLElement>(
      "[data-tab-scroll-root]",
    );
    els.forEach((el) => {
      if (el.scrollTop <= 0) return;
      const key = el.getAttribute("data-tab-scroll-root") || "main";
      entries[key] = { top: el.scrollTop, height: el.scrollHeight };
    });
  }
  if (outgoingTabId) {
    const sources = externalScrollSources.get(outgoingTabId);
    if (sources) {
      for (const [key, source] of sources) {
        const entry = source.capture();
        if (entry) entries[key] = entry;
      }
    }
  }
  return entries;
}

function handleStoreChange(): void {
  const state = useTabStore.getState();
  const active = getActiveTab(state);
  const identity = active
    ? `${state.activeWorkspaceSlug}:${active.id}:${state.mountGeneration}`
    : null;
  const activeUrl = active?.url ?? null;

  // Capture whenever what the DOM currently shows is about to be replaced:
  // a host switch (tab switch / reload / workspace switch / close) OR an
  // in-tab navigation (url change on the same tab — back/forward included).
  // The outgoing DOM belongs to `lastActiveUrl`'s route, so that pathname
  // scopes the commit.
  const hostSwitching = identity !== lastIdentity;
  const inTabNavigation = !hostSwitching && activeUrl !== lastActiveUrl;
  if (hostSwitching || inTabNavigation) {
    const outgoingTabId = lastActiveTabId;
    const outgoingUrl = lastActiveUrl;
    lastIdentity = identity;
    lastActiveTabId = active?.id ?? null;
    lastActiveUrl = activeUrl;
    if (outgoingTabId && outgoingUrl) {
      const routeKey = splitTabUrl(outgoingUrl).pathname;
      // Always commit — REPLACE semantics clear entries for containers that
      // scrolled back to 0; the store skips the write when nothing changed.
      useTabStore
        .getState()
        .commitScrollMemento(
          outgoingTabId,
          routeKey,
          captureScrollEntries(outgoingTabId),
        );
    }
    // The outgoing tab's ActiveTabHost is about to unmount; its views'
    // cleanup will unregister their sources, but a host switch that tears
    // down the whole tree can drop those cleanups without running them.
    // Prune proactively so a future re-mount starts clean and a closed tab
    // never leaks sources.
    if (outgoingTabId && hostSwitching) {
      externalScrollSources.delete(outgoingTabId);
    }
  }

  reconcile();
}

function handleReloadGenerationChange(generation: number): void {
  // RFC: reload = remount + invalidate the current page's query scope.
  // `type: "active"` limits invalidation to queries with mounted observers —
  // i.e. exactly the current page — and is explicitly NOT a global cache
  // invalidation. `refetchType: "none"` avoids fetching into a tree that is
  // about to unmount; the remounted page refetches its now-stale queries.
  void generation;
  queryClient?.invalidateQueries({ type: "active", refetchType: "none" });
}

/**
 * Wire the Coordinator: store → router reconciliation and router-side
 * protocol-error detection. Idempotent — the host calls it on mount.
 */
export function initTabCoordinator(): void {
  if (initialized) return;
  initialized = true;

  const r = getAppRouter();

  r.subscribe(() => {
    if (pendingTokens > 0) {
      // A navigation the Coordinator started. Consume the token.
      pendingTokens--;
      recoveryAttempts = 0;
      return;
    }
    // Location changed without a token. If it happens to match the session
    // (idempotent double-commit), accept silently; otherwise it's a
    // protocol error — recover toward the session URL, bounded.
    const url = currentRouterUrl(r);
    const sessionUrl = activeSessionUrl() ?? "/";
    if (url === sessionUrl) return;
    if (recoveryAttempts >= MAX_RECOVERY_ATTEMPTS) {
      console.error(
        `[tab-coordinator] giving up recovery after ${MAX_RECOVERY_ATTEMPTS} attempts ` +
          `(router at "${url}", session at "${sessionUrl}")`,
      );
      return;
    }
    recoveryAttempts++;
    console.error(
      `[tab-coordinator] protocol error: router moved to "${url}" without a ` +
        `Coordinator token (session at "${sessionUrl}") — recovering ` +
        `(${recoveryAttempts}/${MAX_RECOVERY_ATTEMPTS})`,
    );
    reconcile();
  });

  let prevGeneration = useTabStore.getState().mountGeneration;
  useTabStore.subscribe((state) => {
    if (state.mountGeneration !== prevGeneration) {
      prevGeneration = state.mountGeneration;
      handleReloadGenerationChange(state.mountGeneration);
    }
    handleStoreChange();
  });

  // Prime identity tracking and align the router with whatever the store
  // rehydrated to.
  const state = useTabStore.getState();
  const active = getActiveTab(state);
  lastIdentity = active
    ? `${state.activeWorkspaceSlug}:${active.id}:${state.mountGeneration}`
    : null;
  lastActiveTabId = active?.id ?? null;
  lastActiveUrl = active?.url ?? null;
  reconcile();
}

/** Test-only: reset module state between cases. */
export function __resetTabCoordinatorForTests(): void {
  router = null;
  initialized = false;
  pendingTokens = 0;
  recoveryAttempts = 0;
  activeHostElement = null;
  queryClient = null;
  lastIdentity = null;
  lastActiveTabId = null;
  lastActiveUrl = null;
  externalScrollSources.clear();
}
