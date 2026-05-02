import { useEffect, useMemo, useState } from "react";
import type { DataRouter } from "react-router-dom";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { useAuthStore } from "@multica/core/auth";
import { isReservedSlug } from "@multica/core/paths";
import {
  useTabStore,
  resolveRouteIcon,
  useActiveTabIdentity,
  useActiveTabRouter,
  getActiveTab,
} from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

// Public web app URL resolved by the main process from runtime config.
const APP_URL = window.desktopAPI.runtimeConfig.ok
  ? window.desktopAPI.runtimeConfig.config.appUrl
  : "";

/**
 * Extract the leading workspace slug from a path, or null if the path isn't
 * workspace-scoped (root, login, any reserved prefix).
 */
function extractWorkspaceSlug(path: string): string | null {
  const first = path.split("/").filter(Boolean)[0] ?? "";
  if (!first) return null;
  if (isReservedSlug(first)) return null;
  return first;
}

/**
 * Intercept navigation to "transition" paths — pre-workspace flows that on
 * desktop are rendered as a window-level overlay instead of a tab route.
 * Returns `true` if the navigation was handled (caller should NOT proceed).
 *
 * Side effect: when opening the new-workspace overlay, the tab router is
 * ALSO reset to "/". Rationale — the only way a push lands on
 * /workspaces/new is that the workspace context is gone (fresh install,
 * delete-last, leave-last). Leaving the tab parked on a workspace-scoped
 * path would keep those components mounted under the overlay; the next
 * render after the list cache updates would then throw (useWorkspaceId
 * etc) because the slug no longer resolves.
 */
function tryRouteToOverlay(path: string, router?: DataRouter): boolean {
  const overlay = useWindowOverlayStore.getState();
  if (path === "/workspaces/new") {
    overlay.open({ type: "new-workspace" });
    if (router && router.state.location.pathname !== "/") {
      router.navigate("/", { replace: true });
    }
    return true;
  }
  if (path === "/onboarding") {
    overlay.open({ type: "onboarding" });
    if (router && router.state.location.pathname !== "/") {
      router.navigate("/", { replace: true });
    }
    return true;
  }
  if (path === "/invitations") {
    overlay.open({ type: "invitations" });
    if (router && router.state.location.pathname !== "/") {
      router.navigate("/", { replace: true });
    }
    return true;
  }
  if (path.startsWith("/invite/")) {
    let id = "";
    try {
      id = decodeURIComponent(path.slice("/invite/".length));
    } catch {
      return true;
    }
    if (id) {
      overlay.open({ type: "invite", invitationId: id });
      return true;
    }
  }
  // Any other navigation cancels a live overlay.
  if (overlay.overlay) overlay.close();
  return false;
}

/**
 * Intercept pushes that change workspace. Returns `true` if the navigation
 * was delegated to the tab store (caller should NOT proceed).
 *
 * This is the entry point that makes shared code platform-agnostic:
 * sidebar dropdown, cmd+k "switch workspace", post-delete redirects,
 * invite-accept flow — they all call `useNavigation().push(path)` with a
 * full workspace URL, and on desktop we translate "target slug differs
 * from active" into "switch the tab-group that's visible in the TabBar".
 */
function tryRouteToOtherWorkspace(path: string): boolean {
  const targetSlug = extractWorkspaceSlug(path);
  if (!targetSlug) return false;
  const { activeWorkspaceSlug, switchWorkspace } = useTabStore.getState();
  if (targetSlug === activeWorkspaceSlug) return false;
  switchWorkspace(targetSlug, path);
  return true;
}

/**
 * Root-level navigation provider for components outside the per-tab
 * RouterProviders (sidebar, search dialog, modals, WindowOverlay contents).
 *
 * Reads from the active tab's memory router via router.subscribe().
 * Does NOT use any react-router hooks — it's above all RouterProviders.
 */
export function DesktopNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  // Primitive-only subscriptions so this component doesn't re-render on
  // unrelated store updates (e.g. an inactive tab's router tick). We
  // resolve the active router here only to subscribe once per tab switch.
  const { tabId: activeTabId } = useActiveTabIdentity();
  const router = useActiveTabRouter();
  // Mirror the active tab router's full location (pathname + search) so
  // shell-level consumers of useNavigation() — ChatWindow in particular —
  // can read URL search params. Must stay in sync with TabNavigationProvider
  // below; a partial shape here (just pathname) silently broke focus-mode
  // anchor resolution on `/inbox?issue=…`.
  const [location, setLocation] = useState<{ pathname: string; search: string }>(
    () => ({
      pathname: router?.state.location.pathname ?? "/",
      search: router?.state.location.search ?? "",
    }),
  );

  useEffect(() => {
    if (!router) {
      setLocation({ pathname: "/", search: "" });
      return;
    }
    setLocation({
      pathname: router.state.location.pathname,
      search: router.state.location.search,
    });
    return router.subscribe((state) => {
      setLocation({
        pathname: state.location.pathname,
        search: state.location.search,
      });
    });
  }, [activeTabId, router]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path: string) => {
        if (path === "/login") {
          useAuthStore.getState().logout();
          return;
        }
        const active = currentActiveTab();
        if (tryRouteToOverlay(path, active?.router)) return;
        if (tryRouteToOtherWorkspace(path)) return;
        active?.router.navigate(path);
      },
      replace: (path: string) => {
        const active = currentActiveTab();
        if (tryRouteToOverlay(path, active?.router)) return;
        if (tryRouteToOtherWorkspace(path)) return;
        active?.router.navigate(path, { replace: true });
      },
      back: () => {
        currentActiveTab()?.router.navigate(-1);
      },
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path: string, title?: string) => {
        // Cross-workspace "open in new tab" switches workspace and opens
        // the path there; same-workspace just adds a tab in the current group.
        const slug = extractWorkspaceSlug(path);
        const store = useTabStore.getState();
        if (slug && slug !== store.activeWorkspaceSlug) {
          store.switchWorkspace(slug, path);
          return;
        }
        const icon = resolveRouteIcon(path);
        const tabId = store.openTab(path, title ?? path, icon);
        if (tabId) store.setActiveTab(tabId);
      },
      getShareableUrl: (path: string) => `${APP_URL}${path}`,
    }),
    [location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

function currentActiveTab() {
  return getActiveTab(useTabStore.getState());
}

/**
 * Per-tab navigation provider rendered inside each tab's Activity wrapper.
 * Subscribes to the tab's own router for up-to-date pathname.
 *
 * This is what @multica/views page components read via useNavigation().
 */
export function TabNavigationProvider({
  router,
  children,
}: {
  router: DataRouter;
  children: React.ReactNode;
}) {
  const [location, setLocation] = useState(router.state.location);

  useEffect(() => {
    setLocation(router.state.location);
    return router.subscribe((state) => {
      setLocation(state.location);
    });
  }, [router]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path: string) => {
        if (tryRouteToOverlay(path, router)) return;
        if (tryRouteToOtherWorkspace(path)) return;
        router.navigate(path);
      },
      replace: (path: string) => {
        if (tryRouteToOverlay(path, router)) return;
        if (tryRouteToOtherWorkspace(path)) return;
        router.navigate(path, { replace: true });
      },
      back: () => router.navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path: string, title?: string) => {
        const slug = extractWorkspaceSlug(path);
        const store = useTabStore.getState();
        if (slug && slug !== store.activeWorkspaceSlug) {
          store.switchWorkspace(slug, path);
          return;
        }
        const icon = resolveRouteIcon(path);
        const tabId = store.openTab(path, title ?? path, icon);
        if (tabId) store.setActiveTab(tabId);
      },
      getShareableUrl: (path: string) => `${APP_URL}${path}`,
    }),
    [router, location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
