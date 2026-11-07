// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useProjectViewStore } from "./view-store";
import { setCurrentWorkspace } from "../../platform/workspace-storage";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

beforeEach(() => {
  localStorage.clear();
  useProjectViewStore.setState({ viewMode: "compact" });
  setCurrentWorkspace(null, null);
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useProjectViewStore", () => {

  it("partialize persists view prefs (no actions) under the workspace-namespaced key", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    useProjectViewStore.getState().setViewMode("comfortable");

    const raw = localStorage.getItem("multica_projects_view:acme");
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(Object.keys(parsed.state).sort()).toEqual([
      "filters",
      "hiddenColumns",
      "lifecycleView",
      "sortDirection",
      "sortField",
      "viewMode",
    ]);
    expect(parsed.state.viewMode).toBe("comfortable");
  });

  it("rehydrates a different saved viewMode on workspace switch", async () => {
    localStorage.setItem(
      "multica_projects_view:acme",
      JSON.stringify({ state: { viewMode: "comfortable" }, version: 0 }),
    );
    localStorage.setItem(
      "multica_projects_view:beta",
      JSON.stringify({ state: { viewMode: "compact" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("comfortable");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("compact");
  });

  it("resets to 'compact' when switching to a workspace with no persisted value", async () => {
    localStorage.setItem(
      "multica_projects_view:acme",
      JSON.stringify({ state: { viewMode: "comfortable" }, version: 0 }),
    );

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("comfortable");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    await flush();
    expect(useProjectViewStore.getState().viewMode).toBe("compact");
    expect(localStorage.getItem("multica_projects_view:acme")).not.toBeNull();
  });
});


describe("project lifecycle preferences", () => {
  it("defaults to Active and moves explicit status filters to All", () => {
    useProjectViewStore.getState().setLifecycleView("active");
    useProjectViewStore.getState().toggleFilter("statuses", "completed");
    expect(useProjectViewStore.getState()).toMatchObject({
      lifecycleView: "all", filters: { statuses: ["completed"] },
    });
    useProjectViewStore.getState().toggleFilter("priorities", "high");
    useProjectViewStore.getState().setLifecycleView("closed");
    expect(useProjectViewStore.getState()).toMatchObject({
      lifecycleView: "closed", filters: { statuses: [], priorities: ["high"] },
    });
    useProjectViewStore.getState().clearFilters();
    expect(useProjectViewStore.getState().lifecycleView).toBe("closed");
  });

  it("loads old status filters in All and isolates saved lifecycle views per workspace", async () => {
    localStorage.setItem("multica_projects_view:acme", JSON.stringify({
      state: { filters: { statuses: ["completed", "cancelled"] } }, version: 0,
    }));
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    expect(useProjectViewStore.getState().lifecycleView).toBe("all");
    expect(useProjectViewStore.getState().filters.statuses).toEqual(["completed", "cancelled"]);
    useProjectViewStore.getState().setLifecycleView("closed");
    setCurrentWorkspace("beta", "ws_b");
    await flush();
    expect(useProjectViewStore.getState().lifecycleView).toBe("active");
    expect(useProjectViewStore.getState().filters.statuses).toEqual([]);
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    expect(useProjectViewStore.getState().lifecycleView).toBe("closed");
  });
  it("migrates the former Completed preference to the single Closed history entry", async () => {
    localStorage.setItem("multica_projects_view:acme", JSON.stringify({
      state: { lifecycleView: "completed", sortField: "name", sortDirection: "asc", viewMode: "comfortable" }, version: 0,
    }));
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    expect(useProjectViewStore.getState()).toMatchObject({
      lifecycleView: "closed", sortField: "name", sortDirection: "asc", viewMode: "comfortable",
      filters: { statuses: [], priorities: [], leads: [] },
    });
  });

});
