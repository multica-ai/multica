import { beforeEach, describe, expect, it, vi } from "vitest";

// Keep the memory routers light: the registry only needs a route table that
// matches any path. The real page routes (and their heavy imports) are
// irrelevant to runtime lifecycle.
vi.mock("../routes", () => ({
  appRoutes: [{ path: "*", element: null }],
}));

import { tabRuntimeRegistry } from "./tab-runtime";
import { useTabStore, getActiveTab, getTabById } from "@/stores/tab-store";

function activeTab() {
  const tab = getActiveTab(useTabStore.getState());
  if (!tab) throw new Error("no active tab");
  return tab;
}

beforeEach(() => {
  useTabStore.getState().reset();
  tabRuntimeRegistry.disposeAll();
});

describe("TabRuntimeRegistry", () => {
  it("caches one runtime per tab and seeds the router from the tab session", () => {
    useTabStore.getState().switchWorkspace("acme");
    const tab = activeTab();

    const first = tabRuntimeRegistry.getOrCreate(tab);
    const second = tabRuntimeRegistry.getOrCreate(tab);

    expect(second).toBe(first);
    expect(first.tabId).toBe(tab.id);
    expect(first.router.state.location.pathname).toBe("/acme/issues");
  });

  it("seeds the router at the session's current entry (initialIndex)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = activeTab().id;
    store.syncTabRuntime(tabId, {
      path: "/acme/projects",
      icon: "FolderKanban",
      session: { entries: ["/acme/issues", "/acme/projects"], index: 1 },
    });

    // Drop any runtime the reconcile eagerly created so the next acquire
    // re-seeds from the now multi-entry session — the restore-from-persisted-
    // session path.
    tabRuntimeRegistry.disposeAll();
    const runtime = tabRuntimeRegistry.getOrCreate(activeTab());
    expect(runtime.router.state.location.pathname).toBe("/acme/projects");
  });

  it("getActiveRouter returns the active tab's router instance", () => {
    useTabStore.getState().switchWorkspace("acme");

    const router = tabRuntimeRegistry.getActiveRouter();
    expect(router).not.toBeNull();
    expect(router?.state.location.pathname).toBe("/acme/issues");
    // Same instance the tab subtree would acquire.
    expect(tabRuntimeRegistry.getOrCreate(activeTab()).router).toBe(router);
  });

  it("disposes a tab's runtime once the tab is removed from the store", () => {
    vi.useFakeTimers();
    try {
      const store = useTabStore.getState();
      store.switchWorkspace("acme");
      const extraId = store.addTab("/acme/projects", "Projects", "FolderKanban");
      const tab = useTabStore
        .getState()
        .byWorkspace.acme.tabs.find((t) => t.id === extraId)!;
      const runtime = tabRuntimeRegistry.getOrCreate(tab);
      const routerDispose = vi.spyOn(runtime.router, "dispose");

      // Closing removes the tab; the registry reacts to the store change.
      store.closeTab(extraId);
      // Router disposal is deferred a tick so React unmounts the provider first.
      vi.runAllTimers();

      expect(routerDispose).toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("reloadActive rebuilds the active runtime and resets the session", () => {
    vi.useFakeTimers();
    try {
      const store = useTabStore.getState();
      store.switchWorkspace("acme");
      const tabId = activeTab().id;
      store.syncTabRuntime(tabId, {
        path: "/acme/projects",
        icon: "FolderKanban",
        session: { entries: ["/acme/issues", "/acme/projects"], index: 1 },
      });
      const beforeId = tabRuntimeRegistry.getOrCreate(activeTab()).id;

      tabRuntimeRegistry.reloadActive();

      // Session collapses to a single entry at the current path + generation bump.
      const tab = useTabStore.getState().byWorkspace.acme.tabs[0];
      expect(tab.session).toEqual({ entries: ["/acme/projects"], index: 0 });
      expect(tab.generation).toBe(1);

      // A fresh runtime (new id) is produced on next acquire.
      const afterRuntime = tabRuntimeRegistry.getOrCreate(activeTab());
      expect(afterRuntime.id).not.toBe(beforeId);
      expect(afterRuntime.router.state.location.pathname).toBe("/acme/projects");

      vi.runAllTimers();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps current + most-recent-1 warm and evicts the LRU runtime", () => {
    vi.useFakeTimers();
    try {
      const store = useTabStore.getState();
      store.switchWorkspace("acme");
      const tab1 = activeTab().id;
      const tab2 = store.addTab("/acme/projects", "Projects", "FolderKanban");
      const tab3 = store.addTab("/acme/agents", "Agents", "Bot");

      // Anchor + spy on tab1's runtime (tab1 is active, so it exists).
      const rt1 = tabRuntimeRegistry.getOrCreate(
        getTabById(useTabStore.getState(), tab1)!,
      );
      const rt1Dispose = vi.spyOn(rt1.router, "dispose");

      store.setActiveTab(tab2); // warm { tab1, tab2 }
      const rt2Id = tabRuntimeRegistry.getOrCreate(
        getTabById(useTabStore.getState(), tab2)!,
      ).id;

      store.setActiveTab(tab3); // { tab1, tab2, tab3 } -> evict LRU (tab1)
      vi.runAllTimers();

      // tab1 was the least-recently-used and is evicted.
      expect(rt1Dispose).toHaveBeenCalled();
      // tab2 stayed warm (same runtime instance, not re-created).
      expect(
        tabRuntimeRegistry.getOrCreate(getTabById(useTabStore.getState(), tab2)!).id,
      ).toBe(rt2Id);
    } finally {
      vi.useRealTimers();
    }
  });
});
