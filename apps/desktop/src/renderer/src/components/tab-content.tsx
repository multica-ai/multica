import { Activity, useEffect } from "react";
import { RouterProvider } from "react-router-dom";
import { useTabStore } from "@/stores/tab-store";
import { TabNavigationProvider } from "@/platform/navigation";
import { useTabRouterSync } from "@/hooks/use-tab-router-sync";

/** Inner wrapper rendered inside each tab's RouterProvider. */
function TabRouterInner({ tabId }: { tabId: string }) {
  const tab = useTabStore((s) => s.tabs.find((t) => t.id === tabId));
  useTabRouterSync(tabId, tab!.router);
  return null;
}

/**
 * Renders all tabs using Activity for state preservation.
 * Only the active tab is visible; hidden tabs keep their DOM and React state.
 */
export function TabContent() {
  const tabs = useTabStore((s) => s.tabs);
  const activeTabId = useTabStore((s) => s.activeTabId);

  // Sync document.title when switching tabs
  useEffect(() => {
    const tab = tabs.find((t) => t.id === activeTabId);
    if (tab) document.title = tab.title;
  }, [activeTabId, tabs]);

  return (
    <>
      {tabs.map((tab) => (
        <Activity
          key={tab.id}
          mode={tab.id === activeTabId ? "visible" : "hidden"}
        >
          <TabNavigationProvider router={tab.router}>
            <RouterProvider router={tab.router} />
            <TabRouterInner tabId={tab.id} />
          </TabNavigationProvider>
        </Activity>
      ))}
    </>
  );
}
