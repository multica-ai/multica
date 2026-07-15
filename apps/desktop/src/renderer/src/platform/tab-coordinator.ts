import type { DataRouter } from "react-router-dom";
import type { QueryClient } from "@tanstack/react-query";
import type { ScrollRestorationAdapter } from "@multica/views/platform";
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
 */
export function createScrollRestorationAdapter(
  tabId: string,
): ScrollRestorationAdapter {
  return {
    get(containerKey) {
      const state = useTabStore.getState();
      const active = getActiveTab(state);
      if (!active || active.id !== tabId) return undefined;
      const routeKey = splitTabUrl(active.url).pathname;
      return active.memento.scroll[scrollMementoKey(routeKey, containerKey)];
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
 * Scroll containers self-mark with `data-tab-scroll-root` (the attribute
 * value is the container key, "main" when bare). Containers sitting at 0
 * are simply absent from the result — the store's per-route REPLACE
 * semantics turn that absence into "clear the stale offset".
 */
function captureScrollEntries(): Record<string, { top: number; height: number }> {
  const entries: Record<string, { top: number; height: number }> = {};
  if (!activeHostElement) return entries;
  const els = activeHostElement.querySelectorAll<HTMLElement>(
    "[data-tab-scroll-root]",
  );
  els.forEach((el) => {
    if (el.scrollTop <= 0) return;
    const key = el.getAttribute("data-tab-scroll-root") || "main";
    entries[key] = { top: el.scrollTop, height: el.scrollHeight };
  });
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
        .commitScrollMemento(outgoingTabId, routeKey, captureScrollEntries());
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
}
