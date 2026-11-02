// @vitest-environment node
import { describe, expect, it, beforeEach } from "vitest";
import { createStore, type StoreApi } from "zustand/vanilla";
import {
  mergeViewStatePersisted,
  viewStoreSlice,
  type IssueViewState,
} from "./view-store";

function defaults(): IssueViewState {
  const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  return store.getState();
}

describe("projectStatusFilters", () => {
  let store: StoreApi<IssueViewState>;
  beforeEach(() => {
    store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  });

  it("toggles a status on and off", () => {
    store.getState().toggleProjectStatusFilter("in_progress");
    store.getState().toggleProjectStatusFilter("planned");
    expect(store.getState().projectStatusFilters).toEqual([
      "in_progress",
      "planned",
    ]);
    store.getState().toggleProjectStatusFilter("in_progress");
    expect(store.getState().projectStatusFilters).toEqual(["planned"]);
  });

  it("clears with its own dimension and with clearFilters", () => {
    store.getState().toggleProjectStatusFilter("paused");
    store.getState().clearFilterDimension("projectStatus");
    expect(store.getState().projectStatusFilters).toEqual([]);

    store.getState().toggleProjectStatusFilter("paused");
    store.getState().clearFilters();
    expect(store.getState().projectStatusFilters).toEqual([]);
  });

  it("leaves the project-id dimension alone", () => {
    store.getState().toggleProjectFilter("p-1");
    store.getState().toggleProjectStatusFilter("completed");
    store.getState().clearFilterDimension("projectStatus");
    expect(store.getState().projectFilters).toEqual(["p-1"]);
  });
});

// `seedIssueSurfaceViewState` merges a saved view's server-owned jsonb blob
// straight into the store, and a persisted snapshot can be hand-edited. A
// member the client cannot represent would take a 400 from the backend and
// throw in the filter chip, which resolves its dot through
// PROJECT_STATUS_CONFIG.
describe("mergeViewStatePersisted project statuses", () => {
  it("keeps known statuses", () => {
    const merged = mergeViewStatePersisted(
      { projectStatusFilters: ["in_progress", "cancelled"] },
      defaults(),
    );
    expect(merged.projectStatusFilters).toEqual(["in_progress", "cancelled"]);
  });

  it("drops members the store cannot represent", () => {
    const merged = mergeViewStatePersisted(
      // "backlog" is an issue status; the project lifecycle has no such value.
      { projectStatusFilters: ["in_progress", "backlog", 7, null] },
      defaults(),
    );
    expect(merged.projectStatusFilters).toEqual(["in_progress"]);
  });

  it("falls back to the default for a snapshot saved before the dimension", () => {
    const merged = mergeViewStatePersisted({ projectFilters: ["p-1"] }, defaults());
    expect(merged.projectStatusFilters).toEqual([]);
  });

  it("treats a non-array value as no filter", () => {
    const merged = mergeViewStatePersisted(
      { projectStatusFilters: "in_progress" },
      defaults(),
    );
    expect(merged.projectStatusFilters).toEqual([]);
  });
});
