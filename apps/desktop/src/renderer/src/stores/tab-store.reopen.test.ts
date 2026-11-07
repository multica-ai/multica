// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { getActiveTab, mergePersistedTabs, useTabStore } from "./tab-store";

beforeEach(() => {
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
});

function close(url: string, title = url) {
  const store = useTabStore.getState();
  const id = store.openTab(url, title, { activate: true });
  store.closeTab(id);
  return id;
}

function active() { return getActiveTab(useTabStore.getState())!; }

describe("renderer-local closed destinations", () => {
  it("is LIFO, bounded to the newest twenty, with empty calls leaving live state alone", () => {
    for (let index = 0; index < 25; index++) close(`/acme/issues/${index}`);
    expect(useTabStore.getState().closedTabsByWorkspace.acme).toHaveLength(20);
    for (let index = 24; index >= 5; index--) {
      useTabStore.getState().reopenClosedTab();
      expect(active().url).toBe(`/acme/issues/${index}`);
    }
    const group = useTabStore.getState().byWorkspace.acme;
    useTabStore.getState().reopenClosedTab();
    expect(useTabStore.getState().byWorkspace.acme).toBe(group);
  });

  it("activates the current authoritative session for a resource already opened again", () => {
    close("/acme/issues/ABC?filter=old#old");
    const store = useTabStore.getState();
    const id = store.openTab("/acme/issues/ABC?filter=new#current", "Current title");
    store.commitViewState(id, "/acme/issues/ABC", "state", "current");
    const current = useTabStore.getState().byWorkspace.acme.tabs.find((tab) => tab.id === id);
    store.reopenClosedTab();
    expect(active()).toBe(current);
    expect(useTabStore.getState().byWorkspace.acme.tabs.filter((tab) => tab.resourceKey === "/acme/issues/ABC")).toHaveLength(1);
    expect(useTabStore.getState().closedTabsByWorkspace.acme).toEqual([]);
  });

  it("preserves saved-view resource identity when reopening", () => {
    close("/acme/issues?view=one&sort=old#anchor");
    const store = useTabStore.getState();
    store.openTab("/acme/issues?view=two", "Other view");
    store.reopenClosedTab();
    expect(active().url).toBe("/acme/issues?view=one&sort=old#anchor");
  });

  it("clamps insertion around pinned prefixes after tab movement", () => {
    close("/acme/issues/ABC");
    const store = useTabStore.getState();
    const first = store.byWorkspace.acme.tabs[0].id;
    const second = store.addTab("/acme/projects", "Projects");
    store.togglePin(first);
    store.togglePin(second);
    store.reopenClosedTab();
    expect(useTabStore.getState().byWorkspace.acme.tabs.map((tab) => tab.pinned)).toEqual([true, true, false]);
    close("/acme/agents");
    useTabStore.setState({ closedTabsByWorkspace: { acme: [{ url: "/acme/settings", title: "Settings", index: 100 }] } });
    store.reopenClosedTab();
    expect(useTabStore.getState().byWorkspace.acme.tabs.at(-1)?.url).toBe("/acme/settings");
  });

  it("records inactive-group closes in their owner and only reopens the visible workspace", () => {
    const store = useTabStore.getState();
    const id = store.openTab("/acme/projects", "Projects");
    store.switchWorkspace("other");
    store.closeTab(id);
    expect(useTabStore.getState().closedTabsByWorkspace.acme).toHaveLength(1);
    store.reopenClosedTab();
    expect(active().url).toBe("/other/issues");
    store.switchWorkspace("acme");
    store.reopenClosedTab();
    expect(active().url).toBe("/acme/projects");
  });

  it("excludes pinned, invalid ids, recovery closes and forced final-tab reseeding", () => {
    const store = useTabStore.getState();
    store.closeTab("missing");
    store.closeActiveTab();
    const pinned = store.openTab("/acme/projects", "Projects", { activate: true });
    store.togglePin(pinned);
    store.closeActiveTab();
    expect(useTabStore.getState().byWorkspace.acme.tabs.some((tab) => tab.id === pinned)).toBe(false);
    const recovery = store.openTab("/acme/agents", "Agents", { activate: true });
    store.closeActiveTab({ reason: "recovery" });
    expect(useTabStore.getState().byWorkspace.acme.tabs.some((tab) => tab.id === recovery)).toBe(false);
    expect(useTabStore.getState().closedTabsByWorkspace).toEqual({});
  });

  it("discards invalid and foreign destinations, and consumes them atomically before the next valid one", () => {
    useTabStore.setState({ closedTabsByWorkspace: { acme: [
      { url: "/acme/projects?x=1#saved", title: "Projects", index: 1 },
      { url: "/other/issues/ABC", title: "Foreign", index: 1 },
      { url: "https://example.com/acme/issues", title: "External", index: 1 },
      { url: "//acme/issues", title: "Host", index: 1 },
      { url: "/\\", title: "Malformed host", index: 1 },
      { url: "/acme/../other/issues", title: "Traversal", index: 1 },
      { url: "/invite/token", title: "Invite", index: 1 },
    ] } });
    const snapshots: number[] = [];
    const unsubscribe = useTabStore.subscribe((state) => snapshots.push(state.closedTabsByWorkspace.acme.length));
    useTabStore.getState().reopenClosedTab();
    unsubscribe();
    expect(active().url).toBe("/acme/projects?x=1#saved");
    expect(snapshots).toEqual([0]);
    useTabStore.setState({ activeWorkspaceSlug: null });
    useTabStore.getState().reopenClosedTab();
    expect(useTabStore.getState().activeWorkspaceSlug).toBeNull();
  });

  it("bounds Close Others history and preserves MRU survivors", () => {
    const store = useTabStore.getState();
    const keeper = active();
    for (let index = 0; index < 25; index++) store.addTab(`/acme/issues/${index}`, `${index}`);
    store.closeOtherTabs(keeper.id);
    expect(useTabStore.getState().closedTabsByWorkspace.acme).toHaveLength(20);
    expect(active().id).toBe(keeper.id);
    expect(useTabStore.getState().byWorkspace.acme.recentTabIds).toEqual([]);
    store.reopenClosedTab();
    expect(active().title).toBe("0");
  });

  it("prunes closed-only stale keys even with unchanged live groups, then clears all workspaces on reset", () => {
    close("/acme/projects");
    useTabStore.setState({ closedTabsByWorkspace: { ...useTabStore.getState().closedTabsByWorkspace,
      revoked: [{ url: "/revoked/issues/ABC", title: "Private", index: 1 }],
    } });
    const store = useTabStore.getState();
    store.validateWorkspaceSlugs(new Set(["acme"]));
    expect(Object.keys(useTabStore.getState().closedTabsByWorkspace)).toEqual(["acme"]);
    store.switchWorkspace("other");
    close("/other/projects");
    store.validateWorkspaceSlugs(new Set(["acme"]));
    expect(Object.keys(useTabStore.getState().closedTabsByWorkspace)).toEqual(["acme"]);
    store.reset();
    store.switchWorkspace("acme");
    store.reopenClosedTab();
    expect(active().url).toBe("/acme/issues");
    expect(useTabStore.getState().closedTabsByWorkspace).toEqual({});
  });

  it("ignores a closed-history field injected into a persisted payload", () => {
    const state = useTabStore.getState();
    const result = mergePersistedTabs({ ...state, closedTabsByWorkspace: { acme: [
      { url: "/acme/issues/secret", title: "Untrusted", index: 1 },
    ] } }, state);
    expect(result.closedTabsByWorkspace).toEqual({});
  });
});
