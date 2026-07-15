// @vitest-environment jsdom
import { beforeAll, beforeEach, afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import { ViewStoreProvider, useViewStore } from "./view-store-context";
import { issueViewDefinitionFromState } from "./view-store";
import {
  ISSUE_SURFACE_VIEW_STORAGE_KEY,
  applyIssueSurfaceSavedView,
  clearIssueSurfaceViewState,
  getIssueSurfaceViewStateRegistrySnapshot,
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
  restoreIssueSurfaceDraft,
} from "./surface-view-store";

const flush = async () => {
  await new Promise((resolve) => queueMicrotask(() => resolve(null)));
  await new Promise((resolve) => queueMicrotask(() => resolve(null)));
};

beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (key) => values.get(key) ?? null,
      key: (index) => Array.from(values.keys())[index] ?? null,
      removeItem: (key) => {
        values.delete(key);
      },
      setItem: (key, value) => {
        values.set(key, value);
      },
    };
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
});

beforeEach(async () => {
  localStorage.clear();
  pruneIssueSurfaceViewStates([]);
  setCurrentWorkspace(null, null);
  await flush();
});

afterEach(async () => {
  cleanup();
  pruneIssueSurfaceViewStates([]);
  setCurrentWorkspace(null, null);
  await flush();
});

describe("issue surface view store registry", () => {
  it("isolates view state by surface key inside one workspace registry", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    const projectA = getIssueSurfaceViewStore("project:a");
    const projectB = getIssueSurfaceViewStore("project:b");

    projectA.getState().setViewMode("list");
    projectA.getState().togglePriorityFilter("high");

    expect(projectA.getState().viewMode).toBe("list");
    expect(projectB.getState().viewMode).toBe("board");
    expect(projectB.getState().priorityFilters).toEqual([]);

    const raw = localStorage.getItem(`${ISSUE_SURFACE_VIEW_STORAGE_KEY}:acme`);
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw as string);
    expect(parsed.state.surfaces["project:a"].state.viewMode).toBe("list");
    expect(parsed.state.surfaces["project:a"].state.priorityFilters).toEqual([
      "high",
    ]);
    expect(parsed.state.surfaces["project:b"]).toBeUndefined();
  });

  it("rehydrates existing surface stores when the workspace changes", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    const projectA = getIssueSurfaceViewStore("project:a");
    projectA.getState().setViewMode("list");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    expect(projectA.getState().viewMode).toBe("board");
    projectA.getState().setViewMode("swimlane");

    setCurrentWorkspace("acme", "ws_a");
    await flush();
    expect(projectA.getState().viewMode).toBe("list");

    setCurrentWorkspace("beta", "ws_b");
    await flush();
    expect(projectA.getState().viewMode).toBe("swimlane");
  });

  it("clears one surface without touching sibling surfaces", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    const projectA = getIssueSurfaceViewStore("project:a");
    const projectB = getIssueSurfaceViewStore("project:b");
    projectA.getState().setViewMode("list");
    projectB.getState().setViewMode("gantt");

    clearIssueSurfaceViewState("project:a");

    expect(projectA.getState().viewMode).toBe("board");
    expect(projectB.getState().viewMode).toBe("gantt");
    expect(getIssueSurfaceViewStateRegistrySnapshot()["project:a"]).toBeUndefined();
    expect(getIssueSurfaceViewStateRegistrySnapshot()["project:b"]?.state.viewMode).toBe(
      "gantt",
    );
  });

  it("prunes invalid surfaces and resets live stores for pruned keys", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    const projectA = getIssueSurfaceViewStore("project:a");
    const projectB = getIssueSurfaceViewStore("project:b");
    projectA.getState().setViewMode("list");
    projectB.getState().setViewMode("gantt");

    pruneIssueSurfaceViewStates(["project:a"]);

    expect(projectA.getState().viewMode).toBe("list");
    expect(projectB.getState().viewMode).toBe("board");
    expect(getIssueSurfaceViewStateRegistrySnapshot()["project:a"]?.state.viewMode).toBe(
      "list",
    );
    expect(getIssueSurfaceViewStateRegistrySnapshot()["project:b"]).toBeUndefined();
  });

  it("works as a real StoreApi with ViewStoreProvider subscriptions", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    const store = getIssueSurfaceViewStore("project:provider");

    function Probe() {
      const viewMode = useViewStore((state) => state.viewMode);
      const setViewMode = useViewStore((state) => state.setViewMode);
      return (
        <button type="button" onClick={() => setViewMode("list")}>
          {viewMode}
        </button>
      );
    }

    render(
      <ViewStoreProvider store={store}>
        <Probe />
      </ViewStoreProvider>,
    );

    expect(screen.getByRole("button", { name: "board" })).toBeTruthy();
    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "board" }));
    });
    expect(screen.getByRole("button", { name: "list" })).toBeTruthy();
  });

  it("restores the local draft after editing a saved-view overlay", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();
    const surfaceKey = "workspace:issues";
    const store = getIssueSurfaceViewStore(surfaceKey);
    store.getState().setViewMode("list");
    store.getState().togglePriorityFilter("high");

    const saved = issueViewDefinitionFromState(store.getState(), {
      workspaceActorKind: "agents",
    });
    saved.viewMode = "board";
    saved.priorityFilters = ["urgent"];
    applyIssueSurfaceSavedView(surfaceKey, store, saved);
    store.getState().togglePriorityFilter("medium");

    expect(store.getState().viewMode).toBe("board");
    expect(store.getState().priorityFilters).toEqual(["urgent", "medium"]);

    restoreIssueSurfaceDraft(surfaceKey, store);

    expect(store.getState().viewMode).toBe("list");
    expect(store.getState().priorityFilters).toEqual(["high"]);
  });
});
