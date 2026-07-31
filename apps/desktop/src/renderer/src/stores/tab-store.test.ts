import { describe, expect, it, vi, beforeEach } from "vitest";

import {
  sanitizeTabPath,
  resourceKeyForUrl,
  migrateV1ToV2,
  migrateV2ToV3,
  migrateV3ToV4,
  mergePersistedTabs,
  useTabStore,
  getActiveTab,
  type WorkspaceTabGroup,
} from "./tab-store";

beforeEach(() => {
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

  it("normalizes a bare workspace url to its default surface (replaces the in-router index redirect)", () => {
    expect(sanitizeTabPath("/acme")).toBe("/acme/issues");
    expect(sanitizeTabPath("/acme?welcome=1")).toBe("/acme/issues?welcome=1");
  });
});

describe("resourceKeyForUrl", () => {
  it("is the pathname — search and hash are view state, not identity", () => {
    expect(resourceKeyForUrl("/acme/issues")).toBe("/acme/issues");
    expect(resourceKeyForUrl("/acme/issues?filter=a")).toBe("/acme/issues");
    expect(resourceKeyForUrl("/acme/issues#anchor")).toBe("/acme/issues");
    expect(resourceKeyForUrl("/acme/issues?filter=a#x")).toBe("/acme/issues");
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
    expect(s.byWorkspace.acme.tabs[0].url).toBe("/acme/issues");
    expect(s.byWorkspace.acme.tabs[0].resourceKey).toBe("/acme/issues");
    expect(s.byWorkspace.acme.tabs[0].history).toEqual({
      stack: ["/acme/issues"],
      index: 0,
    });
  });

  it("switchWorkspace without openPath restores the group's last active tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects");
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

  it("switchWorkspace with openPath dedupes into an existing tab with the same resourceKey", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme"); // creates default /acme/issues
    store.addTab("/acme/projects", "Projects");

    store.switchWorkspace("acme", "/acme/issues");
    const s = useTabStore.getState();
    expect(s.byWorkspace.acme.tabs).toHaveLength(2); // no duplicate created
    const activeTab = s.byWorkspace.acme.tabs.find(
      (t) => t.id === s.byWorkspace.acme.activeTabId,
    );
    expect(activeTab?.url).toBe("/acme/issues");
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
    expect(activeTab?.url).toBe("/acme/issues/bug-42");
  });

  it("openTab dedupes by resourceKey within the active workspace", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const id1 = store.openTab("/acme/projects", "Projects");
    const id2 = store.openTab("/acme/projects", "Projects");
    expect(id1).toBe(id2);
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(2); // default + projects
  });

  it("openTab with a different query focuses the existing tab and keeps its url (RFC §8.2 semantic change)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme"); // default tab at /acme/issues
    const defaultTabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;

    const id = store.openTab("/acme/issues?filter=urgent", "Issues");

    const s = useTabStore.getState();
    expect(id).toBe(defaultTabId); // focused, not duplicated
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    // The existing tab's own view state (url) wins; the incoming filter does
    // not overwrite it.
    expect(s.byWorkspace.acme.tabs[0].url).toBe("/acme/issues");
  });

  it("closeTab on the last tab in a workspace reseeds the default tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const onlyTabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    store.closeTab(onlyTabId);
    const s = useTabStore.getState();
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    expect(s.byWorkspace.acme.tabs[0].url).toBe("/acme/issues");
    expect(s.byWorkspace.acme.tabs[0].id).not.toBe(onlyTabId); // fresh tab
  });

  it("ignores updates addressed to a tab after it has been closed", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const closedTabId = store.addTab("/acme/settings", "Settings");

    store.closeTab(closedTabId);
    const before = useTabStore.getState().byWorkspace.acme;

    store.updateTab(closedTabId, { title: "Ghost" });
    store.commitScrollMemento(closedTabId, "/acme/settings", {
      main: { top: 10, height: 100 },
    });

    expect(useTabStore.getState().byWorkspace.acme).toBe(before);
    expect(
      useTabStore.getState().byWorkspace.acme.tabs.some((t) => t.id === closedTabId),
    ).toBe(false);
  });

  it("does not replace the tab group for no-op title-sync updates", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tab = useTabStore.getState().byWorkspace.acme.tabs[0];
    const before = useTabStore.getState().byWorkspace.acme;

    store.updateTab(tab.id, { title: tab.title });

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

  it("validateWorkspaceSlugs seeds the first valid workspace when no group exists", () => {
    const store = useTabStore.getState();
    store.validateWorkspaceSlugs(new Set(["acme", "butter"]));
    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    expect(s.byWorkspace.acme.tabs[0].url).toBe("/acme/issues");
  });

  it("validateWorkspaceSlugs reactivates an existing valid group before seeding", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const existingTabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;

    useTabStore.setState({ activeWorkspaceSlug: null });
    store.validateWorkspaceSlugs(new Set(["acme"]));

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    expect(s.byWorkspace.acme.tabs[0].id).toBe(existingTabId);
  });

  it("validateWorkspaceSlugs seeds a fresh tab for a valid slug after dropping all stale groups", () => {
    const store = useTabStore.getState();
    // The only persisted group points at a workspace the user has lost access
    // to — the stale-tab heal path WorkspaceRouteLayout drives.
    store.switchWorkspace("stale");

    store.validateWorkspaceSlugs(new Set(["acme"]));

    const s = useTabStore.getState();
    expect(Object.keys(s.byWorkspace)).toEqual(["acme"]);
    expect(s.activeWorkspaceSlug).toBe("acme");
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
    expect(s.byWorkspace.acme.tabs[0].url).toBe("/acme/issues");
  });

  it("reset wipes the whole store", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.switchWorkspace("butter");
    store.reloadActiveTab();
    store.reset();
    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBeNull();
    expect(s.byWorkspace).toEqual({});
    expect(s.mountGeneration).toBe(0);
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

describe("navigateActiveSession", () => {
  it("updates url, resourceKey, and pushes onto the virtual history", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");

    store.navigateActiveSession("/acme/projects?sort=name");

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/projects?sort=name");
    expect(active.resourceKey).toBe("/acme/projects");
    expect(active.history).toEqual({
      stack: ["/acme/issues", "/acme/projects?sort=name"],
      index: 1,
    });
  });

  it("replace swaps the current history entry instead of pushing", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.navigateActiveSession("/acme/projects");

    store.navigateActiveSession("/acme/projects?sort=name", { replace: true });

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.history).toEqual({
      stack: ["/acme/issues", "/acme/projects?sort=name"],
      index: 1,
    });
  });

  it("a push after going back truncates the forward stack", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.navigateActiveSession("/acme/projects");
    store.navigateActiveSession("/acme/agents");
    store.goBack();
    store.goBack();

    store.navigateActiveSession("/acme/inbox");

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.history).toEqual({
      stack: ["/acme/issues", "/acme/inbox"],
      index: 1,
    });
  });

  it("rejects cross-workspace urls (those go through switchWorkspace)", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");

    store.navigateActiveSession("/butter/issues");

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/issues");
  });

  it("goBack/goForward project the history stack back into the url", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.navigateActiveSession("/acme/projects");

    store.goBack();
    let active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/issues");
    expect(active.history.index).toBe(0);

    store.goForward();
    active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/projects");
    expect(active.history.index).toBe(1);

    // Bounds: no-ops at the edges.
    store.goForward();
    expect(getActiveTab(useTabStore.getState())!.history.index).toBe(1);
  });
});

describe("reloadActiveTab", () => {
  it("bumps mountGeneration and leaves the session untouched", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const before = getActiveTab(useTabStore.getState())!;

    store.reloadActiveTab();

    const s = useTabStore.getState();
    expect(s.mountGeneration).toBe(1);
    expect(getActiveTab(s)).toBe(before);
  });

  it("is a no-op with no active workspace", () => {
    useTabStore.getState().reloadActiveTab();
    expect(useTabStore.getState().mountGeneration).toBe(0);
  });
});

describe("commitScrollMemento", () => {
  it("stores route-scoped entries on the addressed tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;

    store.commitScrollMemento(tabId, "/acme/issues", {
      "board:status:todo": { top: 420, height: 8000 },
    });

    expect(useTabStore.getState().byWorkspace.acme.tabs[0].memento).toEqual({
      scroll: { "/acme/issues::board:status:todo": { top: 420, height: 8000 } },
    });
  });

  it("REPLACES the route's entries — scrolling back to 0 clears the stale offset", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;

    store.commitScrollMemento(tabId, "/acme/issues", {
      list: { top: 500, height: 8000 },
    });
    // User scrolled back to top before leaving: the capture has no entry
    // for this container. The old 500 must not survive.
    store.commitScrollMemento(tabId, "/acme/issues", {});

    expect(useTabStore.getState().byWorkspace.acme.tabs[0].memento).toEqual({
      scroll: {},
    });
  });

  it("keeps other routes' entries when one route commits", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;

    store.commitScrollMemento(tabId, "/acme/issues", {
      list: { top: 500, height: 8000 },
    });
    store.commitScrollMemento(tabId, "/acme/issues/bug-42", {
      main: { top: 120, height: 3000 },
    });

    expect(useTabStore.getState().byWorkspace.acme.tabs[0].memento.scroll).toEqual({
      "/acme/issues::list": { top: 500, height: 8000 },
      "/acme/issues/bug-42::main": { top: 120, height: 3000 },
    });
  });

  it("skips the store write when nothing changed", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    store.commitScrollMemento(tabId, "/acme/issues", {
      list: { top: 500, height: 8000 },
    });
    const before = useTabStore.getState().byWorkspace.acme;

    store.commitScrollMemento(tabId, "/acme/issues", {
      list: { top: 500, height: 8000 },
    });

    expect(useTabStore.getState().byWorkspace.acme).toBe(before);
  });
});

describe("bulk tab closing", () => {
  it("closes other unpinned tabs, preserves pinned tabs, and activates the target", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const issuesId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    const projectsId = store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    store.addTab("/acme/settings", "Settings");
    store.togglePin(issuesId);
    store.setActiveTab(useTabStore.getState().byWorkspace.acme.tabs[2].id);

    store.closeOtherTabs(projectsId);

    const group = useTabStore.getState().byWorkspace.acme;
    expect(group.tabs.map((tab) => tab.id)).toEqual([issuesId, projectsId]);
    expect(group.activeTabId).toBe(projectsId);
  });

  it("keeps a surviving active tab when closing other tabs", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const issuesId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    const projectsId = store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    store.addTab("/acme/settings", "Settings");
    store.togglePin(issuesId);
    store.setActiveTab(issuesId);

    store.closeOtherTabs(projectsId);

    const group = useTabStore.getState().byWorkspace.acme;
    expect(group.tabs.map((tab) => tab.id)).toEqual([issuesId, projectsId]);
    expect(group.activeTabId).toBe(issuesId);
  });
});

describe("togglePin", () => {
  it("flips a tab's pinned state", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    const tabId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    expect(useTabStore.getState().byWorkspace.acme.tabs[0].pinned).toBe(false);

    store.togglePin(tabId);
    expect(useTabStore.getState().byWorkspace.acme.tabs[0].pinned).toBe(true);

    store.togglePin(tabId);
    expect(useTabStore.getState().byWorkspace.acme.tabs[0].pinned).toBe(false);
  });

  it("moves a newly-pinned tab to the start of the pinned zone", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme"); // creates default unpinned tab at index 0
    store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    const agentsId = useTabStore.getState().byWorkspace.acme.tabs[2].id;

    store.togglePin(agentsId);
    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    expect(tabs[0].id).toBe(agentsId);
    expect(tabs[0].pinned).toBe(true);
    expect(tabs[1].pinned).toBe(false);
    expect(tabs[2].pinned).toBe(false);
  });

  it("appends a second pinned tab after the first pinned tab", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    const projectsId = useTabStore.getState().byWorkspace.acme.tabs[1].id;
    const agentsId = useTabStore.getState().byWorkspace.acme.tabs[2].id;

    store.togglePin(agentsId);
    store.togglePin(projectsId);

    // Both pinned, in the order they were pinned (agents first, projects
    // second), then the unpinned default tab.
    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    expect(tabs.map((t) => t.id)).toEqual([
      agentsId,
      projectsId,
      tabs[2].id,
    ]);
    expect(tabs.map((t) => t.pinned)).toEqual([true, true, false]);
  });

  it("returns an unpinned tab to the start of the unpinned zone", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects");
    const issuesId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    const projectsId = useTabStore.getState().byWorkspace.acme.tabs[1].id;

    // Pin both, then unpin one.
    store.togglePin(issuesId);
    store.togglePin(projectsId);
    store.togglePin(issuesId);

    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    expect(tabs.map((t) => t.id)).toEqual([projectsId, issuesId]);
    expect(tabs.map((t) => t.pinned)).toEqual([true, false]);
  });
});

describe("moveTab boundary clamp", () => {
  it("clamps a pinned-tab move so it never crosses into the unpinned zone", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    const issuesId = useTabStore.getState().byWorkspace.acme.tabs[0].id;

    store.togglePin(issuesId); // [issues(pinned), projects, agents]

    // User tries to drag the pinned tab to index 2 (unpinned zone end).
    store.moveTab(0, 2);
    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    // It should be clamped to index 0 — the only pinned slot — i.e. unchanged.
    expect(tabs[0].id).toBe(issuesId);
    expect(tabs.map((t) => t.pinned)).toEqual([true, false, false]);
  });

  it("clamps an unpinned-tab move so it never crosses into the pinned zone", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    const issuesId = useTabStore.getState().byWorkspace.acme.tabs[0].id;
    const agentsId = useTabStore.getState().byWorkspace.acme.tabs[2].id;

    store.togglePin(issuesId); // [issues(pinned), projects, agents]

    // User tries to drag agents (index 2) to index 0 (pinned zone).
    store.moveTab(2, 0);
    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    // Clamped to index 1 — start of the unpinned zone.
    expect(tabs[0].id).toBe(issuesId);
    expect(tabs[1].id).toBe(agentsId);
    expect(tabs.map((t) => t.pinned)).toEqual([true, false, false]);
  });

  it("reorders freely within the same zone", () => {
    const store = useTabStore.getState();
    store.switchWorkspace("acme");
    store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");

    // All unpinned; move agents (2) to position 0.
    store.moveTab(2, 0);
    const tabs = useTabStore.getState().byWorkspace.acme.tabs;
    expect(tabs.map((t) => t.url)).toEqual([
      "/acme/agents",
      "/acme/issues",
      "/acme/projects",
    ]);
  });
});

describe("migrateV2ToV3", () => {
  it("adds pinned=false to every persisted tab", () => {
    const v2 = {
      activeWorkspaceSlug: "acme",
      byWorkspace: {
        acme: {
          activeTabId: "t1",
          tabs: [
            { id: "t1", path: "/acme/issues", title: "Issues", icon: "ListTodo" },
            { id: "t2", path: "/acme/projects", title: "Projects", icon: "FolderKanban" },
          ],
        },
      },
    };
    const v3 = migrateV2ToV3(v2);
    expect(v3.activeWorkspaceSlug).toBe("acme");
    expect(v3.byWorkspace.acme.tabs).toEqual([
      { id: "t1", path: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: false },
      { id: "t2", path: "/acme/projects", title: "Projects", icon: "FolderKanban", pinned: false },
    ]);
  });

  it("handles missing byWorkspace gracefully", () => {
    const v3 = migrateV2ToV3({ activeWorkspaceSlug: null } as Parameters<typeof migrateV2ToV3>[0]);
    expect(v3.byWorkspace).toEqual({});
    expect(v3.activeWorkspaceSlug).toBeNull();
  });
});

describe("migrateV3ToV4 (legacy view-state import, MUL-4741)", () => {
  it("converts path→url and seeds identity, history, and memento", () => {
    const v3 = {
      activeWorkspaceSlug: "acme",
      byWorkspace: {
        acme: {
          activeTabId: "t1",
          tabs: [
            {
              id: "t1",
              path: "/acme/issues",
              title: "Issues",
              icon: "ListTodo",
              pinned: true,
            },
          ],
        },
      },
    };
    const v4 = migrateV3ToV4(v3);
    expect(v4.activeWorkspaceSlug).toBe("acme");
    // `icon` is intentionally absent: it is derived from the url at render
    // time, so a legacy payload's icon name is never carried forward.
    expect(v4.byWorkspace.acme.tabs).toEqual([
      {
        id: "t1",
        url: "/acme/issues",
        title: "Issues",
        pinned: true,
        history: { stack: ["/acme/issues"], index: 0 },
        memento: { scroll: {} },
      },
    ]);
  });

  it("handles missing byWorkspace gracefully", () => {
    const v4 = migrateV3ToV4({
      activeWorkspaceSlug: null,
    } as Parameters<typeof migrateV3ToV4>[0]);
    expect(v4.byWorkspace).toEqual({});
    expect(v4.activeWorkspaceSlug).toBeNull();
  });
});

describe("mergePersistedTabs (rehydration, MUL-4370)", () => {
  const emptyState = (): {
    activeWorkspaceSlug: string | null;
    byWorkspace: Record<string, WorkspaceTabGroup>;
  } => ({ activeWorkspaceSlug: null, byWorkspace: {} });

  function persistedTab(url: string, extra: Record<string, unknown> = {}) {
    return {
      id: "t1",
      url,
      title: "Tab",
      pinned: false,
      history: { stack: [url], index: 0 },
      memento: { scroll: {} },
      ...extra,
    };
  }

  function rehydrate(tab: Record<string, unknown>) {
    return mergePersistedTabs(
      {
        activeWorkspaceSlug: "acme",
        byWorkspace: { acme: { activeTabId: "t1", tabs: [tab] } },
      },
      emptyState(),
    ).byWorkspace.acme.tabs[0];
  }

  // A user who opened /acme/autopilots on an older build has "ListTodo"
  // persisted for it. Carrying that value forward is what kept the tab bar
  // showing the wrong icon after upgrade, while the sidebar showed the new
  // one. The session must not hold an icon at all.
  it("does not carry a stale persisted icon into the session", () => {
    const tab = rehydrate(persistedTab("/acme/autopilots", { icon: "ListTodo" }));
    expect(tab).not.toHaveProperty("icon");
    expect(tab.url).toBe("/acme/autopilots");
  });

  it("ignores an unknown or corrupted persisted icon", () => {
    const tab = rehydrate(persistedTab("/acme/projects", { icon: "NotARealIcon" }));
    expect(tab).not.toHaveProperty("icon");
    expect(tab.url).toBe("/acme/projects");
  });

  it("rehydrates payloads with no icon field at all", () => {
    const tab = rehydrate(persistedTab("/acme/squads"));
    expect(tab).not.toHaveProperty("icon");
    expect(tab.url).toBe("/acme/squads");
  });
});
