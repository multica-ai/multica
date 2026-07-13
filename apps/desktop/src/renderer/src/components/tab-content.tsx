import { useEffect, useState } from "react";
import { RouterProvider } from "react-router-dom";
import { useActiveGroup, type Tab } from "@/stores/tab-store";
import { TabNavigationProvider } from "@/platform/navigation";
import { tabRuntimeRegistry } from "@/platform/tab-runtime";
import { SurfaceViewStoreProvider } from "@/platform/surface-view-store-provider";
import { useTabRuntimeSync } from "@/hooks/use-tab-runtime-sync";
import { useTabScrollRestore } from "@/hooks/use-tab-scroll-restore";

/**
 * The active tab's rendered subtree — the single live page tree. Inactive
 * tabs are not rendered at all (no DOM, no query observers / effects), which
 * is the whole point of the Session model: a tab is a resumable session, not a
 * kept-alive page. The live router + history mirror come from the tab runtime
 * registry (seeded from the tab's persisted session); the sync hook feeds the
 * mirror back into the store, and the scroll hook persists / restores scroll
 * across the mount cycle. `display: contents` keeps the wrapper transparent to
 * the surrounding flex layout.
 */
function TabView({ tab }: { tab: Tab }) {
  const [runtime] = useState(() => tabRuntimeRegistry.getOrCreate(tab));
  useTabRuntimeSync(tab.id, runtime);
  const scrollRef = useTabScrollRestore(tab.id, tab.path);
  return (
    <div ref={scrollRef} style={{ display: "contents" }}>
      <SurfaceViewStoreProvider tabId={tab.id}>
        <TabNavigationProvider router={runtime.router}>
          <RouterProvider router={runtime.router} />
        </TabNavigationProvider>
      </SurfaceViewStoreProvider>
    </div>
  );
}

/**
 * Renders only the active workspace's active tab. Switching tabs swaps the
 * whole subtree (keyed by `tab.id:generation`): the outgoing tab unmounts —
 * releasing its DOM and observers — and the incoming tab mounts fresh from its
 * session. The registry keeps the active tab plus one recent tab warm so quick
 * back-and-forth reuses the router without re-seeding; a reload (generation
 * bump) remounts on a fresh runtime rather than mutating the RouterProvider's
 * `router` prop (which React Router forbids).
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
  const activeTab = group.tabs.find((t) => t.id === group.activeTabId);
  if (!activeTab) return null;

  return (
    <TabView key={`${activeTab.id}:${activeTab.generation}`} tab={activeTab} />
  );
}
