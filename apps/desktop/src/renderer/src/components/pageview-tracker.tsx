import { useEffect, useRef } from "react";
import { capturePageview } from "@multica/core/analytics";
import { useAuthStore } from "@multica/core/auth";
import {
  getActiveTab,
  useActiveTabIdentity,
  useTabStore,
} from "@/stores/tab-store";
import { useWindowOverlayStore, type WindowOverlay } from "@/stores/window-overlay-store";

/**
 * Fires a PostHog $pageview whenever the user's visible surface changes,
 * EXCEPT for re-activations of an already-known tab on its already-known
 * path.
 *
 * Desktop has three layers that can own the visible page:
 *
 *   1. Logged-out state → `/login`. No workspace context, no tabs.
 *   2. Window overlays (onboarding, new-workspace, invite) → synthetic paths
 *      that match the equivalent web routes. Overlays are NOT tab routes on
 *      desktop (see `stores/window-overlay-store.ts` + `routes.tsx`), so the
 *      tab path alone would either miss them or mislabel them as "/".
 *   3. Otherwise → the active tab's path (workspace-scoped, e.g.
 *      `/acme/issues/123`). Kept in sync by `useTabRouterSync`.
 *
 * Tab-switch suppression: re-activating an already-open tab surfaces a
 * previously-visited path under a `(workspace, tabId)` we have already
 * seen — the pageview was emitted when the user originally navigated
 * there, so re-emitting on every switch just inflates PostHog billing
 * without adding signal (real-data audit: desktop tab switches were
 * ~50% of all `$pageview` events).
 *
 * Newly opened tabs (`openInNewTab`, `addTab`) and cross-workspace
 * `switchWorkspace(slug, path)` to a previously-unseen tab still fire,
 * because their key is not in the observed map yet. The map is seeded
 * from the persisted tab store on first render so tabs restored from a
 * previous session don't all re-emit on first activation.
 *
 * PostHog's `capture_pageview: true` auto-capture is intentionally off (see
 * `initAnalytics`) so this component owns the event shape, matching the web
 * implementation in `apps/web/components/pageview-tracker.tsx`.
 */
export function PageviewTracker() {
  const user = useAuthStore((s) => s.user);
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const { slug: activeWorkspaceSlug, tabId: activeTabId } = useActiveTabIdentity();
  const activeTabPath = useTabStore((s) => getActiveTab(s)?.path ?? null);

  // (slug:tabId) → last path observed while that tab was visible. Lets us
  // tell "re-activating a tab on a path we already saw" (suppress) apart
  // from "newly opened tab" or "intra-tab navigation" (fire). Seeded
  // synchronously on first render from the persisted tab store so
  // session-restored tabs don't re-emit on first click.
  const observedTabsRef = useRef<Map<string, string> | null>(null);
  if (observedTabsRef.current === null) {
    const seed = new Map<string, string>();
    for (const [slug, group] of Object.entries(useTabStore.getState().byWorkspace)) {
      for (const tab of group.tabs) {
        seed.set(`${slug}:${tab.id}`, tab.path);
      }
    }
    observedTabsRef.current = seed;
  }
  const lastSurfaceRef = useRef<{
    kind: "login" | "overlay" | "tab" | null;
    key: string | null;
    path: string | null;
  }>({ kind: null, key: null, path: null });

  useEffect(() => {
    let kind: "login" | "overlay" | "tab";
    let path: string;
    let key: string | null = null;

    if (!user) {
      kind = "login";
      path = "/login";
    } else if (overlay) {
      kind = "overlay";
      path = overlayPath(overlay);
    } else if (activeTabPath && activeTabId && activeWorkspaceSlug) {
      kind = "tab";
      key = `${activeWorkspaceSlug}:${activeTabId}`;
      path = activeTabPath;
    } else {
      return;
    }

    const observed = observedTabsRef.current!;
    const last = lastSurfaceRef.current;
    const next = { kind, key, path };

    if (kind === "tab" && key !== null) {
      const knownPath = observed.get(key);
      const isReactivation =
        last.key !== key && knownPath !== undefined && knownPath === path;
      observed.set(key, path);
      if (isReactivation) {
        lastSurfaceRef.current = next;
        return;
      }
    }

    const unchanged =
      last.kind === kind && last.key === key && last.path === path;
    if (unchanged) return;

    capturePageview(path);
    lastSurfaceRef.current = next;
  }, [user, overlay, activeWorkspaceSlug, activeTabId, activeTabPath]);

  return null;
}

function overlayPath(overlay: WindowOverlay): string {
  switch (overlay.type) {
    case "new-workspace":
      return "/workspaces/new";
    case "onboarding":
      return "/onboarding";
    case "invite":
      return `/invite/${overlay.invitationId}`;
    case "invitations":
      return "/invitations";
  }
}
