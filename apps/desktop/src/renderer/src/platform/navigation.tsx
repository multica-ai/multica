import { useMemo } from "react";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { useAuthStore } from "@multica/core/auth";
import { isReservedSlug } from "@multica/core/paths";
import {
  useTabStore,
  resolveRouteIcon,
  getActiveTab,
  splitTabUrl,
  useActiveTabUrl,
} from "@/stores/tab-store";
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
 * Intercept pushes originating in a pinned tab and force them into a new
 * tab. Returns `true` if the navigation was redirected (caller should NOT
 * proceed). Pathname-only changes (search / hash / same-page state) are
 * allowed through so pinned filter / drawer / form-state interactions
 * still work — see RFC §3 D2a (FINAL: any pathname change → new tab) and
 * D2b (FINAL: same pathname → allowed in pinned tab).
 *
 * Dedupe is preserved (D4a): `openTab` activates an existing tab with the
 * same resourceKey if one exists, otherwise creates a new one. The
 * newly-focused tab is activated foreground — a pinned-tab push is an
 * explicit user action, not a background cmd+click, so the focus follows.
 */
function tryRouteToPinnedNewTab(path: string): boolean {
  const store = useTabStore.getState();
  const active = getActiveTab(store);
  if (!active?.pinned) return false;

  const currentPathname = splitTabUrl(active.url).pathname;
  const newPathname = splitTabUrl(path).pathname;
  if (currentPathname === newPathname) return false;

  const icon = resolveRouteIcon(path);
  store.openTab(path, path, icon, { activate: true });
  return true;
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
    return { pathname, search };
  }, [activeUrl]);

  const adapter: NavigationAdapter = useMemo(
    () => ({
      push: (path: string) => {
        if (path === "/login") {
          useAuthStore.getState().logout();
          return;
        }
        if (tryRouteToOverlay(path)) return;
        const store = useTabStore.getState();
        const active = getActiveTab(store);
        if (active && active.url === path) return;
        if (tryRouteToOtherWorkspace(path)) return;
        if (tryRouteToPinnedNewTab(path)) return;
        store.navigateActiveSession(path);
      },
      replace: (path: string) => {
        if (tryRouteToOverlay(path)) return;
        if (tryRouteToOtherWorkspace(path)) return;
        useTabStore.getState().navigateActiveSession(path, { replace: true });
      },
      back: () => {
        useTabStore.getState().goBack();
      },
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (
        path: string,
        title?: string,
        opts?: { activate?: boolean },
      ) => {
        // Cross-workspace "open in new tab" switches workspace and opens
        // the path there (focus follows the user); same-workspace defaults
        // to background tab (browser cmd+click semantics). Callers that
        // represent an explicit "Open in new tab" CTA pass `activate: true`
        // to bring the new tab to the foreground.
        const slug = extractWorkspaceSlug(path);
        const store = useTabStore.getState();
        if (slug && slug !== store.activeWorkspaceSlug) {
          store.switchWorkspace(slug, path);
          return;
        }
        const icon = resolveRouteIcon(path);
        store.openTab(path, title ?? path, icon, { activate: opts?.activate });
      },
      getShareableUrl: (path: string) => `${appUrl}${path}`,
    }),
    [appUrl, location],
  );

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
