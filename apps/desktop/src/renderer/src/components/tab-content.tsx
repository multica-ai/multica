import { Activity, useEffect, useState, type ReactNode } from "react";
import { RouterProvider } from "react-router-dom";
import { useActiveGroup, type Tab } from "@/stores/tab-store";
import { TabNavigationProvider } from "@/platform/navigation";
import { tabRuntimeRegistry } from "@/platform/tab-runtime";
import { useTabRuntimeSync } from "@/hooks/use-tab-runtime-sync";
import { useTabScrollRestore } from "@/hooks/use-tab-scroll-restore";

/**
 * Wraps a tab's subtree so its scroll position survives the round trip
 * through `<Activity mode="hidden">`. Lives inside Activity so the hook's
 * effects cycle with the tab's visibility — see `useTabScrollRestore` for
 * the mechanism. `display: contents` keeps the wrapper transparent to
 * the surrounding flex layout.
 */
function TabScrollRestoreWrapper({
  tabPath,
  children,
}: {
  tabPath: string;
  children: ReactNode;
}) {
  const ref = useTabScrollRestore(tabPath);
  return (
    <div ref={ref} style={{ display: "contents" }}>
      {children}
    </div>
  );
}

/**
 * One tab's rendered subtree. The live router + history mirror come from the
 * tab runtime registry (seeded from the tab's persisted session); the sync
 * hook feeds the mirror back into the store as the tab session. Mounted once
 * per acquired runtime — TabContent keys this by `tab.id:generation`, so a
 * tab reload (generation bump) remounts on a fresh runtime instead of mutating
 * the RouterProvider's `router` prop (which React Router forbids).
 */
function TabView({ tab, active }: { tab: Tab; active: boolean }) {
  const [runtime] = useState(() => tabRuntimeRegistry.getOrCreate(tab));
  useTabRuntimeSync(tab.id, runtime);
  return (
    <Activity mode={active ? "visible" : "hidden"}>
      <TabScrollRestoreWrapper tabPath={tab.path}>
        <TabNavigationProvider router={runtime.router}>
          <RouterProvider router={runtime.router} />
        </TabNavigationProvider>
      </TabScrollRestoreWrapper>
    </Activity>
  );
}

/**
 * Renders the active workspace's tabs. Only the active tab is visible; hidden
 * tabs keep their DOM and React state via <Activity>. The routers themselves
 * live in the tab runtime registry, not the store.
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
        <TabView
          key={`${tab.id}:${tab.generation}`}
          tab={tab}
          active={tab.id === group.activeTabId}
        />
      ))}
    </>
  );
}
