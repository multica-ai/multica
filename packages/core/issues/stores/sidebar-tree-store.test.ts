// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import { useSidebarTreeStore } from "./sidebar-tree-store";

beforeEach(() => {
  localStorage.clear();
  useSidebarTreeStore.setState({ expanded: [], workingCollapsed: false, needsAttentionCollapsed: false });
});

afterEach(async () => {
  setCurrentWorkspace(null, null);
  await Promise.resolve();
});

describe("useSidebarTreeStore", () => {
  it("toggles expansion idempotently", () => {
    const store = useSidebarTreeStore.getState();
    store.setExpanded("project:p1", true);
    store.setExpanded("project:p1", true);
    expect(useSidebarTreeStore.getState().expanded).toEqual(["project:p1"]);
    store.setExpanded("project:p1", false);
    expect(useSidebarTreeStore.getState().expanded).toEqual([]);
  });

  it("collapses each section on its own", () => {
    useSidebarTreeStore.setState({ viewsCollapsed: false, pinnedCollapsed: false, projectsCollapsed: false });
    useSidebarTreeStore.getState().setPinnedCollapsed(true);
    const state = useSidebarTreeStore.getState();
    expect([state.viewsCollapsed, state.pinnedCollapsed, state.projectsCollapsed]).toEqual([false, true, false]);
    state.setViewsCollapsed(true);
    expect(useSidebarTreeStore.getState().viewsCollapsed).toBe(true);
  });

  it("persists Team section collapse preferences separately for each workspace", async () => {
    setCurrentWorkspace("team-a", "ws-a");
    await Promise.resolve();
    useSidebarTreeStore.getState().setNeedsAttentionCollapsed(true);

    setCurrentWorkspace("team-b", "ws-b");
    await Promise.resolve();
    expect(useSidebarTreeStore.getState().needsAttentionCollapsed).toBe(false);
    useSidebarTreeStore.getState().setWorkingCollapsed(true);

    setCurrentWorkspace("team-a", "ws-a");
    await Promise.resolve();
    expect(useSidebarTreeStore.getState().needsAttentionCollapsed).toBe(true);
    expect(useSidebarTreeStore.getState().workingCollapsed).toBe(false);
    setCurrentWorkspace("team-b", "ws-b");
    await Promise.resolve();
    expect(useSidebarTreeStore.getState().workingCollapsed).toBe(true);
  });

  it("opens new Team sections by default when hydrating older sidebar preferences", async () => {
    localStorage.setItem("multica_sidebar_tree:legacy", JSON.stringify({
      state: { expanded: ["project:p1"], pinnedCollapsed: true },
      version: 0,
    }));
    setCurrentWorkspace("legacy", "ws-legacy");
    await Promise.resolve();
    const state = useSidebarTreeStore.getState();
    expect(state.expanded).toEqual(["project:p1"]);
    expect(state.pinnedCollapsed).toBe(true);
    expect(state.workingCollapsed).toBe(false);
    expect(state.needsAttentionCollapsed).toBe(false);
  });
});
