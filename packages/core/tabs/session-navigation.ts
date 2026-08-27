import { isReservedSlug } from "../paths";
import {
  getActiveTab,
  splitTabUrl,
  useTabStore,
} from "./tab-store";

/**
 * Extract the leading workspace slug from a path, or null if the path isn't
 * workspace-scoped (root, login, any reserved prefix).
 */
export function extractWorkspaceSlug(path: string): string | null {
  const first = path.split("/").filter(Boolean)[0] ?? "";
  if (!first) return null;
  if (isReservedSlug(first)) return null;
  return first;
}

export type SessionLinkDisposition = "push" | "background-tab" | "foreground-tab";

/**
 * Intercept pushes that change workspace. Returns `true` if the navigation
 * was delegated to the tab store (caller should NOT proceed).
 */
export function tryRouteToOtherWorkspace(path: string): boolean {
  const targetSlug = extractWorkspaceSlug(path);
  if (!targetSlug) return false;
  const { activeWorkspaceSlug, switchWorkspace } = useTabStore.getState();
  if (targetSlug === activeWorkspaceSlug) return false;
  switchWorkspace(targetSlug, path);
  return true;
}

/**
 * Intercept pushes originating in a pinned tab and force them into a new
 * tab. Pathname-only changes (search / hash) stay in the pinned tab.
 */
export function tryRouteToPinnedNewTab(path: string): boolean {
  const store = useTabStore.getState();
  const active = getActiveTab(store);
  if (!active?.pinned) return false;

  const currentPathname = splitTabUrl(active.url).pathname;
  const newPathname = splitTabUrl(path).pathname;
  if (currentPathname === newPathname) return false;

  store.openTab(path, "", { activate: true });
  return true;
}

export function navigateSessionPush(path: string): void {
  const store = useTabStore.getState();
  const active = getActiveTab(store);
  if (active && active.url === path) return;
  if (tryRouteToOtherWorkspace(path)) return;
  if (tryRouteToPinnedNewTab(path)) return;
  store.navigateActiveSession(path);
}

export function navigateSessionReplace(path: string): void {
  if (tryRouteToOtherWorkspace(path)) return;
  useTabStore.getState().navigateActiveSession(path, { replace: true });
}

export function openSessionTab(
  path: string,
  title?: string,
  opts?: { activate?: boolean },
): void {
  const slug = extractWorkspaceSlug(path);
  const store = useTabStore.getState();
  if (slug && slug !== store.activeWorkspaceSlug) {
    store.switchWorkspace(slug, path);
    return;
  }
  store.openTab(path, title ?? "", { activate: opts?.activate });
}

export function routeContentLinkPath(
  path: string,
  disposition: SessionLinkDisposition = "push",
): void {
  const store = useTabStore.getState();
  const slug = extractWorkspaceSlug(path);
  if (slug && slug !== store.activeWorkspaceSlug) {
    store.switchWorkspace(slug, path);
    return;
  }
  if (disposition === "push") {
    const active = getActiveTab(store);
    if (active && active.url === path) return;
    if (tryRouteToPinnedNewTab(path)) return;
    store.navigateActiveSession(path);
    return;
  }
  store.openTab(path, "", { activate: disposition === "foreground-tab" });
}
