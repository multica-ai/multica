import { useEffect } from "react";
import { capturePageview } from "@multica/core/analytics";
import { useAuthStore } from "@multica/core/auth";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore, type WindowOverlay } from "@/stores/window-overlay-store";

/**
 * Fires a PostHog $pageview whenever the user's visible surface changes.
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
 * The overlay takes precedence over the tab path because it is visually in
 * front of the tab system; the logged-out state shadows both because the
 * shell doesn't render at all yet. This keeps the `$pageview` stream aligned
 * with what the user actually sees.
 *
 * PostHog's `capture_pageview: true` auto-capture is intentionally off (see
 * `initAnalytics`) so this component owns the event shape, matching the web
 * implementation in `apps/web/components/pageview-tracker.tsx`.
 */
export function PageviewTracker() {
  const user = useAuthStore((s) => s.user);
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const activeTabPath = useTabStore((s) => {
    const slug = s.activeWorkspaceSlug;
    if (!slug) return null;
    const group = s.byWorkspace[slug];
    if (!group) return null;
    return group.tabs.find((t) => t.id === group.activeTabId)?.path ?? null;
  });

  const path = resolvePath(user, overlay, activeTabPath);

  useEffect(() => {
    if (!path) return;
    capturePageview(path);
  }, [path]);

  return null;
}

function resolvePath(
  user: unknown,
  overlay: WindowOverlay | null,
  activeTabPath: string | null,
): string | null {
  if (!user) return "/login";
  if (overlay) return overlayPath(overlay);
  return activeTabPath;
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
