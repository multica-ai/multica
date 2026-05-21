import { describe, expect, it, vi, beforeEach } from "vitest";

// createTabRouter transitively pulls in route modules that expect a browser
// router context. For pure store tests we stub it to a minimal disposable.
const createTabRouterMock = vi.hoisted(() =>
  vi.fn(() => ({
    dispose: vi.fn(),
    state: { location: { pathname: "/" } },
    navigate: vi.fn(),
    subscribe: vi.fn(() => () => {}),
  })),
);
vi.mock("../routes", () => ({
  createTabRouter: createTabRouterMock,
}));

import {
  sanitizeTabPath,
  migrateV1ToV2,
  useTabStore,
} from "./tab-store";

beforeEach(() => {
  createTabRouterMock.mockClear();
  useTabStore.getState().reset();
});

describe("sanitizeTabPath", () => {
  it("rejects the root sentinel — tabs must be workspace-scoped", () => {
    expect(sanitizeTabPath("/")).toBeNull();
    expect(sanitizeTabPath("")).toBeNull();
  });

  it("silently rejects transition paths (no warn — navigation adapter intercepts them)", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(sanitizeTabPath("/workspaces/new")).toBeNull();
    expect(sanitizeTabPath("/invite/abc")).toBeNull();
    expect(warn).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  it("passes through valid workspace-scoped paths", () => {
    expect(sanitizeTabPath("/acme/issues")).toBe("/acme/issues");
    expect(sanitizeTabPath("/my-team/projects/abc")).toBe("/my-team/projects/abc");
  });

  it("rejects paths whose first segment is a reserved slug (missing workspace prefix)", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(sanitizeTabPath("/issues")).toBeNull();
    expect(sanitizeTabPath("/settings")).toBeNull();
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it("passes through user slugs that happen to look path-like but aren't reserved", () => {
    expect(sanitizeTabPath("/acme-issues/issues")).toBe("/acme-issues/issues");
    expect(sanitizeTabPath("/project-x/inbox")).toBe("/project-x/inbox");
  });
});

describe("migrateV1ToV2", () => {
  it("groups v1 flat tabs by workspace slug", () => {
    const v1 = {
      tabs: [
        { id: "t1", path: "/acme/issues", title: "Issues", icon: "ListTodo" },
        { id: "t2", path: "/acme/projects", title: "Projects", icon: "FolderKanban" },
        { id: "t3", path: "/butter/issues", title: "Issues", icon: "ListTodo" },
      ],
      activeTabId: "t2",
    };
    const v2 = migrateV1ToV2(v1);
    expect(Object.keys(v2.byWorkspace).sort()).toEqual(["acme", "butter"]);
    expect(v2.byWorkspace.acme.tabs).toHaveLength(2);
    expect(v2.byWorkspace.butter.tabs).toHaveLength(1);
    expect(v2.byWorkspace.acme.activeTabId).toBe("t2");
    expect(v2.byWorkspace.butter.activeTabId).toBe("t3"); // first tab in group
    expect(v2.activeWorkspaceSlug).toBe("acme"); // contained v1.activeTabId
  });

  it("drops tabs at root / transition / reserved-slug paths", () => {
    const v1 = {
      tabs: [
        { id: "t1", path: "/", title: "Issues", icon: "ListTodo" },
        { id: "t2", path: "/workspaces/new", title: "New", icon: "Plus" },
        { id: "t3", path: "/invite/abc", title: "Invite", icon: "Mail" },
        { id: "t4", path: "/acme/issues", title: "Issues", icon: "ListTodo" },
      ],
      activeTabId: "t1",
    };
    const v2 = migrateV1ToV2(v1);
    expect(Object.keys(v2.byWorkspace)).toEqual(["acme"]);
    expect(v2.byWorkspace.acme.tabs).toHaveLength(1);
    // v1.activeTabId was dropped; active falls back to first group's first tab.
    expect(v2.activeWorkspaceSlug).toBe("acme");
    expect(v2.byWorkspace.acme.activeTabId).toBe("t4");
  });

  it("handles empty v1 state gracefully", () => {
    const v2 = migrateV1ToV2({ tabs: [], activeTabId: "" });
    expect(v2.byWorkspace).toEqual({});
    expect(v2.activeWorkspaceSlug).toBeNull();
  });

  it("handles v1 with no tabs field (corrupted state)", () => {
    const v2 = migrateV1ToV2({});
    expect(v2.byWorkspace).toEqual({});
    expect(v2.activeWorkspaceSlug).toBeNull();
  });
});

describe("useTabStore actions", () => {
  it("switchWorkspace creates a new group with a default tab on first entry", () => {
    useTabStore.getState().switchWorkspace("acme");
    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    expect(s.byWorkspace.acme.tabs[0].path).toBe("/acme/issues");
  });

  it("switchWorkspace without openPath restores the group's last active tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects", "FolderKanban");
    const acmeProjectsId = useTabStore.getState().byWorkspace.acme.tabs[1].id;
    store.setActiveTab(acmeProjectsId);

    // Enter a different workspace then come back
    store.switchWorkspace("butter");
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("butter");

    store.switchWorkspace("acme");
    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.activeTabId).toBe(acmeProjectsId);
  });

  it("switchWorkspace with openPath dedupes into an existing tab with same path", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme"); // creates default /acme/issues
    store.addTab("/acme/projects", "Projects", "FolderKanban");

    store.switchWorkspace("acme", "/acme/issues");
    const s = useTabStore.getState();
    expect(s.byWorkspace.acme.tabs).toHaveLength(2); // no duplicate created
    const activeTab = s.byWorkspace.acme.tabs.find(
      (t) => t.id === s.byWorkspace.acme.activeTabId,
    );
    expect(activeTab?.path).toBe("/acme/issues");
  });

  it("switchWorkspace with openPath not matching any tab adds a new tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("acme", "/acme/issues/bug-42");
    const s = useTabStore.getState();
    expect(s.byWorkspace.acme.tabs).toHaveLength(2);
    const activeTab = s.byWorkspace.acme.tabs.find(
      (t) => t.id === s.byWorkspace.acme.activeTabId,
    );
    expect(activeTab?.path).toBe("/acme/issues/bug-42");
  });

  it("openTab dedupes by path within the active workspace", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const id1 = store.openTab("/acme/projects", "Projects", "FolderKanban");
    const id2 = store.openTab("/acme/projects", "Projects", "FolderKanban");
    expect(id1).toBe(id2);
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(2); // default + projects
  });

  it("closeTab on the last tab in a workspace reseeds the default tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const onlyTabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    store.closeTab(onlyTabId);
    const s = useTabStore.getState();
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    expect(s.byWorkspace.acme.tabs[0].path).toBe("/acme/issues");
    expect(s.byWorkspace.acme.tabs[0].id).not.toBe(onlyTabId); // fresh tab
  });

  it("defers disposing the closed tab router until after the store update", () => {
    vi.useFakeTimers();
    try {
      const store = useTabStore.getState();
      store.switchWorkspace("acme");
      const closedTabId = store.addTab("/acme/settings", "Settings", "Settings");
      const closingTab = useTabStore
        .getState()
        .byWorkspace.acme.tabs.find((t) => t.id === closedTabId);
      const dispose = vi.mocked(closingTab!.router.dispose);

      store.closeTab(closedTabId);

      expect(dispose).not.toHaveBeenCalled();
      expect(
        useTabStore.getState().byWorkspace.acme.tabs.some((t) => t.id === closedTabId),
      ).toBe(false);

      vi.runAllTimers();

      expect(dispose).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("ignores router-sync updates from a tab after it has been closed", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const closedTabId = store.addTab("/acme/settings", "Settings", "Settings");

    store.closeTab(closedTabId);
    const before = useTabStore.getState().byWorkspace.acme;

    store.updateTab(closedTabId, { path: "/acme/runtimes", icon: "Monitor" });
    store.updateTabHistory(closedTabId, 1, 2);

    expect(useTabStore.getState().byWorkspace.acme).toBe(before);
    expect(
      useTabStore.getState().byWorkspace.acme.tabs.some((t) => t.id === closedTabId),
    ).toBe(false);
  });

  it("does not replace the tab group for no-op router-sync updates", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tab = useTabStore.getState().byWorkspace.acme.tabs[0];
    const before = useTabStore.getState().byWorkspace.acme;

    store.updateTab(tab.id, { path: tab.path, icon: tab.icon, title: tab.title });
    store.updateTabHistory(tab.id, tab.historyIndex, tab.historyLength);

    expect(useTabStore.getState().byWorkspace.acme).toBe(before);
  });

  it("validateWorkspaceSlugs drops groups for slugs not in the valid set and repoints active", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");
    store.switchWorkspace("acme");
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("acme");

    // Admin removed the user from acme
    store.validateWorkspaceSlugs(new Set(["butter"]));
    const s = useTabStore.getState();
    expect(Object.keys(s.byWorkspace)).toEqual(["butter"]);
    expect(s.activeWorkspaceSlug).toBe("butter");
  });

  it("validateWorkspaceSlugs sets activeWorkspaceSlug to null when all groups are dropped", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.validateWorkspaceSlugs(new Set());
    const s = useTabStore.getState();
    expect(s.byWorkspace).toEqual({});
    expect(s.activeWorkspaceSlug).toBeNull();
  });

  it("reset wipes the whole store", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");
    store.reset();
    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBeNull();
    expect(s.byWorkspace).toEqual({});
  });

  it("setActiveTab across workspaces also flips the active workspace", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");
    const acmeTabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    store.setActiveTab(acmeTabId);
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("acme");
  });
});
