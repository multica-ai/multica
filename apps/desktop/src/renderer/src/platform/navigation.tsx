import { useEffect, useMemo, useState } from "react";
import type { DataRouter } from "react-router-dom";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";

/**
 * Root-level navigation provider for components outside the per-tab RouterProviders
 * (sidebar, search dialog, modals, etc.).
 *
 * Reads from the active tab's memory router via router.subscribe().
 * Does NOT use any react-router hooks — it's above all RouterProviders.
 */
export function DesktopNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const activeTab = useTabStore((s) => s.tabs.find((t) => t.id === s.activeTabId));
  const [pathname, setPathname] = useState(activeTab?.path ?? "/issues");

  // Subscribe to the active tab's router for pathname updates
  useEffect(() => {
    if (!activeTab) return;
    setPathname(activeTab.router.state.location.pathname);
    return activeTab.router.subscribe((state) => {
      setPathname(state.location.pathname);
    });
  }, [activeTab?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path: string) => {
        if (path === "/login") {
          // DashboardGuard token expired — force back to login screen
          useAuthStore.getState().logout();
          return;
        }
        const tab = useTabStore.getState().tabs.find(
          (t) => t.id === useTabStore.getState().activeTabId,
        );
        tab?.router.navigate(path);
      },
      replace: (path: string) => {
        const tab = useTabStore.getState().tabs.find(
          (t) => t.id === useTabStore.getState().activeTabId,
        );
        tab?.router.navigate(path, { replace: true });
      },
      back: () => {
        const tab = useTabStore.getState().tabs.find(
          (t) => t.id === useTabStore.getState().activeTabId,
        );
        tab?.router.navigate(-1);
      },
      pathname,
      searchParams: new URLSearchParams(),
      openInNewTab: (path: string, title?: string) => {
        const icon = resolveRouteIcon(path);
        const store = useTabStore.getState();
        const tabId = store.openTab(path, title ?? path, icon);
        store.setActiveTab(tabId);
      },
      getShareableUrl: (path: string) => `https://www.multica.ai${path}`,
    }),
    [pathname],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
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
      push: (path: string) => router.navigate(path),
      replace: (path: string) => router.navigate(path, { replace: true }),
      back: () => router.navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path: string, title?: string) => {
        const icon = resolveRouteIcon(path);
        const store = useTabStore.getState();
        const newTabId = store.openTab(path, title ?? path, icon);
        store.setActiveTab(newTabId);
      },
      getShareableUrl: (path: string) => `https://www.multica.ai${path}`,
    }),
    [router, location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
