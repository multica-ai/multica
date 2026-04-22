import { useEffect } from "react";
import { capturePageview } from "@multica/core/analytics";
import { useTabStore } from "@/stores/tab-store";

/**
 * Fires a PostHog $pageview whenever the visible (active) tab's path changes.
 * Desktop routing lives in per-tab memory routers, so the user's "current
 * URL" is whichever path the active tab is on — the tab store keeps that in
 * sync via `useTabRouterSync`. Switching tabs or workspaces is a page
 * transition from the user's perspective and fires too.
 *
 * PostHog's `capture_pageview: true` auto-capture is intentionally off (see
 * `initAnalytics`) so this component owns the event shape, matching the web
 * implementation in `apps/web/components/pageview-tracker.tsx`.
 */
export function PageviewTracker() {
  const activePath = useTabStore((s) => {
    const slug = s.activeWorkspaceSlug;
    if (!slug) return null;
    const group = s.byWorkspace[slug];
    if (!group) return null;
    return group.tabs.find((t) => t.id === group.activeTabId)?.path ?? null;
  });

  useEffect(() => {
    if (!activePath) return;
    capturePageview(activePath);
  }, [activePath]);

  return null;
}
