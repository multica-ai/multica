// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

beforeEach(() => { localStorage.clear(); vi.resetModules(); });

async function windowStore() {
  vi.resetModules();
  return (await import("./tab-store")).useTabStore;
}

describe("closed destination persistence boundary", () => {
  it("shares live layout storage while keeping two renderer histories independent", async () => {
    const first = await windowStore();
    first.getState().switchWorkspace("acme");
    const id = first.getState().openTab("/acme/issues/ABC", "Private", { activate: true });
    first.getState().commitViewState(id, "/acme/issues/ABC", "draft", "sensitive view state");
    first.getState().closeTab(id);
    expect(first.getState().closedTabsByWorkspace.acme).toHaveLength(1);
    const payload = localStorage.getItem("multica_tabs")!;
    expect(payload).not.toContain("closedTabsByWorkspace");
    expect(payload).not.toContain("sensitive view state");
    const second = await windowStore();
    expect(second.getState().closedTabsByWorkspace).toEqual({});
    second.getState().reopenClosedTab();
    expect(second.getState().byWorkspace.acme.tabs).toHaveLength(1);
    const otherId = second.getState().openTab("/acme/projects", "Second window");
    second.getState().closeTab(otherId);
    expect(first.getState().closedTabsByWorkspace.acme[0].url).toBe("/acme/issues/ABC");
    expect(second.getState().closedTabsByWorkspace.acme[0].url).toBe("/acme/projects");
    const restarted = await windowStore();
    expect(restarted.getState().closedTabsByWorkspace).toEqual({});
  });

  it.each([1, 2, 3, 4, 5])("v%s legacy hydration cannot supply closed destinations", async (version) => {
    const tab = { id: "legacy", path: "/acme/issues", url: "/acme/issues", title: "Issues", icon: "ListTodo", pinned: false,
      history: { stack: ["/acme/issues"], index: 0 }, memento: { scroll: {}, view: {} } };
    localStorage.setItem("multica_tabs", JSON.stringify({ version, state: {
      tabs: [tab], activeTabId: tab.id, activeWorkspaceSlug: "acme",
      byWorkspace: { acme: { tabs: [tab], activeTabId: tab.id } },
      closedTabsByWorkspace: { acme: [{ url: "/acme/issues/secret", title: "Untrusted", index: 1 }] },
    } }));
    const store = await windowStore();
    expect(store.getState().byWorkspace.acme.tabs).toHaveLength(1);
    expect(store.getState().closedTabsByWorkspace).toEqual({});
    store.getState().reopenClosedTab();
    expect(store.getState().byWorkspace.acme.tabs[0].url).toBe("/acme/issues");
  });
});
