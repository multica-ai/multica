import type { DataRouter } from "react-router-dom";
import type { QueryClient } from "@tanstack/react-query";
import { createAppRouter } from "@/routes";
import {
  useTabStore,
  getActiveTab,
  emptyMemento,
  type TabMemento,
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
 * Capture the outgoing tab's restorable view state. Runs inside the store
 * subscription, which zustand fires synchronously during `set()` — before
 * React re-renders — so the outgoing tab's DOM is still mounted.
 *
 * Scroll containers self-mark with `data-tab-scroll-root` (the attribute
 * value is the memento key, "main" when bare). `scrollHeight` is saved next
 * to `scrollTop` so the restore path can pre-size not-yet-hydrated content
 * and make the first pre-paint scrollTop assignment stick.
 */
function captureMemento(): TabMemento {
  const memento = emptyMemento();
  if (!activeHostElement) return memento;
  const els = activeHostElement.querySelectorAll<HTMLElement>(
    "[data-tab-scroll-root]",
  );
  els.forEach((el) => {
    if (el.scrollTop <= 0) return;
    const key = el.getAttribute("data-tab-scroll-root") || "main";
    memento.scroll[key] = { top: el.scrollTop, height: el.scrollHeight };
  });
  return memento;
}

function handleStoreChange(): void {
  const state = useTabStore.getState();
  const active = getActiveTab(state);
  const identity = active
    ? `${state.activeWorkspaceSlug}:${active.id}:${state.mountGeneration}`
    : null;

  if (identity !== lastIdentity) {
    // Host is about to switch (tab switch / reload / workspace switch /
    // close). Save the outgoing tab's memento while its DOM is still up.
    const outgoingTabId = lastActiveTabId;
    const generationBumped =
      outgoingTabId !== null && active !== null && outgoingTabId === active.id;
    lastIdentity = identity;
    lastActiveTabId = active?.id ?? null;
    // On reload (same tab, new generation) keep the memento too — a reload
    // preserves scroll position like a browser reload does.
    if (outgoingTabId && (outgoingTabId !== active?.id || generationBumped)) {
      const memento = captureMemento();
      // Only write when something was captured; avoids a store churn (and a
      // re-entrant subscription tick) for tabs that never scrolled.
      if (Object.keys(memento.scroll).length > 0) {
        useTabStore.getState().updateTabMemento(outgoingTabId, memento);
      }
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
}
