import { useEffect, useRef } from "react";
import { capturePageview } from "@multica/core/analytics";
import { useAuthStore } from "@multica/core/auth";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore, type WindowOverlay } from "@/stores/window-overlay-store";

/**
 * Fires a PostHog $pageview whenever the user's visible surface changes,
 * EXCEPT for pure tab / workspace switches.
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
 * Tab-switch suppression: switching between already-open tabs (or
 * workspaces) surfaces a previously-visited path under a new `tabId`. The
 * pageview for that path was already emitted when the user originally
 * navigated there — re-emitting on every tab switch was the single largest
 * source of PostHog quota burn (~50% of all `$pageview` events) and added
 * no new signal. We detect a tab switch as "the active surface stayed
 * `tab` but the `(workspace, tabId)` identity changed" and skip the
 * capture, while still updating the ref so the next in-tab navigation
 * compares against the right baseline.
 *
 * Login transitions, overlay open/close, and intra-tab navigation still
 * fire — those are real surface changes the funnel cares about.
 *
 * PostHog's `capture_pageview: true` auto-capture is intentionally off (see
 * `initAnalytics`) so this component owns the event shape, matching the web
 * implementation in `apps/web/components/pageview-tracker.tsx`.
 */
export function PageviewTracker() {
  const user = useAuthStore((s) => s.user);
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const activeWorkspaceSlug = useTabStore((s) => s.activeWorkspaceSlug);
  const activeTabId = useTabStore((s) => {
    const slug = s.activeWorkspaceSlug;
    if (!slug) return null;
    return s.byWorkspace[slug]?.activeTabId ?? null;
  });
  const activeTabPath = useTabStore((s) => {
    const slug = s.activeWorkspaceSlug;
    if (!slug) return null;
    const group = s.byWorkspace[slug];
    if (!group) return null;
    return group.tabs.find((t) => t.id === group.activeTabId)?.path ?? null;
  });

  const lastRef = useRef<{
    kind: "login" | "overlay" | "tab" | null;
    slug: string | null;
    tabId: string | null;
    path: string | null;
  }>({ kind: null, slug: null, tabId: null, path: null });

  useEffect(() => {
    let kind: "login" | "overlay" | "tab";
    let slug: string | null = null;
    let tabId: string | null = null;
    let path: string;

    if (!user) {
      kind = "login";
      path = "/login";
    } else if (overlay) {
      kind = "overlay";
      path = overlayPath(overlay);
    } else if (activeTabPath && activeTabId && activeWorkspaceSlug) {
      kind = "tab";
      slug = activeWorkspaceSlug;
      tabId = activeTabId;
      path = activeTabPath;
    } else {
      return;
    }

    const last = lastRef.current;
    const next = { kind, slug, tabId, path };

    const isTabSwitch =
      last.kind === "tab" &&
      kind === "tab" &&
      (last.slug !== slug || last.tabId !== tabId);
    if (isTabSwitch) {
      lastRef.current = next;
      return;
    }

    const unchanged =
      last.kind === kind &&
      last.slug === slug &&
      last.tabId === tabId &&
      last.path === path;
    if (unchanged) return;

    capturePageview(path);
    lastRef.current = next;
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
