import { useMemo } from "react";
import {
  NavigationProvider,
  type LinkClickIntent,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { useAuthStore } from "@multica/core/auth";
import {
  useTabStore,
  getActiveTab,
  splitTabUrl,
  useActiveTabUrl,
  navigateSessionPush,
  navigateSessionReplace,
  openSessionTab,
  routeContentLinkPath as routeSessionContentLink,
} from "@multica/core/tabs";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

function requireRuntimeAppUrl(scope: string): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      `Invariant violated: ${scope} rendered before App accepted runtime config`,
    );
  }
  return runtimeConfig.config.appUrl;
}

/**
 * Intercept navigation to "transition" paths — pre-workspace flows that on
 * desktop are rendered as a window-level overlay instead of a tab route.
 * Returns `true` if the navigation was handled (caller should NOT proceed).
 *
 * MUL-4741 note: the old adapter also parked the tab's router at "/" when
 * opening these overlays. Under the session architecture the Coordinator
 * parks the single router automatically whenever `activeWorkspaceSlug` goes
 * null (the zero-workspace flows), and an overlay opened over a still-valid
 * workspace simply covers the mounted tab — no navigation happens at all.
 */
function tryRouteToOverlay(path: string): boolean {
  const overlay = useWindowOverlayStore.getState();
  if (path === "/workspaces/new") {
    overlay.open({ type: "new-workspace" });
    return true;
  }
  if (path === "/onboarding") {
    overlay.open({ type: "onboarding" });
    return true;
  }
  if (path === "/invitations") {
    overlay.open({ type: "invitations" });
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

export function routeContentLinkPath(
  path: string,
  disposition: LinkClickIntent = "push",
): void {
  routeSessionContentLink(path, disposition);
}

/**
 * Navigation provider for the whole desktop shell — sidebar, search dialog,
 * modals, WindowOverlay contents, AND the page tree inside the single
 * RouterProvider (there is no per-tab provider anymore; the active session's
 * URL is the location for everyone).
 *
 * MUL-4741 invariant 1: none of these operations touch the router. They
 * mutate tab sessions in the store; the Coordinator reconciles the single
 * router to the active session URL with a navigation token.
 */
export function DesktopNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const appUrl = requireRuntimeAppUrl("DesktopNavigationProvider");
  // The active session's url IS the location. Primitive subscription: this
  // only re-renders when the active url actually changes.
  const activeUrl = useActiveTabUrl();
  const location = useMemo(() => {
    const url = activeUrl ?? "/";
    const { pathname, suffix } = splitTabUrl(url);
    const hashIdx = suffix.indexOf("#");
    const search = hashIdx === -1 ? suffix : suffix.slice(0, hashIdx);
    const hash = hashIdx === -1 ? "" : suffix.slice(hashIdx);
    return { pathname, search, hash };
  }, [activeUrl]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path: string) => {
        if (path === "/login") {
          useAuthStore.getState().logout();
          return;
        }
        if (tryRouteToOverlay(path)) return;
        navigateSessionPush(path);
      },
      replace: (path: string) => {
        if (tryRouteToOverlay(path)) return;
        navigateSessionReplace(path);
      },
      back: () => {
        useTabStore.getState().goBack();
      },
      forward: () => {
        useTabStore.getState().goForward();
      },
      // The active tab's virtual history, same source the shell's back button
      // reads. A tab opened straight onto a destination sits at index 0 and
      // has nothing behind it.
      canGoBack: () => {
        const active = getActiveTab(useTabStore.getState());
        return (active?.history.index ?? 0) > 0;
      },
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      // The tab's URL is the only place the fragment survives on desktop: the
      // renderer's own `window.location` is the packaged file:// page.
      hash: location.hash,
      openInNewTab: (
        path: string,
        title?: string,
        opts?: { activate?: boolean },
      ) => {
        openSessionTab(path, title, opts);
      },
      getShareableUrl: (path: string) => `${appUrl}${path}`,
    }),
    [appUrl, location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
