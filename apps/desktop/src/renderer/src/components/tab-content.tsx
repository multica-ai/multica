import { Activity, useEffect } from "react";
import { RouterProvider } from "react-router-dom";
import { useActiveGroup } from "@/stores/tab-store";
import { TabNavigationProvider } from "@/platform/navigation";
import { useTabRouterSync } from "@/hooks/use-tab-router-sync";
import type { Tab } from "@/stores/tab-store";

/**
 * Inner wrapper rendered inside each tab's RouterProvider. The router
 * reference is stable for a tab's lifetime, so passing it in directly
 * (instead of re-deriving from the store) avoids needless re-renders.
 */
function TabRouterInner({ tab }: { tab: Tab }) {
  useTabRouterSync(tab.id, tab.router);
  return null;
}

/**
 * Renders the active workspace's tabs using Activity for state preservation.
 * Only the active tab is visible; hidden tabs keep their DOM and React state.
 *
 * When switching workspaces, the previous workspace's tabs unmount entirely
 * and the new workspace's tabs mount fresh — cross-workspace state
 * preservation is an explicit non-goal (keeping all workspaces' tabs warm
 * simultaneously would bloat memory and make workspace switching feel
 * anything but "switching").
 */
export function TabContent() {
  const group = useActiveGroup();

  // Sync document.title when switching tabs within the active workspace.
  useEffect(() => {
    if (!group) return;
    const tab = group.tabs.find((t) => t.id === group.activeTabId);
    if (tab) document.title = tab.title;
  }, [group?.activeTabId, group?.tabs]);

  if (!group) return null;

  return (
    <>
      {group.tabs.map((tab) => (
        <Activity
          key={tab.id}
          mode={tab.id === group.activeTabId ? "visible" : "hidden"}
        >
          <TabNavigationProvider router={tab.router}>
            <RouterProvider router={tab.router} />
            <TabRouterInner tab={tab} />
          </TabNavigationProvider>
        </Activity>
      ))}
    </>
  );
}
